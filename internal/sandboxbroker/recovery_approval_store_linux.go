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
	"slices"
)

const (
	// recoveryApprovalName is the fixed file name for the approval receipt.
	recoveryApprovalName = "approval.bin"

	// recoveryApprovalMagic is the version header for the approval receipt.
	recoveryApprovalMagic = "PIPITRA1"

	// recoveryApprovalHeaderBytes is the byte length of the version header.
	recoveryApprovalHeaderBytes = len(recoveryApprovalMagic)

	// recoveryApprovalBytes is the exact size of a valid approval record.
	recoveryApprovalBytes = recoveryApprovalHeaderBytes + 2*sha256.Size
)

// LinuxRecoveryApprovalStore retains approval separately from checkpoint bytes. Provision
// it on trusted local storage outside all grants and other recovery stores.
type LinuxRecoveryApprovalStore struct {
	// storage is the locked private approval directory.
	storage *LinuxRecoveryJournal
}

// Store publishes captured host approval exactly once without replacing existing bytes.
//
// Takes approved ([sha256.Size]byte) which is a digest captured from original host
// authority.
// Takes binding ([sha256.Size]byte) which is the independently known security binding.
//
// Returns success only after approval file and directory synchronisation.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryApprovalStore) Store(approved, binding [sha256.Size]byte) error {
	if owner == nil || owner.storage == nil {
		return ErrClosed
	}
	encoded, err := encodeRecoveryApproval(approved, binding)
	if err != nil {
		return err
	}
	owner.storage.mutex.Lock()
	defer owner.storage.mutex.Unlock()
	return owner.storeValidated(encoded)
}

// Load selects approval only when the current independently supplied binding matches. The
// caller must then validate checkpoint bytes against the returned digest.
//
// Takes binding ([sha256.Size]byte) which is the current kernel-and-policy binding, never
// a binding read from checkpoint data.
//
// Returns no digest on missing, ambiguous, unsafe or mismatched approval metadata.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryApprovalStore) Load(binding [sha256.Size]byte) ([sha256.Size]byte, error) {
	var empty [sha256.Size]byte
	if owner == nil || owner.storage == nil {
		return empty, ErrClosed
	}
	owner.storage.mutex.Lock()
	defer owner.storage.mutex.Unlock()
	if owner.storage.closed {
		return empty, ErrClosed
	}
	names, err := owner.storage.metadataEntries(recoveryApprovalName, serviceReleaseReadyName)
	if err != nil {
		return empty, err
	}
	if !slices.Contains(names, recoveryApprovalName) {
		return empty, ErrDenied
	}
	encoded, err := owner.storage.readEntry(recoveryApprovalName, int64(recoveryApprovalBytes))
	if err != nil {
		return empty, err
	}
	approved, err := decodeRecoveryApproval(encoded, binding)
	if err != nil {
		return empty, err
	}
	if _, err := owner.serviceReleaseReady(approved, binding); err != nil {
		return empty, err
	}
	return approved, nil
}

// Close releases approval storage ownership without deleting independently retained
// policy.
//
// Returns descriptor and unlock failures; repeated closure is harmless.
func (owner *LinuxRecoveryApprovalStore) Close() error {
	if owner == nil || owner.storage == nil {
		return nil
	}
	return owner.storage.Close()
}

// storeValidated publishes fixed-size approval while the storage mutex is held.
//
// Takes encoded ([]byte) which contains canonical approval bytes prepared from original
// host authority.
//
// Returns an error and poisons further writes on uncertain publication.
func (owner *LinuxRecoveryApprovalStore) storeValidated(encoded []byte) error {
	storage := owner.storage
	if storage.closed || storage.failed {
		return ErrClosed
	}
	names, err := storage.metadataEntries(recoveryApprovalName)
	if err != nil {
		storage.failed = true
		return err
	}
	if len(names) != 0 {
		return ErrDenied
	}
	if err := storage.persistEntry(recoveryApprovalName, encoded); err != nil {
		storage.failed = true
		return err
	}
	return nil
}

// OpenLinuxRecoveryApprovalStore pins an existing private approval directory. It applies
// the same ownership, ancestry and exclusive-lock checks as the journal.
//
// Takes directory (string) which is an absolute host-selected path whose directory has
// already been made durable.
//
// Returns independent approval ownership without reading checkpoint contents.
func OpenLinuxRecoveryApprovalStore(directory string) (*LinuxRecoveryApprovalStore, error) {
	var storage LinuxRecoveryJournal
	if err := storage.open(directory); err != nil {
		return nil, errors.Join(err, storage.Close())
	}
	owner := &LinuxRecoveryApprovalStore{storage: &storage}
	if _, err := storage.metadataEntries(recoveryApprovalName, serviceReleaseReadyName); err != nil {
		return nil, errors.Join(err, owner.Close())
	}
	return owner, nil
}

// encodeRecoveryApproval frames captured approval without embedding a checkpoint.
//
// Takes approved ([sha256.Size]byte) which is the independently captured nonzero digest.
// Takes binding ([sha256.Size]byte) which is the independently captured nonzero binding.
//
// Returns one fixed-size versioned record without host paths or mutable authority.
func encodeRecoveryApproval(approved, binding [sha256.Size]byte) ([]byte, error) {
	if approved == ([sha256.Size]byte{}) || binding == ([sha256.Size]byte{}) {
		return nil, ErrInvalidPolicy
	}
	encoded := make([]byte, recoveryApprovalBytes)
	copy(encoded, recoveryApprovalMagic)
	copy(encoded[recoveryApprovalHeaderBytes:], approved[:])
	copy(encoded[recoveryApprovalHeaderBytes+sha256.Size:], binding[:])
	return encoded, nil
}

// decodeRecoveryApproval validates one private record against current independent policy.
//
// Takes encoded ([]byte) which contains bounded file bytes.
// Takes binding ([sha256.Size]byte) which is a binding supplied by the trusted host.
//
// Returns the recorded digest, never a digest computed from stored checkpoint bytes.
func decodeRecoveryApproval(encoded []byte, binding [sha256.Size]byte) ([sha256.Size]byte, error) {
	var empty [sha256.Size]byte
	if len(encoded) != recoveryApprovalBytes || string(encoded[:recoveryApprovalHeaderBytes]) != recoveryApprovalMagic ||
		binding == empty {
		return empty, ErrInvalidPolicy
	}
	approved := [sha256.Size]byte(encoded[recoveryApprovalHeaderBytes : recoveryApprovalHeaderBytes+sha256.Size])
	captured := [sha256.Size]byte(encoded[recoveryApprovalHeaderBytes+sha256.Size:])
	if approved == empty || captured != binding {
		return empty, ErrDenied
	}
	return approved, nil
}
