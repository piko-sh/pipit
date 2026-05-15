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
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	// maximumImageStoreLeases is the upper bound on concurrent staging directory leases.
	maximumImageStoreLeases = 16

	// maximumImageStoreMetadata is the upper bound on excluded metadata directories.
	maximumImageStoreMetadata = 3

	// maximumImageStorePathBytes is the upper bound on host-selected store path length.
	maximumImageStorePathBytes = 4096
)

// LinuxImageStoreIdentity identifies a private staging parent for future recovery
// binding. It does not itself approve deletion or establish that any worker has
// terminated.
type LinuxImageStoreIdentity struct {
	// Device is the kernel device number of the staging directory.
	Device uint64

	// Inode is the kernel inode number of the staging directory.
	Inode uint64

	// MountID is the kernel mount identifier of the staging directory.
	MountID uint64
}

// LinuxImageStore exclusively owns a private staging parent outside all script grants.
// Borrowed directory handles retain the store lease until explicitly returned.
type LinuxImageStore struct {
	// storage is the private recovery journal for this store.
	storage *LinuxRecoveryJournal

	// leases tracks outstanding borrowed directory handles.
	leases map[*os.File]struct{}

	// path is the host-selected absolute staging directory.
	path string

	// exclusions holds pinned grant and metadata directories.
	exclusions []imageStoreExclusion

	// identity holds the kernel identity of the staging parent.
	identity LinuxImageStoreIdentity

	// mutex guards all mutable store state.
	mutex sync.Mutex

	// closed is true after Close releases storage.
	closed bool

	// recovery is true when opened for orphan cleanup only.
	recovery bool

	// cleaned is true after successful orphan removal.
	cleaned bool
}

// Identity revalidates the staging parent and exclusions before exposing kernel identity.
//
// Returns a value copy only while exclusive storage ownership remains valid.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxImageStore) Identity() (LinuxImageStoreIdentity, error) {
	if owner == nil {
		return LinuxImageStoreIdentity{}, ErrClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if err := owner.validate(); err != nil {
		return LinuxImageStoreIdentity{}, err
	}
	return owner.identity, nil
}

// InitialIdentity captures storage only while empty and without outstanding image leases.
// The caller must publish original approval before using this store for staging.
//
// Returns no initial authority once image allocation is in progress.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxImageStore) InitialIdentity() (LinuxImageStoreIdentity, error) {
	if owner == nil {
		return LinuxImageStoreIdentity{}, ErrClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if err := owner.validate(); err != nil {
		return LinuxImageStoreIdentity{}, err
	}
	if owner.recovery || len(owner.leases) != 0 {
		return LinuxImageStoreIdentity{}, ErrDenied
	}
	if _, err := owner.storage.metadataEntries(); err != nil {
		return LinuxImageStoreIdentity{}, err
	}
	return owner.identity, nil
}

// ValidatePolicy requires every actual broker root and metadata path to be excluded. All
// retained inode identities are rechecked.
//
// Takes grants ([]LinuxRootGrant) which are the broker root paths to verify exclusion
// for.
// Takes metadata ([]string) which are the private metadata paths to verify exclusion for.
//
// Returns failure when a store was opened with a narrower exclusion policy.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxImageStore) ValidatePolicy(grants []LinuxRootGrant, metadata []string) error {
	if owner == nil {
		return ErrClosed
	}
	if len(grants) > maximumRoots || len(metadata) > maximumImageStoreMetadata {
		return errLimit
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if err := owner.validate(); err != nil {
		return err
	}
	for _, grant := range grants {
		if !owner.excludesPath(grant.Path) {
			return ErrDenied
		}
	}
	for _, path := range metadata {
		if !owner.excludesPath(path) {
			return ErrDenied
		}
	}
	return nil
}

// AcquireDirectory lends one close-on-exec staging parent under a finite lease limit. The
// borrower must return this exact handle after all of its image resources are removed.
//
// Returns a pinned parent and launch path, never a fallback temporary directory.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxImageStore) AcquireDirectory() (*os.File, string, error) {
	if owner == nil {
		return nil, "", ErrClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if err := owner.validate(); err != nil {
		return nil, "", err
	}
	if owner.recovery && !owner.cleaned {
		return nil, "", ErrDenied
	}
	if len(owner.leases) >= maximumImageStoreLeases {
		return nil, "", errLimit
	}
	if err := owner.checkCapacity(); err != nil {
		return nil, "", err
	}
	directory, err := openLinuxBeneath(owner.storage.directory, ".", unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, "", err
	}
	owner.leases[directory] = struct{}{}
	return directory, owner.path, nil
}

// ReleaseDirectory closes one borrowed handle without deleting any image resources.
//
// Takes directory (*os.File) which is the original borrowed handle returned only after
// its associated private root has been removed.
//
// Returns failure for foreign or already returned handles.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxImageStore) ReleaseDirectory(directory *os.File) error {
	if owner == nil {
		return ErrClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed {
		return ErrClosed
	}
	if _, exists := owner.leases[directory]; !exists {
		return ErrDenied
	}
	delete(owner.leases, directory)
	return directory.Close()
}

