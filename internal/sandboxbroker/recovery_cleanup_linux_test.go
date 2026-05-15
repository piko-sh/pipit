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

package sandboxbroker

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRecoveryCleanupRemnantAndCompletedPublication(t *testing.T) {
	for _, state := range []string{"linked", "absent", "published"} {
		t.Run(state, func(t *testing.T) {
			root, namespace, record, directory := recoveryCleanupFixture(t)
			name, err := StagingName(namespace, record.Operation)
			if err != nil {
				t.Fatal(err)
			}
			staging := filepath.Join(directory, "parent", name)
			expected := "original"
			switch state {
			case "absent":
				if err := os.Remove(staging); err != nil {
					t.Fatal(err)
				}
			case "published":
				if err := os.Rename(staging, filepath.Join(directory, "parent", "file")); err != nil {
					t.Fatal(err)
				}
				expected = "new"
			}
			for range 2 {
				if err := recoverLinuxStaging(root, namespace, record); err != nil {
					t.Fatal("recovery or repeat failed:", err)
				}
			}
			assertJournalledTarget(t, filepath.Join(directory, "parent"), expected)
			if _, err := os.Lstat(staging); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("staging entry remains:", err)
			}
		})
	}
}

func TestRecoveryCleanupPreservesCollisions(t *testing.T) {
	for _, variant := range []string{"inode", "mode", "size", "hard-link", "symlink", "fifo", "directory"} {
		t.Run(variant, func(t *testing.T) {
			root, namespace, record, directory := recoveryCleanupFixture(t)
			name, err := StagingName(namespace, record.Operation)
			if err != nil {
				t.Fatal(err)
			}
			staging := filepath.Join(directory, "parent", name)
			switch variant {
			case "inode":
				replacement := filepath.Join(directory, "replacement")
				err = os.WriteFile(replacement, []byte("new"), 0600)
				if err == nil {
					err = os.Rename(replacement, staging)
				}
			case "mode":
				err = os.Chmod(staging, 0644)
			case "size":
				err = os.WriteFile(staging, []byte("changed"), 0600)
			case "hard-link":
				err = os.Link(staging, filepath.Join(directory, "extra"))
			default:
				if err = os.Remove(staging); err != nil {
					t.Fatal(err)
				}
				switch variant {
				case "symlink":
					err = os.Symlink("file", staging)
				case "fifo":
					err = unix.Mkfifo(staging, 0600)
				case "directory":
					err = os.Mkdir(staging, 0700)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(staging)
			if err != nil {
				t.Fatal(err)
			}
			if err := recoverLinuxStaging(root, namespace, record); err == nil {
				t.Fatal("mismatched entry removed")
			}
			after, err := os.Lstat(staging)
			if err != nil || !os.SameFile(before, after) {
				t.Fatal("collision altered:", err)
			}
			assertJournalledTarget(t, filepath.Join(directory, "parent"), "original")
		})
	}
}

func TestRecoveryCleanupRejectsForgedAuthority(t *testing.T) {
	root, namespace, record, directory := recoveryCleanupFixture(t)
	mutations := []func(*RecoveryRecord){
		func(record *RecoveryRecord) { record.Namespace = strings.Repeat("f", 32) },
		func(record *RecoveryRecord) { record.Root = "other" },
		func(record *RecoveryRecord) { record.RootDevice++; record.ParentDevice++; record.Device++ },
		func(record *RecoveryRecord) { record.RootInode++ },
		func(record *RecoveryRecord) { record.RootMountID++ },
		func(record *RecoveryRecord) { record.ParentInode++ },
		func(record *RecoveryRecord) { record.Inode++ },
		func(record *RecoveryRecord) { record.Size++ },
		func(record *RecoveryRecord) { record.Path = "../parent/file" },
		func(record *RecoveryRecord) { record.Path = "/parent/file" },
		func(record *RecoveryRecord) { record.Path = "parent/.pipit-stage-file" },
	}
	for _, mutate := range mutations {
		changed := record
		mutate(&changed)
		if err := recoverLinuxStaging(root, namespace, changed); err == nil {
			t.Fatal("forged authority accepted:", changed)
		}
	}
	readOnly := root
	readOnly.identity.Rights = Read
	if err := recoverLinuxStaging(readOnly, namespace, record); !errors.Is(err, ErrDenied) {
		t.Fatal("read-only root used for cleanup:", err)
	}
	other, err := openLinuxDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	substituted := root
	substituted.file = other
	if err := recoverLinuxStaging(substituted, namespace, record); !errors.Is(err, ErrDenied) {
		t.Fatal("substituted root used:", err)
	}
	assertJournalledTarget(t, filepath.Join(directory, "parent"), "original")
	entries, err := os.ReadDir(filepath.Join(directory, "parent"))
	if err != nil || len(entries) != 2 {
		t.Fatal("rejected metadata removed a remnant:", entries, err)
	}
}

func TestRecoveryCleanupRejectsChangedParent(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		root, namespace, record, directory := recoveryCleanupFixture(t)
		parent := filepath.Join(directory, "parent")
		moved := filepath.Join(directory, "moved")
		if err := os.Rename(parent, moved); err != nil {
			t.Fatal(err)
		}
		if symlink {
			if err := os.Symlink("moved", parent); err != nil {
				t.Fatal(err)
			}
		} else if err := os.Mkdir(parent, 0700); err != nil {
			t.Fatal(err)
		}
		if err := recoverLinuxStaging(root, namespace, record); err == nil {
			t.Fatal("replacement parent accepted")
		}
		entries, err := os.ReadDir(moved)
		if err != nil || len(entries) != 2 {
			t.Fatal("original parent altered:", entries, err)
		}
		assertJournalledTarget(t, moved, "original")
	}
}

func recoveryCleanupFixture(t *testing.T) (linuxRecoveryRoot, string, RecoveryRecord, string) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "parent"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "parent", "file"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := openTestFilesystem(t, directory, Write, FilesystemLimits{})
	root, record := stageRecoveryRemnant(t, backend, "data", "parent/file")
	return root, backend.namespace, record, directory
}

func stageRecoveryRemnant(t *testing.T, backend *LinuxFilesystem, rootName, target string) (linuxRecoveryRoot, RecoveryRecord) {
	t.Helper()
	root := linuxRecoveryRoot{file: backend.roots[rootName], identity: backend.identities[rootName]}
	prepared, err := prepareLinuxWrite(root.file, target, []byte("new"), 1)
	if prepared != nil {
		t.Cleanup(func() { _ = prepared.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	record := RecoveryRecord{
		Profile: recoveryRecordProfile, Namespace: backend.namespace, Root: rootName, Path: target, Operation: 1,
		RootDevice: root.identity.Device, RootInode: root.identity.Inode, RootMountID: root.identity.MountID,
		ParentDevice: prepared.identity.ParentDevice, ParentInode: prepared.identity.ParentInode,
		Device: prepared.identity.Device, Inode: prepared.identity.Inode, Size: prepared.identity.Size,
	}
	name, err := StagingName(backend.namespace, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Linkat(int(backend.proc.Fd()), strconv.FormatUint(uint64(prepared.temporary.Fd()), 10),
		int(prepared.parent.Fd()), name, unix.AT_SYMLINK_FOLLOW); err != nil {
		t.Fatal(err)
	}
	if err := prepared.parent.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	return root, record
}
