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
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxbroker"
)

func runJournalledOwner(t *testing.T, config WorkerConfig, root, mode string) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	grants := []sandboxbroker.LinuxRootGrant{{Name: "data", Path: root, Rights: sandboxbroker.Read | sandboxbroker.Write}}
	if mode == "journal-owner-startup-failure" {
		config.Digest[0] ^= 1
	}
	var parent *Group
	var storage FilesystemRecoveryStorage
	var owner *FilesystemBroker
	var err error
	if mode == "journal-owner-aggregate" || mode == "journal-owner-collision" {
		parent, err = newGroup(config.CgroupParent, config.Limits)
		if parent != nil {
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), workerCleanupTimeout)
				defer cancel()
				if err := parent.closeGroup(ctx); err != nil {
					t.Error(err)
				}
			}()
		}
		if err != nil {
			t.Fatal(err)
		}
		storage = checkpointedOwnerStorage(t)
		storage.Binding, err = RecoveryHostBinding(context.Background(), config.Digest)
		if err != nil {
			t.Fatal(err)
		}
		owner, err = OpenCheckpointedFilesystemBrokerInGroup(context.Background(), config, grants, sandboxbroker.FilesystemLimits{}, storage, parent)
	} else {
		owner, err = OpenJournalledFilesystemBroker(context.Background(), config, grants, sandboxbroker.FilesystemLimits{}, directory)
	}
	if owner != nil {
		defer owner.Close()
	}
	if mode == "journal-owner-startup-failure" {
		if err == nil || owner == nil || !owner.recovery.complete || owner.process.releaseAdmission != nil {
			t.Fatal("failed startup lost cleanup state:", owner, err)
		}
		if _, err := owner.recovery.journal.RecoveryRecords(); !errors.Is(err, sandboxbroker.ErrClosed) {
			t.Fatal("failed startup retained journal ownership:", err)
		}
		return
	}
	if err != nil {
		t.Fatal("journalled owner startup:", err)
	}
	if owner.recovery.checkpoint != nil {
		assertCheckpointedOwnerStoresLeased(t, storage)
		approved, err := owner.recovery.approval.Load(storage.Binding)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := owner.recovery.checkpoint.Load(approved, storage.Binding)
		if err != nil {
			t.Fatal(err)
		}
		if err := sandboxbroker.ValidateCheckpointJournal(encoded, approved, storage.Binding, owner.recovery.journal); err != nil {
			t.Fatal(err)
		}
	}
	response, err := owner.Execute(context.Background(), []byte(`{"operation":"fs.write","root":"data","path":"file","data":"bmV3"}`))
	if err != nil || response.Written != 3 {
		t.Fatal("journalled owner write:", response, err)
	}
	records, err := owner.recovery.journal.Records()
	if err != nil || len(records) != 1 {
		t.Fatal("missing owner intent:", records, err)
	}
	stagingName, err := sandboxbroker.StagingName(records[0].Namespace, records[0].Operation)
	if err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(root, stagingName)
	prepareJournalledOwnerFailure(t, owner, root, staging, mode)
	if mode == "journal-owner-concurrent" {
		results := make(chan error, 2)
		go func() { results <- owner.Close() }()
		go func() { results <- owner.Close() }()
		for range 2 {
			if err := <-results; err != nil {
				t.Fatal("concurrent close failed:", err)
			}
		}
	} else {
		err = owner.Close()
		if err != nil && owner.recovery.checkpoint != nil {
			assertCheckpointedOwnerStoresLeased(t, storage)
		}
		retryJournalledOwner(t, owner, staging, mode, config, err)
	}
	if !owner.recovery.complete {
		t.Fatal("owner did not complete recovery")
	}
	if owner.recovery.checkpoint != nil {
		assertCheckpointedOwnerStoresReleased(t, storage)
	}
	journalledOwnerAdmission.mutex.Lock()
	active := journalledOwnerAdmission.active
	journalledOwnerAdmission.mutex.Unlock()
	if active {
		t.Fatal("completed recovery retained lifecycle admission")
	}
	if owner.recovery.process == nil || owner.recovery.process.releaseAdmission != nil {
		t.Fatal("recovery process ownership not cleaned up")
	}
	if _, err := owner.recovery.journal.RecoveryRecords(); !errors.Is(err, sandboxbroker.ErrClosed) {
		t.Fatal("completed owner retained journal descriptors:", err)
	}
	if _, err := owner.Execute(context.Background(), nil); !errors.Is(err, sandboxbroker.ErrClosed) {
		t.Fatal("closed owner accepted work:", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "file"))
	if err != nil || string(data) != "new" {
		t.Fatal("recovery changed destination:", string(data), err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatal("automatic recovery left remnant:", entries, err)
	}
}

func prepareJournalledOwnerFailure(t *testing.T, owner *FilesystemBroker, root, staging, mode string) {
	t.Helper()
	switch mode {
	case "journal-owner-remnant":
		if err := owner.client.Close(); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "file")
		if err := os.Rename(target, staging); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("new"), 0600); err != nil {
			t.Fatal(err)
		}
	case "journal-owner-collision":
		if err := os.WriteFile(staging, []byte("collision"), 0600); err != nil {
			t.Fatal(err)
		}
	case "journal-owner-approval":
		owner.recovery.config.Digest[0] ^= 1
	case "journal-owner-cleanup":
		if err := unix.Mkdirat(int(owner.process.group.directory.Fd()), "owner-blocker", 0700); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = unix.Unlinkat(int(owner.process.group.directory.Fd()), "owner-blocker", unix.AT_REMOVEDIR) })
	case "journal-owner-denied":
		if _, err := owner.Execute(context.Background(), []byte("{}")); err == nil {
			t.Fatal("invalid request succeeded")
		}
	}
}

func retryJournalledOwner(t *testing.T, owner *FilesystemBroker, staging, mode string, config WorkerConfig, failure error) {
	t.Helper()
	switch mode {
	case "journal-owner-collision", "journal-owner-approval", "journal-owner-cleanup":
		if failure == nil || owner.recovery.complete {
			t.Fatal("cleanup failure discarded ownership:", failure)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		next, err := OpenJournalledFilesystemBroker(ctx, config, nil, sandboxbroker.FilesystemLimits{}, "relative")
		cancel()
		if next != nil || !errors.Is(err, context.DeadlineExceeded) {
			if next != nil {
				_ = next.Close()
			}
			t.Fatal("failed recovery released lifecycle admission:", err)
		}
		if _, err := owner.recovery.journal.RecoveryRecords(); err != nil {
			t.Fatal("failed cleanup closed original journal:", err)
		}
		switch mode {
		case "journal-owner-collision":
			data, err := os.ReadFile(staging)
			if err != nil || string(data) != "collision" {
				t.Fatal("collision was removed:", string(data), err)
			}
			if err := os.Remove(staging); err != nil {
				t.Fatal(err)
			}
		case "journal-owner-approval":
			owner.recovery.config = config
		case "journal-owner-cleanup":
			if owner.recovery.process != nil {
				t.Fatal("recovery started before original cleanup")
			}
			if err := unix.Unlinkat(int(owner.process.group.directory.Fd()), "owner-blocker", unix.AT_REMOVEDIR); err != nil {
				t.Fatal(err)
			}
		}
		if err := owner.Close(); err != nil {
			t.Fatal("recovery retry failed:", err)
		}
	case "journal-owner-denied":
		if failure == nil {
			t.Fatal("automatic recovery hid original failure")
		}
	default:
		if failure != nil {
			t.Fatal("automatic recovery failed:", failure)
		}
	}
}
