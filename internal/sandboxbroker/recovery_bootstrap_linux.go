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
	"encoding/json"
	"errors"
	"os"
	"slices"
	"sync"
)

// RecoveryBootstrap seals a bounded cleanup batch using retained original roots.
// Admission must be closed and every reservation finished before calling.
//
// Takes journal (*LinuxRecoveryJournal) which is the private journal belonging to this
// authority's original namespace.
//
// Returns a recovery-only bootstrap with reduced write rights.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryAuthority) RecoveryBootstrap(journal *LinuxRecoveryJournal) (*LinuxBootstrap, error) {
	if owner == nil || journal == nil {
		return nil, ErrInvalidPolicy
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed || owner.budget == nil {
		return nil, ErrClosed
	}
	owner.budget.mutex.Lock()
	defer owner.budget.mutex.Unlock()
	if !owner.budget.closed || owner.budget.outstanding != 0 || journal.namespace != owner.namespace {
		return nil, ErrInvalidPolicy
	}
	records, err := journal.RecoveryRecords()
	if err != nil {
		return nil, err
	}
	policy, err := owner.recoveryPolicy(records)
	if err != nil {
		return nil, err
	}
	return sealRecoveryPolicy(policy, owner.roots)
}

// recoveryPolicy narrows the sealed roots to those referenced by admitted intents.
//
// Takes records ([]RecoveryRecord) which are detached journal records while the authority
// and admission locks are held.
//
// Returns a bounded recovery-only policy, rejecting foreign or unadmitted identities.
func (owner *LinuxRecoveryAuthority) recoveryPolicy(records []RecoveryRecord) (filesystemBootstrap, error) {
	if len(records) == 0 || len(records) > owner.budget.calls {
		return filesystemBootstrap{}, ErrInvalidPolicy
	}
	roots := make(map[string]filesystemBootstrapRoot)
	encoded := make([]json.RawMessage, 0, len(records))
	var previous uint64
	total := 0
	for index := range records {
		record := records[index]
		root, exists := owner.roots[record.Root]
		if !exists || record.Namespace != owner.namespace || record.Operation <= previous ||
			record.Operation > owner.budget.lastID || !matchesRecoveryRoot(root.identity, record) {
			return filesystemBootstrap{}, ErrDenied
		}
		data, err := encodeRecoveryRecord(record)
		if err != nil {
			return filesystemBootstrap{}, err
		}
		total += int(record.Size)
		if total > owner.budget.limits.WriteBytes-owner.budget.writeRemaining {
			return filesystemBootstrap{}, errLimit
		}
		previous = record.Operation
		identity := root.identity
		identity.Rights = Write
		roots[record.Root] = identity
		encoded = append(encoded, data)
	}
	selected := make([]filesystemBootstrapRoot, 0, len(roots))
	for _, root := range roots {
		selected = append(selected, root)
	}
	slices.SortFunc(selected, func(first, second filesystemBootstrapRoot) int {
		return cmp.Compare(first.Name, second.Name)
	})
	limits := owner.budget.limits
	return filesystemBootstrap{
		Profile: filesystemRecoveryBootstrapProfile, Namespace: owner.namespace,
		Roots: selected, Limits: &limits, Recovery: encoded,
	}, nil
}

// sealRecoveryPolicy duplicates retained roots into a recovery-only sealed handoff.
//
// Takes policy (filesystemBootstrap) which is validated cleanup metadata.
// Takes roots (map[string]linuxRecoveryRoot) which are independently owned, matching root
// handles.
//
// Returns a bootstrap that cannot create normal filesystem execution authority.
func sealRecoveryPolicy(policy filesystemBootstrap, roots map[string]linuxRecoveryRoot) (*LinuxBootstrap, error) {
	bootstrap := &LinuxBootstrap{
		namespace: policy.Namespace, files: make([]*os.File, len(policy.Roots)+1),
		mutex: sync.Mutex{}, closed: false, recoveryOnly: true,
	}
	var err error
	for index, root := range policy.Roots {
		bootstrap.files[index+1], err = duplicateLinuxBootstrapFile(roots[root.Name].file)
		if err != nil {
			return nil, errors.Join(err, bootstrap.Close())
		}
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return nil, errors.Join(err, bootstrap.Close())
	}
	bootstrap.files[0], err = sealLinuxBootstrapBytes(encoded, maximumRecoveryBootstrapBytes)
	if err != nil {
		return nil, errors.Join(err, bootstrap.Close())
	}
	return bootstrap, nil
}

