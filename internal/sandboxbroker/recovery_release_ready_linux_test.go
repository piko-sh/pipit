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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestServiceReleaseReadyPersistence(t *testing.T) {
	t.Parallel()
	directories, binding := recoveryClaimFixture(t)
	claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	require.NoError(t, err)
	defer claim.Close()
	ready, err := claim.ServiceReleaseReady()
	require.NoError(t, err)
	require.False(t, ready, "a fresh claim is not ready")
	require.NoError(t, claim.RecordServiceReleaseReady())
	path := filepath.Join(directories[2], serviceReleaseReadyName)
	before, err := os.Stat(path)
	require.NoError(t, err)
	for range 3 {
		require.NoError(t, claim.RecordServiceReleaseReady(), "publication is idempotent")
	}
	after, err := os.Stat(path)
	require.NoError(t, err, "receipt replaced on retry")
	require.True(t, os.SameFile(before, after))
	ready, err = claim.ServiceReleaseReady()
	require.NoError(t, err, "the published receipt is present")
	require.True(t, ready)
	bootstrap, err := claim.PrepareRecovery(nil)
	require.ErrorIs(t, err, ErrDenied, "completed cleanup admitted another helper")
	require.Nil(t, bootstrap)
	claim.approval.storage.failed = true
	require.NoError(t, claim.RecordServiceReleaseReady(), "uncertain publication could not be synchronised")
	require.NoError(t, claim.Close())
	_, err = claim.ServiceReleaseReady()
	require.ErrorIs(t, err, ErrClosed, "closed claim exposed readiness")
	require.ErrorIs(t, claim.RecordServiceReleaseReady(), ErrClosed, "closed claim published readiness")
	reopened, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	require.NoError(t, err)
	defer reopened.Close()
	ready, err = reopened.ServiceReleaseReady()
	require.NoError(t, err, "reopened claim lost readiness")
	require.True(t, ready)
}

func TestServiceReleaseReadyRejectsUnsafeRecords(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"truncated", "oversized", "magic", "digest", "binding", "mode", "symlink", "hardlink", "fifo", "directory", "unexpected", "missing-approval"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			directories, binding := recoveryClaimFixture(t)
			claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
			require.NoError(t, err)
			require.NoError(t, claim.RecordServiceReleaseReady())
			require.NoError(t, claim.Close())
			corruptReleaseReady(t, directories[2], mode)
			reopened, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
			if reopened != nil {
				_ = reopened.Close()
			}
			require.Error(t, err, "unsafe readiness accepted")
			require.Nil(t, reopened)
			for _, directory := range directories {
				var storage LinuxRecoveryJournal
				require.NoError(t, storage.open(directory), "a failed claim keeps no lease")
				require.NoError(t, storage.Close())
			}
		})
	}
}

func corruptReleaseReady(t *testing.T, directory, mode string) {
	t.Helper()
	path := filepath.Join(directory, serviceReleaseReadyName)
	encoded, err := os.ReadFile(path)
	require.NoError(t, err)
	switch mode {
	case "truncated":
		encoded = encoded[:len(encoded)-1]
	case "oversized":
		encoded = append(encoded, 0)
	case "magic":
		encoded[0] ^= 1
	case "digest":
		encoded[len(serviceReleaseReadyMagic)] ^= 1
	case "binding":
		encoded[len(encoded)-1] ^= 1
	case "mode":
		err = os.Chmod(path, 0644)
	case "symlink", "fifo", "directory":
		require.NoError(t, os.Remove(path))
		switch mode {
		case "symlink":
			err = os.Symlink(recoveryApprovalName, path)
		case "fifo":
			err = unix.Mkfifo(path, 0600)
		case "directory":
			err = os.Mkdir(path, 0700)
		}
	case "hardlink":
		err = os.Link(path, filepath.Join(t.TempDir(), "alias"))
	case "unexpected":
		err = os.WriteFile(filepath.Join(directory, "unexpected"), []byte("bad"), 0600)
	case "missing-approval":
		err = os.Remove(filepath.Join(directory, recoveryApprovalName))
	}
	require.NoError(t, err)
	if mode == "truncated" || mode == "oversized" || mode == "magic" || mode == "digest" || mode == "binding" {
		require.NoError(t, os.WriteFile(path, encoded, 0600))
	}
}

func TestServiceReleaseReadyRejectsChangedApproval(t *testing.T) {
	t.Parallel()
	directories, binding := recoveryClaimFixture(t)
	claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	require.NoError(t, err)
	defer claim.Close()
	changed := claim.digest
	changed[0] ^= 1
	encoded, err := encodeRecoveryApproval(changed, binding)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directories[2], recoveryApprovalName), encoded, 0600))
	require.Error(t, claim.RecordServiceReleaseReady(), "changed independent approval accepted")
	_, err = os.Stat(filepath.Join(directories[2], serviceReleaseReadyName))
	require.True(t, os.IsNotExist(err), "changed approval published a receipt: %v", err)
}

func TestServiceReleaseReadyRecordFraming(t *testing.T) {
	t.Parallel()
	directories, binding := recoveryClaimFixture(t)
	claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	require.NoError(t, err)
	defer claim.Close()
	encoded, err := encodeServiceReleaseReady(claim.digest, binding)
	require.NoError(t, err, "invalid fixed-size framing")
	require.Len(t, encoded, recoveryApprovalBytes)
	require.Len(t, serviceReleaseReadyMagic, recoveryApprovalHeaderBytes)
	approval, err := encodeRecoveryApproval(claim.digest, binding)
	require.NoError(t, err, "readiness aliases approval")
	require.False(t, bytes.Equal(encoded, approval))
	var absent *LinuxRecoveryClaim
	require.ErrorIs(t, absent.RecordServiceReleaseReady(), ErrClosed, "missing owner published receipt")
	_, err = absent.ServiceReleaseReady()
	require.ErrorIs(t, err, ErrClosed, "missing owner read receipt")
}
