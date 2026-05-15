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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	// filesystemBootstrapProfile is the sealed policy profile for standard filesystem broker
	// handoffs.
	filesystemBootstrapProfile = "filesystem-bootstrap-v2"

	// filesystemRecoveryBootstrapProfile is the sealed policy profile for recovery-only
	// broker handoffs.
	filesystemRecoveryBootstrapProfile = "filesystem-recovery-bootstrap-v1"

	// maximumBootstrapBytes is the upper bound on standard bootstrap policy size in bytes.
	maximumBootstrapBytes = 16 << 10

	// maximumRecoveryBootstrapBytes is the upper bound on recovery bootstrap policy size in
	// bytes.
	maximumRecoveryBootstrapBytes = 4 << 20

	// bootstrapExecutableBits is the permission mask used to reject executable policy
	// descriptors.
	bootstrapExecutableBits = 0111

	// requiredBootstrapSeals is the set of memfd seals that a valid bootstrap policy
	// descriptor must carry.
	requiredBootstrapSeals = unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL | unix.F_SEAL_EXEC
)

// LinuxBootstrap owns a sealed policy descriptor followed by pinned root handles. It
// carries host-selected authority only, never script source or script-selected paths.
type LinuxBootstrap struct {
	// namespace is the host-selected staging namespace for recovery.
	namespace string

	// files holds the sealed policy descriptor followed by root handles.
	files []*os.File

	// mutex guards concurrent access to closed.
	mutex sync.Mutex

	// closed is true after Close releases every handle.
	closed bool

	// recoveryOnly is true when the bootstrap carries recovery authority without standard
	// filesystem grants.
	recoveryOnly bool
}

// Files returns a copied slice of borrowed descriptors in fixed bootstrap order. The
// launcher must finish exec before Close.
//
// Returns []*os.File with the configuration handle followed by ordered roots.
//
// Safe for concurrent use by multiple goroutines.
func (bootstrap *LinuxBootstrap) Files() ([]*os.File, error) {
	if bootstrap == nil {
		return nil, ErrClosed
	}
	bootstrap.mutex.Lock()
	defer bootstrap.mutex.Unlock()
	if bootstrap.closed {
		return nil, ErrClosed
	}
	return slices.Clone(bootstrap.files), nil
}

// StagingNamespace returns the immutable host-selected namespace for recovery records. It
// remains available after Close but carries no filesystem authority.
//
// Returns string with the staging namespace, or empty when bootstrap is nil.
func (bootstrap *LinuxBootstrap) StagingNamespace() string {
	if bootstrap == nil {
		return ""
	}
	return bootstrap.namespace
}

// RecoveryOnly reports the immutable role selected by the host constructor. It does not
// replace native validation of the sealed policy during adoption.
//
// Returns bool which is true when the bootstrap carries only recovery authority.
func (bootstrap *LinuxBootstrap) RecoveryOnly() bool {
	return bootstrap != nil && bootstrap.recoveryOnly
}

// Close releases every host-owned bootstrap handle after exec or launch failure.
//
// Returns error when closing any handle fails.
//
// Safe for concurrent use by multiple goroutines.
func (bootstrap *LinuxBootstrap) Close() error {
	if bootstrap == nil {
		return nil
	}
	bootstrap.mutex.Lock()
	defer bootstrap.mutex.Unlock()
	if bootstrap.closed {
		return nil
	}
	bootstrap.closed = true
	var result error
	for _, file := range bootstrap.files {
		if file != nil {
			result = errors.Join(result, file.Close())
		}
	}
	return result
}

// filesystemBootstrap is the sealed JSON schema carried by bootstrap policy descriptors.
type filesystemBootstrap struct {
	// Namespace is the host-selected staging namespace.
	Namespace string `json:"namespace"`

	// Limits holds the cumulative quotas for the broker budget.
	Limits *FilesystemLimits `json:"limits"`

	// Profile identifies the bootstrap schema version and role.
	Profile string `json:"profile"`

	// Roots lists the pinned directory grants in bootstrap order.
	Roots []filesystemBootstrapRoot `json:"roots"`

	// Recovery holds optional recovery context as raw JSON fragments.
	Recovery []json.RawMessage `json:"recovery,omitempty"`
}

