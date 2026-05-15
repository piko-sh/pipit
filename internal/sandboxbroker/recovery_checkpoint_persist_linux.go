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

	"golang.org/x/sys/unix"
)

// persistCheckpoint captures and durably stores policy before admitting operations.
// Checkpoint storage must be separate from every grant and from the intent journal.
//
// Takes journal (*LinuxRecoveryJournal) which is the original intent journal.
// Takes store (*LinuxRecoveryCheckpointStore) which is the separately locked checkpoint
// store.
// Takes binding ([sha256.Size]byte) which is the host security binding.
//
// Returns the captured digest, including on uncertain publication.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryAuthority) persistCheckpoint(journal *LinuxRecoveryJournal,
	store *LinuxRecoveryCheckpointStore, binding [sha256.Size]byte,
) ([sha256.Size]byte, error) {
	var empty [sha256.Size]byte
	if owner == nil || journal == nil || store == nil || store.storage == nil {
		return empty, ErrClosed
	}
	if store.storage == journal {
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
	store.storage.mutex.Lock()
	defer store.storage.mutex.Unlock()
	if store.storage.closed || store.storage.failed {
		return empty, ErrClosed
	}
	if err := owner.rejectJournalOverlap(store.storage.directory); err != nil {
		return empty, err
	}
	if err := rejectCheckpointJournalOverlap(journal, store); err != nil {
		return empty, err
	}
	approved := sha256.Sum256(encoded)
	if err := store.storeValidated(encoded); err != nil {
		owner.budget.closed = true
		return approved, err
	}
	return approved, nil
}

// rejectCheckpointJournalOverlap separates independently locked metadata directories.
// Exact aliases and both containment directions are refused through pinned handles.
//
// Takes journal (*LinuxRecoveryJournal) which is the original journal with its mutex
// held.
// Takes store (*LinuxRecoveryCheckpointStore) which is the checkpoint store with its
// mutex held.
//
// Returns an error without creating, modifying or removing either store's contents.
func rejectCheckpointJournalOverlap(journal *LinuxRecoveryJournal, store *LinuxRecoveryCheckpointStore) (result error) {
	return rejectRecoveryStorageOverlap(journal, store.storage)
}

// rejectRecoveryStorageOverlap rejects aliases and ancestry between private stores.
//
// Takes originalStore (*LinuxRecoveryJournal) which is the first exclusively held
// metadata owner.
// Takes otherStore (*LinuxRecoveryJournal) which is the second exclusively held metadata
// owner.
//
// Returns an error without interpreting stored contents as authority.
func rejectRecoveryStorageOverlap(originalStore, otherStore *LinuxRecoveryJournal) (result error) {
	first, err := openLinuxBeneath(originalStore.directory, ".", unix.O_PATH|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, first.Close()) }()
	second, err := openLinuxBeneath(otherStore.directory, ".", unix.O_PATH|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, second.Close()) }()
	original, err := linuxBootstrapRootIdentity(first)
	if err != nil {
		return err
	}
	checkpoint, err := linuxBootstrapRootIdentity(second)
	if err != nil {
		return err
	}
	if err := rejectJournalAncestor(first, checkpoint); err != nil {
		return err
	}
	return rejectJournalAncestor(second, original)
}
