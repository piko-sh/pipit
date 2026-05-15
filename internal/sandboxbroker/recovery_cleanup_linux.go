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

// recoverLinuxStaging removes only an identity-matched private publication link. It must
// run in a separately supervised confined recovery process.
//
// Takes root (linuxRecoveryRoot) which is the original sealed root authority.
// Takes namespace (string) which is the sealed staging namespace.
// Takes record (RecoveryRecord) which is one validated journal record.
//
// Returns error on any mismatch without deleting the destination or a collision. An
// absent remnant still synchronises its parent, allowing a failed sync to retry.
func recoverLinuxStaging(root linuxRecoveryRoot, namespace string, record RecoveryRecord) (result error) {
	if !validRecoveryRecord(record) || namespace != record.Namespace || root.file == nil ||
		root.identity.Name != record.Root || root.identity.Rights&Write == 0 ||
		root.identity.Device != record.RootDevice || root.identity.Inode != record.RootInode ||
		root.identity.MountID != record.RootMountID {
		return ErrDenied
	}
	if err := verifyRecoveryDirectory(root.file, record.RootDevice, record.RootInode); err != nil {
		return err
	}
	parent, err := openLinuxBeneath(root.file, path.Dir(record.Path), unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, parent.Close()) }()
	if err := verifyRecoveryDirectory(parent, record.ParentDevice, record.ParentInode); err != nil {
		return err
	}
	name, err := StagingName(namespace, record.Operation)
	if err != nil {
		return err
	}
	return unlinkLinuxRecoveryEntry(parent, name, record)
}

// verifyRecoveryDirectory checks a pinned directory without resolving any new path.
//
// Takes directory (*os.File) which is an owned descriptor for the directory.
// Takes device (uint64) which is the expected original kernel device number.
// Takes inode (uint64) which is the expected original kernel inode number.
//
// Returns error when the descriptor no longer identifies the approved directory.
func verifyRecoveryDirectory(directory *os.File, device, inode uint64) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(directory.Fd()), &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || uint64(stat.Dev) != device || stat.Ino != inode {
		return ErrDenied
	}
	return nil
}

// unlinkLinuxRecoveryEntry checks a path-only inode before unlinking its private name.
// Directory trees must not be rearranged by other host processes during recovery.
//
// Takes parent (*os.File) which is a verified pinned parent directory.
// Takes name (string) which is the derived staging basename.
// Takes record (RecoveryRecord) which is the original inode record.
//
// Returns error without deleting symlinks, special files, extra links or replacements.
func unlinkLinuxRecoveryEntry(parent *os.File, name string, record RecoveryRecord) (result error) {
	pinned, err := openLinuxBeneath(parent, name, unix.O_PATH)
	if errors.Is(err, unix.ENOENT) {
		return parent.Sync()
	}
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, pinned.Close()) }()
	var stat unix.Stat_t
	if err := unix.Fstat(int(pinned.Fd()), &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&stagingPermissionMask != stagingFileMode ||
		stat.Nlink != 1 || uint64(stat.Dev) != record.Device || stat.Ino != record.Inode || stat.Size != record.Size {
		return ErrDenied
	}
	if err := unix.Unlinkat(int(parent.Fd()), name, 0); err != nil {
		return err
	}
	return parent.Sync()
}