// filesystemBootstrapRoot is a pinned directory identity within the sealed bootstrap
// policy.
type filesystemBootstrapRoot struct {
	// Name is the opaque root name selected by the host.
	Name string `json:"name"`

	// Device is the kernel device number of the pinned directory.
	Device uint64 `json:"device"`

	// Inode is the kernel inode number of the pinned directory.
	Inode uint64 `json:"inode"`

	// MountID is the kernel mount identifier of the pinned directory.
	MountID uint64 `json:"mount_id"`

	// Rights holds the host-approved operations for this root.
	Rights Rights `json:"rights"`
}

// PrepareLinuxBootstrap seals immutable policy and pins grant roots for exec handoff.
// Policy contains only opaque names, rights and quotas; host paths are not encoded.
//
// Takes grants ([]LinuxRootGrant) which are the host-approved root directory bindings.
// Takes limits (FilesystemLimits) which are the host-approved cumulative quotas.
//
// Returns *LinuxBootstrap which must outlive child creation and be closed afterwards.
// Returns error which is non-nil on invalid grants or sealing failure.
func PrepareLinuxBootstrap(grants []LinuxRootGrant, limits FilesystemLimits) (*LinuxBootstrap, error) {
	budget, err := newLinuxGrantBudget(grants, limits)
	if err != nil {
		return nil, err
	}
	namespace, err := newStagingNamespace()
	if err != nil {
		return nil, err
	}
	policy := filesystemBootstrap{Profile: filesystemBootstrapProfile, Namespace: namespace,
		Roots: make([]filesystemBootstrapRoot, len(grants)), Limits: &budget.limits, Recovery: nil}
	bootstrap := &LinuxBootstrap{files: make([]*os.File, len(grants)+1), namespace: namespace, mutex: sync.Mutex{}, closed: false, recoveryOnly: false}
	for index, grant := range grants {
		root, err := openLinuxDirectory(grant.Path)
		if err != nil {
			return nil, errors.Join(err, bootstrap.Close())
		}
		bootstrap.files[index+1] = root
		identity, err := linuxBootstrapRootIdentity(root)
		if err != nil {
			return nil, errors.Join(err, bootstrap.Close())
		}
		identity.Name, identity.Rights = grant.Name, grant.Rights
		policy.Roots[index] = identity
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return nil, errors.Join(err, bootstrap.Close())
	}
	bootstrap.files[0], err = sealLinuxBootstrap(encoded)
	if err != nil {
		return nil, errors.Join(err, bootstrap.Close())
	}
	return bootstrap, nil
}

// newLinuxGrantBudget validates host paths and copies immutable authority.
//
// Takes grants ([]LinuxRootGrant) which are the host-selected root bindings with absolute
// paths.
// Takes limits (FilesystemLimits) which are the finite cumulative quotas before any
// directory is opened.
//
// Returns a private reservation budget or an invalid policy error.
func newLinuxGrantBudget(grants []LinuxRootGrant, limits FilesystemLimits) (*FilesystemBudget, error) {
	if len(grants) > maximumRoots {
		return nil, ErrInvalidPolicy
	}
	authority := make([]RootGrant, len(grants))
	for index, grant := range grants {
		if !filepath.IsAbs(grant.Path) {
			return nil, ErrInvalidPolicy
		}
		authority[index] = RootGrant{Name: grant.Name, Rights: grant.Rights}
	}
	return NewFilesystemBudget(authority, limits)
}

// sealLinuxBootstrap writes a bounded non-executable memfd and seals every mutation.
//
// Takes encoded ([]byte) containing trusted startup policy.
//
// Returns an owned immutable descriptor positioned at the start, or an error.
func sealLinuxBootstrap(encoded []byte) (*os.File, error) {
	return sealLinuxBootstrapBytes(encoded, maximumBootstrapBytes)
}

// sealLinuxBootstrapBytes seals a policy under its role-specific byte ceiling.
//
// Takes encoded ([]byte) which contains trusted host-constructed startup policy.
// Takes limit (int) which is the role-specific maximum byte ceiling.
//
// Returns an immutable non-executable policy descriptor.
func sealLinuxBootstrapBytes(encoded []byte, limit int) (*os.File, error) {
	if len(encoded) == 0 || len(encoded) > limit {
		return nil, ErrInvalidPolicy
	}
	descriptor, err := unix.MemfdCreate("pipit-filesystem-policy", unix.MFD_CLOEXEC|unix.MFD_NOEXEC_SEAL)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), "broker-policy")
	if _, err := file.WriteAt(encoded, 0); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if _, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, requiredBootstrapSeals); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}
