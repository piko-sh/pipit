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
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNativeServiceRecoveryQuiesce(t *testing.T) {
	for _, releaseStage := range []string{"normal", "before-removal", "after-removal", "helper-running"} {
		t.Run(releaseStage, func(t *testing.T) {
			exerciseNativeServiceRecovery(t, releaseStage)
		})
	}
}

func exerciseNativeServiceRecovery(t *testing.T, releaseStage string) {
	t.Helper()
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
	storage := checkpointedOwnerStorage(t)
	storage.Binding, err = RecoveryHostBinding(context.Background(), [sha256.Size]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(storage)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeServiceRecoveryQuiesceChild$")
	filesystemRoot := t.TempDir()
	command.Env = append(os.Environ(), "PIPIT_TEST_RECOVERY_SERVICE_PARENT="+root, "PIPIT_TEST_RECOVERY_SERVICE_STORAGE="+string(encoded))
	command.Env = append(command.Env, "PIPIT_TEST_RECOVERY_FILESYSTEM_ROOT="+filesystemRoot)
	imageDirectory := ""
	if releaseStage == "helper-running" {
		imageDirectory = t.TempDir()
		if err := os.Chmod(imageDirectory, 0700); err != nil {
			t.Fatal(err)
		}
		command.Env = append(command.Env, "PIPIT_TEST_RECOVERY_IMAGES="+imageDirectory)
	}
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
		t.Fatal("original host did not become ready:", ready, err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("original host survived SIGKILL")
	}
	claim, err := ClaimServiceRecovery(ctx, storage)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if claim.pending {
			_ = claim.Quiesce(cleanup, root)
		}
		if err := claim.Close(); err != nil {
			t.Error(err)
		}
	}()
	exerciseRecoveryQuiesce(t, ctx, claim, storage, aggregate, parent, root)
	if releaseStage == "helper-running" {
		claim = exerciseRecoveryHelperCrash(t, ctx, claim, storage, root, filesystemRoot, imageDirectory)
		releaseStage = "normal"
	}
	exerciseClaimedFilesystemRecovery(t, ctx, claim, storage, root, filesystemRoot, releaseStage)
}

func exerciseRecoveryQuiesce(t *testing.T, ctx context.Context, claim *ServiceRecoveryClaim, storage FilesystemRecoveryStorage, aggregate *Group, wrongParent, parent string) {
	t.Helper()
	if err := claim.Quiesce(ctx, wrongParent); !errors.Is(err, ErrUnavailable) || claim.group != nil {
		t.Fatal("different delegated parent accepted:", err)
	}
	changed := claim.identity
	changed.Group.Inode ^= 1
	mismatched, err := pinRecoveryService(parent, changed)
	if mismatched != nil {
		_ = mismatched.closeHandles()
	}
	if !errors.Is(err, ErrUnavailable) || mismatched != nil {
		t.Fatal("different original service identity accepted:", err)
	}
	events, err := readControl(aggregate.directory, "cgroup.events")
	if err != nil {
		t.Fatal(err)
	}
	if empty, err := parseEmpty(events); err != nil || empty {
		t.Fatal("orphan task was not alive before authorised termination:", err)
	}
	claim.group, err = pinRecoveryService(parent, claim.identity)
	if err != nil {
		t.Fatal(err)
	}
	claim.parentPath = parent
	if err := claim.group.kill.Close(); err != nil {
		t.Fatal(err)
	}
	if err := claim.Quiesce(ctx, parent); err == nil || !claim.pending {
		t.Fatal("failed termination discarded pending ownership:", err)
	}
	if err := claim.Close(); !errors.Is(err, ErrServiceBusy) || claim.closed {
		t.Fatal("failed termination released ownership:", err)
	}
	competing, err := ClaimServiceRecovery(ctx, storage)
	if competing != nil {
		_ = competing.Close()
	}
	if err == nil || competing != nil {
		t.Fatal("failed termination released metadata leases:", err)
	}
	claim.group.kill, err = openControl(claim.group.directory, "cgroup.kill", unix.O_WRONLY)
	if err != nil {
		t.Fatal(err)
	}
	if err := claim.Quiesce(ctx, parent); err != nil || claim.pending {
		t.Fatal("termination retry failed:", err)
	}
	if err := claim.group.start(exec.Command("/bin/true")); !errors.Is(err, errClosed) {
		t.Fatal("recovery owner admitted normal execution:", err)
	}
	events, err = readControl(aggregate.directory, "cgroup.events")
	if err != nil {
		t.Fatal(err)
	}
	if empty, err := parseEmpty(events); err != nil || !empty {
		t.Fatal("successful termination left live descendants:", err)
	}
	if _, err := os.Stat(filepath.Join(parent, serviceGroupNameFor(testTenant))); err != nil {
		t.Fatal("quiescence deleted the original reservation:", err)
	}
	exerciseRecoveryPrune(t, ctx, claim)
}

