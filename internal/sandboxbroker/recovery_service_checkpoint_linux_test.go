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
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServiceCheckpointRoundTrip(t *testing.T) {
	t.Parallel()
	checkpointDirectory, approvalDirectory := testRecoveryDirectory(t), testRecoveryDirectory(t)
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(checkpointDirectory)
	require.NoError(t, err)
	approval, err := OpenLinuxRecoveryApprovalStore(approvalDirectory)
	require.NoError(t, err)
	binding := [sha256.Size]byte{7}
	metadata := []byte(`{"service":"context"}`)
	approved, err := PersistServiceCheckpoint(checkpoint, approval, binding, metadata)
	require.NoError(t, err)
	_, err = PersistServiceCheckpoint(checkpoint, approval, binding, metadata)
	require.ErrorIs(t, err, ErrDenied, "second checkpoint accepted in a used store")
	require.NoError(t, errors.Join(checkpoint.Close(), approval.Close()))

	claim, err := OpenLinuxRecoveryClaim("", checkpointDirectory, approvalDirectory, binding)
	require.NoError(t, err)
	defer claim.Close()
	require.True(t, claim.Service(), "service checkpoint claimed as a filesystem one")
	encoded, digest, err := claim.Checkpoint()
	require.NoError(t, err, "claim digest mismatch")
	require.Equal(t, approved, digest)
	context, err := RecoveryCheckpointContext(encoded, digest, binding)
	require.NoError(t, err, "service context lost")
	require.True(t, bytes.Equal(context, metadata))
	ready, err := claim.ServiceReleaseReady()
	require.NoError(t, err, "fresh claim reports release")
	require.False(t, ready)
	require.NoError(t, claim.RecordServiceReleaseReady())
	ready, err = claim.ServiceReleaseReady()
	require.NoError(t, err, "release marker not durable without a journal")
	require.True(t, ready)
	require.NoError(t, claim.Close())
	_, err = OpenLinuxRecoveryClaim(testRecoveryDirectory(t), checkpointDirectory, approvalDirectory, binding)
	require.ErrorIs(t, err, ErrInvalidPolicy, "service checkpoint claimed with a journal directory")
}

func TestServiceCheckpointRejectsBadInputs(t *testing.T) {
	t.Parallel()
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(testRecoveryDirectory(t))
	require.NoError(t, err)
	defer checkpoint.Close()
	approval, err := OpenLinuxRecoveryApprovalStore(testRecoveryDirectory(t))
	require.NoError(t, err)
	defer approval.Close()
	_, err = PersistServiceCheckpoint(checkpoint, approval, [sha256.Size]byte{}, nil)
	require.ErrorIs(t, err, ErrInvalidPolicy, "empty binding accepted")
	_, err = PersistServiceCheckpoint(nil, approval, [sha256.Size]byte{1}, nil)
	require.ErrorIs(t, err, ErrClosed, "nil checkpoint store accepted")
	_, err = PersistServiceCheckpoint(checkpoint, approval, [sha256.Size]byte{1}, make([]byte, maximumRecoveryContextBytes+1))
	require.ErrorIs(t, err, errLimit, "oversized context accepted")
}
