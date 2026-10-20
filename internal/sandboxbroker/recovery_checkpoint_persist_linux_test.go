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
	"time"

	"github.com/stretchr/testify/require"
)

func TestPersistCheckpointSeparatesStorage(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"disjoint", "store-in-root", "root-in-store", "same-root", "store-in-journal", "journal-in-store"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			base := t.TempDir()
			root, directory, checkpoint := filepath.Join(base, "root"), filepath.Join(base, "journal"), filepath.Join(base, "checkpoint")
			switch mode {
			case "store-in-root":
				checkpoint = filepath.Join(root, "checkpoint")
			case "root-in-store":
				root = filepath.Join(checkpoint, "root")
			case "same-root":
				root = checkpoint
			case "store-in-journal":
				checkpoint = filepath.Join(directory, "checkpoint")
			case "journal-in-store":
				directory = filepath.Join(checkpoint, "journal")
			}
			var store *LinuxRecoveryCheckpointStore
			openStore := func() {
				require.NoError(t, os.MkdirAll(checkpoint, 0700))
				var err error
				store, err = OpenLinuxRecoveryCheckpointStore(checkpoint)
				require.NoError(t, err)
				t.Cleanup(func() { _ = store.Close() })
			}
			if mode != "store-in-journal" {
				openStore()
			}
			require.NoError(t, os.MkdirAll(root, 0700))
			bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{{Name: "data", Path: root, Rights: Read | Write}}, FilesystemLimits{})
			require.NoError(t, err)
			defer bootstrap.Close()
			authority, err := bootstrap.RecoveryAuthority()
			require.NoError(t, err)
			defer authority.Close()
			require.NoError(t, os.MkdirAll(directory, 0700))
			journal, err := OpenLinuxRecoveryJournal(directory, authority.namespace)
			require.NoError(t, err)
			defer journal.Close()
			if mode == "store-in-journal" {
				openStore()
			}
			binding := [sha256.Size]byte{1}
			approved, err := authority.persistCheckpoint(journal, store, binding)
			if mode == "disjoint" {
				require.NoError(t, err, "disjoint persistence failed")
				require.NotZero(t, approved)
				data, err := store.Load(approved, binding)
				require.NoError(t, err)
				require.NoError(t, ValidateCheckpointJournal(data, approved, binding, journal))
			} else {
				require.Error(t, err, "overlapping checkpoint storage accepted")
				require.Zero(t, approved)
				_, err := os.Lstat(filepath.Join(checkpoint, recoveryCheckpointName))
				require.True(t, os.IsNotExist(err), "rejected persistence published metadata: %v", err)
			}
		})
	}
}

func TestPersistCheckpointSerialisesAdmission(t *testing.T) {
	t.Parallel()
	authority := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, authority)
	store, err := OpenLinuxRecoveryCheckpointStore(testRecoveryDirectory(t))
	require.NoError(t, err)
	defer store.Close()
	store.storage.mutex.Lock()
	locked := true
	defer func() {
		if locked {
			store.storage.mutex.Unlock()
		}
	}()
	persisted := make(chan error, 1)
	go func() {
		_, err := authority.persistCheckpoint(journal, store, [sha256.Size]byte{1})
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
		require.FailNow(t, "the operation waits for a durable checkpoint", "%v", err)
	case <-time.After(20 * time.Millisecond):
	}
	store.storage.mutex.Unlock()
	locked = false
	require.NoError(t, <-persisted)
	require.NoError(t, <-admitted)
	_, err = authority.persistCheckpoint(journal, store, [sha256.Size]byte{1})
	require.Error(t, err, "checkpoint persisted after admission")
}

func TestPersistCheckpointPublicationFailureClosesAdmission(t *testing.T) {
	t.Parallel()
	authority := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, authority)
	store, err := OpenLinuxRecoveryCheckpointStore(testRecoveryDirectory(t))
	require.NoError(t, err)
	defer store.Close()
	binding := [sha256.Size]byte{1}
	expected, err := authority.persistCheckpoint(journal, store, binding)
	require.NoError(t, err)
	approved, err := authority.persistCheckpoint(journal, store, binding)
	require.ErrorIs(t, err, ErrDenied, "collision lost captured approval")
	require.Equal(t, expected, approved)
	call, err := authority.admit(brokerMessage(1, recoveryWritePayload))
	require.Nil(t, call, "failed persistence left admission open")
	require.ErrorIs(t, err, ErrClosed)
	_, err = store.Load(approved, binding)
	require.NoError(t, err, "failed publication prevented inspection")
}

func TestPersistCheckpointMissingAndAliasedOwners(t *testing.T) {
	t.Parallel()
	authority := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, authority)
	binding := [sha256.Size]byte{1}
	store, err := OpenLinuxRecoveryCheckpointStore(testRecoveryDirectory(t))
	require.NoError(t, err)
	defer store.Close()
	var absent *LinuxRecoveryAuthority
	approved, err := absent.persistCheckpoint(journal, store, binding)
	require.ErrorIs(t, err, ErrClosed, "nil authority produced approval")
	require.Zero(t, approved)
	approved, err = authority.persistCheckpoint(nil, store, binding)
	require.ErrorIs(t, err, ErrClosed, "nil journal produced approval")
	require.Zero(t, approved)
	approved, err = authority.persistCheckpoint(journal, nil, binding)
	require.ErrorIs(t, err, ErrClosed, "nil checkpoint store produced approval")
	require.Zero(t, approved)
	alias := &LinuxRecoveryCheckpointStore{storage: journal}
	approved, err = authority.persistCheckpoint(journal, alias, binding)
	require.ErrorIs(t, err, ErrDenied, "same storage owner accepted")
	require.Zero(t, approved)
	require.NoError(t, store.Close())
	approved, err = authority.persistCheckpoint(journal, store, binding)
	require.ErrorIs(t, err, ErrClosed, "closed checkpoint store produced approval")
	require.Zero(t, approved)
}
