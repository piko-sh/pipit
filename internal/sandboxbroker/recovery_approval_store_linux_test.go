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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestRecoveryApprovalStoreRoundTrip(t *testing.T) {
	t.Parallel()
	directory := testRecoveryDirectory(t)
	store, err := OpenLinuxRecoveryApprovalStore(directory)
	require.NoError(t, err)
	defer store.Close()
	approved, binding := [sha256.Size]byte{1}, [sha256.Size]byte{2}
	missing, err := store.Load(binding)
	require.Error(t, err, "a missing approval is refused")
	require.Zero(t, missing)
	require.NoError(t, store.Store(approved, binding))
	require.ErrorIs(t, store.Store(approved, binding), ErrDenied, "approval overwritten")
	require.NoError(t, store.Close())
	reopened, err := OpenLinuxRecoveryApprovalStore(directory)
	require.NoError(t, err)
	defer reopened.Close()
	digest, err := reopened.Load(binding)
	require.NoError(t, err, "approval not durable")
	require.Equal(t, approved, digest)
	digest, err = reopened.Load([sha256.Size]byte{3})
	require.Error(t, err, "foreign policy accepted")
	require.Zero(t, digest)
	require.NoError(t, os.Remove(filepath.Join(directory, recoveryApprovalName)))
	digest, err = reopened.Load(binding)
	require.Error(t, err, "missing approval used cached state")
	require.Zero(t, digest)
}

func TestRecoveryApprovalStoreRejectsUnsafeMetadata(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"short", "oversized", "magic", "zero-digest", "zero-binding", "fifo", "symlink", "hardlink", "permissions", "extra"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			approved, binding := [sha256.Size]byte{1}, [sha256.Size]byte{2}
			data, err := encodeRecoveryApproval(approved, binding)
			require.NoError(t, err)
			directory := testRecoveryDirectory(t)
			name := filepath.Join(directory, recoveryApprovalName)
			switch mode {
			case "short":
				data = data[:len(data)-1]
			case "oversized":
				data = append(data, 0)
			case "magic":
				data[0] ^= 1
			case "zero-digest":
				clear(data[recoveryApprovalHeaderBytes : recoveryApprovalHeaderBytes+sha256.Size])
			case "zero-binding":
				clear(data[recoveryApprovalHeaderBytes+sha256.Size:])
			}
			switch mode {
			case "fifo":
				require.NoError(t, unix.Mkfifo(name, 0600))
			case "symlink":
				target := filepath.Join(t.TempDir(), "target")
				require.NoError(t, os.WriteFile(target, data, 0600))
				require.NoError(t, os.Symlink(target, name))
			default:
				require.NoError(t, os.WriteFile(name, data, 0600))
			}
			switch mode {
			case "hardlink":
				require.NoError(t, os.Link(name, filepath.Join(t.TempDir(), "alias")))
			case "permissions":
				require.NoError(t, os.Chmod(name, 0644))
			case "extra":
				require.NoError(t, os.WriteFile(filepath.Join(directory, "extra"), nil, 0600))
			}
			store, err := OpenLinuxRecoveryApprovalStore(directory)
			if store != nil {
				defer store.Close()
			}
			if mode == "extra" {
				require.Nil(t, store, "unexpected approval entry accepted")
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			digest, err := store.Load(binding)
			require.Error(t, err, "unsafe approval accepted")
			require.Zero(t, digest)
		})
	}
}

func TestPersistApprovedCheckpoint(t *testing.T) {
	t.Parallel()
	authority := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, authority)
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(testRecoveryDirectory(t))
	require.NoError(t, err)
	defer checkpoint.Close()
	approval, err := OpenLinuxRecoveryApprovalStore(testRecoveryDirectory(t))
	require.NoError(t, err)
	defer approval.Close()
	binding := [sha256.Size]byte{1}
	metadata := []byte("original service identity")
	expected, err := authority.PersistApprovedCheckpointWithContext(journal, checkpoint, approval, binding, metadata)
	require.NoError(t, err)
	captured, err := approval.Load(binding)
	require.NoError(t, err, "independent approval missing")
	require.Equal(t, expected, captured)
	data, err := checkpoint.Load(captured, binding)
	require.NoError(t, err)
	require.Equal(t, captured, sha256.Sum256(data), "approval selected different bytes")
	metadata[0] ^= 1
	stored, err := RecoveryCheckpointContext(data, captured, binding)
	require.NoError(t, err, "independent approval lost captured service metadata")
	require.Equal(t, "original service identity", string(stored))
	require.NoError(t, ValidateCheckpointJournal(data, captured, binding, journal))
}