// decodeBootstrapRecovery rejects role mixing and validates every sealed intent.
//
// Takes policy (filesystemBootstrap) which is a strictly decoded, bounded bootstrap
// policy.
//
// Returns nil for normal operation or a non-empty validated recovery-only batch.
func decodeBootstrapRecovery(policy filesystemBootstrap) ([]RecoveryRecord, error) {
	if policy.Profile == filesystemBootstrapProfile && policy.Recovery == nil {
		return nil, nil
	}
	if policy.Profile != filesystemRecoveryBootstrapProfile || len(policy.Recovery) == 0 || len(policy.Recovery) > defaultCalls {
		return nil, ErrInvalidPolicy
	}
	roots, err := recoveryBootstrapRoots(policy.Roots)
	if err != nil {
		return nil, err
	}
	records := make([]RecoveryRecord, 0, len(policy.Recovery))
	var previous uint64
	for _, encoded := range policy.Recovery {
		record, err := decodeRecoveryRecord(encoded)
		if err != nil {
			return nil, err
		}
		if record.Namespace != policy.Namespace || record.Operation <= previous || !matchesRecoveryRoot(roots[record.Root], record) {
			return nil, ErrDenied
		}
		previous = record.Operation
		records = append(records, record)
	}
	if err := validateRecoveryBatchLimits(policy.Limits, records); err != nil {
		return nil, err
	}
	return records, nil
}

// recoveryBootstrapRoots validates the bounded write-only cleanup root set.
//
// Takes approved ([]filesystemBootstrapRoot) which are identities from a sealed recovery
// policy.
//
// Returns uniquely named roots without silently widening native rights.
func recoveryBootstrapRoots(approved []filesystemBootstrapRoot) (map[string]filesystemBootstrapRoot, error) {
	if len(approved) == 0 || len(approved) > maximumRoots {
		return nil, ErrInvalidPolicy
	}
	roots := make(map[string]filesystemBootstrapRoot, len(approved))
	for _, root := range approved {
		if root.Rights != Write || !validRootName(root.Name) || root.Inode == 0 || root.MountID == 0 {
			return nil, ErrDenied
		}
		if _, exists := roots[root.Name]; exists {
			return nil, ErrInvalidPolicy
		}
		roots[root.Name] = root
	}
	return roots, nil
}

// validateRecoveryBatchLimits enforces sealed call and byte ceilings during adoption.
//
// Takes limits (*FilesystemLimits) which are the original host's finite policy limits.
// Takes records ([]RecoveryRecord) which are the bounded decoded batch.
//
// Returns error before cleanup when reduced limits would be exceeded.
func validateRecoveryBatchLimits(limits *FilesystemLimits, records []RecoveryRecord) error {
	if limits == nil {
		return ErrInvalidPolicy
	}
	budget, err := NewFilesystemBudget(nil, *limits)
	if err != nil {
		return err
	}
	if len(records) > budget.limits.Calls {
		return errLimit
	}
	total := 0
	for index := range records {
		total += int(records[index].Size)
		if total > budget.limits.WriteBytes {
			return errLimit
		}
	}
	return nil
}

// matchesRecoveryRoot checks original root authority without filesystem access.
//
// Takes root (filesystemBootstrapRoot) which is a sealed root identity.
// Takes record (RecoveryRecord) which is untrusted journal metadata.
//
// Returns false when the intent claims another root or lacks a write grant.
func matchesRecoveryRoot(root filesystemBootstrapRoot, record RecoveryRecord) bool {
	return root.Name == record.Root && root.Rights&Write != 0 &&
		root.Device == record.RootDevice && root.Inode == record.RootInode && root.MountID == record.RootMountID
}