func exerciseRecoveryPrune(t *testing.T, ctx context.Context, claim *ServiceRecoveryClaim) {
	t.Helper()
	for index := range maximumRecoveryCgroups + 1 {
		if err := unix.Mkdirat(int(claim.group.directory.Fd()), fmt.Sprintf("prune-limit-%d", index), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := claim.Prune(ctx); !errors.Is(err, ErrUnavailable) || !claim.pending {
		t.Fatal("unbounded pruning was accepted:", err)
	}
	if err := claim.Close(); !errors.Is(err, ErrServiceBusy) {
		t.Fatal("failed pruning released ownership:", err)
	}
	for index := range maximumRecoveryCgroups + 1 {
		if err := unix.Unlinkat(int(claim.group.directory.Fd()), fmt.Sprintf("prune-limit-%d", index), unix.AT_REMOVEDIR); err != nil {
			t.Fatal("planning failure removed a directory:", err)
		}
	}
	if err := unix.Mkdirat(int(claim.group.directory.Fd()), "nested", 0700); err != nil {
		t.Fatal(err)
	}
	nested, err := openDirectory(int(claim.group.directory.Fd()), "nested")
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(unix.Mkdirat(int(nested.Fd()), "leaf", 0700), nested.Close()); err != nil {
		t.Fatal(err)
	}
	if err := claim.Prune(ctx); err != nil || claim.pending {
		t.Fatal("bounded pruning retry failed:", err)
	}
	if err := claim.Prune(ctx); err != nil {
		t.Fatal("empty service pruning was not idempotent:", err)
	}
	exerciseRecoveryPruneDepth(t, ctx, claim)
	exerciseRecoveryPruneReplacement(t, ctx, claim)
	root, err := openDirectory(int(claim.group.directory.Fd()), ".")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	entries, err := root.ReadDir(maximumRecoveryDirectoryEntries + 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatal("abandoned child group survived pruning:", entry.Name())
		}
	}
	if err := claim.group.verifyDirectoryEntry(); err != nil {
		t.Fatal("pruning removed the original service reservation:", err)
	}
}

func exerciseRecoveryPruneDepth(t *testing.T, ctx context.Context, claim *ServiceRecoveryClaim) {
	t.Helper()
	parent := claim.group.directory
	directories := make([]*os.File, 0, maximumRecoveryGroupDepth+1)
	defer func() {
		for _, directory := range directories {
			_ = directory.Close()
		}
	}()
	for range maximumRecoveryGroupDepth + 1 {
		if err := unix.Mkdirat(int(parent.Fd()), "deep", 0700); err != nil {
			t.Fatal(err)
		}
		directory, err := openDirectory(int(parent.Fd()), "deep")
		if err != nil {
			t.Fatal(err)
		}
		directories = append(directories, directory)
		parent = directory
	}
	if err := claim.Prune(ctx); !errors.Is(err, ErrUnavailable) || !claim.pending {
		t.Fatal("excessive pruning depth accepted:", err)
	}
	if err := unix.Unlinkat(int(directories[len(directories)-2].Fd()), "deep", unix.AT_REMOVEDIR); err != nil {
		t.Fatal("depth rejection removed the deepest directory:", err)
	}
	if err := claim.Prune(ctx); err != nil {
		t.Fatal("maximum allowed depth rejected:", err)
	}
}

func exerciseRecoveryPruneReplacement(t *testing.T, ctx context.Context, claim *ServiceRecoveryClaim) {
	t.Helper()
	parent := claim.group.directory
	if err := unix.Mkdirat(int(parent.Fd()), "replace", 0700); err != nil {
		t.Fatal(err)
	}
	original, err := openDirectory(int(parent.Fd()), "replace")
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	identity, err := cgroupDirectoryIdentity(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Unlinkat(int(parent.Fd()), "replace", unix.AT_REMOVEDIR); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkdirat(int(parent.Fd()), "replace", 0700); err != nil {
		t.Fatal(err)
	}
	node := recoveryPruneNode{parent: parent, directory: original, name: "replace", identity: identity}
	if err := removeRecoveryPruneNode(ctx, node); !errors.Is(err, ErrUnavailable) {
		t.Fatal("stale pruning plan accepted replacement:", err)
	}
	replacement, err := openDirectory(int(parent.Fd()), "replace")
	if err != nil {
		t.Fatal("stale plan removed replacement:", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
	if err := claim.Prune(ctx); err != nil {
		t.Fatal("fresh plan could not remove fixture directory:", err)
	}
}

func TestNativeServiceRecoveryQuiesceChild(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_RECOVERY_SERVICE_PARENT")
	if parent == "" {
		return
	}
	var storage FilesystemRecoveryStorage
	if err := json.Unmarshal([]byte(os.Getenv("PIPIT_TEST_RECOVERY_SERVICE_STORAGE")), &storage); err != nil {
		t.Fatal(err)
	}
	group, err := newServiceGroup(parent, Limits{}, testTenant)
	if err != nil {
		t.Fatal(err)
	}
	storage.Binding, err = RecoveryHostBinding(context.Background(), [sha256.Size]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	config := originalRecoveryFixtureConfig(t, &storage, group, parent)
	owner := persistOriginalFilesystemRecovery(t, storage, group, config)
	defer owner.Close()
	child, err := group.newChild(Limits{})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/bin/sleep", "30")
	if err := child.start(command); err != nil {
		t.Fatal(err)
	}
	fmt.Println("ready")
	var signal [1]byte
	_, _ = io.ReadFull(os.Stdin, signal[:])
}
