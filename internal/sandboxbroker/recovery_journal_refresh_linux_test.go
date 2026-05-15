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
	"testing"
)

func TestRecoveryJournalRefreshesUncertainAppend(t *testing.T) {
	directory := testRecoveryDirectory(t)
	record := recoveryRecordFixture()
	journal, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if err := journal.appendRecord(record); err != nil {
		t.Fatal(err)
	}
	record.Operation = 2
	encoded, err := encodeRecoveryRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.persist(record, encoded); err != nil {
		t.Fatal(err)
	}
	journal.failed = true
	if records, err := journal.Records(); !errors.Is(err, ErrClosed) || records != nil {
		t.Fatal("uncertain append exposed cached records:", records, err)
	}
	for range 3 {
		records, err := journal.RecoveryRecords()
		if err != nil || len(records) != 2 || records[1] != record {
			t.Fatal("uncertain linked intent missed:", records, err)
		}
		records[0].Path = "changed"
	}
	if len(journal.records) != 1 {
		t.Fatal("recovery changed the acknowledged-record cache")
	}
	record.Operation++
	if err := journal.appendRecord(record); !errors.Is(err, ErrClosed) {
		t.Fatal("recovery re-enabled failed admission:", err)
	}
	if other, err := OpenLinuxRecoveryJournal(directory, record.Namespace); err == nil || other != nil {
		if other != nil {
			_ = other.Close()
		}
		t.Fatal("recovery released the original journal lock")
	}
}

func TestRecoveryJournalRefreshRejectsChangedStorage(t *testing.T) {
	for _, mode := range []string{"missing", "changed", "malformed", "extra-corrupt", "permissions"} {
		t.Run(mode, func(t *testing.T) {
			directory := testRecoveryDirectory(t)
			record := recoveryRecordFixture()
			journal, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			if err := journal.appendRecord(record); err != nil {
				t.Fatal(err)
			}
			name, err := recoveryRecordName(record)
			if err != nil {
				t.Fatal(err)
			}
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
			if err != nil {
				t.Fatal(err)
			}
			if records, err := journal.RecoveryRecords(); err == nil || records != nil {
				t.Fatal("changed storage accepted or partially returned:", records, err)
			}
			if records, err := journal.Records(); !errors.Is(err, ErrClosed) || records != nil {
				t.Fatal("invalid recovery storage retained writable state:", err)
			}
			record.Operation++
			if err := journal.appendRecord(record); !errors.Is(err, ErrClosed) {
				t.Fatal("invalid recovery storage accepted another append:", err)
			}
		})
	}
}

func TestRecoveryJournalRefreshKeepsPinnedDirectory(t *testing.T) {
	directory := testRecoveryDirectory(t)
	record := recoveryRecordFixture()
	journal, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if err := journal.appendRecord(record); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(t.TempDir(), "original")
	if err := os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	records, err := journal.RecoveryRecords()
	if err != nil || len(records) != 1 || records[0] != record {
		t.Fatal("recovery followed replacement pathname:", records, err)
	}
	if other, err := OpenLinuxRecoveryJournal(moved, record.Namespace); err == nil || other != nil {
		if other != nil {
			_ = other.Close()
		}
		t.Fatal("recovery dropped original directory lease")
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if records, err := journal.RecoveryRecords(); !errors.Is(err, ErrClosed) || records != nil {
		t.Fatal("closed recovery journal returned records:", err)
	}
	var empty LinuxRecoveryJournal
	for _, absent := range []*LinuxRecoveryJournal{nil, &empty} {
		if records, err := absent.RecoveryRecords(); !errors.Is(err, ErrClosed) || records != nil {
			t.Fatal("empty journal returned records:", err)
		}
	}
}

func TestRecoveryBootstrapUsesUncertainDurableIntent(t *testing.T) {
	authority := testRecoveryAuthority(t, Write)
	journal := testRecoveryAuthorityJournal(t, authority)
	record := testRecoveryAuthorityRecord(t, authority, 1)
	call, err := authority.admit(brokerMessage(1, recoveryWritePayload))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeRecoveryRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.persist(record, encoded); err != nil {
		t.Fatal(err)
	}
	journal.failed = true
	if err := call.Finish(0); err != nil {
		t.Fatal(err)
	}
	authority.budget.Close()
	bootstrap, err := authority.RecoveryBootstrap(journal)
	if err != nil {
		t.Fatal("uncertain durable intent blocked recovery:", err)
	}
	defer bootstrap.Close()
	files, err := bootstrap.Files()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := readLinuxBootstrap(files[0])
	if err != nil {
		t.Fatal(err)
	}
	records, err := decodeBootstrapRecovery(policy)
	if err != nil || len(records) != 1 || records[0] != record {
		t.Fatal("uncertain intent omitted from sealed batch:", records, err)
	}
	if err := journal.appendRecord(record); !errors.Is(err, ErrClosed) {
		t.Fatal("bootstrap construction revived failed journal:", err)
	}
}
