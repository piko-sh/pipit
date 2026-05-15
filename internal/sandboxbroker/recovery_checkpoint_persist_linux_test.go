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
)

func TestPersistCheckpointSeparatesStorage(t *testing.T) {
	for _, mode := range []string{"disjoint", "store-in-root", "root-in-store", "same-root", "store-in-journal", "journal-in-store"} {
		t.Run(mode, func(t *testing.T) {
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
				if err := os.MkdirAll(checkpoint, 0700); err != nil {
					t.Fatal(err)
				}
				var err error
				store, err = OpenLinuxRecoveryCheckpointStore(checkpoint)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = store.Close() })
			}
			if mode != "store-in-journal" {
				openStore()
			}
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
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
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			journal, err := OpenLinuxRecoveryJournal(directory, authority.namespace)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			if mode == "store-in-journal" {
				openStore()
			}
			binding := [sha256.Size]byte{1}
			approved, err := authority.persistCheckpoint(journal, store, binding)
			if mode == "disjoint" {
				if err != nil || approved == ([sha256.Size]byte{}) {
					t.Fatal("disjoint persistence failed:", err)
				}
				data, err := store.Load(approved, binding)
				if err != nil {
					t.Fatal(err)
				}
				if err := ValidateCheckpointJournal(data, approved, binding, journal); err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil || approved != ([sha256.Size]byte{}) {
					t.Fatal("overlapping checkpoint storage accepted:", err)
				}
				if _, err := os.Lstat(filepath.Join(checkpoint, recoveryCheckpointName)); !os.IsNotExist(err) {
					t.Fatal("rejected persistence published metadata:", err)
				}
			}
		})
	}
}

func TestPersistCheckpointSerialisesAdmission(t *testing.T) {
	authority := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, authority)
	store, err := OpenLinuxRecoveryCheckpointStore(testRecoveryDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
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
		t.Fatal("operation admitted before durable checkpoint:", err)
	case <-time.After(20 * time.Millisecond):
	}
	store.storage.mutex.Unlock()
	locked = false
	if err := <-persisted; err != nil {
		t.Fatal(err)
	}
	if err := <-admitted; err != nil {
		t.Fatal(err)
	}
	if _, err := authority.persistCheckpoint(journal, store, [sha256.Size]byte{1}); err == nil {
		t.Fatal("checkpoint persisted after admission")
	}
}

func TestPersistCheckpointPublicationFailureClosesAdmission(t *testing.T) {
	authority := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, authority)
	store, err := OpenLinuxRecoveryCheckpointStore(testRecoveryDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	binding := [sha256.Size]byte{1}
	expected, err := authority.persistCheckpoint(journal, store, binding)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := authority.persistCheckpoint(journal, store, binding)
	if !errors.Is(err, ErrDenied) || approved != expected {
		t.Fatal("collision lost captured approval:", err)
	}
	if call, err := authority.admit(brokerMessage(1, recoveryWritePayload)); call != nil || !errors.Is(err, ErrClosed) {
		t.Fatal("failed persistence left admission open:", err)
	}
	if _, err := store.Load(approved, binding); err != nil {
		t.Fatal("failed publication prevented inspection:", err)
	}
}

func TestPersistCheckpointMissingAndAliasedOwners(t *testing.T) {
	authority := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, authority)
	binding := [sha256.Size]byte{1}
	store, err := OpenLinuxRecoveryCheckpointStore(testRecoveryDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var absent *LinuxRecoveryAuthority
	if approved, err := absent.persistCheckpoint(journal, store, binding); !errors.Is(err, ErrClosed) || approved != ([sha256.Size]byte{}) {
		t.Fatal("nil authority produced approval:", err)
	}
	if approved, err := authority.persistCheckpoint(nil, store, binding); !errors.Is(err, ErrClosed) || approved != ([sha256.Size]byte{}) {
		t.Fatal("nil journal produced approval:", err)
	}
	if approved, err := authority.persistCheckpoint(journal, nil, binding); !errors.Is(err, ErrClosed) || approved != ([sha256.Size]byte{}) {
		t.Fatal("nil checkpoint store produced approval:", err)
	}
	alias := &LinuxRecoveryCheckpointStore{storage: journal}
	if approved, err := authority.persistCheckpoint(journal, alias, binding); !errors.Is(err, ErrDenied) || approved != ([sha256.Size]byte{}) {
		t.Fatal("same storage owner accepted:", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if approved, err := authority.persistCheckpoint(journal, store, binding); !errors.Is(err, ErrClosed) || approved != ([sha256.Size]byte{}) {
		t.Fatal("closed checkpoint store produced approval:", err)
	}
}
