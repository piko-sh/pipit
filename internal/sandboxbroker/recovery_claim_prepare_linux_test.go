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
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryClaimPreparesCleanupOnly(t *testing.T) {
	t.Parallel()
	claim, grants, _ := prepareRecoveryClaimFixture(t, true)
	bootstrap, err := claim.PrepareRecovery(grants)
	require.NoError(t, err, "recovery preparation failed")
	require.NotNil(t, bootstrap)
	defer bootstrap.Close()
	require.True(t, bootstrap.RecoveryOnly(), "claim restored normal execution")
	renewed, err := bootstrap.RecoveryAuthority()
	if renewed != nil {
		_ = renewed.Close()
	}
	require.ErrorIs(t, err, ErrInvalidPolicy, "a claim does not renew operation admission")
	require.Nil(t, renewed)
	files, err := bootstrap.Files()
	require.NoError(t, err)
	policy, err := readLinuxBootstrap(files[0])
	require.NoError(t, err)
	records, err := decodeBootstrapRecovery(policy)
	require.NoError(t, err, "original journal intent was lost")
	require.Len(t, records, 1)
	require.Equal(t, uint64(1), records[0].Operation)
	require.Len(t, policy.Roots, 1, "cleanup authority widened")
	require.Equal(t, Write, policy.Roots[0].Rights)
	require.NoError(t, claim.Close())
	_, err = linuxBootstrapRootIdentity(files[1])
	require.NoError(t, err, "bootstrap did not own its root handle")
	bootstrap, err = claim.PrepareRecovery(grants)
	require.ErrorIs(t, err, ErrClosed, "closed claim prepared new authority")
	require.Nil(t, bootstrap)
}

func TestRecoveryClaimEmptyJournalStillValidatesRoots(t *testing.T) {
	t.Parallel()
	claim, grants, _ := prepareRecoveryClaimFixture(t, false)
	bootstrap, err := claim.PrepareRecovery(grants)
	require.NoError(t, err, "validated empty journal did not report no work")
	require.Nil(t, bootstrap)
	changed := append([]LinuxRootGrant(nil), grants...)
	changed[0].Path = testRecoveryDirectory(t)
	bootstrap, err = claim.PrepareRecovery(changed)
	require.ErrorIs(t, err, ErrDenied, "empty journal bypassed original root identity")
	require.Nil(t, bootstrap)
	changed = append([]LinuxRootGrant(nil), grants...)
	changed[0].Rights = Read
	bootstrap, err = claim.PrepareRecovery(changed)
	require.ErrorIs(t, err, ErrDenied, "empty journal bypassed original rights")
	require.Nil(t, bootstrap)
}

func TestRecoveryClaimRejectsMetadataMovedIntoGrant(t *testing.T) {
	t.Parallel()
	for _, index := range []int{1, 2} {
		for _, withRecord := range []bool{false, true} {
			claim, grants, directories := prepareRecoveryClaimFixture(t, withRecord)
			require.NoError(t, os.Rename(directories[index], filepath.Join(grants[0].Path, "moved-metadata")))
			prepared, err := claim.PrepareRecovery(grants)
			if prepared != nil {
				_ = prepared.Close()
			}
			require.ErrorIs(t, err, ErrDenied, "a restored grant holding recovery metadata is refused (%d, %v)", index, withRecord)
			require.Nil(t, prepared)
		}
	}
}

func TestRecoveryClaimRejectsGrantMovedIntoMetadata(t *testing.T) {
	t.Parallel()
	for _, index := range []int{1, 2} {
		claim, grants, directories := prepareRecoveryClaimFixture(t, true)
		moved := filepath.Join(directories[index], "moved-root")
		require.NoError(t, os.Rename(grants[0].Path, moved))
		grants[0].Path = moved
		prepared, err := claim.PrepareRecovery(grants)
		if prepared != nil {
			_ = prepared.Close()
		}
		require.ErrorIs(t, err, ErrDenied, "a restored grant inside recovery metadata is refused (%d)", index)
		require.Nil(t, prepared)
	}
}

func prepareRecoveryClaimFixture(t *testing.T, withRecord bool) (*LinuxRecoveryClaim, []LinuxRootGrant, [3]string) {
	t.Helper()
	grants := []LinuxRootGrant{{Name: "data", Path: testRecoveryDirectory(t), Rights: Read | Write | List}}
	bootstrap, err := PrepareLinuxBootstrap(grants, FilesystemLimits{})
	require.NoError(t, err)
	defer bootstrap.Close()
	authority, err := bootstrap.RecoveryAuthority()
	require.NoError(t, err)
	defer authority.Close()
	directories := [3]string{testRecoveryDirectory(t), testRecoveryDirectory(t), testRecoveryDirectory(t)}
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
	if withRecord {
		record := testRecoveryAuthorityRecord(t, authority, 1)
		call, err := authority.admit(brokerMessage(1, recoveryWritePayload))
		require.NoError(t, err)
		require.NoError(t, errors.Join(authority.record(journal, call, record), call.Finish(0)))
	}
	require.NoError(t, errors.Join(authority.Close(), journal.Close(), checkpoint.Close(), approval.Close()))
	claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	require.NoError(t, err)
	t.Cleanup(func() { _ = claim.Close() })
	return claim, grants, directories
}
