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

func TestServiceCheckpointRoundTrip(t *testing.T) {
	checkpointDirectory, approvalDirectory := testRecoveryDirectory(t), testRecoveryDirectory(t)
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(checkpointDirectory)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := OpenLinuxRecoveryApprovalStore(approvalDirectory)
	if err != nil {
		t.Fatal(err)
	}
	binding := [sha256.Size]byte{7}
	metadata := []byte(`{"service":"context"}`)
	approved, err := PersistServiceCheckpoint(checkpoint, approval, binding, metadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PersistServiceCheckpoint(checkpoint, approval, binding, metadata); !errors.Is(err, ErrDenied) {
		t.Fatal("second checkpoint accepted in a used store:", err)
	}
	if err := errors.Join(checkpoint.Close(), approval.Close()); err != nil {
		t.Fatal(err)
	}

	claim, err := OpenLinuxRecoveryClaim("", checkpointDirectory, approvalDirectory, binding)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	if !claim.Service() {
		t.Fatal("service checkpoint claimed as a filesystem one")
	}
	encoded, digest, err := claim.Checkpoint()
	if err != nil || digest != approved {
		t.Fatalf("claim digest mismatch: %v", err)
	}
	context, err := RecoveryCheckpointContext(encoded, digest, binding)
	if err != nil || !bytes.Equal(context, metadata) {
		t.Fatalf("service context lost: %v %q", err, context)
	}
	if ready, err := claim.ServiceReleaseReady(); err != nil || ready {
		t.Fatalf("fresh claim reports release ready: %v %v", ready, err)
	}
	if err := claim.RecordServiceReleaseReady(); err != nil {
		t.Fatal(err)
	}
	if ready, err := claim.ServiceReleaseReady(); err != nil || !ready {
		t.Fatalf("release marker not durable without a journal: %v %v", ready, err)
	}
	if err := claim.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenLinuxRecoveryClaim(testRecoveryDirectory(t), checkpointDirectory, approvalDirectory, binding); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal("service checkpoint claimed with a journal directory:", err)
	}
}

func TestServiceCheckpointRejectsBadInputs(t *testing.T) {
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(testRecoveryDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	defer checkpoint.Close()
	approval, err := OpenLinuxRecoveryApprovalStore(testRecoveryDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	defer approval.Close()
	if _, err := PersistServiceCheckpoint(checkpoint, approval, [sha256.Size]byte{}, nil); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal("empty binding accepted:", err)
	}
	if _, err := PersistServiceCheckpoint(nil, approval, [sha256.Size]byte{1}, nil); !errors.Is(err, ErrClosed) {
		t.Fatal("nil checkpoint store accepted:", err)
	}
	if _, err := PersistServiceCheckpoint(checkpoint, approval, [sha256.Size]byte{1}, make([]byte, maximumRecoveryContextBytes+1)); !errors.Is(err, errLimit) {
		t.Fatal("oversized context accepted:", err)
	}
}
