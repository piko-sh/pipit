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
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestRecoveryClaimExclusiveOwnership(t *testing.T) {
	directories, binding := recoveryClaimFixture(t)
	claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	encoded, approved, err := claim.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRecoveryCheckpoint(encoded, approved, binding); err != nil {
		t.Fatal(err)
	}
	encoded[0] ^= 1
	fresh, captured, err := claim.Checkpoint()
	if err != nil || captured != approved || bytes.Equal(fresh, encoded) {
		t.Fatal("checkpoint aliases caller bytes:", err)
	}
	var contenders sync.WaitGroup
	for range 8 {
		contenders.Go(func() {
			competing, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
			if competing != nil {
				_ = competing.Close()
			}
			if err == nil {
				t.Error("simultaneous recovery claim accepted")
			}
		})
	}
	contenders.Wait()
	for _, directory := range directories {
		var storage LinuxRecoveryJournal
		err := storage.open(directory)
		if closeErr := storage.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if err == nil {
			t.Fatal("claim did not retain every store lease")
		}
	}
	if err := claim.Close(); err != nil {
		t.Fatal(err)
	}
	if encoded, approved, err := claim.Checkpoint(); !errors.Is(err, ErrClosed) || encoded != nil || approved != ([sha256.Size]byte{}) {
		t.Fatal("released claim exposed authority:", err)
	}
	replacement, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	if err != nil {
		t.Fatal("released claim kept leases:", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryClaimRejectsChangedStorage(t *testing.T) {
	for _, mode := range []string{"binding", "checkpoint", "approval", "journal", "missing-checkpoint", "missing-approval"} {
		t.Run(mode, func(t *testing.T) {
			directories, binding := recoveryClaimFixture(t)
			switch mode {
			case "binding":
				binding[1] = 1
			case "checkpoint":
				if err := os.WriteFile(filepath.Join(directories[1], recoveryCheckpointName), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "approval":
				if err := os.WriteFile(filepath.Join(directories[2], recoveryApprovalName), []byte("invalid"), 0600); err != nil {
					t.Fatal(err)
				}
			case "journal":
				directories[0] = testRecoveryDirectory(t)
			case "missing-checkpoint":
				if err := os.Remove(filepath.Join(directories[1], recoveryCheckpointName)); err != nil {
					t.Fatal(err)
				}
			case "missing-approval":
				if err := os.Remove(filepath.Join(directories[2], recoveryApprovalName)); err != nil {
					t.Fatal(err)
				}
			}
			claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
			if claim != nil {
				_ = claim.Close()
			}
			if err == nil || claim != nil {
				t.Fatal("changed authority accepted:", err)
			}
			for _, directory := range directories {
				var storage LinuxRecoveryJournal
				if err := storage.open(directory); err != nil {
					t.Fatal("failed claim retained a partial lease:", err)
				}
				if err := storage.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func recoveryClaimFixture(t *testing.T) ([3]string, [sha256.Size]byte) {
	t.Helper()
	directories := [3]string{testRecoveryDirectory(t), testRecoveryDirectory(t), testRecoveryDirectory(t)}
	authority := testRecoveryAuthority(t, Read|Write)
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
	return directories, binding
}
