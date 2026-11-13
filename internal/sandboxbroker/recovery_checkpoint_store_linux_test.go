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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func checkpointStoreFixture(t *testing.T) ([]byte, [sha256.Size]byte, [sha256.Size]byte) {
	t.Helper()
	authority := testRecoveryAuthority(t, Read|Write)
	binding := [sha256.Size]byte{1}
	encoded, err := authority.checkpoint(testRecoveryAuthorityJournal(t, authority), binding)
	require.NoError(t, err)
	return encoded, sha256.Sum256(encoded), binding
}

func TestRecoveryCheckpointStoreRoundTrip(t *testing.T) {
	t.Parallel()
	encoded, approved, binding := checkpointStoreFixture(t)
	directory := testRecoveryDirectory(t)
	owner, err := OpenLinuxRecoveryCheckpointStore(directory)
	require.NoError(t, err)
	defer owner.Close()
	missing, err := owner.Load(approved, binding)
	require.Nil(t, missing, "a missing checkpoint is refused")
	require.Error(t, err)
	concurrent, err := OpenLinuxRecoveryCheckpointStore(directory)
	if concurrent != nil {
		_ = concurrent.Close()
	}
	require.Nil(t, concurrent, "a concurrent checkpoint owner is refused")
	require.Error(t, err)
	require.NoError(t, owner.store(encoded, approved, binding))
	require.ErrorIs(t, owner.store(encoded, approved, binding), ErrDenied, "checkpoint overwrite accepted")
	stored, err := owner.Load(approved, binding)
	require.NoError(t, err, "the stored checkpoint matches")
	require.True(t, bytes.Equal(stored, encoded))
	owner.storage.failed = true
	require.ErrorIs(t, owner.store(encoded, approved, binding), ErrClosed, "failed storage refuses another publication")
	inspected, err := owner.Load(approved, binding)
	require.NoError(t, err, "an uncertain publication can be inspected")
	require.True(t, bytes.Equal(inspected, encoded))
	require.NoError(t, owner.Close())
	reopened, err := OpenLinuxRecoveryCheckpointStore(directory)
	require.NoError(t, err)
	defer reopened.Close()
	data, err := reopened.Load(approved, binding)
	require.NoError(t, err, "durable checkpoint mismatch")
	require.True(t, bytes.Equal(data, encoded))
	data[0] ^= 1
	data, err = reopened.Load(approved, binding)
	require.NoError(t, err, "returned bytes changed durable state")
	require.True(t, bytes.Equal(data, encoded))
	data, err = reopened.Load(approved, [sha256.Size]byte{2})
	require.Nil(t, data, "foreign binding accepted")
	require.ErrorIs(t, err, ErrDenied)
	require.NoError(t, os.Remove(filepath.Join(directory, recoveryCheckpointName)))
	data, err = reopened.Load(approved, binding)
	require.Nil(t, data, "missing durable checkpoint used cached state")
	require.Error(t, err)
	require.ErrorIs(t, owner.store(encoded, approved, binding), ErrClosed, "closed store accepted write")
}

func TestRecoveryCheckpointStoreRejectsUnsafeEntries(t *testing.T) {
	t.Parallel()
	encoded, approved, binding := checkpointStoreFixture(t)
	for _, mode := range []string{"fifo", "symlink", "hardlink", "permissions", "oversized", "corrupt", "unknown-entry", "extra-entry"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			directory := testRecoveryDirectory(t)
			path := filepath.Join(directory, recoveryCheckpointName)
			switch mode {
			case "fifo":
				require.NoError(t, unix.Mkfifo(path, 0600))
			case "symlink":
				target := filepath.Join(t.TempDir(), "target")
				require.NoError(t, os.WriteFile(target, encoded, 0600))
				require.NoError(t, os.Symlink(target, path))
			default:
				data := encoded
				if mode == "oversized" {
					data = bytes.Repeat([]byte("x"), maximumBootstrapBytes+1)
				}
				if mode == "corrupt" {
					data = []byte("{}")
				}
				require.NoError(t, os.WriteFile(path, data, 0600))
				switch mode {
				case "hardlink":
					require.NoError(t, os.Link(path, filepath.Join(t.TempDir(), "alias")))
				case "permissions":
					require.NoError(t, os.Chmod(path, 0644))
				case "unknown-entry":
					require.NoError(t, os.Rename(path, filepath.Join(directory, "foreign")))
				case "extra-entry":
					require.NoError(t, os.WriteFile(filepath.Join(directory, "extra"), nil, 0600))
				}
			}
			owner, err := OpenLinuxRecoveryCheckpointStore(directory)
			if owner != nil {
				defer owner.Close()
			}
			if mode == "unknown-entry" || mode == "extra-entry" {
				require.Nil(t, owner, "unexpected directory contents accepted")
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			data, err := owner.Load(approved, binding)
			require.Nil(t, data, "unsafe checkpoint accepted")
			require.Error(t, err)
		})
	}
}

func TestRecoveryCheckpointStorePinnedDirectory(t *testing.T) {
	t.Parallel()
	encoded, approved, binding := checkpointStoreFixture(t)
	directory := testRecoveryDirectory(t)
	owner, err := OpenLinuxRecoveryCheckpointStore(directory)
	require.NoError(t, err)
	defer owner.Close()
	moved := filepath.Join(t.TempDir(), "original")
	require.NoError(t, os.Rename(directory, moved))
	require.NoError(t, os.Mkdir(directory, 0700))
	require.NoError(t, owner.store(encoded, approved, binding))
	data, err := owner.Load(approved, binding)
	require.NoError(t, err, "checkpoint lost original directory")
	require.True(t, bytes.Equal(data, encoded))
	entries, err := os.ReadDir(directory)
	require.NoError(t, err, "checkpoint followed replacement directory")
	require.Empty(t, entries)
}

func TestRecoveryCheckpointStoreInvalidApproval(t *testing.T) {
	t.Parallel()
	encoded, approved, binding := checkpointStoreFixture(t)
	directory := testRecoveryDirectory(t)
	owner, err := OpenLinuxRecoveryCheckpointStore(directory)
	require.NoError(t, err)
	defer owner.Close()
	require.Error(t, owner.store(encoded, [sha256.Size]byte{}, binding), "missing independent approval accepted")
	require.Error(t, owner.store(encoded, approved, [sha256.Size]byte{}), "empty binding accepted")
	entries, err := os.ReadDir(directory)
	require.NoError(t, err, "invalid approval modified storage")
	require.Empty(t, entries)
	require.NoError(t, owner.store(encoded, approved, binding), "rejected approval poisoned empty store")
	var absent *LinuxRecoveryCheckpointStore
	require.NoError(t, absent.Close(), "a nil cleanup succeeds")
	require.ErrorIs(t, absent.store(encoded, approved, binding), ErrClosed)
	data, err := absent.Load(approved, binding)
	require.Nil(t, data)
	require.ErrorIs(t, err, ErrClosed)
}