// Close releases storage only after every borrower returns and the directory is empty.
//
// Returns failure while images or unexpected entries remain; closure is retryable.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxImageStore) Close() error {
	if owner == nil {
		return nil
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed {
		return nil
	}
	if len(owner.leases) != 0 {
		return ErrDenied
	}
	if err := owner.validate(); err != nil {
		return err
	}
	if _, err := owner.storage.metadataEntries(); err != nil {
		return err
	}
	owner.closed = true
	return owner.closeHandles()
}

// excludesPath checks coverage of an actual host-selected capability path.
//
// Takes path (string) naming a host-selected grant or metadata directory.
//
// Returns true only for a path retained and revalidated by the original store owner.
func (owner *LinuxImageStore) excludesPath(path string) bool {
	for _, excluded := range owner.exclusions {
		if excluded.path == path {
			return true
		}
	}
	return false
}

// open acquires private storage and exclusions without modifying their contents.
//
// Takes grants ([]LinuxRootGrant) which are the host-approved root directories to
// exclude.
// Takes metadata ([]string) which are the private metadata paths to exclude while this
// owner is not yet exposed to callers.
//
// Returns error on storage, identity or overlap failure.
func (owner *LinuxImageStore) open(grants []LinuxRootGrant, metadata []string) error {
	if err := owner.storage.open(owner.path); err != nil {
		return err
	}
	if !owner.recovery {
		if _, err := owner.storage.metadataEntries(); err != nil {
			return err
		}
	}
	identity, err := imageStoreIdentity(owner.storage.directory)
	if err != nil {
		return err
	}
	if owner.recovery && identity != owner.identity {
		return ErrDenied
	}
	owner.identity = identity
	owner.exclusions = make([]imageStoreExclusion, 0, len(grants)+len(metadata))
	for _, grant := range grants {
		if err := owner.pinExclusion(grant.Path, false); err != nil {
			return err
		}
	}
	for _, path := range metadata {
		if err := owner.pinExclusion(path, true); err != nil {
			return err
		}
	}
	return owner.validate()
}

// pinExclusion captures a grant or private metadata directory without taking its lease.
//
// Takes path (string) which is the host-selected grant or metadata directory to pin.
// Takes private (bool) which indicates whether the directory requires recovery-journal
// ownership checks.
//
// Returns failure before any image may be staged when a path is unsafe or overlapping.
func (owner *LinuxImageStore) pinExclusion(path string, private bool) error {
	if !validImageStorePath(path) {
		return ErrInvalidPolicy
	}
	var file *os.File
	var err error
	if private {
		file, err = openRecoveryJournalDirectory(path)
	} else {
		file, err = openLinuxDirectory(path)
	}
	if err != nil {
		return err
	}
	owner.exclusions = append(owner.exclusions, imageStoreExclusion{file: file, path: path})
	return nil
}

// validate checks current path selection and pinned ancestry without trusting names
// alone.
//
// Returns failure after closure, replacement, permission changes or grant overlap.
func (owner *LinuxImageStore) validate() error {
	if owner.closed || owner.storage == nil || owner.storage.directory == nil {
		return ErrClosed
	}
	if err := checkJournalDirectory(owner.storage.directory); err != nil {
		return err
	}
	current, err := openRecoveryJournalDirectory(owner.path)
	if err != nil {
		return err
	}
	identity, err := imageStoreIdentity(current)
	if err := errors.Join(err, current.Close()); err != nil {
		return err
	}
	if identity != owner.identity {
		return ErrDenied
	}
	for _, excluded := range owner.exclusions {
		if err := owner.validateExclusion(excluded); err != nil {
			return err
		}
	}
	return nil
}

// validateExclusion rechecks both the selected name and original pinned directory.
//
// Takes excluded (imageStoreExclusion) holding the pinned handle and path.
//
// Returns failure when a grant or metadata tree could expose private image storage.
func (owner *LinuxImageStore) validateExclusion(excluded imageStoreExclusion) (result error) {
	current, err := openLinuxDirectory(excluded.path)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, current.Close()) }()
	original, err := imageStoreIdentity(excluded.file)
	if err != nil {
		return err
	}
	identity, err := imageStoreIdentity(current)
	if err != nil {
		return err
	}
	if identity != original {
		return ErrDenied
	}
	var other LinuxRecoveryJournal
	other.directory = excluded.file
	return rejectRecoveryStorageOverlap(owner.storage, &other)
}

// checkCapacity counts directory entries before granting another staging lease.
//
// Returns a limit error before repeated partial allocations can grow storage unbounded.
func (owner *LinuxImageStore) checkCapacity() (result error) {
	cursor, err := openLinuxBeneath(owner.storage.directory, ".", unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, cursor.Close()) }()
	names, err := cursor.Readdirnames(maximumImageStoreLeases)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(names)+len(owner.leases) >= maximumImageStoreLeases {
		return errLimit
	}
	return nil
}

