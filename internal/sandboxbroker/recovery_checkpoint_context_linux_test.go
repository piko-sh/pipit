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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryCheckpointContextApproval(t *testing.T) {
	t.Parallel()
	owner := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, owner)
	binding := [sha256.Size]byte{1}
	original, err := owner.checkpoint(journal, binding)
	require.NoError(t, err)
	metadata := []byte("original host and service")
	encoded, err := attachRecoveryContext(original, metadata, binding)
	require.NoError(t, err)
	approved := sha256.Sum256(encoded)
	captured, err := RecoveryCheckpointContext(encoded, approved, binding)
	require.NoError(t, err, "context round trip failed")
	require.True(t, bytes.Equal(captured, metadata))
	captured[0] ^= 1
	again, err := RecoveryCheckpointContext(encoded, approved, binding)
	require.NoError(t, err, "returned context aliases checkpoint")
	require.True(t, bytes.Equal(again, metadata))
	data, err := RecoveryCheckpointContext(encoded, sha256.Sum256(original), binding)
	require.ErrorIs(t, err, ErrDenied, "original context-free approval accepted new authority")
	require.Nil(t, data)
	data, err = RecoveryCheckpointContext(encoded, approved, [sha256.Size]byte{2})
	require.ErrorIs(t, err, ErrDenied, "different host binding exposed recovery metadata")
	require.Nil(t, data)
	data, err = attachRecoveryContext(original, make([]byte, maximumRecoveryContextBytes+1), binding)
	require.ErrorIs(t, err, errLimit, "oversized metadata accepted")
	require.Nil(t, data)
	legacy := bytes.ReplaceAll(original, []byte(recoveryCheckpointProfile), []byte("filesystem-recovery-checkpoint-v2"))
	require.Error(t, ValidateRecoveryCheckpoint(legacy, sha256.Sum256(legacy), binding), "legacy context-free profile accepted")
}
