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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRecoveryJournalPolicyPinnedOverlap(t *testing.T) {
	for _, mode := range []string{"same", "journal-child", "root-child", "siblings", "replaced-path"} {
		t.Run(mode, func(t *testing.T) {
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
				if err := os.MkdirAll(name, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(name, 0700); err != nil {
					t.Fatal(err)
				}
			}
			bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{{Name: "data", Path: root, Rights: Read}}, FilesystemLimits{})
			if err != nil {
				t.Fatal(err)
			}
			defer bootstrap.Close()
			authority, err := bootstrap.RecoveryAuthority()
			if err != nil {
				t.Fatal(err)
			}
			defer authority.Close()
			if mode == "replaced-path" {
				if err := os.Rename(root, filepath.Join(base, "original")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
				journalPath = filepath.Join(base, "original", "journal")
				if err := os.Mkdir(journalPath, 0700); err != nil {
					t.Fatal(err)
				}
			}
			journal, err := OpenLinuxRecoveryJournal(journalPath, authority.namespace)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			if mode == "root-child" {
				if err := os.Rename(root, filepath.Join(journalPath, "root")); err != nil {
					t.Fatal(err)
				}
			}
			err = authority.ValidateJournal(journal)
			if mode == "siblings" {
				if err != nil {
					t.Fatal("disjoint pinned directories rejected:", err)
				}
			} else if !errors.Is(err, ErrDenied) {
				t.Fatal("pinned overlap accepted:", err)
			}
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
			if err := authority.ValidateJournal(journal); !errors.Is(err, ErrClosed) {
				t.Fatal("closed journal accepted:", err)
			}
		})
	}
}

func TestRecoveryJournalPolicyRejectsInvalidOwners(t *testing.T) {
	authority := testRecoveryAuthority(t, Read)
	journal := testRecoveryAuthorityJournal(t, authority)
	var empty LinuxRecoveryAuthority
	for _, owner := range []*LinuxRecoveryAuthority{nil, &empty} {
		if err := owner.ValidateJournal(journal); !errors.Is(err, ErrClosed) {
			t.Fatal("empty authority accepted:", err)
		}
	}
	if err := authority.ValidateJournal(nil); !errors.Is(err, ErrClosed) {
		t.Fatal("nil journal accepted:", err)
	}
	record := testRecoveryAuthorityRecord(t, authority, 1)
	if err := journal.appendRecord(record); err != nil {
		t.Fatal(err)
	}
	if err := authority.ValidateJournal(journal); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal("populated journal accepted:", err)
	}
}

func TestRecoveryJournalAncestorBound(t *testing.T) {
	base := t.TempDir()
	deep := filepath.Join(base, strings.Repeat("level/", maximumJournalAncestorSteps))
	if err := os.MkdirAll(deep, 0700); err != nil {
		t.Fatal(err)
	}
	descriptor, err := unix.Open(deep, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	pinned := os.NewFile(uintptr(descriptor), "deep-directory")
	defer pinned.Close()
	if err := rejectJournalAncestor(pinned, filesystemBootstrapRoot{}); !errors.Is(err, errLimit) {
		t.Fatal("ancestry scan exceeded its bound:", err)
	}
}
