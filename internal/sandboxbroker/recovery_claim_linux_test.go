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
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoveryClaimExclusiveOwnership(t *testing.T) {
	t.Parallel()
	directories, binding := recoveryClaimFixture(t)
	claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	require.NoError(t, err)
	defer claim.Close()
	encoded, approved, err := claim.Checkpoint()
	require.NoError(t, err)
	require.NoError(t, ValidateRecoveryCheckpoint(encoded, approved, binding))
	encoded[0] ^= 1
	fresh, captured, err := claim.Checkpoint()
	require.NoError(t, err, "checkpoint aliases caller bytes")
	require.Equal(t, approved, captured)
	require.False(t, bytes.Equal(fresh, encoded))
	var contenders sync.WaitGroup
	for range 8 {
		contenders.Go(func() {
			competing, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
			if competing != nil {
				_ = competing.Close()
			}
			assert.Error(t, err, "simultaneous recovery claim accepted")
		})
	}
	contenders.Wait()
	for _, directory := range directories {
		var storage LinuxRecoveryJournal
		err := storage.open(directory)
		require.NoError(t, storage.Close())
		require.Error(t, err, "the claim keeps every store lease")
	}
	require.NoError(t, claim.Close())
	encoded, approved, err = claim.Checkpoint()
	require.ErrorIs(t, err, ErrClosed, "released claim exposed authority")
	require.Nil(t, encoded)
	require.Zero(t, approved)
	replacement, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	require.NoError(t, err, "released claim kept leases")
	require.NoError(t, replacement.Close())
}

func TestRecoveryClaimRejectsChangedStorage(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"binding", "checkpoint", "approval", "journal", "missing-checkpoint", "missing-approval"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			directories, binding := recoveryClaimFixture(t)
			switch mode {
			case "binding":
				binding[1] = 1
			case "checkpoint":
				require.NoError(t, os.WriteFile(filepath.Join(directories[1], recoveryCheckpointName), []byte("{}"), 0600))
			case "approval":
				require.NoError(t, os.WriteFile(filepath.Join(directories[2], recoveryApprovalName), []byte("invalid"), 0600))
			case "journal":
				directories[0] = testRecoveryDirectory(t)
			case "missing-checkpoint":
				require.NoError(t, os.Remove(filepath.Join(directories[1], recoveryCheckpointName)))
			case "missing-approval":
				require.NoError(t, os.Remove(filepath.Join(directories[2], recoveryApprovalName)))
			}
			claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
			if claim != nil {
				_ = claim.Close()
			}
			require.Error(t, err, "changed authority accepted")
			require.Nil(t, claim)
			for _, directory := range directories {
				var storage LinuxRecoveryJournal
				require.NoError(t, storage.open(directory), "a failed claim keeps no partial lease")
				require.NoError(t, storage.Close())
			}
		})
	}
}

func recoveryClaimFixture(t *testing.T) ([3]string, [sha256.Size]byte) {
	t.Helper()
	directories := [3]string{testRecoveryDirectory(t), testRecoveryDirectory(t), testRecoveryDirectory(t)}
	authority := testRecoveryAuthority(t, Read|Write)
	journal, err := OpenLinuxRecoveryJournal(directories[0], authority.namespace)
	require.NoError(t, err)
	defer journal.Close()
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(directories[1])
	require.NoError(t, err)
	defer checkpoint.Close()
	approval, err := OpenLinuxRecoveryApprovalStore(directories[2])
	require.NoError(t, err)
	defer approval.Close()
	binding := [sha256.Size]byte{1}
	_, err = authority.PersistApprovedCheckpoint(journal, checkpoint, approval, binding)
	require.NoError(t, err)
	return directories, binding
}
