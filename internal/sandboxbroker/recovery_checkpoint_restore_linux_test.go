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
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

type checkpointRestoreFixture struct {
	encoded  []byte
	grants   []LinuxRootGrant
	journal  *LinuxRecoveryJournal
	record   RecoveryRecord
	binding  [sha256.Size]byte
	approved [sha256.Size]byte
}

func TestCheckpointRecoveryRejectsUnapprovedIntents(t *testing.T) {
	fixture := prepareCheckpointRestoreFixture(t)
	var original recoveryCheckpoint
	if err := json.Unmarshal(fixture.encoded, &original); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"namespace", "root", "inode", "mount", "operation", "path", "bytes", "duplicate", "read-only", "call-budget"} {
		t.Run(mode, func(t *testing.T) {
			policy := original
			policy.Roots = append([]filesystemBootstrapRoot(nil), original.Roots...)
			record := fixture.record
			switch mode {
			case "namespace":
				record.Namespace = "ffffffffffffffffffffffffffffffff"
			case "root":
				record.Root = "foreign"
			case "inode":
				record.RootInode++
			case "mount":
				record.RootMountID++
			case "operation":
				record.Operation = 0
			case "path":
				record.Path = "../escape"
			case "bytes":
				record.Size = int64(policy.Limits.WriteBytes) + 1
			case "read-only":
				policy.Roots[0].Rights = Read
			case "call-budget":
				policy.Limits.Calls = 0
			}
			records := []RecoveryRecord{record}
			if mode == "duplicate" {
				records = append(records, record)
			}
			if _, err := checkpointRecoveryPolicy(policy, records); err == nil {
				t.Fatal("unapproved intent became cleanup authority")
			}
		})
	}
}

func TestCheckpointRecoveryChecksApprovalBeforeOpeningRoots(t *testing.T) {
	fixture := prepareCheckpointRestoreFixture(t)
	fixture.approved[0] ^= 1
	fixture.grants[0].Path = filepath.Join(t.TempDir(), "missing", "root")
	bootstrap, err := PrepareCheckpointRecovery(fixture.encoded, fixture.approved, fixture.binding, fixture.journal, fixture.grants)
	if bootstrap != nil || !errors.Is(err, ErrDenied) {
		if bootstrap != nil {
			_ = bootstrap.Close()
		}
		t.Fatal("host roots opened before independent approval validation:", err)
	}
}

func prepareCheckpointRestoreFixture(t *testing.T) checkpointRestoreFixture {
	t.Helper()
	grants := []LinuxRootGrant{{Name: "data", Path: testRecoveryDirectory(t), Rights: Read | Write | List}}
	original, err := PrepareLinuxBootstrap(grants, FilesystemLimits{WriteBytes: 3})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := original.RecoveryAuthority()
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	directory := testRecoveryDirectory(t)
	journal, err := OpenLinuxRecoveryJournal(directory, authority.namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	binding := [sha256.Size]byte{1}
	encoded, err := authority.checkpoint(journal, binding)
	if err != nil {
		t.Fatal(err)
	}
	record := testRecoveryAuthorityRecord(t, authority, 1)
	call, err := authority.admit(brokerMessage(1, recoveryWritePayload))
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.record(journal, call, record); err != nil {
		t.Fatal(err)
	}
	if err := call.Finish(0); err != nil {
		t.Fatal(err)
	}
	if err := authority.Close(); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLinuxRecoveryJournal(directory, authority.namespace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	return checkpointRestoreFixture{encoded: encoded, grants: grants, journal: reopened, record: record,
		binding: binding, approved: sha256.Sum256(encoded)}
}

func TestCheckpointRecoveryRestoresOnlyCleanup(t *testing.T) {
	fixture := prepareCheckpointRestoreFixture(t)
	bootstrap, err := PrepareCheckpointRecovery(fixture.encoded, fixture.approved, fixture.binding, fixture.journal, fixture.grants)
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	if !bootstrap.RecoveryOnly() || bootstrap.StagingNamespace() != fixture.record.Namespace {
		t.Fatal("restored wrong role or namespace")
	}
	if authority, err := bootstrap.RecoveryAuthority(); authority != nil || !errors.Is(err, ErrInvalidPolicy) {
		if authority != nil {
			_ = authority.Close()
		}
		t.Fatal("recovery restored normal execution authority:", err)
	}
	files, err := bootstrap.Files()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := readLinuxBootstrap(files[0])
	if err != nil {
		t.Fatal(err)
	}
	records, err := decodeBootstrapRecovery(policy)
	if err != nil || len(records) != 1 || records[0] != fixture.record {
		t.Fatal("restored wrong intents:", records, err)
	}
	if len(policy.Roots) != 1 || policy.Roots[0].Rights != Write || policy.Limits.WriteBytes != 3 {
		t.Fatal("restored authority widened:", policy)
	}
	if err := fixture.journal.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := linuxBootstrapRootIdentity(files[1]); err != nil {
		t.Fatal("restored bootstrap lost independent root pin:", err)
	}
}

func TestCheckpointRecoveryRejectsChangedAuthority(t *testing.T) {
	for _, mode := range []string{"approval", "binding", "journal", "root", "rights", "name", "missing-root", "extra-root", "closed-journal", "empty-journal"} {
		t.Run(mode, func(t *testing.T) {
			fixture := prepareCheckpointRestoreFixture(t)
			switch mode {
			case "approval":
				fixture.approved[0] ^= 1
			case "binding":
				fixture.binding[0] ^= 1
			case "journal":
				other, err := OpenLinuxRecoveryJournal(testRecoveryDirectory(t), fixture.record.Namespace)
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				fixture.journal = other
			case "root":
				moved := filepath.Join(t.TempDir(), "original")
				if err := os.Rename(fixture.grants[0].Path, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(fixture.grants[0].Path, 0700); err != nil {
					t.Fatal(err)
				}
			case "rights":
				fixture.grants[0].Rights = Read
			case "name":
				fixture.grants[0].Name = "other"
			case "missing-root":
				fixture.grants = nil
			case "extra-root":
				fixture.grants = append(fixture.grants, LinuxRootGrant{Name: "extra", Path: testRecoveryDirectory(t), Rights: Write})
			case "closed-journal":
				if err := fixture.journal.Close(); err != nil {
					t.Fatal(err)
				}
			case "empty-journal":
				fixture.journal.records = nil
				name, err := recoveryRecordName(fixture.record)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join("/proc/self/fd", strconv.Itoa(int(fixture.journal.directory.Fd())), name)); err != nil {
					t.Fatal(err)
				}
			}
			bootstrap, err := PrepareCheckpointRecovery(fixture.encoded, fixture.approved, fixture.binding, fixture.journal, fixture.grants)
			if bootstrap != nil {
				_ = bootstrap.Close()
			}
			if bootstrap != nil || err == nil {
				t.Fatal("changed authority restored")
			}
		})
	}
}
