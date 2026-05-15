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
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRecoveryJournalDurableRoundTrip(t *testing.T) {
	directory := testRecoveryDirectory(t)
	record := recoveryRecordFixture()
	owner, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if other, err := OpenLinuxRecoveryJournal(directory, record.Namespace); err == nil || other != nil {
		if other != nil {
			_ = other.Close()
		}
		t.Fatal("concurrent journal owner admitted")
	}
	for _, operation := range []uint64{1, 3, 12} {
		record.Operation = operation
		if err := owner.appendRecord(record); err != nil {
			t.Fatal(err)
		}
	}
	records, err := owner.Records()
	if err != nil || len(records) != 3 {
		t.Fatal("missing durable records:", records, err)
	}
	records[0].Path = "changed"
	records, err = owner.Records()
	if err != nil || records[0].Path != record.Path {
		t.Fatal("caller mutated retained records:", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.appendRecord(record); !errors.Is(err, ErrClosed) {
		t.Fatal("closed journal accepted write:", err)
	}
	if records, err := owner.Records(); !errors.Is(err, ErrClosed) || records != nil {
		t.Fatal("closed journal returned records:", err)
	}
	reopened, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	records, err = reopened.Records()
	if err != nil || len(records) != 3 || records[0].Operation != 1 || records[1].Operation != 3 || records[2].Operation != 12 {
		t.Fatal("reopened journal lost ordering or records:", records, err)
	}
	if err := reopened.appendRecord(record); err == nil {
		t.Fatal("replayed operation accepted")
	}
	record.Operation++
	if err := reopened.appendRecord(record); err != nil {
		t.Fatal("reopened journal cannot append:", err)
	}
	if other, err := OpenLinuxRecoveryJournal(directory, strings.Repeat("b", 32)); err == nil || other != nil {
		t.Fatal("second namespace admitted while locked")
	}
}

func TestRecoveryJournalRejectsUnsafeStorage(t *testing.T) {
	for _, variant := range []string{"directory-mode", "directory-link", "file-mode", "file-link", "hard-link",
		"fifo", "directory", "oversized", "empty", "corrupt", "wrong-name", "wrong-namespace", "excess-records"} {
		t.Run(variant, func(t *testing.T) {
			directory := testRecoveryDirectory(t)
			record := recoveryRecordFixture()
			encoded, err := encodeRecoveryRecord(record)
			if err != nil {
				t.Fatal(err)
			}
			name, err := recoveryRecordName(record)
			if err != nil {
				t.Fatal(err)
			}
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
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			owner, err := OpenLinuxRecoveryJournal(openPath, recoveryRecordFixture().Namespace)
			if err == nil || owner != nil {
				if owner != nil {
					_ = owner.Close()
				}
				t.Fatal("unsafe journal accepted")
			}
			after, err := os.ReadDir(directory)
			if err != nil || len(after) != len(before) {
				t.Fatal("failed open deleted entries:", err)
			}
		})
	}
}

func TestRecoveryJournalCollisionPoisonsOwner(t *testing.T) {
	directory := testRecoveryDirectory(t)
	record := recoveryRecordFixture()
	owner, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	name, err := recoveryRecordName(record)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, name)
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := owner.appendRecord(record); !errors.Is(err, unix.EEXIST) {
		t.Fatal("colliding journal entry replaced:", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "original" {
		t.Fatal("collision changed original:", string(data), err)
	}
	if err := owner.appendRecord(record); !errors.Is(err, ErrClosed) {
		t.Fatal("uncertain owner remained usable:", err)
	}
	if records, err := owner.Records(); !errors.Is(err, ErrClosed) || records != nil {
		t.Fatal("uncertain owner exposed cached state:", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatal("anonymous staging left named debris:", entries, err)
	}
}

func TestRecoveryJournalQuotaAndNamespace(t *testing.T) {
	record := recoveryRecordFixture()
	directory := testRecoveryDirectory(t)
	owner, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	record.Namespace = strings.Repeat("b", 32)
	if err := owner.appendRecord(record); err == nil {
		t.Fatal("wrong namespace appended")
	}
	record = recoveryRecordFixture()
	for operation := uint64(1); operation <= defaultCalls; operation++ {
		record.Operation = operation
		if err := owner.appendRecord(record); err != nil {
			t.Fatal(err)
		}
	}
	record.Operation++
	if err := owner.appendRecord(record); err == nil {
		t.Fatal("journal exceeded operation budget")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	records, err := reopened.Records()
	if err != nil || len(records) != defaultCalls {
		t.Fatal("bounded journal did not reopen:", len(records), err)
	}
}

func TestRecoveryJournalInvalidConfiguration(t *testing.T) {
	for _, name := range []string{"", ".", "relative/path"} {
		if owner, err := OpenLinuxRecoveryJournal(name, recoveryRecordFixture().Namespace); err == nil || owner != nil {
			t.Fatal("relative journal path accepted")
		}
	}
	if owner, err := OpenLinuxRecoveryJournal(t.TempDir(), "invalid"); err == nil || owner != nil {
		t.Fatal("invalid namespace accepted")
	}
	var owner *LinuxRecoveryJournal
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.appendRecord(recoveryRecordFixture()); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if records, err := owner.Records(); !errors.Is(err, ErrClosed) || records != nil {
		t.Fatal("nil owner returned records")
	}
}

func testRecoveryDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return directory
}