func TestApprovalPublicationFailureClosesAdmission(t *testing.T) {
	t.Parallel()
	authority := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, authority)
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(testRecoveryDirectory(t))
	require.NoError(t, err)
	defer checkpoint.Close()
	approval, err := OpenLinuxRecoveryApprovalStore(testRecoveryDirectory(t))
	require.NoError(t, err)
	defer approval.Close()
	binding := [sha256.Size]byte{1}
	previous := [sha256.Size]byte{2}
	require.NoError(t, approval.Store(previous, binding))
	captured, err := authority.PersistApprovedCheckpoint(journal, checkpoint, approval, binding)
	require.ErrorIs(t, err, ErrDenied, "approval collision lost ownership")
	require.NotZero(t, captured)
	call, err := authority.admit(brokerMessage(1, recoveryWritePayload))
	require.Nil(t, call, "failed approval allowed execution")
	require.ErrorIs(t, err, ErrClosed)
	digest, err := approval.Load(binding)
	require.NoError(t, err, "old approval overwritten")
	require.Equal(t, previous, digest)
	_, err = checkpoint.Load(captured, binding)
	require.NoError(t, err, "uncertain captured bytes cannot be inspected")
	data, err := checkpoint.Load(previous, binding)
	require.Nil(t, data, "old approval authorised a new checkpoint")
	require.Error(t, err)
}

func TestApprovalPersistenceSerialisesAdmission(t *testing.T) {
	t.Parallel()
	authority := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, authority)
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(testRecoveryDirectory(t))
	require.NoError(t, err)
	defer checkpoint.Close()
	approval, err := OpenLinuxRecoveryApprovalStore(testRecoveryDirectory(t))
	require.NoError(t, err)
	defer approval.Close()
	approval.storage.mutex.Lock()
	locked := true
	defer func() {
		if locked {
			approval.storage.mutex.Unlock()
		}
	}()
	persisted := make(chan error, 1)
	go func() {
		_, err := authority.PersistApprovedCheckpoint(journal, checkpoint, approval, [sha256.Size]byte{1})
		persisted <- err
	}()
	deadline := time.Now().Add(time.Second)
	for authority.mutex.TryLock() {
		authority.mutex.Unlock()
		require.False(t, time.Now().After(deadline), "persistence acquires admission ownership")
		time.Sleep(time.Millisecond)
	}
	admitted := make(chan error, 1)
	go func() {
		call, err := authority.admit(brokerMessage(1, recoveryWritePayload))
		if call != nil {
			err = errors.Join(err, call.Finish(0))
		}
		admitted <- err
	}()
	select {
	case err := <-admitted:
		require.FailNow(t, "the operation waits for independent approval", "%v", err)
	case <-time.After(20 * time.Millisecond):
	}
	approval.storage.mutex.Unlock()
	locked = false
	require.NoError(t, <-persisted)
	require.NoError(t, <-admitted)
}

func FuzzRecoveryApprovalRecord(fuzzer *testing.F) {
	seed, err := encodeRecoveryApproval([sha256.Size]byte{1}, [sha256.Size]byte{2})
	require.NoError(fuzzer, err)
	fuzzer.Add(seed)
	fuzzer.Add([]byte{})
	fuzzer.Fuzz(func(t *testing.T, data []byte) {
		approved, err := decodeRecoveryApproval(data, [sha256.Size]byte{2})
		if err != nil {
			require.Zero(t, approved, "a partial approval does not escape")
			return
		}
		encoded, err := encodeRecoveryApproval(approved, [sha256.Size]byte{2})
		require.NoError(t, err, "noncanonical approval accepted")
		require.True(t, bytes.Equal(data, encoded))
	})
}

func TestApprovalStorageSeparation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"grant", "journal", "checkpoint", "same-grant"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			root := testRecoveryDirectory(t)
			bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{{Name: "data", Path: root, Rights: Read | Write}}, FilesystemLimits{})
			require.NoError(t, err)
			defer bootstrap.Close()
			authority, err := bootstrap.RecoveryAuthority()
			require.NoError(t, err)
			defer authority.Close()
			journalDirectory := testRecoveryDirectory(t)
			journal, err := OpenLinuxRecoveryJournal(journalDirectory, authority.namespace)
			require.NoError(t, err)
			defer journal.Close()
			checkpointDirectory := testRecoveryDirectory(t)
			checkpoint, err := OpenLinuxRecoveryCheckpointStore(checkpointDirectory)
			require.NoError(t, err)
			defer checkpoint.Close()
			directory := filepath.Join(root, "approval")
			switch mode {
			case "journal":
				directory = filepath.Join(journalDirectory, "approval")
			case "checkpoint":
				directory = filepath.Join(checkpointDirectory, "approval")
			case "same-grant":
				directory = root
			}
			require.NoError(t, os.MkdirAll(directory, 0700))
			approval, err := OpenLinuxRecoveryApprovalStore(directory)
			require.NoError(t, err)
			defer approval.Close()
			captured, err := authority.PersistApprovedCheckpoint(journal, checkpoint, approval, [sha256.Size]byte{1})
			require.ErrorIs(t, err, ErrDenied, "overlapping approval store accepted")
			require.Zero(t, captured)
			_, err = os.Lstat(filepath.Join(checkpointDirectory, recoveryCheckpointName))
			require.True(t, os.IsNotExist(err), "checkpoint published before separation validation: %v", err)
			_, err = os.Lstat(filepath.Join(directory, recoveryApprovalName))
			require.True(t, os.IsNotExist(err), "approval published before separation validation: %v", err)
		})
	}
}