// closeHandles aborts a partial constructor or releases fully drained storage.
//
// Returns all closure failures without deleting any user or recovery content.
func (owner *LinuxImageStore) closeHandles() error {
	var result error
	for _, excluded := range owner.exclusions {
		result = errors.Join(result, excluded.file.Close())
	}
	return errors.Join(result, owner.storage.Close())
}

// imageStoreExclusion pins one grant or metadata directory for overlap detection.
type imageStoreExclusion struct {
	// file is the pinned directory handle.
	file *os.File

	// path is the original host-selected absolute path.
	path string
}

// OpenLinuxImageStore locks empty staging storage and pins every excluded directory.
// Ancestry checks reject overlap in both directions and exact inode aliases.
//
// Takes directory (string) which is the host-selected private staging path.
// Takes grants ([]LinuxRootGrant) which are the approved broker root directories.
// Takes metadata ([]string) which are up to three private metadata store paths.
//
// Returns no owner on ambiguous storage, invalid policy, overlap or occupied storage.
func OpenLinuxImageStore(directory string, grants []LinuxRootGrant, metadata []string) (*LinuxImageStore, error) {
	var identity LinuxImageStoreIdentity
	return openLinuxImageStore(directory, grants, metadata, identity)
}

// ReopenLinuxImageStore claims only independently approved original image storage. The
// caller must prove original host and worker termination before orphan cleanup.
//
// Takes directory (string) which is the host-selected private staging path.
// Takes approved (LinuxImageStoreIdentity) which is the authenticated original directory
// identity.
// Takes grants ([]LinuxRootGrant) which are the current host-approved root directories.
// Takes metadata ([]string) which are the current private metadata store paths.
//
// Returns recovery-only ownership; staging remains closed until cleanup succeeds.
func ReopenLinuxImageStore(directory string, approved LinuxImageStoreIdentity, grants []LinuxRootGrant, metadata []string) (*LinuxImageStore, error) {
	if approved.Inode == 0 || approved.MountID == 0 {
		return nil, ErrInvalidPolicy
	}
	return openLinuxImageStore(directory, grants, metadata, approved)
}

// openLinuxImageStore verifies private ownership before exposing fresh or recovery state.
//
// Takes directory (string) which is the host-selected private staging path.
// Takes grants ([]LinuxRootGrant) which are the host-approved root directories to
// exclude.
// Takes metadata ([]string) which are the private metadata store paths to exclude.
// Takes approved (LinuxImageStoreIdentity) which is zero for fresh storage or a
// previously captured identity for recovery.
//
// Returns partial leases to the kernel on every failed claim without deleting contents.
func openLinuxImageStore(directory string, grants []LinuxRootGrant, metadata []string, approved LinuxImageStoreIdentity) (*LinuxImageStore, error) {
	if !validImageStorePath(directory) || len(metadata) > maximumImageStoreMetadata {
		return nil, ErrInvalidPolicy
	}
	var limits FilesystemLimits
	budget, err := newLinuxGrantBudget(grants, limits)
	if err != nil {
		return nil, err
	}
	budget.Close()
	var storage LinuxRecoveryJournal
	owner := &LinuxImageStore{
		storage: &storage, leases: make(map[*os.File]struct{}), path: directory,
		exclusions: nil, identity: approved, mutex: sync.Mutex{}, closed: false, recovery: approved.Inode != 0, cleaned: false,
	}
	if err := owner.open(grants, metadata); err != nil {
		return nil, errors.Join(err, owner.closeHandles())
	}
	return owner, nil
}

// imageStoreIdentity measures the original pinned directory using mandatory statx data.
//
// Takes directory (*os.File) which is the pinned staging parent.
//
// Returns no authority to reopen or delete resources by itself.
func imageStoreIdentity(directory *os.File) (identity LinuxImageStoreIdentity, result error) {
	pinned, err := openLinuxBeneath(directory, ".", unix.O_PATH|unix.O_DIRECTORY)
	if err != nil {
		return identity, err
	}
	defer func() {
		result = errors.Join(result, pinned.Close())
		if result != nil {
			identity = LinuxImageStoreIdentity{Device: 0, Inode: 0, MountID: 0}
		}
	}()
	captured, err := linuxBootstrapRootIdentity(pinned)
	if err != nil {
		return identity, err
	}
	if captured.Inode == 0 || captured.MountID == 0 {
		return identity, ErrDenied
	}
	return LinuxImageStoreIdentity{Device: captured.Device, Inode: captured.Inode, MountID: captured.MountID}, nil
}

// validImageStorePath bounds a host-selected absolute path before filesystem access.
//
// Takes path (string) which is the host-selected store directory.
//
// Returns bool which is true for a bounded absolute path.
func validImageStorePath(path string) bool {
	return len(path) <= maximumImageStorePathBytes && filepath.IsAbs(path)
}
