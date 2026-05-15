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
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	// orphanImagePrefix is the required prefix for orphan image root directory names.
	orphanImagePrefix = "pipit-worker-"

	// orphanImageTokenBytes is the length of the random suffix in orphan image names.
	orphanImageTokenBytes = 26

	// orphanImageExecutable is the sole permitted file name inside an orphan image root.
	orphanImageExecutable = "worker"

	// maximumOrphanImageBytes is the upper bound on a staged executable file size.
	maximumOrphanImageBytes = (128 << 20) + 1

	// orphanImageExecutableMode is the permission mask for a staged executable.
	orphanImageExecutableMode = 0500
)

// orphanImageRoot holds pinned handles for one image root pending cleanup.
type orphanImageRoot struct {
	// directory is the pinned orphan staging directory.
	directory *os.File

	// executable is the pinned staged file, or nil when absent.
	executable *os.File

	// name is the directory entry name in the parent store.
	name string

	// identity holds the kernel identity of the staging directory.
	identity LinuxImageStoreIdentity

	// fileIdentity holds the kernel identity of the staged file.
	fileIdentity LinuxImageStoreIdentity
}

// pinExecutable recognises an empty partial root or its sole staged regular file.
//
// Returns failure for additional entries, links, oversized files or mount crossings.
func (root *orphanImageRoot) pinExecutable() error {
	names, err := root.directory.Readdirnames(2)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(names) > 1 || len(names) == 1 && names[0] != orphanImageExecutable {
		return ErrDenied
	}
	if len(names) == 0 {
		return nil
	}
	root.executable, err = openLinuxBeneath(root.directory, orphanImageExecutable, unix.O_PATH|unix.O_NOFOLLOW)
	if err != nil {
		return err
	}
	root.fileIdentity, err = orphanExecutableIdentity(root.executable)
	if err != nil {
		return err
	}
	if root.fileIdentity.Device != root.identity.Device || root.fileIdentity.MountID != root.identity.MountID {
		return ErrDenied
	}
	return nil
}

// close releases a plan's pins without deleting or adopting any resource.
//
// Returns error when closing any pinned handle fails.
func (root *orphanImageRoot) close() error {
	var result error
	if root.executable != nil {
		result = root.executable.Close()
	}
	if root.directory != nil {
		result = errors.Join(result, root.directory.Close())
	}
	return result
}

