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
	"io"
	"slices"

	"golang.org/x/sys/unix"
)

// recoveryCheckpointName is the fixed file name for the checkpoint metadata.
const recoveryCheckpointName = "checkpoint.json"

// LinuxRecoveryCheckpointStore owns one private immutable checkpoint directory. It is
// separate from the write-intent journal and never stores its own approval.
type LinuxRecoveryCheckpointStore struct {
	// storage is the locked private checkpoint directory.
	storage *LinuxRecoveryJournal
}

// Load reads pinned durable bytes and validates independently retained approval. Missing,
// changed or unsafe files never fall back to a cached checkpoint.
//
// Takes approved ([sha256.Size]byte) which is the approval digest from private host
// state, not from the file being loaded.
// Takes binding ([sha256.Size]byte) which is the current host security binding.
//
// Returns authenticated checkpoint bytes only, without reopening roots or running
// cleanup.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryCheckpointStore) Load(approved, binding [sha256.Size]byte) ([]byte, error) {
	if owner == nil || owner.storage == nil {
		return nil, ErrClosed
	}
	storage := owner.storage
	storage.mutex.Lock()
	defer storage.mutex.Unlock()
	if storage.closed {
		return nil, ErrClosed
	}
	names, err := owner.entries()
	if err != nil {
		return nil, err
	}
	if len(names) != 1 {
		return nil, ErrDenied
	}
	encoded, err := storage.readEntry(recoveryCheckpointName, maximumBootstrapBytes)
	if err != nil {
		return nil, err
	}
	if err := ValidateRecoveryCheckpoint(encoded, approved, binding); err != nil {
		return nil, err
	}
	return encoded, nil
}

// Close releases private storage ownership without removing persistent metadata.
//
// Returns descriptor and unlock errors; repeated closure is harmless.
func (owner *LinuxRecoveryCheckpointStore) Close() error {
	if owner == nil || owner.storage == nil {
		return nil
	}
	return owner.storage.Close()
}

// store durably publishes one independently approved checkpoint without replacement.
// Storage errors close admission to further writes.
//
// Takes encoded ([]byte) which contains canonical checkpoint bytes.
// Takes approved ([sha256.Size]byte) which is the approved digest.
// Takes binding ([sha256.Size]byte) which is the current host security binding.
//
// Returns success only after file and directory synchronisation.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryCheckpointStore) store(encoded []byte, approved, binding [sha256.Size]byte) error {
	if owner == nil || owner.storage == nil {
		return ErrClosed
	}
	if len(encoded) > maximumBootstrapBytes {
		return errLimit
	}
	encoded = slices.Clone(encoded)
	if err := ValidateRecoveryCheckpoint(encoded, approved, binding); err != nil {
		return err
	}
	storage := owner.storage
	storage.mutex.Lock()
	defer storage.mutex.Unlock()
	return owner.storeValidated(encoded)
}

// storeValidated publishes trusted canonical bytes while the storage mutex is held.
//
// Takes encoded ([]byte) which contains already validated bounded checkpoint metadata
// from the host authority.
//
// Returns a publication error while retaining the exclusive storage lease.
func (owner *LinuxRecoveryCheckpointStore) storeValidated(encoded []byte) error {
	storage := owner.storage
	if storage.closed || storage.failed {
		return ErrClosed
	}
	names, err := owner.entries()
	if err != nil {
		storage.failed = true
		return err
	}
	if len(names) != 0 {
		return ErrDenied
	}
	if err := storage.persistEntry(recoveryCheckpointName, encoded); err != nil {
		storage.failed = true
		return err
	}
	return nil
}

// entries checks the complete bounded directory without granting metadata authority.
//
// Returns an empty directory or exactly the reserved checkpoint name.
func (owner *LinuxRecoveryCheckpointStore) entries() (names []string, result error) {
	return owner.storage.metadataEntries(recoveryCheckpointName)
}

// metadataEntries bounds a private metadata directory to internally named entries.
//
// Takes expected basenames while the storage owner is exclusively held.
//
// Returns no unexpected names and never removes colliding entries.
func (owner *LinuxRecoveryJournal) metadataEntries(expected ...string) (names []string, result error) {
	directory, err := openLinuxBeneath(owner.directory, ".", unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, directory.Close()) }()
	if err := checkJournalDirectory(directory); err != nil {
		return nil, err
	}
	names, err = directory.Readdirnames(len(expected) + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > len(expected) {
		return nil, ErrDenied
	}
	for _, name := range names {
		if !slices.Contains(expected, name) {
			return nil, ErrDenied
		}
	}
	return names, nil
}

// OpenLinuxRecoveryCheckpointStore pins and locks an existing private directory. Opening
// storage authenticates neither a checkpoint nor old-process termination.
//
// Takes directory (string) which is an absolute host-selected directory with trusted
// ownership and ancestry.
//
// Returns exclusive storage ownership, refusing unexpected entries without deletion.
func OpenLinuxRecoveryCheckpointStore(directory string) (*LinuxRecoveryCheckpointStore, error) {
	var storage LinuxRecoveryJournal
	if err := storage.open(directory); err != nil {
		return nil, errors.Join(err, storage.Close())
	}
	owner := &LinuxRecoveryCheckpointStore{storage: &storage}
	if _, err := owner.entries(); err != nil {
		return nil, errors.Join(err, owner.Close())
	}
	return owner, nil
}
