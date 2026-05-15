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
	"testing"
)

func TestRecoveryCheckpointContextApproval(t *testing.T) {
	owner := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, owner)
	binding := [sha256.Size]byte{1}
	original, err := owner.checkpoint(journal, binding)
	if err != nil {
		t.Fatal(err)
	}
	metadata := []byte("original host and service")
	encoded, err := attachRecoveryContext(original, metadata, binding)
	if err != nil {
		t.Fatal(err)
	}
	approved := sha256.Sum256(encoded)
	captured, err := RecoveryCheckpointContext(encoded, approved, binding)
	if err != nil || !bytes.Equal(captured, metadata) {
		t.Fatal("context round trip failed:", err)
	}
	captured[0] ^= 1
	again, err := RecoveryCheckpointContext(encoded, approved, binding)
	if err != nil || !bytes.Equal(again, metadata) {
		t.Fatal("returned context aliases checkpoint:", err)
	}
	if data, err := RecoveryCheckpointContext(encoded, sha256.Sum256(original), binding); !errors.Is(err, ErrDenied) || data != nil {
		t.Fatal("original context-free approval accepted new authority:", err)
	}
	if data, err := RecoveryCheckpointContext(encoded, approved, [sha256.Size]byte{2}); !errors.Is(err, ErrDenied) || data != nil {
		t.Fatal("different host binding exposed recovery metadata:", err)
	}
	if data, err := attachRecoveryContext(original, make([]byte, maximumRecoveryContextBytes+1), binding); !errors.Is(err, errLimit) || data != nil {
		t.Fatal("oversized metadata accepted:", err)
	}
	legacy := bytes.ReplaceAll(original, []byte(recoveryCheckpointProfile), []byte("filesystem-recovery-checkpoint-v2"))
	if err := ValidateRecoveryCheckpoint(legacy, sha256.Sum256(legacy), binding); err == nil {
		t.Fatal("legacy context-free profile accepted")
	}
}
