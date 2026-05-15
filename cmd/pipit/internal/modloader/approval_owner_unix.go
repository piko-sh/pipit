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

//go:build unix

package modloader

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

const (
	// approvalPublicPermissions is the mask for group and other bits that make an approval
	// file publicly visible.
	approvalPublicPermissions = 0o077

	// approvalDirectoryWritePermissions is the mask for group and other write bits on an
	// approval directory.
	approvalDirectoryWritePermissions = 0o022
)

// syncApprovalDirectory makes publication durable on supporting local Unix storage.
//
// Takes root (*os.Root) which is the pinned transaction directory.
//
// Returns error which joins sync and close failures without claiming success on
// unsupported storage.
func syncApprovalDirectory(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

// openApprovalFile opens a single component relative to a pinned directory. Native openat
// is used because os.Root may resolve a terminal symlink itself.
//
// Takes root (*os.Root) which is the owned directory handle.
// Takes name (string) which is the target basename.
// Takes flags (int) which holds the trusted open flags.
// Takes mode (fs.FileMode) which sets the creation permission bits.
//
// Returns *os.File which is an independent descriptor opened without following the final
// component.
// Returns error which reports open or ownership failure.
func openApprovalFile(root *os.Root, name string, flags int, mode fs.FileMode) (*os.File, error) {
	directory, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	if err := validateApprovalDirectoryOwner(directory); err != nil {
		return nil, errors.Join(err, directory.Close())
	}
	descriptor, openErr := unix.Openat(int(directory.Fd()), name,
		flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_NOCTTY|unix.O_CLOEXEC, uint32(mode.Perm()))
	if err := errors.Join(openErr, directory.Close()); err != nil {
		if descriptor >= 0 {
			err = errors.Join(err, unix.Close(descriptor))
		}
		return nil, err
	}
	if descriptor < 0 {
		return nil, fs.ErrInvalid
	}
	return os.NewFile(uintptr(descriptor), name), nil
}

// validateApprovalDirectoryOwner refuses directories replaceable by other users.
//
// Takes directory (*os.File) which is the pinned containing directory, not a pathname to
// re-open.
//
// Returns error for foreign ownership or group/other write permission.
func validateApprovalDirectoryOwner(directory *os.File) error {
	var metadata unix.Stat_t
	if err := unix.Fstat(int(directory.Fd()), &metadata); err != nil {
		return err
	}
	if int64(metadata.Uid) != int64(os.Geteuid()) || metadata.Mode&approvalDirectoryWritePermissions != 0 {
		return fs.ErrPermission
	}
	return nil
}

// validateApprovalFileOwner checks the opened approval inode, not its pathname.
//
// Takes file (*os.File) which is the descriptor that supplies the approval bytes.
//
// Returns error unless the file is privately owned by the current effective user.
func validateApprovalFileOwner(file *os.File) error {
	var metadata unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &metadata); err != nil {
		return err
	}
	if int64(metadata.Uid) != int64(os.Geteuid()) || metadata.Nlink != 1 || metadata.Mode&approvalPublicPermissions != 0 {
		return fs.ErrPermission
	}
	return nil
}
