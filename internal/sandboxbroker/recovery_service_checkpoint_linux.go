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
)

// serviceCheckpointProfile marks a checkpoint recorded for a worker launch that has no
// filesystem broker: no roots, no journal and no filesystem limits, only the host binding
// and the service context the launch attaches (the cgroup identity and the image store it
// owns).
const serviceCheckpointProfile = "linux-service-checkpoint-v1"

var (
	// noCheckpointJournal is the journal identity a service-only checkpoint records.
	noCheckpointJournal recoveryCheckpointJournal

	// noFilesystemLimits is the filesystem budget a service-only checkpoint records.
	noFilesystemLimits FilesystemLimits
)

// PersistServiceCheckpoint records an approved service-only checkpoint: the profile, the
// host binding and metadata, in an empty checkpoint store with its approval receipt in an
// empty approval store. A later OpenLinuxRecoveryClaim with no journal directory reads it
// back.
//
// Takes checkpoint (*LinuxRecoveryCheckpointStore) which must be open and empty.
// Takes approval (*LinuxRecoveryApprovalStore) which must be open, empty and on storage
// distinct from the checkpoint store's.
// Takes binding ([sha256.Size]byte) which is the host binding the launch computed.
// Takes metadata ([]byte) which is the service context, at most
// maximumRecoveryContextBytes long.
//
// Returns the approved checkpoint digest and any error.
//
// Safe for concurrent use by multiple goroutines.
func PersistServiceCheckpoint(checkpoint *LinuxRecoveryCheckpointStore, approval *LinuxRecoveryApprovalStore,
	binding [sha256.Size]byte, metadata []byte,
) ([sha256.Size]byte, error) {
	var empty [sha256.Size]byte
	if len(metadata) > maximumRecoveryContextBytes {
		return empty, errLimit
	}
	if checkpoint == nil || checkpoint.storage == nil || approval == nil || approval.storage == nil {
		return empty, ErrClosed
	}
	if checkpoint.storage == approval.storage {
		return empty, ErrDenied
	}
	if binding == empty {
		return empty, ErrInvalidPolicy
	}
	checkpoint.storage.mutex.Lock()
	defer checkpoint.storage.mutex.Unlock()
	approval.storage.mutex.Lock()
	defer approval.storage.mutex.Unlock()
	if err := checkServiceCheckpointStores(checkpoint.storage, approval.storage); err != nil {
		return empty, err
	}
	encoded, err := json.Marshal(recoveryCheckpoint{
		Profile: serviceCheckpointProfile, Namespace: "", Roots: []filesystemBootstrapRoot{}, Context: nil,
		Journal: noCheckpointJournal, Limits: noFilesystemLimits, Binding: binding,
	})
	if err != nil {
		return empty, err
	}
	encoded, err = attachRecoveryContext(encoded, metadata, binding)
	if err != nil {
		return empty, err
	}
	approved := sha256.Sum256(encoded)
	receipt, err := encodeRecoveryApproval(approved, binding)
	if err != nil {
		return empty, err
	}
	if err := checkpoint.storeValidated(encoded); err != nil {
		return approved, err
	}
	if err := approval.storeValidated(receipt); err != nil {
		return approved, err
	}
	return approved, nil
}

// checkServiceCheckpointStores requires both stores to be open, healthy, private and
// disjoint; the caller holds both storage mutexes.
//
// Takes checkpoint (*LinuxRecoveryJournal) which backs the checkpoint store.
// Takes approval (*LinuxRecoveryJournal) which backs the approval store.
//
// Returns error when either store cannot take a record.
func checkServiceCheckpointStores(checkpoint, approval *LinuxRecoveryJournal) error {
	for _, storage := range []*LinuxRecoveryJournal{checkpoint, approval} {
		if storage.closed || storage.failed || storage.directory == nil {
			return ErrClosed
		}
		if err := checkJournalDirectory(storage.directory); err != nil {
			return err
		}
	}
	return rejectRecoveryStorageOverlap(checkpoint, approval)
}

// validateServiceCheckpoint checks the service-only profile: nothing a filesystem
// checkpoint carries may be present, and the context stays bounded.
//
// Takes policy (recoveryCheckpoint) which decoded canonically and matches its binding.
//
// Returns error which is ErrInvalidPolicy when a filesystem field is set.
func validateServiceCheckpoint(policy recoveryCheckpoint) error {
	if policy.Namespace != "" || len(policy.Roots) != 0 || policy.Journal != noCheckpointJournal ||
		policy.Limits != noFilesystemLimits || len(policy.Context) > maximumRecoveryContextBytes {
		return ErrInvalidPolicy
	}
	return nil
}
