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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestRecoveryCleanupRemnantAndCompletedPublication(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"linked", "absent", "published"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			root, namespace, record, directory := recoveryCleanupFixture(t)
			name, err := StagingName(namespace, record.Operation)
			require.NoError(t, err)
			staging := filepath.Join(directory, "parent", name)
			expected := "original"
			switch state {
			case "absent":
				require.NoError(t, os.Remove(staging))
			case "published":
				require.NoError(t, os.Rename(staging, filepath.Join(directory, "parent", "file")))
				expected = "new"
			}
			for range 2 {
				require.NoError(t, recoverLinuxStaging(root, namespace, record), "recovery and its repeat succeed")
			}
			assertJournalledTarget(t, filepath.Join(directory, "parent"), expected)
			_, err = os.Lstat(staging)
			require.ErrorIs(t, err, os.ErrNotExist, "staging entry remains")
		})
	}
}

func TestRecoveryCleanupPreservesCollisions(t *testing.T) {
	t.Parallel()
	for _, variant := range []string{"inode", "mode", "size", "hard-link", "symlink", "fifo", "directory"} {
		t.Run(variant, func(t *testing.T) {
			t.Parallel()
			root, namespace, record, directory := recoveryCleanupFixture(t)
			name, err := StagingName(namespace, record.Operation)
			require.NoError(t, err)
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
				err = os.Remove(staging)
				require.NoError(t, err)
				switch variant {
				case "symlink":
					err = os.Symlink("file", staging)
				case "fifo":
					err = unix.Mkfifo(staging, 0600)
				case "directory":
					err = os.Mkdir(staging, 0700)
				}
			}
			require.NoError(t, err)
			before, err := os.Lstat(staging)
			require.NoError(t, err)
			require.Error(t, recoverLinuxStaging(root, namespace, record), "mismatched entry removed")
			after, err := os.Lstat(staging)
			require.NoError(t, err, "collision altered")
			require.True(t, os.SameFile(before, after))
			assertJournalledTarget(t, filepath.Join(directory, "parent"), "original")
		})
	}
}

func TestRecoveryCleanupRejectsForgedAuthority(t *testing.T) {
	t.Parallel()
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
		require.Error(t, recoverLinuxStaging(root, namespace, changed), "forged authority accepted: %v", changed)
	}
	readOnly := root
	readOnly.identity.Rights = Read
	require.ErrorIs(t, recoverLinuxStaging(readOnly, namespace, record), ErrDenied, "read-only root used for cleanup")
	other, err := openLinuxDirectory(t.TempDir())
	require.NoError(t, err)
	defer other.Close()
	substituted := root
	substituted.file = other
	require.ErrorIs(t, recoverLinuxStaging(substituted, namespace, record), ErrDenied, "substituted root used")
	assertJournalledTarget(t, filepath.Join(directory, "parent"), "original")
	entries, err := os.ReadDir(filepath.Join(directory, "parent"))
	require.NoError(t, err, "rejected metadata removed a remnant")
	require.Len(t, entries, 2)
}

func TestRecoveryCleanupRejectsChangedParent(t *testing.T) {
	t.Parallel()
	for _, symlink := range []bool{false, true} {
		root, namespace, record, directory := recoveryCleanupFixture(t)
		parent := filepath.Join(directory, "parent")
		moved := filepath.Join(directory, "moved")
		require.NoError(t, os.Rename(parent, moved))
		if symlink {
			require.NoError(t, os.Symlink("moved", parent))
		} else {
			require.NoError(t, os.Mkdir(parent, 0700))
		}
		require.Error(t, recoverLinuxStaging(root, namespace, record), "replacement parent accepted")
		entries, err := os.ReadDir(moved)
		require.NoError(t, err, "original parent altered")
		require.Len(t, entries, 2)
		assertJournalledTarget(t, moved, "original")
	}
}

func recoveryCleanupFixture(t *testing.T) (linuxRecoveryRoot, string, RecoveryRecord, string) {
	t.Helper()
	directory := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(directory, "parent"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "parent", "file"), []byte("original"), 0600))
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
	require.NoError(t, err)
	record := RecoveryRecord{
		Profile: recoveryRecordProfile, Namespace: backend.namespace, Root: rootName, Path: target, Operation: 1,
		RootDevice: root.identity.Device, RootInode: root.identity.Inode, RootMountID: root.identity.MountID,
		ParentDevice: prepared.identity.ParentDevice, ParentInode: prepared.identity.ParentInode,
		Device: prepared.identity.Device, Inode: prepared.identity.Inode, Size: prepared.identity.Size,
	}
	name, err := StagingName(backend.namespace, 1)
	require.NoError(t, err)
	require.NoError(t, unix.Linkat(int(backend.proc.Fd()), strconv.FormatUint(uint64(prepared.temporary.Fd()), 10),
		int(prepared.parent.Fd()), name, unix.AT_SYMLINK_FOLLOW))
	require.NoError(t, prepared.parent.Sync())
	require.NoError(t, prepared.Close())
	return root, record
}
