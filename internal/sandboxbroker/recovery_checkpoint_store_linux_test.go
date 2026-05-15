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
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func checkpointStoreFixture(t *testing.T) ([]byte, [sha256.Size]byte, [sha256.Size]byte) {
	t.Helper()
	authority := testRecoveryAuthority(t, Read|Write)
	binding := [sha256.Size]byte{1}
	encoded, err := authority.checkpoint(testRecoveryAuthorityJournal(t, authority), binding)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, sha256.Sum256(encoded), binding
}

func TestRecoveryCheckpointStoreRoundTrip(t *testing.T) {
	encoded, approved, binding := checkpointStoreFixture(t)
	directory := testRecoveryDirectory(t)
	owner, err := OpenLinuxRecoveryCheckpointStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if data, err := owner.Load(approved, binding); data != nil || err == nil {
		t.Fatal("missing checkpoint accepted")
	}
	if concurrent, err := OpenLinuxRecoveryCheckpointStore(directory); concurrent != nil || err == nil {
		if concurrent != nil {
			_ = concurrent.Close()
		}
		t.Fatal("concurrent checkpoint owner admitted")
	}
	if err := owner.store(encoded, approved, binding); err != nil {
		t.Fatal(err)
	}
	if err := owner.store(encoded, approved, binding); !errors.Is(err, ErrDenied) {
		t.Fatal("checkpoint overwrite accepted:", err)
	}
	if data, err := owner.Load(approved, binding); err != nil || !bytes.Equal(data, encoded) {
		t.Fatal("stored checkpoint mismatch:", err)
	}
	owner.storage.failed = true
	if err := owner.store(encoded, approved, binding); !errors.Is(err, ErrClosed) {
		t.Fatal("failed storage accepted another publication:", err)
	}
	if data, err := owner.Load(approved, binding); err != nil || !bytes.Equal(data, encoded) {
		t.Fatal("uncertain publication could not be inspected:", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLinuxRecoveryCheckpointStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	data, err := reopened.Load(approved, binding)
	if err != nil || !bytes.Equal(data, encoded) {
		t.Fatal("durable checkpoint mismatch:", err)
	}
	data[0] ^= 1
	if data, err := reopened.Load(approved, binding); err != nil || !bytes.Equal(data, encoded) {
		t.Fatal("returned bytes changed durable state:", err)
	}
	if data, err := reopened.Load(approved, [sha256.Size]byte{2}); data != nil || !errors.Is(err, ErrDenied) {
		t.Fatal("foreign binding accepted:", err)
	}
	if err := os.Remove(filepath.Join(directory, recoveryCheckpointName)); err != nil {
		t.Fatal(err)
	}
	if data, err := reopened.Load(approved, binding); data != nil || err == nil {
		t.Fatal("missing durable checkpoint used cached state")
	}
	if err := owner.store(encoded, approved, binding); !errors.Is(err, ErrClosed) {
		t.Fatal("closed store accepted write:", err)
	}
}

func TestRecoveryCheckpointStoreRejectsUnsafeEntries(t *testing.T) {
	encoded, approved, binding := checkpointStoreFixture(t)
	for _, mode := range []string{"fifo", "symlink", "hardlink", "permissions", "oversized", "corrupt", "unknown-entry", "extra-entry"} {
		t.Run(mode, func(t *testing.T) {
			directory := testRecoveryDirectory(t)
			path := filepath.Join(directory, recoveryCheckpointName)
			switch mode {
			case "fifo":
				if err := unix.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(t.TempDir(), "target")
				if err := os.WriteFile(target, encoded, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			default:
				data := encoded
				if mode == "oversized" {
					data = bytes.Repeat([]byte("x"), maximumBootstrapBytes+1)
				}
				if mode == "corrupt" {
					data = []byte("{}")
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "hardlink":
					if err := os.Link(path, filepath.Join(t.TempDir(), "alias")); err != nil {
						t.Fatal(err)
					}
				case "permissions":
					if err := os.Chmod(path, 0644); err != nil {
						t.Fatal(err)
					}
				case "unknown-entry":
					if err := os.Rename(path, filepath.Join(directory, "foreign")); err != nil {
						t.Fatal(err)
					}
				case "extra-entry":
					if err := os.WriteFile(filepath.Join(directory, "extra"), nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			owner, err := OpenLinuxRecoveryCheckpointStore(directory)
			if owner != nil {
				defer owner.Close()
			}
			if mode == "unknown-entry" || mode == "extra-entry" {
				if owner != nil || err == nil {
					t.Fatal("unexpected directory contents accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if data, err := owner.Load(approved, binding); data != nil || err == nil {
				t.Fatal("unsafe checkpoint accepted")
			}
		})
	}
}

func TestRecoveryCheckpointStorePinnedDirectory(t *testing.T) {
	encoded, approved, binding := checkpointStoreFixture(t)
	directory := testRecoveryDirectory(t)
	owner, err := OpenLinuxRecoveryCheckpointStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	moved := filepath.Join(t.TempDir(), "original")
	if err := os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := owner.store(encoded, approved, binding); err != nil {
		t.Fatal(err)
	}
	if data, err := owner.Load(approved, binding); err != nil || !bytes.Equal(data, encoded) {
		t.Fatal("checkpoint lost original directory:", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("checkpoint followed replacement directory:", entries, err)
	}
}

func TestRecoveryCheckpointStoreInvalidApproval(t *testing.T) {
	encoded, approved, binding := checkpointStoreFixture(t)
	directory := testRecoveryDirectory(t)
	owner, err := OpenLinuxRecoveryCheckpointStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := owner.store(encoded, [sha256.Size]byte{}, binding); err == nil {
		t.Fatal("missing independent approval accepted")
	}
	if err := owner.store(encoded, approved, [sha256.Size]byte{}); err == nil {
		t.Fatal("empty binding accepted")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("invalid approval modified storage:", entries, err)
	}
	if err := owner.store(encoded, approved, binding); err != nil {
		t.Fatal("rejected approval poisoned empty store:", err)
	}
	var absent *LinuxRecoveryCheckpointStore
	if absent.Close() != nil {
		t.Fatal("nil cleanup failed")
	}
	if err := absent.store(encoded, approved, binding); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if data, err := absent.Load(approved, binding); data != nil || !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}
