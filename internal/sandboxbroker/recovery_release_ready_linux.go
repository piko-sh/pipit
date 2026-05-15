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
	"slices"
)

const (
	// serviceReleaseReadyName is the fixed file name for the release readiness receipt.
	serviceReleaseReadyName = "release-ready.bin"

	// serviceReleaseReadyMagic is the version header for the release readiness receipt.
	serviceReleaseReadyMagic = "PIPITRR1"
)

// ServiceReleaseReady reads independent readiness without granting cleanup authority. The
// native owner must still verify original host and service identity.
//
// Returns false only when the optional receipt is absent from otherwise valid storage.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryClaim) ServiceReleaseReady() (bool, error) {
	if owner == nil {
		return false, ErrClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	return owner.serviceReleaseReadyLocked()
}

// RecordServiceReleaseReady records successful cleanup before native service removal.
// Only a trusted native owner may call this after recovery, process reaping and
// emptiness.
//
// Returns success after immutable publication and sync in independent approval storage.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryClaim) RecordServiceReleaseReady() error {
	if owner == nil {
		return ErrClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if err := owner.validateReleaseApproval(); err != nil {
		return err
	}
	storage := owner.approval.storage
	storage.mutex.Lock()
	defer storage.mutex.Unlock()
	ready, err := owner.approval.serviceReleaseReady(owner.digest, owner.binding)
	if err != nil {
		return err
	}
	if ready {
		return storage.directory.Sync()
	}
	if storage.failed {
		return ErrClosed
	}
	encoded, err := encodeServiceReleaseReady(owner.digest, owner.binding)
	if err != nil {
		return err
	}
	if err := storage.persistEntry(serviceReleaseReadyName, encoded); err != nil {
		storage.failed = true
		return err
	}
	return nil
}

// serviceReleaseReadyLocked validates progress while exclusive claim ownership is held.
//
// Returns no readiness authority if independent approval or private storage has changed.
//
// Not safe for concurrent use. The caller must hold owner.mutex.
func (owner *LinuxRecoveryClaim) serviceReleaseReadyLocked() (bool, error) {
	if err := owner.validateReleaseApproval(); err != nil {
		return false, err
	}
	owner.approval.storage.mutex.Lock()
	defer owner.approval.storage.mutex.Unlock()
	return owner.approval.serviceReleaseReady(owner.digest, owner.binding)
}

// validateReleaseApproval rechecks the original independent approval under claim
// ownership.
//
// Returns failure for incomplete, closed or changed authority without publishing a
// receipt.
func (owner *LinuxRecoveryClaim) validateReleaseApproval() error {
	if owner.closed || (!owner.service && owner.journal == nil) || owner.checkpoint == nil || owner.approval == nil || owner.approval.storage == nil {
		return ErrClosed
	}
	approved, err := owner.approval.Load(owner.binding)
	if err != nil {
		return err
	}
	if approved != owner.digest {
		return ErrDenied
	}
	return nil
}

// serviceReleaseReady validates optional progress while the approval storage mutex is
// held.
//
// Takes approved ([sha256.Size]byte) which is the authenticated original checkpoint
// digest.
// Takes binding ([sha256.Size]byte) which is the independently current security binding.
//
// Returns no progress for malformed, unsafe or transplanted records.
func (owner *LinuxRecoveryApprovalStore) serviceReleaseReady(approved, binding [sha256.Size]byte) (bool, error) {
	names, err := owner.storage.metadataEntries(recoveryApprovalName, serviceReleaseReadyName)
	if err != nil {
		return false, err
	}
	if !slices.Contains(names, recoveryApprovalName) {
		return false, ErrDenied
	}
	if !slices.Contains(names, serviceReleaseReadyName) {
		return false, nil
	}
	expected, err := encodeServiceReleaseReady(approved, binding)
	if err != nil {
		return false, err
	}
	encoded, err := owner.storage.readEntry(serviceReleaseReadyName, int64(len(expected)))
	if err != nil {
		return false, err
	}
	if !bytes.Equal(encoded, expected) {
		return false, ErrDenied
	}
	return true, nil
}

// encodeServiceReleaseReady binds fixed-size progress to the entire original approval.
//
// Takes approved ([sha256.Size]byte) which is the digest from the original authenticated
// checkpoint.
// Takes binding ([sha256.Size]byte) which is the binding from the original authenticated
// checkpoint.
//
// Returns a versioned record distinct from approval itself.
func encodeServiceReleaseReady(approved, binding [sha256.Size]byte) ([]byte, error) {
	encoded, err := encodeRecoveryApproval(approved, binding)
	if err != nil {
		return nil, err
	}
	copy(encoded, serviceReleaseReadyMagic)
	return encoded, nil
}
