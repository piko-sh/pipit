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
	"os"
	"path"

	"golang.org/x/sys/unix"
)

// stagingPermissionMask is the full permission bit range for staged file inspection.
const stagingPermissionMask = 07777

// stagedWriteIdentity captures the kernel identity of a staged inode and its parent.
type stagedWriteIdentity struct {
	// ParentDevice is the kernel device number of the parent directory.
	ParentDevice uint64

	// ParentInode is the kernel inode number of the parent directory.
	ParentInode uint64

	// Device is the kernel device number of the staged inode.
	Device uint64

	// Inode is the kernel inode number of the staged inode.
	Inode uint64

	// Size is the byte length of the staged inode.
	Size int64
}

// linuxPreparedWrite owns an anonymous staged inode before publication.
type linuxPreparedWrite struct {
	// parent is the pinned grant parent directory.
	parent *os.File

	// temporary is the anonymous staging inode.
	temporary *os.File

	// base is the target entry name within the parent.
	base string

	// identity holds the kernel identity captured after staging.
	identity stagedWriteIdentity

	// operation is the admitted broker call identifier.
	operation uint64

	// written counts the staged bytes.
	written int

	// closed is true after Close releases both handles.
	closed bool
}

// Close abandons the anonymous inode and closes the pinned parent idempotently. The
// caller must serialise this private owner with commit.
//
// Returns closure errors without deleting any named filesystem entry.
func (prepared *linuxPreparedWrite) Close() error {
	if prepared == nil || prepared.closed {
		return nil
	}
	prepared.closed = true
	var result error
	if prepared.temporary != nil {
		result = prepared.temporary.Close()
	}
	return errors.Join(result, prepared.parent.Close())
}

// commit checks the prepared inode again before linking or changing the destination. This
// private operation does not itself prove a durable host recovery record exists.
//
// Takes backend (*LinuxFilesystem) which is the sealed backend while its operation lock
// remains exclusively held.
//
// Returns an error after closing ownership; publication errors do not imply rollback.
func (prepared *linuxPreparedWrite) commit(backend *LinuxFilesystem) (result error) {
	if prepared == nil || prepared.closed {
		return ErrClosed
	}
	defer func() { result = errors.Join(result, prepared.Close()) }()
	if backend == nil || prepared.temporary == nil {
		return ErrInvalidPolicy
	}
	current, err := inspectPreparedWrite(prepared.parent, prepared.temporary)
	if err != nil {
		return err
	}
	if current != prepared.identity {
		return ErrDenied
	}
	if err := validateLinuxWriteTarget(prepared.parent, prepared.base); err != nil {
		return err
	}
	return backend.publish(prepared.parent, prepared.temporary, prepared.base, prepared.operation)
}

// prepareLinuxWrite creates and synchronises an anonymous inode without publication. No
// named staging entry or destination change occurs during preparation.
//
// Takes root (*os.File) which is the pinned grant root directory.
// Takes name (string) which is the validated relative path beneath the root.
// Takes data ([]byte) which is the bounded write payload.
// Takes operation (uint64) which is the admitted broker call identity.
//
// Returns cleanup ownership even when preparation fails partway through.
func prepareLinuxWrite(root *os.File, name string, data []byte, operation uint64) (*linuxPreparedWrite, error) {
	if root == nil || !validRelativePath(name, false) || len(data) > maximumFileBytes || operation == 0 {
		return nil, ErrInvalidPolicy
	}
	parentName, base := path.Split(name)
	if parentName == "" {
		parentName = "."
	} else {
		parentName = parentName[:len(parentName)-1]
	}
	parent, err := openLinuxBeneath(root, parentName, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, err
	}
	prepared := &linuxPreparedWrite{
		parent: parent, temporary: nil, base: base,
		identity:  stagedWriteIdentity{ParentDevice: 0, ParentInode: 0, Device: 0, Inode: 0, Size: 0},
		operation: operation, written: 0, closed: false,
	}
	if err := validateLinuxWriteTarget(parent, base); err != nil {
		return prepared, err
	}
	prepared.temporary, err = createLinuxStagingFile(parent)
	if err != nil {
		return prepared, err
	}
	prepared.written, err = prepared.temporary.Write(data)
	if err != nil {
		return prepared, err
	}
	if err := prepared.temporary.Sync(); err != nil {
		return prepared, err
	}
	prepared.identity, err = inspectPreparedWrite(parent, prepared.temporary)
	if err != nil {
		return prepared, err
	}
	if prepared.identity.Size != int64(prepared.written) {
		return prepared, ErrDenied
	}
	return prepared, nil
}

// inspectPreparedWrite identifies a private, unlinked regular inode and its parent. It
// uses descriptor-only stat calls already allowed by the broker filter.
//
// Takes parent (*os.File) which is the pinned grant parent directory descriptor.
// Takes temporary (*os.File) which is the anonymous staging inode descriptor.
//
// Returns exact inode identity only for the expected private staging form.
func inspectPreparedWrite(parent, temporary *os.File) (stagedWriteIdentity, error) {
	if parent == nil || temporary == nil {
		return stagedWriteIdentity{}, ErrInvalidPolicy
	}
	var directory, file unix.Stat_t
	if err := unix.Fstat(int(parent.Fd()), &directory); err != nil {
		return stagedWriteIdentity{}, err
	}
	if err := unix.Fstat(int(temporary.Fd()), &file); err != nil {
		return stagedWriteIdentity{}, err
	}
	if directory.Mode&unix.S_IFMT != unix.S_IFDIR || file.Mode&unix.S_IFMT != unix.S_IFREG ||
		file.Mode&stagingPermissionMask != stagingFileMode || file.Nlink != 0 || file.Dev != directory.Dev ||
		file.Size < 0 || file.Size > maximumFileBytes {
		return stagedWriteIdentity{}, ErrDenied
	}
	return stagedWriteIdentity{
		ParentDevice: uint64(directory.Dev), ParentInode: directory.Ino, Device: uint64(file.Dev), Inode: file.Ino, Size: file.Size,
	}, nil
}
