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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestRecoveryJournalDurableRoundTrip(t *testing.T) {
	t.Parallel()
	directory := testRecoveryDirectory(t)
	record := recoveryRecordFixture()
	owner, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	require.NoError(t, err)
	t.Cleanup(func() { _ = owner.Close() })
	requireJournalLocked(t, directory, record.Namespace, "a concurrent journal owner is refused")
	for _, operation := range []uint64{1, 3, 12} {
		record.Operation = operation
		require.NoError(t, owner.appendRecord(record))
	}
	records, err := owner.Records()
	require.NoError(t, err, "missing durable records")
	require.Len(t, records, 3)
	records[0].Path = "changed"
	records, err = owner.Records()
	require.NoError(t, err, "caller mutated retained records")
	require.Equal(t, record.Path, records[0].Path)
	require.NoError(t, owner.Close())
	require.NoError(t, owner.Close())
	require.ErrorIs(t, owner.appendRecord(record), ErrClosed, "closed journal accepted write")
	records, err = owner.Records()
	require.ErrorIs(t, err, ErrClosed, "closed journal returned records")
	require.Nil(t, records)
	reopened, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	require.NoError(t, err)
	defer reopened.Close()
	records, err = reopened.Records()
	require.NoError(t, err, "reopened journal lost ordering or records")
	require.Len(t, records, 3)
	require.Equal(t, uint64(1), records[0].Operation)
	require.Equal(t, uint64(3), records[1].Operation)
	require.Equal(t, uint64(12), records[2].Operation)
	require.Error(t, reopened.appendRecord(record), "replayed operation accepted")
	record.Operation++
	require.NoError(t, reopened.appendRecord(record), "reopened journal cannot append")
	other, err := OpenLinuxRecoveryJournal(directory, strings.Repeat("b", 32))
	require.Error(t, err, "second namespace admitted while locked")
	require.Nil(t, other)
}

func TestRecoveryJournalRejectsUnsafeStorage(t *testing.T) {
	t.Parallel()
	for _, variant := range []string{"directory-mode", "directory-link", "file-mode", "file-link", "hard-link",
		"fifo", "directory", "oversized", "empty", "corrupt", "wrong-name", "wrong-namespace", "excess-records"} {
		t.Run(variant, func(t *testing.T) {
			t.Parallel()
			directory := testRecoveryDirectory(t)
			record := recoveryRecordFixture()
			encoded, err := encodeRecoveryRecord(record)
			require.NoError(t, err)
			name, err := recoveryRecordName(record)
			require.NoError(t, err)
			target := filepath.Join(directory, name)
			openPath := directory
			switch variant {
			case "directory-mode":
				err = os.Chmod(directory, 0755)
			case "directory-link":
				openPath = filepath.Join(t.TempDir(), "link")
				err = os.Symlink(directory, openPath)
			case "file-mode":
				err = os.WriteFile(target, encoded, 0644)
			case "file-link":
				err = os.Symlink(filepath.Join(t.TempDir(), "absent"), target)
			case "hard-link":
				err = os.WriteFile(target, encoded, 0600)
				if err == nil {
					err = os.Link(target, filepath.Join(t.TempDir(), "alias"))
				}
			case "fifo":
				err = unix.Mkfifo(target, 0600)
			case "directory":
				err = os.Mkdir(target, 0700)
			case "oversized":
				err = os.WriteFile(target, []byte(strings.Repeat(" ", maximumRecoveryRecordBytes+1)), 0600)
			case "empty":
				err = os.WriteFile(target, nil, 0600)
			case "corrupt":
				err = os.WriteFile(target, []byte("{"), 0600)
			case "wrong-name":
				err = os.WriteFile(filepath.Join(directory, "unrelated"), encoded, 0600)
			case "wrong-namespace":
				record.Namespace = strings.Repeat("b", 32)
				encoded, err = encodeRecoveryRecord(record)
				if err == nil {
					name, err = recoveryRecordName(record)
					if err == nil {
						err = os.WriteFile(filepath.Join(directory, name), encoded, 0600)
					}
				}
			case "excess-records":
				for operation := uint64(1); operation <= defaultCalls+1; operation++ {
					record.Operation = operation
					name, err = recoveryRecordName(record)
					if err != nil {
						break
					}
					err = os.WriteFile(filepath.Join(directory, name), encoded, 0600)
					if err != nil {
						break
					}
				}
			}
			require.NoError(t, err)
			before, err := os.ReadDir(directory)
			require.NoError(t, err)
			owner, err := OpenLinuxRecoveryJournal(openPath, recoveryRecordFixture().Namespace)
			if owner != nil {
				_ = owner.Close()
			}
			require.Error(t, err, "an unsafe journal is refused")
			require.Nil(t, owner)
			after, err := os.ReadDir(directory)
			require.NoError(t, err, "failed open deleted entries")
			require.Len(t, after, len(before))
		})
	}
}

