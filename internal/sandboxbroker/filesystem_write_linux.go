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
	"strconv"

	"golang.org/x/sys/unix"
)

// stagingFileMode is the permission mask for anonymous staging inodes.
const stagingFileMode = 0600

// write stages a new private inode, then atomically replaces one directory entry.
// Existing files are never truncated or modified in place.
//
// Takes root (*os.File) which is the pinned grant directory from a valid reservation.
// Takes name (string) which is the validated relative path from a valid reservation.
// Takes data ([]byte) which is the bounded write payload from a valid reservation.
// Takes identity (uint64) which is the admitted broker call identifier.
//
// Returns the number of staged bytes and any staging, publication or cleanup error.
func (backend *LinuxFilesystem) write(root *os.File, name string, data []byte, identity uint64) (written int, result error) {
	prepared, err := prepareLinuxWrite(root, name, data, identity)
	if prepared != nil {
		written = prepared.written
		defer func() { result = errors.Join(result, prepared.Close()) }()
	}
	if err != nil {
		return written, err
	}
	return written, prepared.commit(backend)
}

// publish links a fully staged inode and atomically renames it over the target. A failure
// after rename may report an error even though publication happened.
//
// Takes parent (*os.File) which is the backend-owned pinned parent directory.
// Takes temporary (*os.File) which is the anonymous staging inode to publish.
// Takes base (string) which is the target entry name within the parent.
// Takes identity (uint64) which is the admitted broker call identifier.
//
// Returns error on linking, renaming, directory synchronisation or cleanup.
func (backend *LinuxFilesystem) publish(parent, temporary *os.File, base string, identity uint64) (result error) {
	staging, err := StagingName(backend.namespace, identity)
	if err != nil {
		return err
	}
	descriptorName := strconv.FormatUint(uint64(temporary.Fd()), 10)
	if err := unix.Linkat(int(backend.proc.Fd()), descriptorName, int(parent.Fd()), staging, unix.AT_SYMLINK_FOLLOW); err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			result = errors.Join(result, unix.Unlinkat(int(parent.Fd()), staging, 0))
		}
	}()
	if err := unix.Renameat2(int(parent.Fd()), staging, int(parent.Fd()), base, 0); err != nil {
		return err
	}
	published = true
	return parent.Sync()
}

// validateLinuxWriteTarget permits absence or a single-link regular target. O_PATH
// inspection does not open a device or FIFO for I/O.
//
// Takes parent (*os.File) which is the pinned directory selected by validated resolution.
// Takes base (string) which is the target entry name selected by validated resolution.
//
// Returns error for symlinks, mount crossings, non-regular files or hard links.
func validateLinuxWriteTarget(parent *os.File, base string) (result error) {
	target, err := openLinuxBeneath(parent, base, unix.O_PATH|unix.O_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, target.Close()) }()
	var stat unix.Stat_t
	if err := unix.Fstat(int(target.Fd()), &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
		return ErrDenied
	}
	return nil
}

// createLinuxStagingFile creates an unnamed regular inode on the target mount. The inode
// disappears on close unless publish links it into the approved tree.
//
// Takes parent (*os.File) owned by the broker.
//
// Returns an owned private file, or error without a named partial write.
func createLinuxStagingFile(parent *os.File) (*os.File, error) {
	descriptor, err := unix.Openat2(int(parent.Fd()), ".", &unix.OpenHow{
		Flags: unix.O_TMPFILE | unix.O_WRONLY | unix.O_CLOEXEC,
		Mode:  stagingFileMode, Resolve: linuxResolveFlags,
	})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), "broker-stage"), nil
}