// CleanupOrphans removes only bounded image roots beneath authenticated original storage.
// The native owner must first prove original service termination and retain all leases.
//
// Takes a cleanup context after original host and worker termination.
//
// Returns failure with storage ownership retained and new staging admission closed.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxImageStore) CleanupOrphans(ctx context.Context) error {
	if owner == nil {
		return ErrClosed
	}
	if ctx == nil {
		return ErrInvalidPolicy
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if err := owner.validate(); err != nil {
		return err
	}
	if !owner.recovery || len(owner.leases) != 0 {
		return ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	owner.cleaned = false
	if err := owner.cleanupOrphans(ctx); err != nil {
		return err
	}
	if err := owner.storage.directory.Sync(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	owner.cleaned = true
	return nil
}

// cleanupOrphans validates the complete bounded plan before the first deletion.
//
// Returns all removal and handle closure failures without recursively deleting entries.
func (owner *LinuxImageStore) cleanupOrphans(ctx context.Context) (result error) {
	plan, err := owner.orphanPlan(ctx)
	defer func() {
		for _, root := range plan {
			result = errors.Join(result, root.close())
		}
	}()
	if err != nil {
		return err
	}
	for _, root := range plan {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := owner.removeOrphan(root); err != nil {
			return err
		}
	}
	_, err = owner.storage.metadataEntries()
	return err
}

// orphanPlan pins at most the original finite image-store capacity.
//
// Returns partially pinned ownership to its caller even when a later entry is invalid.
func (owner *LinuxImageStore) orphanPlan(ctx context.Context) (plan []orphanImageRoot, result error) {
	cursor, err := openLinuxBeneath(owner.storage.directory, ".", unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, cursor.Close()) }()
	names, err := cursor.Readdirnames(maximumImageStoreLeases + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > maximumImageStoreLeases {
		return nil, errLimit
	}
	plan = make([]orphanImageRoot, 0, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		root, err := owner.pinOrphan(name)
		plan = append(plan, root)
		if err != nil {
			return plan, err
		}
	}
	return plan, nil
}

// pinOrphan recognises only the private single-executable staging layout.
//
// Takes name (string) which is the directory entry name in the parent store.
//
// Returns pinned original entries, never a followed symlink or a nested tree.
func (owner *LinuxImageStore) pinOrphan(name string) (root orphanImageRoot, result error) {
	root.name = name
	if !validOrphanImageName(name) {
		return root, ErrDenied
	}
	root.directory, result = openLinuxBeneath(owner.storage.directory, name, unix.O_RDONLY|unix.O_DIRECTORY)
	if result != nil {
		return root, result
	}
	if err := checkJournalDirectory(root.directory); err != nil {
		return root, err
	}
	root.identity, result = imageStoreIdentity(root.directory)
	if result != nil {
		return root, result
	}
	if root.identity.Device != owner.identity.Device || root.identity.MountID != owner.identity.MountID {
		return root, ErrDenied
	}
	err := root.pinExecutable()
	return root, err
}

// removeOrphan rechecks each pinned entry immediately before descriptor-relative unlink.
//
// Takes root (orphanImageRoot) holding the pinned handles and identity.
//
// Returns failure if a name now denotes a different object or unsafe layout.
func (owner *LinuxImageStore) removeOrphan(root orphanImageRoot) (result error) {
	current, err := owner.pinOrphan(root.name)
	defer func() { result = errors.Join(result, current.close()) }()
	if err != nil {
		return err
	}
	if current.identity != root.identity || current.fileIdentity != root.fileIdentity {
		return ErrDenied
	}
	if root.executable != nil {
		if err := unix.Unlinkat(int(root.directory.Fd()), orphanImageExecutable, 0); err != nil {
			return err
		}
	}
	if err := root.directory.Sync(); err != nil {
		return err
	}
	return unix.Unlinkat(int(owner.storage.directory.Fd()), root.name, unix.AT_REMOVEDIR)
}

// orphanExecutableIdentity accepts sealed or partially staged private regular files.
//
// Takes file (*os.File) which is the pinned staged executable.
//
// Returns mandatory inode and mount identity without reading executable contents.
func orphanExecutableIdentity(file *os.File) (LinuxImageStoreIdentity, error) {
	var empty LinuxImageStoreIdentity
	var status unix.Statx_t
	const required = unix.STATX_INO | unix.STATX_MNT_ID | unix.STATX_TYPE | unix.STATX_MODE | unix.STATX_UID | unix.STATX_NLINK | unix.STATX_SIZE
	if err := unix.Statx(int(file.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, required, &status); err != nil {
		return empty, err
	}
	mode := status.Mode & stagingPermissionMask
	if status.Mask&required != required || status.Mode&unix.S_IFMT != unix.S_IFREG ||
		int(status.Uid) != os.Geteuid() || status.Nlink != 1 || status.Size > maximumOrphanImageBytes ||
		mode != stagingFileMode && mode != orphanImageExecutableMode || status.Ino == 0 || status.Mnt_id == 0 {
		return empty, ErrDenied
	}
	return LinuxImageStoreIdentity{Device: unix.Mkdev(status.Dev_major, status.Dev_minor), Inode: status.Ino, MountID: status.Mnt_id}, nil
}

// validOrphanImageName accepts only the host image format's fixed random basename.
//
// Takes name (string) which is the directory entry to validate.
//
// Returns bool which is true for a well-formed orphan image name.
func validOrphanImageName(name string) bool {
	if !strings.HasPrefix(name, orphanImagePrefix) || len(name) != len(orphanImagePrefix)+orphanImageTokenBytes {
		return false
	}
	for _, character := range name[len(orphanImagePrefix):] {
		if character < 'A' || character > 'Z' {
			if character < '2' || character > '7' {
				return false
			}
		}
	}
	return true
}
