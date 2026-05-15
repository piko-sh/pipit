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
	"time"

	"golang.org/x/sys/unix"
)

func TestRecoveryApprovalStoreRoundTrip(t *testing.T) {
	directory := testRecoveryDirectory(t)
	store, err := OpenLinuxRecoveryApprovalStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	approved, binding := [sha256.Size]byte{1}, [sha256.Size]byte{2}
	if digest, err := store.Load(binding); err == nil || digest != ([sha256.Size]byte{}) {
		t.Fatal("missing approval accepted")
	}
	if err := store.Store(approved, binding); err != nil {
		t.Fatal(err)
	}
	if err := store.Store(approved, binding); !errors.Is(err, ErrDenied) {
		t.Fatal("approval overwritten:", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLinuxRecoveryApprovalStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if digest, err := reopened.Load(binding); err != nil || digest != approved {
		t.Fatal("approval not durable:", err)
	}
	if digest, err := reopened.Load([sha256.Size]byte{3}); err == nil || digest != ([sha256.Size]byte{}) {
		t.Fatal("foreign policy accepted")
	}
	if err := os.Remove(filepath.Join(directory, recoveryApprovalName)); err != nil {
		t.Fatal(err)
	}
	if digest, err := reopened.Load(binding); err == nil || digest != ([sha256.Size]byte{}) {
		t.Fatal("missing approval used cached state")
	}
}

func TestRecoveryApprovalStoreRejectsUnsafeMetadata(t *testing.T) {
	for _, mode := range []string{"short", "oversized", "magic", "zero-digest", "zero-binding", "fifo", "symlink", "hardlink", "permissions", "extra"} {
		t.Run(mode, func(t *testing.T) {
			approved, binding := [sha256.Size]byte{1}, [sha256.Size]byte{2}
			data, err := encodeRecoveryApproval(approved, binding)
			if err != nil {
				t.Fatal(err)
			}
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
				if err := unix.Mkfifo(name, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(t.TempDir(), "target")
				if err := os.WriteFile(target, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, name); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(name, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "hardlink":
				if err := os.Link(name, filepath.Join(t.TempDir(), "alias")); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				if err := os.Chmod(name, 0644); err != nil {
					t.Fatal(err)
				}
			case "extra":
				if err := os.WriteFile(filepath.Join(directory, "extra"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			store, err := OpenLinuxRecoveryApprovalStore(directory)
			if store != nil {
				defer store.Close()
			}
			if mode == "extra" {
				if store != nil || err == nil {
					t.Fatal("unexpected approval entry accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if digest, err := store.Load(binding); err == nil || digest != ([sha256.Size]byte{}) {
				t.Fatal("unsafe approval accepted")
			}
		})
	}
}

func TestPersistApprovedCheckpoint(t *testing.T) {
	authority := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, authority)
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(testRecoveryDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	defer checkpoint.Close()
	approval, err := OpenLinuxRecoveryApprovalStore(testRecoveryDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	defer approval.Close()
	binding := [sha256.Size]byte{1}
	metadata := []byte("original service identity")
	expected, err := authority.PersistApprovedCheckpointWithContext(journal, checkpoint, approval, binding, metadata)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := approval.Load(binding)
	if err != nil || captured != expected {
		t.Fatal("independent approval missing:", err)
	}
	data, err := checkpoint.Load(captured, binding)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(data) != captured {
		t.Fatal("approval selected different bytes")
	}
	metadata[0] ^= 1
	stored, err := RecoveryCheckpointContext(data, captured, binding)
	if err != nil || string(stored) != "original service identity" {
		t.Fatal("independent approval lost captured service metadata:", err)
	}
	if err := ValidateCheckpointJournal(data, captured, binding, journal); err != nil {
		t.Fatal(err)
	}
}

func TestApprovalPublicationFailureClosesAdmission(t *testing.T) {
	authority := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, authority)
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(testRecoveryDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	defer checkpoint.Close()
	approval, err := OpenLinuxRecoveryApprovalStore(testRecoveryDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	defer approval.Close()
	binding := [sha256.Size]byte{1}
	previous := [sha256.Size]byte{2}
	if err := approval.Store(previous, binding); err != nil {
		t.Fatal(err)
	}
	captured, err := authority.PersistApprovedCheckpoint(journal, checkpoint, approval, binding)
	if !errors.Is(err, ErrDenied) || captured == ([sha256.Size]byte{}) {
		t.Fatal("approval collision lost ownership:", err)
	}
	if call, err := authority.admit(brokerMessage(1, recoveryWritePayload)); call != nil || !errors.Is(err, ErrClosed) {
		t.Fatal("failed approval allowed execution:", err)
	}
	if digest, err := approval.Load(binding); err != nil || digest != previous {
		t.Fatal("old approval overwritten:", err)
	}
	if _, err := checkpoint.Load(captured, binding); err != nil {
		t.Fatal("uncertain captured bytes cannot be inspected:", err)
	}
	if data, err := checkpoint.Load(previous, binding); data != nil || err == nil {
		t.Fatal("old approval authorised a new checkpoint")
	}
}

func TestApprovalPersistenceSerialisesAdmission(t *testing.T) {
	authority := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, authority)
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(testRecoveryDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	defer checkpoint.Close()
	approval, err := OpenLinuxRecoveryApprovalStore(testRecoveryDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
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
		if time.Now().After(deadline) {
			t.Fatal("persistence did not acquire admission ownership")
		}
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
		t.Fatal("operation admitted before independent approval:", err)
	case <-time.After(20 * time.Millisecond):
	}
	approval.storage.mutex.Unlock()
	locked = false
	if err := <-persisted; err != nil {
		t.Fatal(err)
	}
	if err := <-admitted; err != nil {
		t.Fatal(err)
	}
}

func FuzzRecoveryApprovalRecord(fuzzer *testing.F) {
	seed, err := encodeRecoveryApproval([sha256.Size]byte{1}, [sha256.Size]byte{2})
	if err != nil {
		fuzzer.Fatal(err)
	}
	fuzzer.Add(seed)
	fuzzer.Add([]byte{})
	fuzzer.Fuzz(func(t *testing.T, data []byte) {
		approved, err := decodeRecoveryApproval(data, [sha256.Size]byte{2})
		if err != nil {
			if approved != ([sha256.Size]byte{}) {
				t.Fatal("partial approval escaped")
			}
			return
		}
		encoded, err := encodeRecoveryApproval(approved, [sha256.Size]byte{2})
		if err != nil || !bytes.Equal(data, encoded) {
			t.Fatal("noncanonical approval accepted")
		}
	})
}

func TestApprovalStorageSeparation(t *testing.T) {
	for _, mode := range []string{"grant", "journal", "checkpoint", "same-grant"} {
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
			journalDirectory := testRecoveryDirectory(t)
			journal, err := OpenLinuxRecoveryJournal(journalDirectory, authority.namespace)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			checkpointDirectory := testRecoveryDirectory(t)
			checkpoint, err := OpenLinuxRecoveryCheckpointStore(checkpointDirectory)
			if err != nil {
				t.Fatal(err)
			}
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
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			approval, err := OpenLinuxRecoveryApprovalStore(directory)
			if err != nil {
				t.Fatal(err)
			}
			defer approval.Close()
			captured, err := authority.PersistApprovedCheckpoint(journal, checkpoint, approval, [sha256.Size]byte{1})
			if !errors.Is(err, ErrDenied) || captured != ([sha256.Size]byte{}) {
				t.Fatal("overlapping approval store accepted:", err)
			}
			if _, err := os.Lstat(filepath.Join(checkpointDirectory, recoveryCheckpointName)); !os.IsNotExist(err) {
				t.Fatal("checkpoint published before separation validation:", err)
			}
			if _, err := os.Lstat(filepath.Join(directory, recoveryApprovalName)); !os.IsNotExist(err) {
				t.Fatal("approval published before separation validation:", err)
			}
		})
	}
}
