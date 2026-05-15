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
	"errors"
	"os"
	"path/filepath"
	"testing"

	"pipit.sh/pipit/internal/sandboxbroker"
)

func TestCheckpointedOwnerRejectsStorageBeforeLaunch(t *testing.T) {
	for _, mode := range []string{"missing", "overlap", "collision", "approval-open", "zero-binding", "relative", "no-parent"} {
		t.Run(mode, func(t *testing.T) {
			storage := checkpointedOwnerStorage(t)
			parent := &Group{}
			switch mode {
			case "missing":
				storage.CheckpointDirectory = filepath.Join(storage.CheckpointDirectory, "missing")
			case "overlap":
				storage.CheckpointDirectory = storage.JournalDirectory
			case "collision":
				approval, err := sandboxbroker.OpenLinuxRecoveryApprovalStore(storage.ApprovalDirectory)
				if err != nil {
					t.Fatal(err)
				}
				if err := errors.Join(approval.Store([sha256.Size]byte{2}, storage.Binding), approval.Close()); err != nil {
					t.Fatal(err)
				}
			case "approval-open":
				if err := os.Chmod(storage.ApprovalDirectory, 0755); err != nil {
					t.Fatal(err)
				}
			case "zero-binding":
				storage.Binding = [sha256.Size]byte{}
			case "relative":
				storage.ApprovalDirectory = "relative"
			case "no-parent":
				parent = nil
			}
			owner, err := OpenCheckpointedFilesystemBrokerInGroup(context.Background(), WorkerConfig{Tenant: testTenant}, nil,
				sandboxbroker.FilesystemLimits{}, storage, parent)
			if err == nil {
				if owner != nil {
					_ = owner.Close()
				}
				t.Fatal("invalid storage reached native launch")
			}
			if owner != nil {
				if owner.process != nil || !owner.recovery.complete || owner.RecoveryPending() {
					t.Fatal("storage failure launched a child or leaked recovery")
				}
				if err := owner.Close(); err != nil {
					t.Fatal(err)
				}
			}
			journalledOwnerAdmission.mutex.Lock()
			active := journalledOwnerAdmission.active
			journalledOwnerAdmission.mutex.Unlock()
			if active {
				t.Fatal("failed storage retained constructor admission")
			}
			if mode == "collision" {
				assertCheckpointedOwnerStoresReleased(t, storage)
			}
		})
	}
}

func TestCheckpointedOwnerRetainsStorageUntilClose(t *testing.T) {
	storage := checkpointedOwnerStorage(t)
	bootstrap, err := sandboxbroker.PrepareLinuxBootstrap(nil, sandboxbroker.FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	authority, err := bootstrap.RecoveryAuthority()
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	journal, err := sandboxbroker.OpenLinuxRecoveryJournal(storage.JournalDirectory, bootstrap.StagingNamespace())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	state := &filesystemBrokerRecovery{base: context.Background(), authority: authority, journal: journal}
	owner := &FilesystemBroker{recovery: state}
	defer owner.Close()
	if err := state.persistCheckpoint(storage); err != nil {
		t.Fatal(err)
	}
	approved, err := state.approval.Load(storage.Binding)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := state.checkpoint.Load(approved, storage.Binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := sandboxbroker.ValidateCheckpointJournal(encoded, approved, storage.Binding, journal); err != nil {
		t.Fatal(err)
	}
	assertCheckpointedOwnerStoresLeased(t, storage)
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	assertCheckpointedOwnerStoresReleased(t, storage)
}

func checkpointedOwnerStorage(t *testing.T) FilesystemRecoveryStorage {
	t.Helper()
	directory := t.TempDir()
	for _, name := range []string{"journal", "checkpoint", "approval"} {
		if err := os.Mkdir(filepath.Join(directory, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return FilesystemRecoveryStorage{
		JournalDirectory:    filepath.Join(directory, "journal"),
		CheckpointDirectory: filepath.Join(directory, "checkpoint"),
		ApprovalDirectory:   filepath.Join(directory, "approval"),
		Binding:             [sha256.Size]byte{1},
	}
}

func assertCheckpointedOwnerStoresLeased(t *testing.T, storage FilesystemRecoveryStorage) {
	t.Helper()
	checkpoint, err := sandboxbroker.OpenLinuxRecoveryCheckpointStore(storage.CheckpointDirectory)
	if checkpoint != nil {
		_ = checkpoint.Close()
	}
	if err == nil {
		t.Fatal("live owner released checkpoint lease")
	}
	approval, err := sandboxbroker.OpenLinuxRecoveryApprovalStore(storage.ApprovalDirectory)
	if approval != nil {
		_ = approval.Close()
	}
	if err == nil {
		t.Fatal("live owner released approval lease")
	}
}

func assertCheckpointedOwnerStoresReleased(t *testing.T, storage FilesystemRecoveryStorage) {
	t.Helper()
	checkpoint, err := sandboxbroker.OpenLinuxRecoveryCheckpointStore(storage.CheckpointDirectory)
	if err != nil {
		t.Fatal("closed owner retained checkpoint lease:", err)
	}
	defer checkpoint.Close()
	approval, err := sandboxbroker.OpenLinuxRecoveryApprovalStore(storage.ApprovalDirectory)
	if err != nil {
		t.Fatal("closed owner retained approval lease:", err)
	}
	defer approval.Close()
}
