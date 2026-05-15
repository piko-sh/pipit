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
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxwire"
)

const (
	// recoveryCheckpointProfile is the sealed schema version for recovery checkpoint
	// metadata.
	recoveryCheckpointProfile = "filesystem-recovery-checkpoint-v3"

	// maximumRecoveryContextBytes is the upper bound on embedded host context bytes.
	maximumRecoveryContextBytes = 1024
)

// recoveryCheckpoint captures the original sealed policy for later authenticated restart.
type recoveryCheckpoint struct {
	// Profile identifies the checkpoint schema version.
	Profile string `json:"profile"`

	// Namespace is the original sealed staging namespace.
	Namespace string `json:"namespace"`

	// Roots lists the original pinned root identities.
	Roots []filesystemBootstrapRoot `json:"roots"`

	// Context holds optional opaque host metadata.
	Context []byte `json:"context"`

	// Journal holds the kernel identity of the original journal.
	Journal recoveryCheckpointJournal `json:"journal"`

	// Limits holds the original cumulative quotas.
	Limits FilesystemLimits `json:"limits"`

	// Binding is the independently derived host security binding.
	Binding [sha256.Size]byte `json:"binding"`
}

// recoveryCheckpointJournal holds the kernel identity of a journal directory.
type recoveryCheckpointJournal struct {
	// Device is the kernel device number.
	Device uint64 `json:"device"`

	// Inode is the kernel inode number.
	Inode uint64 `json:"inode"`

	// MountID is the kernel mount identifier.
	MountID uint64 `json:"mount_id"`
}

// checkpoint captures original recovery policy before any operation is admitted. The host
// must durably retain its digest independently before admitting work.
//
// Takes journal (*LinuxRecoveryJournal) which is the original empty journal.
// Takes binding ([sha256.Size]byte) which is a nonzero host-selected security binding.
//
// Returns bounded checkpoint bytes, not persisted storage.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryAuthority) checkpoint(journal *LinuxRecoveryJournal, binding [sha256.Size]byte) ([]byte, error) {
	if owner == nil || journal == nil {
		return nil, ErrClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed || owner.budget == nil {
		return nil, ErrClosed
	}
	owner.budget.mutex.Lock()
	defer owner.budget.mutex.Unlock()
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	return owner.checkpointLocked(journal, binding)
}

// checkpointLocked captures policy while authority, budget and journal are locked.
//
// Takes journal (*LinuxRecoveryJournal) which is an existing journal with its mutex held.
// Takes binding ([sha256.Size]byte) which is a separately approved host security binding.
//
// Returns no checkpoint after admission or while storage policy is invalid.
func (owner *LinuxRecoveryAuthority) checkpointLocked(journal *LinuxRecoveryJournal, binding [sha256.Size]byte) ([]byte, error) {
	if owner.budget.closed || owner.budget.calls != 0 || owner.budget.lastID != 0 || owner.budget.outstanding != 0 {
		return nil, ErrInvalidPolicy
	}
	if journal.closed || journal.failed || journal.directory == nil {
		return nil, ErrClosed
	}
	if journal.namespace != owner.namespace || len(journal.records) != 0 {
		return nil, ErrInvalidPolicy
	}
	if err := owner.rejectJournalOverlap(journal.directory); err != nil {
		return nil, err
	}
	identity, err := checkpointJournalIdentity(journal)
	if err != nil {
		return nil, err
	}
	return owner.encodeCheckpoint(identity, binding)
}

// encodeCheckpoint captures roots while authority, budget and journal locks are held.
//
// Takes journal (recoveryCheckpointJournal) which is the pinned journal identity.
// Takes binding ([sha256.Size]byte) which is a separately supplied host security binding.
//
// Returns canonical bounded metadata only after verifying all retained root handles.
func (owner *LinuxRecoveryAuthority) encodeCheckpoint(journal recoveryCheckpointJournal, binding [sha256.Size]byte) ([]byte, error) {
	policy := recoveryCheckpoint{
		Profile: recoveryCheckpointProfile, Namespace: owner.namespace,
		Journal: journal, Context: nil,
		Roots: make([]filesystemBootstrapRoot, 0, len(owner.roots)), Limits: owner.budget.limits, Binding: binding,
	}
	for _, root := range owner.roots {
		identity, err := linuxBootstrapRootIdentity(root.file)
		if err != nil {
			return nil, err
		}
		identity.Name, identity.Rights = root.identity.Name, root.identity.Rights
		if identity != root.identity {
			return nil, ErrDenied
		}
		policy.Roots = append(policy.Roots, identity)
	}
	slices.SortFunc(policy.Roots, func(first, second filesystemBootstrapRoot) int { return cmp.Compare(first.Name, second.Name) })
	if err := validateRecoveryCheckpoint(policy, binding); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maximumBootstrapBytes {
		return nil, errLimit
	}
	return encoded, nil
}