func TestRecoveryJournalCollisionPoisonsOwner(t *testing.T) {
	t.Parallel()
	directory := testRecoveryDirectory(t)
	record := recoveryRecordFixture()
	owner, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	require.NoError(t, err)
	defer owner.Close()
	name, err := recoveryRecordName(record)
	require.NoError(t, err)
	target := filepath.Join(directory, name)
	require.NoError(t, os.WriteFile(target, []byte("original"), 0600))
	require.ErrorIs(t, owner.appendRecord(record), unix.EEXIST, "colliding journal entry replaced")
	data, err := os.ReadFile(target)
	require.NoError(t, err, "collision changed original")
	require.Equal(t, "original", string(data))
	require.ErrorIs(t, owner.appendRecord(record), ErrClosed, "uncertain owner remained usable")
	records, err := owner.Records()
	require.ErrorIs(t, err, ErrClosed, "uncertain owner exposed cached state")
	require.Nil(t, records)
	entries, err := os.ReadDir(directory)
	require.NoError(t, err, "anonymous staging left named debris")
	require.Len(t, entries, 1)
}

func TestRecoveryJournalQuotaAndNamespace(t *testing.T) {
	t.Parallel()
	record := recoveryRecordFixture()
	directory := testRecoveryDirectory(t)
	owner, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	require.NoError(t, err)
	defer owner.Close()
	record.Namespace = strings.Repeat("b", 32)
	require.Error(t, owner.appendRecord(record), "wrong namespace appended")
	record = recoveryRecordFixture()
	for operation := uint64(1); operation <= defaultCalls; operation++ {
		record.Operation = operation
		require.NoError(t, owner.appendRecord(record))
	}
	record.Operation++
	require.Error(t, owner.appendRecord(record), "journal exceeded operation budget")
	require.NoError(t, owner.Close())
	reopened, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	require.NoError(t, err)
	defer reopened.Close()
	records, err := reopened.Records()
	require.NoError(t, err, "bounded journal did not reopen")
	require.Len(t, records, defaultCalls)
}

func TestRecoveryJournalInvalidConfiguration(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", ".", "relative/path"} {
		owner, err := OpenLinuxRecoveryJournal(name, recoveryRecordFixture().Namespace)
		require.Error(t, err, "relative journal path accepted")
		require.Nil(t, owner)
	}
	invalid, err := OpenLinuxRecoveryJournal(t.TempDir(), "invalid")
	require.Error(t, err, "invalid namespace accepted")
	require.Nil(t, invalid)
	var owner *LinuxRecoveryJournal
	require.NoError(t, owner.Close())
	require.ErrorIs(t, owner.appendRecord(recoveryRecordFixture()), ErrClosed)
	records, err := owner.Records()
	require.ErrorIs(t, err, ErrClosed, "nil owner returned records")
	require.Nil(t, records)
}

func testRecoveryDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	require.NoError(t, os.Chmod(directory, 0700))
	return directory
}

func requireJournalLocked(t *testing.T, directory, namespace, what string) {
	t.Helper()
	other, err := OpenLinuxRecoveryJournal(directory, namespace)
	if other != nil {
		_ = other.Close()
	}
	require.Error(t, err, what)
	require.Nil(t, other, what)
}
