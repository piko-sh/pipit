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

// PrepareRecovery reconstructs only cleanup authority while retaining all metadata
// leases. Current grant paths are host-selected and must match the approved original
// roots.
//
// Takes grants ([]LinuxRootGrant) which are independently approved current host grants,
// never roots derived from the journal.
//
// Returns a sealed recovery-only bootstrap, or nil on a fully validated empty journal.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryClaim) PrepareRecovery(grants []LinuxRootGrant) (*LinuxBootstrap, error) {
	if owner == nil {
		return nil, ErrClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed || owner.journal == nil || owner.checkpoint == nil || owner.approval == nil {
		return nil, ErrClosed
	}
	ready, err := owner.serviceReleaseReadyLocked()
	if err != nil {
		return nil, err
	}
	if ready {
		return nil, ErrDenied
	}
	stores := []*LinuxRecoveryJournal{owner.checkpoint.storage, owner.approval.storage}
	for index, storage := range stores {
		if err := rejectRecoveryStorageOverlap(owner.journal, storage); err != nil {
			return nil, err
		}
		for _, other := range stores[:index] {
			if err := rejectRecoveryStorageOverlap(storage, other); err != nil {
				return nil, err
			}
		}
	}
	return prepareCheckpointRecovery(owner.encoded, owner.digest, owner.binding, owner.journal, grants, stores, true)
}
