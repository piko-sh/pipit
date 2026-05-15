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
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
)

// PrepareCheckpointRecovery restores only bounded cleanup from approved host metadata.
// Grant paths come from current private host policy, never the checkpoint or journal.
//
// Takes encoded ([]byte) which is the approved checkpoint bytes.
// Takes approved ([sha256.Size]byte) which is the original checkpoint digest.
// Takes binding ([sha256.Size]byte) which is the current host binding identity.
// Takes journal (*LinuxRecoveryJournal) which is the original locked journal.
// Takes grants ([]LinuxRootGrant) which are the current host grant paths.
//
// Returns a recovery-only sealed bootstrap with independent handles, or no authority.
func PrepareCheckpointRecovery(encoded []byte, approved, binding [sha256.Size]byte,
	journal *LinuxRecoveryJournal, grants []LinuxRootGrant,
) (*LinuxBootstrap, error) {
	return prepareCheckpointRecovery(encoded, approved, binding, journal, grants, nil, false)
}

// prepareCheckpointRecovery validates restored authority against every retained store.
//
// Takes encoded ([]byte) which is the approved checkpoint bytes.
// Takes approved ([sha256.Size]byte) which is the original checkpoint digest.
// Takes binding ([sha256.Size]byte) which is the current host binding identity.
// Takes journal (*LinuxRecoveryJournal) which is the original locked journal.
// Takes grants ([]LinuxRootGrant) which are the current host grant paths.
// Takes stores ([]*LinuxRecoveryJournal) which are additional retained metadata owners to
// validate.
// Takes allowEmpty (bool) which permits a validated empty journal to return no authority.
//
// Returns only sealed cleanup authority, optionally accepting a validated empty journal.
func prepareCheckpointRecovery(encoded []byte, approved, binding [sha256.Size]byte,
	journal *LinuxRecoveryJournal, grants []LinuxRootGrant, stores []*LinuxRecoveryJournal, allowEmpty bool,
) (bootstrap *LinuxBootstrap, result error) {
	if len(encoded) > maximumBootstrapBytes {
		return nil, errLimit
	}
	encoded = slices.Clone(encoded)
	if err := ValidateCheckpointJournal(encoded, approved, binding, journal); err != nil {
		return nil, err
	}
	var checkpoint recoveryCheckpoint
	if err := json.Unmarshal(encoded, &checkpoint); err != nil {
		return nil, err
	}
	roots, err := reopenCheckpointRoots(checkpoint, grants)
	if err != nil {
		return nil, err
	}
	defer func() {
		result = errors.Join(result, roots.Close())
		if result != nil && bootstrap != nil {
			result = errors.Join(result, bootstrap.Close())
			bootstrap = nil
		}
	}()
	if err := checkRestoredJournalOverlap(roots, journal); err != nil {
		return nil, err
	}
	for _, storage := range stores {
		if err := checkRestoredJournalOverlap(roots, storage); err != nil {
			return nil, err
		}
	}
	records, err := journal.RecoveryRecords()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 && allowEmpty {
		return nil, nil
	}
	policy, err := checkpointRecoveryPolicy(checkpoint, records)
	if err != nil {
		return nil, err
	}
	return sealRecoveryPolicy(policy, roots.roots)
}

// checkRestoredJournalOverlap checks current ancestry without racing journal closure.
//
// Takes roots (*LinuxRecoveryAuthority) which are the new private root pins.
// Takes journal (*LinuxRecoveryJournal) which is the original locked journal owner.
//
// Returns an error if closure or directory movement invalidates storage separation.
//
// Safe for concurrent use by multiple goroutines.
func checkRestoredJournalOverlap(roots *LinuxRecoveryAuthority, journal *LinuxRecoveryJournal) error {
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	if journal.closed || journal.directory == nil {
		return ErrClosed
	}
	if err := checkJournalDirectory(journal.directory); err != nil {
		return err
	}
	return roots.rejectJournalOverlap(journal.directory)
}

// reopenCheckpointRoots checks current host-selected handles against original identities.
//
// Takes checkpoint (recoveryCheckpoint) which is the approved original metadata.
// Takes grants ([]LinuxRootGrant) which are the separately supplied current host root
// handles.
//
// Returns private pins only when every root and right matches, without exposing
// admission.
func reopenCheckpointRoots(checkpoint recoveryCheckpoint, grants []LinuxRootGrant) (*LinuxRecoveryAuthority, error) {
	if len(grants) != len(checkpoint.Roots) {
		return nil, ErrDenied
	}
	bootstrap, err := PrepareLinuxBootstrap(slices.Clone(grants), checkpoint.Limits)
	if err != nil {
		return nil, err
	}
	roots, err := bootstrap.RecoveryAuthority()
	if failure := errors.Join(err, bootstrap.Close()); failure != nil {
		if roots != nil {
			failure = errors.Join(failure, roots.Close())
		}
		return nil, failure
	}
	roots.budget.Close()
	for _, original := range checkpoint.Roots {
		current, exists := roots.roots[original.Name]
		if !exists || current.identity != original {
			return nil, errors.Join(ErrDenied, roots.Close())
		}
	}
	return roots, nil
}

// checkpointRecoveryPolicy reduces original grants to journal-referenced write cleanup.
//
// Takes checkpoint (recoveryCheckpoint) which is the approved policy.
// Takes records ([]RecoveryRecord) which is a freshly validated snapshot from the
// original private journal.
//
// Returns finite recovery metadata without trusting records to create roots or rights.
func checkpointRecoveryPolicy(checkpoint recoveryCheckpoint, records []RecoveryRecord) (filesystemBootstrap, error) {
	if len(records) == 0 || len(records) > checkpoint.Limits.Calls {
		return filesystemBootstrap{}, ErrInvalidPolicy
	}
	approved := make(map[string]filesystemBootstrapRoot, len(checkpoint.Roots))
	for _, root := range checkpoint.Roots {
		approved[root.Name] = root
	}
	used := make(map[string]filesystemBootstrapRoot)
	encoded := make([]json.RawMessage, len(records))
	var previous uint64
	for index := range records {
		record := records[index]
		root, exists := approved[record.Root]
		if !exists || record.Namespace != checkpoint.Namespace || record.Operation <= previous || !matchesRecoveryRoot(root, record) {
			return filesystemBootstrap{}, ErrDenied
		}
		data, err := encodeRecoveryRecord(record)
		if err != nil {
			return filesystemBootstrap{}, err
		}
		previous = record.Operation
		root.Rights = Write
		used[root.Name] = root
		encoded[index] = data
	}
	if err := validateRecoveryBatchLimits(&checkpoint.Limits, records); err != nil {
		return filesystemBootstrap{}, err
	}
	selected := make([]filesystemBootstrapRoot, 0, len(used))
	for _, root := range used {
		selected = append(selected, root)
	}
	slices.SortFunc(selected, func(first, second filesystemBootstrapRoot) int { return cmp.Compare(first.Name, second.Name) })
	limits := checkpoint.Limits
	return filesystemBootstrap{
		Profile: filesystemRecoveryBootstrapProfile, Namespace: checkpoint.Namespace,
		Roots: selected, Limits: &limits, Recovery: encoded,
	}, nil
}
