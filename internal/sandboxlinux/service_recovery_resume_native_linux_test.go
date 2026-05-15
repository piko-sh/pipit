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
)

func exerciseReleaseCrash(t *testing.T, ctx context.Context, original *ServiceRecoveryClaim, storage FilesystemRecoveryStorage, parent, stage string) *ServiceRecoveryClaim {
	t.Helper()
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(storage)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeServiceResumeReleaseChild$")
	command.Env = append(os.Environ(), "PIPIT_TEST_RELEASE_STORAGE="+string(encoded),
		"PIPIT_TEST_RELEASE_PARENT="+parent, "PIPIT_TEST_RELEASE_STAGE="+stage)
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
		t.Fatal("replacement host did not reach release boundary:", ready, err)
	}
	competing, err := ClaimServiceRecovery(ctx, storage)
	if competing != nil {
		_ = competing.Close()
	}
	if err == nil || competing != nil {
		t.Fatal("release-boundary owner did not retain metadata leases:", err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("replacement host survived SIGKILL")
	}
	resumed, err := ClaimServiceRecovery(ctx, storage)
	if err != nil {
		t.Fatal("replacement crash retained metadata leases:", err)
	}
	if err := resumed.ResumeRelease(ctx, filepath.Dir(parent)); !errors.Is(err, ErrUnavailable) || resumed.released {
		_ = resumed.Close()
		t.Fatal("different delegated parent accepted:", err)
	}
	return resumed
}

func TestNativeServiceResumeReleaseChild(t *testing.T) {
	encoded := os.Getenv("PIPIT_TEST_RELEASE_STORAGE")
	if encoded == "" {
		return
	}
	var storage FilesystemRecoveryStorage
	if err := json.Unmarshal([]byte(encoded), &storage); err != nil {
		t.Fatal(err)
	}
	claim, err := ClaimServiceRecovery(context.Background(), storage)
	if err != nil {
		t.Fatal(err)
	}
	if ready, err := claim.snapshot.ServiceReleaseReady(); err != nil || !ready {
		t.Fatal("missing durable release readiness:", err)
	}
	claim.group, err = pinReadyRecoveryService(os.Getenv("PIPIT_TEST_RELEASE_PARENT"), claim.identity)
	if err != nil || claim.group == nil {
		t.Fatal("original empty service not found:", err)
	}
	switch os.Getenv("PIPIT_TEST_RELEASE_STAGE") {
	case "before-removal":
	case "after-removal":
		if err := claim.group.closeGroup(context.Background()); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("unknown release crash boundary")
	}
	fmt.Println("ready")
	var signal [1]byte
	_, _ = io.ReadFull(os.Stdin, signal[:])
}

func TestNativeReadyRecoveryRefusesPopulatedOriginal(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup-v2 parent")
	}
	aggregate, err := newGroup(parent, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, aggregate.name)
	defer cleanupCrashFixture(t, aggregate, root)
	if err := enableChildControllers(aggregate.directory); err != nil {
		t.Fatal(err)
	}
	original, err := newServiceGroup(root, Limits{}, testTenant)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := original.closeGroup(ctx); err != nil {
			t.Error(err)
		}
	}()
	encoded, err := original.RecoveryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := decodeServiceRecoveryIdentity(encoded)
	if err != nil {
		t.Fatal(err)
	}
	startRecoveryFixtureSleeper(t, original)
	pinned, err := pinReadyRecoveryService(root, identity)
	if pinned != nil {
		_ = pinned.closeHandles()
	}
	if !errors.Is(err, ErrServiceBusy) || pinned != nil {
		t.Fatal("release readiness accepted populated original:", err)
	}
	if err := recoveryDirectoryEmpty(original.directory); !errors.Is(err, ErrServiceBusy) {
		t.Fatal("rejected readiness signalled original tasks:", err)
	}
}

func startRecoveryFixtureSleeper(t *testing.T, group *Group) {
	t.Helper()
	command := exec.Command("/bin/sleep", "30")
	if err := group.start(command); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	if err := recoveryDirectoryEmpty(group.directory); !errors.Is(err, ErrServiceBusy) {
		t.Fatal("fixture did not populate the resource group:", err)
	}
}
