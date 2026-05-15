//go:build linux && (amd64 || arm64) && !race

// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package sandboxlinux

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxwire"
)

func originalRecoveryFixtureConfig(t *testing.T, storage *FilesystemRecoveryStorage, group *Group, parent string) WorkerConfig {
	t.Helper()
	config := serviceRecoveryBrokerConfig(t, parent)
	var err error
	if directory := os.Getenv("PIPIT_TEST_RECOVERY_IMAGES"); directory != "" {
		grants := recoveryCrashGrants(os.Getenv("PIPIT_TEST_RECOVERY_FILESYSTEM_ROOT"))
		config.ImageStore, err = sandboxbroker.OpenLinuxImageStore(directory, grants,
			[]string{storage.JournalDirectory, storage.CheckpointDirectory, storage.ApprovalDirectory})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = config.ImageStore.Close() })
		storage.ServiceContext, err = group.RecoveryIdentityWithImages(config.ImageStore)
	} else {
		storage.ServiceContext, err = group.RecoveryIdentity()
	}
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func recoveryCrashGrants(root string) []sandboxbroker.LinuxRootGrant {
	return []sandboxbroker.LinuxRootGrant{{Name: "data", Path: root, Rights: sandboxbroker.Read | sandboxbroker.Write}}
}

func exerciseRecoveryHelperCrash(t *testing.T, ctx context.Context, original *ServiceRecoveryClaim, storage FilesystemRecoveryStorage, parent, root, images string) *ServiceRecoveryClaim {
	t.Helper()
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(storage)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeServiceRecoveryHelperCrashChild$")
	command.Env = append(os.Environ(), "PIPIT_TEST_RECOVERY_HELPER_STORAGE="+string(encoded),
		"PIPIT_TEST_RECOVERY_HELPER_PARENT="+parent, "PIPIT_TEST_RECOVERY_FILESYSTEM_ROOT="+root,
		"PIPIT_TEST_RECOVERY_IMAGES="+images)
	command.Stderr = os.Stderr
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	ready, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || ready != "ready\n" {
		t.Fatal("replacement recovery helper did not reach confined readiness:", ready, err)
	}
	competing, err := ClaimServiceRecovery(ctx, storage)
	if competing != nil {
		_ = competing.Close()
	}
	if err == nil || competing != nil {
		t.Fatal("running recovery helper lost exclusive metadata ownership:", err)
	}
	if entries, err := os.ReadDir(images); err != nil || len(entries) != 2 {
		t.Fatal("recovery helper and watchdog did not retain approved images:", err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("replacement host survived SIGKILL")
	}
	claim, err := ClaimServiceRecovery(ctx, storage)
	if err != nil {
		t.Fatal(err)
	}
	if err := claim.RecoverImages(ctx, images, recoveryCrashGrants(root)); !errors.Is(err, ErrServiceBusy) {
		t.Fatal("new claimant reclaimed images before quiescence:", err)
	}
	if err := claim.Quiesce(ctx, parent); err != nil {
		t.Fatal("new claimant could not terminate abandoned recovery helpers:", err)
	}
	if err := claim.RecoverImages(ctx, images, recoveryCrashGrants(root)); err != nil {
		t.Fatal("abandoned recovery helper images could not be reclaimed:", err)
	}
	if entries, err := os.ReadDir(images); err != nil || len(entries) != 0 {
		t.Fatal("abandoned helper images remain:", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "file")); err != nil || string(data) != "published" {
		t.Fatal("recovery-host crash changed published data:", err)
	}
	return claim
}

func TestNativeServiceRecoveryHelperCrashChild(t *testing.T) {
	encoded := os.Getenv("PIPIT_TEST_RECOVERY_HELPER_STORAGE")
	if encoded == "" {
		return
	}
	var storage FilesystemRecoveryStorage
	if err := json.Unmarshal([]byte(encoded), &storage); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	claim, err := ClaimServiceRecovery(ctx, storage)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	parent := os.Getenv("PIPIT_TEST_RECOVERY_HELPER_PARENT")
	if err := claim.Quiesce(ctx, parent); err != nil {
		t.Fatal(err)
	}
	grants := recoveryCrashGrants(os.Getenv("PIPIT_TEST_RECOVERY_FILESYSTEM_ROOT"))
	if err := claim.RecoverImages(ctx, os.Getenv("PIPIT_TEST_RECOVERY_IMAGES"), grants); err != nil {
		t.Fatal(err)
	}
	bootstrap, err := claim.snapshot.PrepareRecovery(grants)
	if err != nil || bootstrap == nil || !bootstrap.RecoveryOnly() {
		t.Fatal("missing sealed original recovery batch:", err)
	}
	defer bootstrap.Close()
	files, err := bootstrap.Files()
	if err != nil {
		t.Fatal(err)
	}
	if err := claim.allowRecoveryChildren(); err != nil {
		t.Fatal(err)
	}
	config := serviceRecoveryBrokerConfig(t, parent)
	config.ImageStore = claim.images
	claim.pending, claim.recovering, claim.quiesced = true, true, false
	claim.process, err = launchConfinedProcess(ctx, config, &filesystemBrokerAdmission, files, claim.group)
	claim.group.mutex.Lock()
	claim.group.closing = true
	claim.group.mutex.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	defer claim.process.Close()
	waitForRecoveryHelperReady(t, claim.process)
	if err := claim.Close(); !errors.Is(err, ErrServiceBusy) {
		t.Fatal("running recovery helper allowed claim closure:", err)
	}
	fmt.Println("ready")
	var signal [1]byte
	_, err = io.ReadFull(os.Stdin, signal[:])
	t.Fatal("replacement host returned before SIGKILL:", err)
}

func waitForRecoveryHelperReady(t *testing.T, process *WorkerProcess) {
	t.Helper()
	if err := process.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	codec, err := sandboxwire.New(process, process, sandboxwire.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	hello, err := codec.Read()
	if err != nil || hello.Kind != sandboxwire.Hello || hello.ID != 0 {
		t.Fatal("recovery helper did not complete confinement:", err)
	}
	var configuration sandboxbroker.FilesystemConfiguration
	if err := hello.DecodePayload(&configuration); err != nil || configuration.Profile != sandboxbroker.FilesystemRecoveryProfile {
		t.Fatal("recovery helper announced the wrong profile:", err)
	}
	if err := codec.Write(sandboxwire.Message{Kind: sandboxwire.Configure, ID: 0, Payload: hello.Payload}); err != nil {
		t.Fatal(err)
	}
	ready, err := codec.Read()
	if err != nil || ready.Kind != sandboxwire.Ready || ready.ID != 0 {
		t.Fatal("recovery helper did not accept its sealed profile:", err)
	}
	if err := ready.DecodePayload(&struct{}{}); err != nil {
		t.Fatal(err)
	}
}
