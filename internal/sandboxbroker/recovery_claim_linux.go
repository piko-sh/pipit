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
	"encoding/json"
	"errors"
	"slices"
	"sync"
)

// LinuxRecoveryClaim exclusively owns all original recovery metadata stores. It grants no
// execution or cleanup authority and does not prove process death.
type LinuxRecoveryClaim struct {
	// journal is the locked original intent storage.
	journal *LinuxRecoveryJournal

	// checkpoint is the locked checkpoint metadata store.
	checkpoint *LinuxRecoveryCheckpointStore

	// approval is the locked independent approval store.
	approval *LinuxRecoveryApprovalStore

	// encoded holds the authenticated checkpoint bytes.
	encoded []byte

	// digest is the independently stored checkpoint hash.
	digest [sha256.Size]byte

	// binding is the current host security binding.
	binding [sha256.Size]byte

	// mutex guards all mutable claim state.
	mutex sync.Mutex

	// closed is true after Close releases all leases.
	closed bool

	// service is true for a checkpoint recorded without a journal (a worker launch with no
	// filesystem broker).
	service bool
}

// Service reports whether the claimed checkpoint is service-only: it carries no journal,
// so filesystem recovery does not apply to it.
//
// Returns bool which is true for a checkpoint PersistServiceCheckpoint recorded.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryClaim) Service() bool {
	if owner == nil {
		return false
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	return owner.service
}

// Checkpoint returns an owned copy and its independently stored approval.
//
// Returns no metadata once the exclusive claim has been released.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryClaim) Checkpoint() ([]byte, [sha256.Size]byte, error) {
	if owner == nil {
		return nil, [sha256.Size]byte{}, ErrClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed {
		return nil, [sha256.Size]byte{}, ErrClosed
	}
	return slices.Clone(owner.encoded), owner.digest, nil
}

// Close releases the claim without deleting records or changing any native resource. A
// destructive recovery owner must retain this claim until its own cleanup completes.
//
// Returns storage closure failures; repeated closure is harmless.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryClaim) Close() error {
	if owner == nil {
		return nil
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed {
		return nil
	}
	owner.closed = true
	owner.encoded = nil
	owner.digest = [sha256.Size]byte{}
	owner.binding = [sha256.Size]byte{}
	return errors.Join(owner.journal.Close(), owner.checkpoint.Close(), owner.approval.Close())
}

// open acquires non-blocking leases and authenticates original journal identity.
//
// Takes journalDirectory (string) which is the original host-selected journal path.
// Takes checkpointDirectory (string) which is the original host-selected checkpoint path.
// Takes approvalDirectory (string) which is the original host-selected approval path.
// Takes binding ([sha256.Size]byte) which is the current independent security binding.
//
// Returns failure before recovery authority is exposed on any mismatch or overlap.
func (owner *LinuxRecoveryClaim) open(journalDirectory, checkpointDirectory, approvalDirectory string, binding [sha256.Size]byte) error {
	var err error
	owner.approval, err = OpenLinuxRecoveryApprovalStore(approvalDirectory)
	if err != nil {
		return err
	}
	owner.digest, err = owner.approval.Load(binding)
	if err != nil {
		return err
	}
	owner.checkpoint, err = OpenLinuxRecoveryCheckpointStore(checkpointDirectory)
	if err != nil {
		return err
	}
	owner.encoded, err = owner.checkpoint.Load(owner.digest, binding)
	if err != nil {
		return err
	}
	var policy recoveryCheckpoint
	if err := json.Unmarshal(owner.encoded, &policy); err != nil {
		return err
	}
	if policy.Profile == serviceCheckpointProfile {
		if journalDirectory != "" {
			return ErrInvalidPolicy
		}
		owner.service = true
		return rejectRecoveryStorageOverlap(owner.checkpoint.storage, owner.approval.storage)
	}
	if journalDirectory == "" {
		return ErrInvalidPolicy
	}
	return owner.openJournal(journalDirectory, policy.Namespace, binding)
}

// openJournal leases the filesystem checkpoint's journal and checks it against the
// checkpoint and the other stores.
//
// Takes journalDirectory (string) which the host selected for the original launch.
// Takes namespace (string) which the checkpoint recorded for the journal.
// Takes binding ([sha256.Size]byte) which is the current host binding.
//
// Returns error on identity mismatch or storage overlap.
func (owner *LinuxRecoveryClaim) openJournal(journalDirectory, namespace string, binding [sha256.Size]byte) error {
	var err error
	owner.journal, err = OpenLinuxRecoveryJournal(journalDirectory, namespace)
	if err != nil {
		return err
	}
	if err := ValidateCheckpointJournal(owner.encoded, owner.digest, binding, owner.journal); err != nil {
		return err
	}
	stores := []*LinuxRecoveryJournal{owner.journal, owner.checkpoint.storage, owner.approval.storage}
	for index, storage := range stores {
		for _, other := range stores[:index] {
			if err := rejectRecoveryStorageOverlap(storage, other); err != nil {
				return err
			}
		}
	}
	return nil
}

// OpenLinuxRecoveryClaim authenticates a checkpoint while holding all storage leases.
// Approval is loaded from independent storage, never calculated from checkpoint bytes.
//
// Takes journalDirectory (string) which is the host-selected original journal path.
// Takes checkpointDirectory (string) which is the host-selected original checkpoint path.
// Takes approvalDirectory (string) which is the host-selected original approval path.
// Takes binding ([sha256.Size]byte) which is the independently derived current binding.
//
// Returns exclusive read-only recovery ownership, releasing partial claims on failure.
func OpenLinuxRecoveryClaim(journalDirectory, checkpointDirectory, approvalDirectory string, binding [sha256.Size]byte) (*LinuxRecoveryClaim, error) {
	if binding == ([sha256.Size]byte{}) {
		return nil, ErrInvalidPolicy
	}
	owner := &LinuxRecoveryClaim{
		journal: nil, checkpoint: nil, approval: nil, encoded: nil,
		digest: [sha256.Size]byte{}, binding: binding, mutex: sync.Mutex{}, closed: false, service: false,
	}
	if err := owner.open(journalDirectory, checkpointDirectory, approvalDirectory, binding); err != nil {
		return nil, errors.Join(err, owner.Close())
	}
	return owner, nil
}
