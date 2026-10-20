//go:build linux && (amd64 || arm64)

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
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxbroker"
)

type nativeRecoveryCheckpoint struct {
	encoded  []byte
	approved [sha256.Size]byte
}

func runJournalledBrokerClient(t *testing.T, config WorkerConfig, process *WorkerProcess, authority *sandboxbroker.LinuxRecoveryAuthority,
	namespace, root, mode string,
) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	journal, err := sandboxbroker.OpenLinuxRecoveryJournal(directory, namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	var checkpoint nativeRecoveryCheckpoint
	if mode == "journalled-recovery-checkpoint" {
		binding, bindingErr := RecoveryHostBinding(context.Background(), config.Digest)
		if bindingErr != nil {
			t.Fatal(bindingErr)
		}
		checkpointDirectory := t.TempDir()
		if err := os.Chmod(checkpointDirectory, 0700); err != nil {
			t.Fatal(err)
		}
		store, storeErr := sandboxbroker.OpenLinuxRecoveryCheckpointStore(checkpointDirectory)
		if storeErr != nil {
			t.Fatal("opening checkpoint store:", storeErr)
		}
		defer store.Close()
		approvalDirectory := t.TempDir()
		if err := os.Chmod(approvalDirectory, 0700); err != nil {
			t.Fatal(err)
		}
		approval, approvalErr := sandboxbroker.OpenLinuxRecoveryApprovalStore(approvalDirectory)
		if approvalErr != nil {
			t.Fatal(approvalErr)
		}
		defer approval.Close()
		captured, persistErr := authority.PersistApprovedCheckpoint(journal, store, approval, binding)
		if persistErr != nil {
			t.Fatal("persisting checkpoint:", persistErr)
		}
		checkpoint.approved, err = approval.Load(binding)
		if err != nil || checkpoint.approved != captured {
			t.Fatal("independent approval changed:", err)
		}
		checkpoint.encoded, err = store.Load(checkpoint.approved, binding)
		if err != nil {
			t.Fatal(err)
		}
	}
	client, err := sandboxbroker.OpenJournalledFilesystemClient(context.Background(), process, authority, journal)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if mode == "journalled-failure" {
		if err := journal.Close(); err != nil {
			t.Fatal(err)
		}
	}
	response, err := client.Execute(context.Background(), []byte(`{"operation":"fs.write","root":"data","path":"file","data":"bmV3"}`))
	expected := "new"
	if mode == "journalled-failure" {
		expected = "original"
		if err == nil || response.Written != 0 {
			t.Fatal("failed journal allowed native publication:", response, err)
		}
		select {
		case <-process.Done():
		default:
			t.Fatal("failed journal did not reap native broker")
		}
	} else {
		if err != nil || response.Written != 3 {
			t.Fatal("native journalled write failed:", response, err)
		}
		records, err := journal.Records()
		if err != nil || len(records) != 1 || records[0].Namespace != namespace || records[0].Path != "file" {
			t.Fatal("native publication has no durable intent:", records, err)
		}
		if mode == "journalled-recovery-cleanup" {
			if err := unix.Mkdirat(int(process.group.directory.Fd()), "recovery-blocker", 0700); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = unix.Unlinkat(int(process.group.directory.Fd()), "recovery-blocker", unix.AT_REMOVEDIR) })
		}
		if mode == "journalled-recovery-failed-broker" {
			if _, err := client.Execute(context.Background(), []byte("{}")); err == nil {
				t.Fatal("invalid request did not terminate original broker")
			}
		}
		closeErr := client.Close()
		expectedFailure := mode == "journalled-recovery-failed-broker" || mode == "journalled-recovery-cleanup"
		if (closeErr != nil) != expectedFailure {
			t.Fatal("unexpected original shutdown status:", closeErr)
		}
		if strings.HasPrefix(mode, "journalled-recovery") {
			runNativeRecoveryBatch(t, config, process, authority, journal, root, mode, checkpoint)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "file"))
	if err != nil || string(data) != expected {
		t.Fatal("native publication differs from acknowledgement:", string(data), err)
	}
	entries, err := os.ReadDir(root)
	expectedEntries := 1
	if mode == "journalled-recovery-collision" {
		expectedEntries = 2
	}
	if err != nil || len(entries) != expectedEntries {
		t.Fatal("native publication left staging names:", entries, err)
	}
}
