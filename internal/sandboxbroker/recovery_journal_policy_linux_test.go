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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestRecoveryJournalPolicyPinnedOverlap(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"same", "journal-child", "root-child", "siblings", "replaced-path"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			base := t.TempDir()
			root, journalPath := base, base
			switch mode {
			case "journal-child":
				journalPath = filepath.Join(base, "journal")
			case "siblings", "replaced-path", "root-child":
				root = filepath.Join(base, "root")
				journalPath = filepath.Join(base, "journal")
			}
			for _, name := range []string{root, journalPath} {
				require.NoError(t, os.MkdirAll(name, 0700))
				require.NoError(t, os.Chmod(name, 0700))
			}
			bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{{Name: "data", Path: root, Rights: Read}}, FilesystemLimits{})
			require.NoError(t, err)
			defer bootstrap.Close()
			authority, err := bootstrap.RecoveryAuthority()
			require.NoError(t, err)
			defer authority.Close()
			if mode == "replaced-path" {
				require.NoError(t, os.Rename(root, filepath.Join(base, "original")))
				require.NoError(t, os.Mkdir(root, 0700))
				journalPath = filepath.Join(base, "original", "journal")
				require.NoError(t, os.Mkdir(journalPath, 0700))
			}
			journal, err := OpenLinuxRecoveryJournal(journalPath, authority.namespace)
			require.NoError(t, err)
			defer journal.Close()
			if mode == "root-child" {
				require.NoError(t, os.Rename(root, filepath.Join(journalPath, "root")))
			}
			err = authority.ValidateJournal(journal)
			if mode == "siblings" {
				require.NoError(t, err, "disjoint pinned directories rejected")
			} else {
				require.ErrorIs(t, err, ErrDenied, "a pinned overlap is refused")
			}
			require.NoError(t, journal.Close())
			require.ErrorIs(t, authority.ValidateJournal(journal), ErrClosed, "closed journal accepted")
		})
	}
}

func TestRecoveryJournalPolicyRejectsInvalidOwners(t *testing.T) {
	t.Parallel()
	authority := testRecoveryAuthority(t, Read)
	journal := testRecoveryAuthorityJournal(t, authority)
	var empty LinuxRecoveryAuthority
	for _, owner := range []*LinuxRecoveryAuthority{nil, &empty} {
		require.ErrorIs(t, owner.ValidateJournal(journal), ErrClosed, "empty authority accepted")
	}
	require.ErrorIs(t, authority.ValidateJournal(nil), ErrClosed, "nil journal accepted")
	record := testRecoveryAuthorityRecord(t, authority, 1)
	require.NoError(t, journal.appendRecord(record))
	require.ErrorIs(t, authority.ValidateJournal(journal), ErrInvalidPolicy, "populated journal accepted")
}

func TestRecoveryJournalAncestorBound(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	deep := filepath.Join(base, strings.Repeat("level/", maximumJournalAncestorSteps))
	require.NoError(t, os.MkdirAll(deep, 0700))
	descriptor, err := unix.Open(deep, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	require.NoError(t, err)
	pinned := os.NewFile(uintptr(descriptor), "deep-directory")
	defer pinned.Close()
	err = rejectJournalAncestor(pinned, filesystemBootstrapRoot{})
	require.ErrorIs(t, err, errLimit, "ancestry scan exceeded its bound")
}
