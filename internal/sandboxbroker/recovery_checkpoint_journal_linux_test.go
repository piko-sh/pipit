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
)

func TestRecoveryCheckpointPinsOriginalJournal(t *testing.T) {
	authority := testRecoveryAuthority(t, Read|Write)
	directory := testRecoveryDirectory(t)
	journal, err := OpenLinuxRecoveryJournal(directory, authority.namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	binding := [sha256.Size]byte{1}
	encoded, err := authority.checkpoint(journal, binding)
	if err != nil {
		t.Fatal(err)
	}
	approved := sha256.Sum256(encoded)
	if err := ValidateCheckpointJournal(encoded, approved, binding, journal); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(t.TempDir(), "original")
	if err := os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	replacement, err := OpenLinuxRecoveryJournal(directory, authority.namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if err := ValidateCheckpointJournal(encoded, approved, binding, replacement); !errors.Is(err, ErrDenied) {
		t.Fatal("replacement journal accepted original checkpoint:", err)
	}
	if err := ValidateCheckpointJournal(encoded, approved, binding, journal); err != nil {
		t.Fatal("lost pinned journal:", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLinuxRecoveryJournal(moved, authority.namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := ValidateCheckpointJournal(encoded, approved, binding, reopened); err != nil {
		t.Fatal("reopened original journal rejected:", err)
	}
	if err := ValidateCheckpointJournal(encoded, approved, [sha256.Size]byte{2}, reopened); err == nil {
		t.Fatal("foreign binding accepted")
	}
	if err := ValidateCheckpointJournal(encoded, [sha256.Size]byte{}, binding, reopened); err == nil {
		t.Fatal("unapproved checkpoint accepted")
	}
	if err := ValidateCheckpointJournal(encoded, approved, binding, nil); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := ValidateCheckpointJournal(encoded, approved, binding, journal); !errors.Is(err, ErrClosed) {
		t.Fatal("closed journal accepted:", err)
	}
}

func TestRecoveryCheckpointRejectsIneligibleJournal(t *testing.T) {
	for _, mode := range []string{"nil", "closed", "failed", "namespace", "nonempty", "overlap", "unsafe-mode"} {
		t.Run(mode, func(t *testing.T) {
			root := testRecoveryDirectory(t)
			bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{{Name: "data", Path: root, Rights: Read | Write}}, FilesystemLimits{})
			if err != nil {
				t.Fatal(err)
			}
			defer bootstrap.Close()
			authority, err := bootstrap.RecoveryAuthority()
			if err != nil {
				t.Fatal(err)
			}
			defer authority.Close()
			directory := testRecoveryDirectory(t)
			if mode == "overlap" {
				directory = root
			}
			journal, err := OpenLinuxRecoveryJournal(directory, authority.namespace)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			switch mode {
			case "nil":
				journal = nil
			case "closed":
				if err := journal.Close(); err != nil {
					t.Fatal(err)
				}
			case "failed":
				journal.failed = true
			case "namespace":
				journal.namespace = "ffffffffffffffffffffffffffffffff"
			case "nonempty":
				if err := journal.appendRecord(testRecoveryAuthorityRecord(t, authority, 1)); err != nil {
					t.Fatal(err)
				}
			case "unsafe-mode":
				if err := os.Chmod(directory, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if data, err := authority.checkpoint(journal, [sha256.Size]byte{1}); data != nil || err == nil {
				t.Fatal("ineligible journal captured")
			}
		})
	}
}
