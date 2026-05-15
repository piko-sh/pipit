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
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryClaimPreparesCleanupOnly(t *testing.T) {
	claim, grants, _ := prepareRecoveryClaimFixture(t, true)
	bootstrap, err := claim.PrepareRecovery(grants)
	if err != nil || bootstrap == nil {
		t.Fatal("recovery preparation failed:", err)
	}
	defer bootstrap.Close()
	if !bootstrap.RecoveryOnly() {
		t.Fatal("claim restored normal execution")
	}
	if authority, err := bootstrap.RecoveryAuthority(); !errors.Is(err, ErrInvalidPolicy) || authority != nil {
		if authority != nil {
			_ = authority.Close()
		}
		t.Fatal("claim renewed operation admission:", err)
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
	if err != nil || len(records) != 1 || records[0].Operation != 1 {
		t.Fatal("original journal intent was lost:", err)
	}
	if len(policy.Roots) != 1 || policy.Roots[0].Rights != Write {
		t.Fatal("cleanup authority widened")
	}
	if err := claim.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := linuxBootstrapRootIdentity(files[1]); err != nil {
		t.Fatal("bootstrap did not own its root handle:", err)
	}
	if bootstrap, err := claim.PrepareRecovery(grants); !errors.Is(err, ErrClosed) || bootstrap != nil {
		t.Fatal("closed claim prepared new authority:", err)
	}
}

func TestRecoveryClaimEmptyJournalStillValidatesRoots(t *testing.T) {
	claim, grants, _ := prepareRecoveryClaimFixture(t, false)
	bootstrap, err := claim.PrepareRecovery(grants)
	if err != nil || bootstrap != nil {
		t.Fatal("validated empty journal did not report no work:", err)
	}
	changed := append([]LinuxRootGrant(nil), grants...)
	changed[0].Path = testRecoveryDirectory(t)
	if bootstrap, err := claim.PrepareRecovery(changed); !errors.Is(err, ErrDenied) || bootstrap != nil {
		t.Fatal("empty journal bypassed original root identity:", err)
	}
	changed = append([]LinuxRootGrant(nil), grants...)
	changed[0].Rights = Read
	if bootstrap, err := claim.PrepareRecovery(changed); !errors.Is(err, ErrDenied) || bootstrap != nil {
		t.Fatal("empty journal bypassed original rights:", err)
	}
}

func TestRecoveryClaimRejectsMetadataMovedIntoGrant(t *testing.T) {
	for _, index := range []int{1, 2} {
		for _, withRecord := range []bool{false, true} {
			claim, grants, directories := prepareRecoveryClaimFixture(t, withRecord)
			if err := os.Rename(directories[index], filepath.Join(grants[0].Path, "moved-metadata")); err != nil {
				t.Fatal(err)
			}
			if bootstrap, err := claim.PrepareRecovery(grants); !errors.Is(err, ErrDenied) || bootstrap != nil {
				if bootstrap != nil {
					_ = bootstrap.Close()
				}
				t.Fatal("restored grant contains recovery metadata:", index, withRecord, err)
			}
		}
	}
}

func TestRecoveryClaimRejectsGrantMovedIntoMetadata(t *testing.T) {
	for _, index := range []int{1, 2} {
		claim, grants, directories := prepareRecoveryClaimFixture(t, true)
		moved := filepath.Join(directories[index], "moved-root")
		if err := os.Rename(grants[0].Path, moved); err != nil {
			t.Fatal(err)
		}
		grants[0].Path = moved
		if bootstrap, err := claim.PrepareRecovery(grants); !errors.Is(err, ErrDenied) || bootstrap != nil {
			if bootstrap != nil {
				_ = bootstrap.Close()
			}
			t.Fatal("restored grant inside recovery metadata accepted:", index, err)
		}
	}
}

func prepareRecoveryClaimFixture(t *testing.T, withRecord bool) (*LinuxRecoveryClaim, []LinuxRootGrant, [3]string) {
	t.Helper()
	grants := []LinuxRootGrant{{Name: "data", Path: testRecoveryDirectory(t), Rights: Read | Write | List}}
	bootstrap, err := PrepareLinuxBootstrap(grants, FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	authority, err := bootstrap.RecoveryAuthority()
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	directories := [3]string{testRecoveryDirectory(t), testRecoveryDirectory(t), testRecoveryDirectory(t)}
	journal, err := OpenLinuxRecoveryJournal(directories[0], authority.namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(directories[1])
	if err != nil {
		t.Fatal(err)
	}
	defer checkpoint.Close()
	approval, err := OpenLinuxRecoveryApprovalStore(directories[2])
	if err != nil {
		t.Fatal(err)
	}
	defer approval.Close()
	binding := [sha256.Size]byte{1}
	if _, err := authority.PersistApprovedCheckpoint(journal, checkpoint, approval, binding); err != nil {
		t.Fatal(err)
	}
	if withRecord {
		record := testRecoveryAuthorityRecord(t, authority, 1)
		call, err := authority.admit(brokerMessage(1, recoveryWritePayload))
		if err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(authority.record(journal, call, record), call.Finish(0)); err != nil {
			t.Fatal(err)
		}
	}
	if err := errors.Join(authority.Close(), journal.Close(), checkpoint.Close(), approval.Close()); err != nil {
		t.Fatal(err)
	}
	claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = claim.Close() })
	return claim, grants, directories
}