// ValidateRecoveryCheckpoint checks exact independently approved checkpoint bytes. The
// digest and binding must come from private host policy, never the checkpoint.
//
// Takes encoded ([]byte) which contains bounded stored checkpoint bytes.
// Takes approved ([sha256.Size]byte) which is the independently stored digest.
// Takes binding ([sha256.Size]byte) which is the current host security binding.
//
// Returns an error for altered, ambiguous, stale-policy or excessive metadata.
func ValidateRecoveryCheckpoint(encoded []byte, approved, binding [sha256.Size]byte) error {
	if len(encoded) == 0 || len(encoded) > maximumBootstrapBytes || approved == ([sha256.Size]byte{}) {
		return ErrInvalidPolicy
	}
	if sha256.Sum256(encoded) != approved {
		return ErrDenied
	}
	message := sandboxwire.Message{Kind: sandboxwire.Configure, ID: 0, Payload: encoded}
	var policy recoveryCheckpoint
	if err := message.DecodePayload(&policy); err != nil {
		return err
	}
	canonical, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	if !bytes.Equal(encoded, canonical) {
		return ErrInvalidPolicy
	}
	return validateRecoveryCheckpoint(policy, binding)
}

// ValidateCheckpointJournal binds independently approved policy to pinned intent storage.
//
// Takes encoded ([]byte) which contains the checkpoint bytes.
// Takes approved ([sha256.Size]byte) which is the independent approval digest.
// Takes binding ([sha256.Size]byte) which is the current host security binding.
// Takes journal (*LinuxRecoveryJournal) which is the locked journal to validate against.
//
// Returns an error if namespace, directory identity or private storage metadata differ.
//
// Safe for concurrent use by multiple goroutines.
func ValidateCheckpointJournal(encoded []byte, approved, binding [sha256.Size]byte, journal *LinuxRecoveryJournal) error {
	if err := ValidateRecoveryCheckpoint(encoded, approved, binding); err != nil {
		return err
	}
	if journal == nil {
		return ErrClosed
	}
	var policy recoveryCheckpoint
	if err := json.Unmarshal(encoded, &policy); err != nil {
		return err
	}
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	if journal.closed || journal.directory == nil {
		return ErrClosed
	}
	identity, err := checkpointJournalIdentity(journal)
	if err != nil {
		return err
	}
	if journal.namespace != policy.Namespace || identity != policy.Journal {
		return ErrDenied
	}
	return nil
}

// validateRecoveryCheckpoint validates an exact finite policy without opening paths.
//
// Takes policy (recoveryCheckpoint) which is the captured checkpoint metadata.
// Takes binding ([sha256.Size]byte) which is a separately trusted current host binding.
//
// Returns an error for incomplete identities, ambiguous roots or noncanonical quotas.
func validateRecoveryCheckpoint(policy recoveryCheckpoint, binding [sha256.Size]byte) error {
	if binding == ([sha256.Size]byte{}) || policy.Binding != binding {
		return ErrDenied
	}
	if policy.Profile == serviceCheckpointProfile {
		return validateServiceCheckpoint(policy)
	}
	if policy.Profile != recoveryCheckpointProfile || !validStagingNamespace(policy.Namespace) ||
		policy.Roots == nil || len(policy.Roots) > maximumRoots || policy.Journal.Inode == 0 || policy.Journal.MountID == 0 ||
		len(policy.Context) > maximumRecoveryContextBytes {
		return ErrInvalidPolicy
	}
	grants := make([]RootGrant, len(policy.Roots))
	for index, root := range policy.Roots {
		if root.Inode == 0 || root.MountID == 0 || index > 0 && policy.Roots[index-1].Name >= root.Name {
			return ErrInvalidPolicy
		}
		grants[index] = RootGrant{Name: root.Name, Rights: root.Rights}
	}
	budget, err := NewFilesystemBudget(grants, policy.Limits)
	if err != nil {
		return err
	}
	if budget.limits != policy.Limits {
		return ErrInvalidPolicy
	}
	return nil
}

// checkpointJournalIdentity inspects the retained journal while its mutex is held.
//
// Takes journal (*LinuxRecoveryJournal) which is a pinned private journal, never a
// reopened directory pathname.
//
// Returns descriptor-derived device, inode and mount identity without granting access.
func checkpointJournalIdentity(journal *LinuxRecoveryJournal) (identity recoveryCheckpointJournal, result error) {
	if err := checkJournalDirectory(journal.directory); err != nil {
		return identity, err
	}
	pinned, err := openLinuxBeneath(journal.directory, ".", unix.O_PATH|unix.O_DIRECTORY)
	if err != nil {
		return identity, err
	}
	defer func() { result = errors.Join(result, pinned.Close()) }()
	root, err := linuxBootstrapRootIdentity(pinned)
	if err != nil {
		return identity, err
	}
	return recoveryCheckpointJournal{Device: root.Device, Inode: root.Inode, MountID: root.MountID}, nil
}
