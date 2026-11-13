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

//go:build linux && (amd64 || arm64)

package sandboxbroker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryJournalRefreshesUncertainAppend(t *testing.T) {
	t.Parallel()
	directory := testRecoveryDirectory(t)
	record := recoveryRecordFixture()
	journal, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	require.NoError(t, err)
	defer journal.Close()
	require.NoError(t, journal.appendRecord(record))
	record.Operation = 2
	encoded, err := encodeRecoveryRecord(record)
	require.NoError(t, err)
	require.NoError(t, journal.persist(record, encoded))
	journal.failed = true
	records, err := journal.Records()
	require.ErrorIs(t, err, ErrClosed, "uncertain append exposed cached records")
	require.Nil(t, records)
	for range 3 {
		records, err := journal.RecoveryRecords()
		require.NoError(t, err, "uncertain linked intent missed")
		require.Len(t, records, 2)
		require.Equal(t, record, records[1])
		records[0].Path = "changed"
	}
	require.Len(t, journal.records, 1, "recovery changed the acknowledged-record cache")
	record.Operation++
	require.ErrorIs(t, journal.appendRecord(record), ErrClosed, "recovery does not re-enable a failed admission")
	requireJournalLocked(t, directory, record.Namespace, "recovery keeps the original journal lock")
}

func TestRecoveryJournalRefreshRejectsChangedStorage(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"missing", "changed", "malformed", "extra-corrupt", "permissions"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			directory := testRecoveryDirectory(t)
			record := recoveryRecordFixture()
			journal, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
			require.NoError(t, err)
			defer journal.Close()
			require.NoError(t, journal.appendRecord(record))
			name, err := recoveryRecordName(record)
			require.NoError(t, err)
			filename := filepath.Join(directory, name)
			switch mode {
			case "missing":
				err = os.Remove(filename)
			case "changed":
				record.Inode++
				var encoded []byte
				encoded, err = encodeRecoveryRecord(record)
				if err == nil {
					err = os.WriteFile(filename, encoded, 0600)
				}
			case "malformed":
				err = os.WriteFile(filename, []byte("{"), 0600)
			case "extra-corrupt":
				err = os.WriteFile(filepath.Join(directory, "unexpected"), []byte("{}"), 0600)
			case "permissions":
				err = os.Chmod(directory, 0755)
			}
			require.NoError(t, err)
			records, err := journal.RecoveryRecords()
			require.Error(t, err, "changed storage accepted or partially returned")
			require.Nil(t, records)
			records, err = journal.Records()
			require.ErrorIs(t, err, ErrClosed, "invalid recovery storage retained writable state")
			require.Nil(t, records)
			record.Operation++
			require.ErrorIs(t, journal.appendRecord(record), ErrClosed, "invalid recovery storage accepted another append")
		})
	}
}

func TestRecoveryJournalRefreshKeepsPinnedDirectory(t *testing.T) {
	t.Parallel()
	directory := testRecoveryDirectory(t)
	record := recoveryRecordFixture()
	journal, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	require.NoError(t, err)
	defer journal.Close()
	require.NoError(t, journal.appendRecord(record))
	moved := filepath.Join(t.TempDir(), "original")
	require.NoError(t, os.Rename(directory, moved))
	require.NoError(t, os.Mkdir(directory, 0700))
	records, err := journal.RecoveryRecords()
	require.NoError(t, err, "recovery followed replacement pathname")
	require.Len(t, records, 1)
	require.Equal(t, record, records[0])
	requireJournalLocked(t, moved, record.Namespace, "recovery keeps the original directory lease")
	require.NoError(t, journal.Close())
	records, err = journal.RecoveryRecords()
	require.ErrorIs(t, err, ErrClosed, "closed recovery journal returned records")
	require.Nil(t, records)
	var empty LinuxRecoveryJournal
	for _, absent := range []*LinuxRecoveryJournal{nil, &empty} {
		records, err := absent.RecoveryRecords()
		require.ErrorIs(t, err, ErrClosed, "empty journal returned records")
		require.Nil(t, records)
	}
}

func TestRecoveryBootstrapUsesUncertainDurableIntent(t *testing.T) {
	t.Parallel()
	authority := testRecoveryAuthority(t, Write)
	journal := testRecoveryAuthorityJournal(t, authority)
	record := testRecoveryAuthorityRecord(t, authority, 1)
	call, err := authority.admit(brokerMessage(1, recoveryWritePayload))
	require.NoError(t, err)
	encoded, err := encodeRecoveryRecord(record)
	require.NoError(t, err)
	require.NoError(t, journal.persist(record, encoded))
	journal.failed = true
	require.NoError(t, call.Finish(0))
	authority.budget.Close()
	bootstrap, err := authority.RecoveryBootstrap(journal)
	require.NoError(t, err, "uncertain durable intent blocked recovery")
	defer bootstrap.Close()
	files, err := bootstrap.Files()
	require.NoError(t, err)
	policy, err := readLinuxBootstrap(files[0])
	require.NoError(t, err)
	records, err := decodeBootstrapRecovery(policy)
	require.NoError(t, err, "uncertain intent omitted from sealed batch")
	require.Len(t, records, 1)
	require.Equal(t, record, records[0])
	require.ErrorIs(t, journal.appendRecord(record), ErrClosed, "bootstrap construction does not revive a failed journal")
}
