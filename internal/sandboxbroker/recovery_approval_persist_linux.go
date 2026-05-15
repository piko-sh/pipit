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

import "crypto/sha256"

// PersistApprovedCheckpoint publishes checkpoint bytes and independent approval together.
// All three private stores must be disjoint from each other and from every grant.
//
// Takes journal (*LinuxRecoveryJournal) which is the original intent storage.
// Takes checkpoint (*LinuxRecoveryCheckpointStore) which is the locked checkpoint
// metadata store.
// Takes approval (*LinuxRecoveryApprovalStore) which is the locked independent approval
// store.
// Takes binding ([sha256.Size]byte) which is the current host security binding.
//
// Returns the captured digest for inspection, including on uncertain publication. Success
// means both stores are durable, not that any original process is terminated.
func (owner *LinuxRecoveryAuthority) PersistApprovedCheckpoint(journal *LinuxRecoveryJournal,
	checkpoint *LinuxRecoveryCheckpointStore, approval *LinuxRecoveryApprovalStore, binding [sha256.Size]byte,
) ([sha256.Size]byte, error) {
	return owner.PersistApprovedCheckpointWithContext(journal, checkpoint, approval, binding, nil)
}

// PersistApprovedCheckpointWithContext also approves bounded original host metadata. Its
// interpretation and process-death verification remain the native owner's duty.
//
// Takes journal (*LinuxRecoveryJournal) which is the original intent storage.
// Takes checkpoint (*LinuxRecoveryCheckpointStore) which is the locked checkpoint
// metadata store.
// Takes approval (*LinuxRecoveryApprovalStore) which is the locked independent approval
// store.
// Takes binding ([sha256.Size]byte) which is the current host security binding.
// Takes metadata ([]byte) which contains independently captured context bytes.
//
// Returns the digest after the same fail-closed publication protocol as the plain
// variant.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryAuthority) PersistApprovedCheckpointWithContext(journal *LinuxRecoveryJournal,
	checkpoint *LinuxRecoveryCheckpointStore, approval *LinuxRecoveryApprovalStore, binding [sha256.Size]byte, metadata []byte,
) ([sha256.Size]byte, error) {
	var empty [sha256.Size]byte
	if len(metadata) > maximumRecoveryContextBytes {
		return empty, errLimit
	}
	if owner == nil || journal == nil || checkpoint == nil || checkpoint.storage == nil || approval == nil || approval.storage == nil {
		return empty, ErrClosed
	}
	if journal == checkpoint.storage || journal == approval.storage || checkpoint.storage == approval.storage {
		return empty, ErrDenied
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed || owner.budget == nil {
		return empty, ErrClosed
	}
	owner.budget.mutex.Lock()
	defer owner.budget.mutex.Unlock()
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	encoded, err := owner.checkpointLocked(journal, binding)
	if err != nil {
		return empty, err
	}
	encoded, err = attachRecoveryContext(encoded, metadata, binding)
	if err != nil {
		return empty, err
	}
	checkpoint.storage.mutex.Lock()
	defer checkpoint.storage.mutex.Unlock()
	approval.storage.mutex.Lock()
	defer approval.storage.mutex.Unlock()
	if err := owner.checkApprovalStores(journal, checkpoint.storage, approval.storage); err != nil {
		return empty, err
	}
	approved := sha256.Sum256(encoded)
	receipt, err := encodeRecoveryApproval(approved, binding)
	if err != nil {
		return empty, err
	}
	if err := checkpoint.storeValidated(encoded); err != nil {
		owner.budget.closed = true
		return approved, err
	}
	if err := approval.storeValidated(receipt); err != nil {
		owner.budget.closed = true
		return approved, err
	}
	return approved, nil
}

// checkApprovalStores validates disjoint private ownership before either publication.
//
// Takes journal (*LinuxRecoveryJournal) which is the exclusively held intent storage.
// Takes checkpoint (*LinuxRecoveryJournal) which is the exclusively held checkpoint
// storage.
// Takes approval (*LinuxRecoveryJournal) which is the exclusively held approval storage.
//
// Returns an error before writing when stores overlap, close or fail validation.
func (owner *LinuxRecoveryAuthority) checkApprovalStores(journal, checkpoint, approval *LinuxRecoveryJournal) error {
	for _, storage := range []*LinuxRecoveryJournal{checkpoint, approval} {
		if storage.closed || storage.failed || storage.directory == nil {
			return ErrClosed
		}
		if err := checkJournalDirectory(storage.directory); err != nil {
			return err
		}
		if err := owner.rejectJournalOverlap(storage.directory); err != nil {
			return err
		}
		if err := rejectRecoveryStorageOverlap(journal, storage); err != nil {
			return err
		}
	}
	return rejectRecoveryStorageOverlap(checkpoint, approval)
}
