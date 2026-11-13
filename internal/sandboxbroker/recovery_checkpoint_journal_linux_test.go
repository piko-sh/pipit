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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryCheckpointPinsOriginalJournal(t *testing.T) {
	t.Parallel()
	authority := testRecoveryAuthority(t, Read|Write)
	directory := testRecoveryDirectory(t)
	journal, err := OpenLinuxRecoveryJournal(directory, authority.namespace)
	require.NoError(t, err)
	defer journal.Close()
	binding := [sha256.Size]byte{1}
	encoded, err := authority.checkpoint(journal, binding)
	require.NoError(t, err)
	approved := sha256.Sum256(encoded)
	require.NoError(t, ValidateCheckpointJournal(encoded, approved, binding, journal))
	moved := filepath.Join(t.TempDir(), "original")
	require.NoError(t, os.Rename(directory, moved))
	require.NoError(t, os.Mkdir(directory, 0700))
	replacement, err := OpenLinuxRecoveryJournal(directory, authority.namespace)
	require.NoError(t, err)
	defer replacement.Close()
	require.ErrorIs(t, ValidateCheckpointJournal(encoded, approved, binding, replacement), ErrDenied, "replacement journal accepted original checkpoint")
	require.NoError(t, ValidateCheckpointJournal(encoded, approved, binding, journal), "the pinned journal is kept")
	require.NoError(t, journal.Close())
	reopened, err := OpenLinuxRecoveryJournal(moved, authority.namespace)
	require.NoError(t, err)
	defer reopened.Close()
	require.NoError(t, ValidateCheckpointJournal(encoded, approved, binding, reopened), "reopened original journal rejected")
	require.Error(t, ValidateCheckpointJournal(encoded, approved, [sha256.Size]byte{2}, reopened), "foreign binding accepted")
	require.Error(t, ValidateCheckpointJournal(encoded, [sha256.Size]byte{}, binding, reopened), "unapproved checkpoint accepted")
	require.ErrorIs(t, ValidateCheckpointJournal(encoded, approved, binding, nil), ErrClosed)
	require.ErrorIs(t, ValidateCheckpointJournal(encoded, approved, binding, journal), ErrClosed, "closed journal accepted")
}

func TestRecoveryCheckpointRejectsIneligibleJournal(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"nil", "closed", "failed", "namespace", "nonempty", "overlap", "unsafe-mode"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			root := testRecoveryDirectory(t)
			bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{{Name: "data", Path: root, Rights: Read | Write}}, FilesystemLimits{})
			require.NoError(t, err)
			defer bootstrap.Close()
			authority, err := bootstrap.RecoveryAuthority()
			require.NoError(t, err)
			defer authority.Close()
			directory := testRecoveryDirectory(t)
			if mode == "overlap" {
				directory = root
			}
			journal, err := OpenLinuxRecoveryJournal(directory, authority.namespace)
			require.NoError(t, err)
			defer journal.Close()
			switch mode {
			case "nil":
				journal = nil
			case "closed":
				require.NoError(t, journal.Close())
			case "failed":
				journal.failed = true
			case "namespace":
				journal.namespace = "ffffffffffffffffffffffffffffffff"
			case "nonempty":
				require.NoError(t, journal.appendRecord(testRecoveryAuthorityRecord(t, authority, 1)))
			case "unsafe-mode":
				require.NoError(t, os.Chmod(directory, 0755))
			}
			data, err := authority.checkpoint(journal, [sha256.Size]byte{1})
			require.Nil(t, data, "ineligible journal captured")
			require.Error(t, err)
		})
	}
}
