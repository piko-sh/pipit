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

package sandboxlinux

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// cgroupDirectoryID holds the device, inode and mount identity of a pinned cgroup
// directory.
type cgroupDirectoryID struct {
	// Inode holds the directory inode number.
	Inode uint64

	// Mount holds the kernel mount identifier.
	Mount uint64

	// Major holds the device major number.
	Major uint32

	// Minor holds the device minor number.
	Minor uint32
}

// verifyDirectoryEntry checks that the cleanup name still denotes the pinned group. The
// trusted delegated parent must not be concurrently rearranged by other hosts.
//
// Returns failure without adopting or removing a replacement at the original name.
func (group *Group) verifyDirectoryEntry() (result error) {
	if group.parent == nil || group.directory == nil || group.name == "" ||
		group.name == "." || group.name == ".." || strings.ContainsAny(group.name, "/\x00") {
		return ErrUnavailable
	}
	original, err := cgroupDirectoryIdentity(group.directory)
	if err != nil {
		return err
	}
	if _, err := cgroupDirectoryIdentity(group.parent); err != nil {
		return err
	}
	current, err := openDirectory(int(group.parent.Fd()), group.name)
	if err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	defer func() { result = errors.Join(result, current.Close()) }()
	identity, err := cgroupDirectoryIdentity(current)
	if err != nil {
		return err
	}
	if identity != original {
		return ErrUnavailable
	}
	return nil
}

// cgroupDirectoryIdentity measures a pinned directory on a verified cgroup-v2 mount.
//
// Takes directory (*os.File) which is the retained kernel directory handle, not an
// identity supplied by a pathname.
//
// Returns device, inode and mount identity, rejecting missing kernel metadata.
func cgroupDirectoryIdentity(directory *os.File) (cgroupDirectoryID, error) {
	var empty cgroupDirectoryID
	if directory == nil {
		return empty, ErrUnavailable
	}
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(int(directory.Fd()), &filesystem); err != nil || filesystem.Type != unix.CGROUP2_SUPER_MAGIC {
		return empty, errors.Join(ErrUnavailable, err)
	}
	var status unix.Statx_t
	const required = unix.STATX_TYPE | unix.STATX_INO | unix.STATX_MNT_ID
	if err := unix.Statx(int(directory.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, required, &status); err != nil {
		return empty, errors.Join(ErrUnavailable, err)
	}
	if status.Mask&required != required || status.Mode&unix.S_IFMT != unix.S_IFDIR || status.Ino == 0 || status.Mnt_id == 0 {
		return empty, ErrUnavailable
	}
	return cgroupDirectoryID{Major: status.Dev_major, Minor: status.Dev_minor, Inode: status.Ino, Mount: status.Mnt_id}, nil
}
