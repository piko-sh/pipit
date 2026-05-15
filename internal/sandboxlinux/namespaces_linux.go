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

package sandboxlinux

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/safeconv"
)

// workerRootMode is the permission mode for a private worker root directory.
const workerRootMode = 0700

// WorkerAttributes constructs the mandatory namespace and chroot settings. The host must
// own a private root containing only its approved static worker.
//
// Takes root (string) which names the host-prepared, private worker directory.
//
// Returns *syscall.SysProcAttr which must not be weakened before launch.
// Returns error when the root is invalid or namespace setup fails.
func WorkerAttributes(root string) (*syscall.SysProcAttr, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" {
		return nil, fmt.Errorf("%w: worker root must be a private absolute directory", ErrUnavailable)
	}
	directory, err := openDirectory(unix.AT_FDCWD, root)
	if err != nil {
		return nil, fmt.Errorf("%w: opening private worker root: %w", ErrUnavailable, err)
	}
	info, statErr := directory.Stat()
	err = errors.Join(statErr, directory.Close())
	if err != nil || !info.IsDir() || info.Mode().Perm() != workerRootMode {
		return nil, fmt.Errorf("%w: worker root must be a private directory", ErrUnavailable)
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != safeconv.IntToUint32(os.Geteuid()) {
		return nil, fmt.Errorf("%w: worker root must belong to the host user", ErrUnavailable)
	}
	return &syscall.SysProcAttr{
		Chroot:     root,
		Credential: &syscall.Credential{Uid: 0, Gid: 0, Groups: nil, NoSetGroups: true},
		Ptrace:     false, Setsid: true, Setpgid: false, Setctty: false, Noctty: false,
		Ctty: 0, Foreground: false, Pgid: 0, Pdeathsig: 0,
		Cloneflags: unix.CLONE_NEWUSER | unix.CLONE_NEWPID | unix.CLONE_NEWNET |
			unix.CLONE_NEWIPC | unix.CLONE_NEWUTS | unix.CLONE_NEWCGROUP,
		Unshareflags:               unix.CLONE_NEWNS,
		UidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Geteuid(), Size: 1}},
		GidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getegid(), Size: 1}},
		GidMappingsEnableSetgroups: false, AmbientCaps: nil,
		UseCgroupFD: false, CgroupFD: 0, PidFD: nil,
	}, nil
}

// sealWorkerFilesystem creates a detached read-only, nosuid, nodev root mount. Call only
// in a disposable worker launched with WorkerAttributes and cwd "/".
//
// Returns error unless the mount is installed and its flags are verified. Every failure
// is terminal for the worker; there is no writable-root fallback.
func sealWorkerFilesystem() error {
	if os.Getpid() != 1 || os.Geteuid() != 0 {
		return fmt.Errorf("%w: filesystem sealing requires namespace init", ErrUnavailable)
	}
	if err := checkWorkerRoot(); err != nil {
		return err
	}
	descriptor, err := unix.OpenTree(unix.AT_FDCWD, "/", unix.OPEN_TREE_CLONE|unix.OPEN_TREE_CLOEXEC)
	if err != nil {
		return fmt.Errorf("%w: cloning worker root: %w", ErrUnavailable, err)
	}
	defer unix.Close(descriptor)
	attributes := unix.MountAttr{
		Attr_set: unix.MOUNT_ATTR_RDONLY | unix.MOUNT_ATTR_NOSUID | unix.MOUNT_ATTR_NODEV,
		Attr_clr: 0, Propagation: 0, Userns_fd: 0,
	}
	if err := unix.MountSetattr(descriptor, "", unix.AT_EMPTY_PATH, &attributes); err != nil {
		return fmt.Errorf("%w: sealing root mount: %w", ErrUnavailable, err)
	}
	if err := unix.Fchdir(descriptor); err != nil {
		return fmt.Errorf("%w: entering root mount: %w", ErrUnavailable, err)
	}
	if err := unix.Chroot("."); err != nil {
		return fmt.Errorf("%w: pinning root mount: %w", ErrUnavailable, err)
	}
	var filesystem unix.Statfs_t
	if err := unix.Statfs("/", &filesystem); err != nil {
		return fmt.Errorf("%w: checking root mount: %w", ErrUnavailable, err)
	}
	const required = unix.ST_RDONLY | unix.ST_NOSUID | unix.ST_NODEV
	if filesystem.Flags&required != required {
		return fmt.Errorf("%w: root mount flags are incomplete", ErrUnavailable)
	}
	return nil
}

// checkWorkerRoot checks the minimal bootstrap filesystem before mounting.
//
// Returns error unless cwd is "/" and the root contains only a regular worker.
func checkWorkerRoot() error {
	directory, err := os.Getwd()
	if err != nil || directory != "/" {
		return fmt.Errorf("%w: worker cwd is not its root", ErrUnavailable)
	}
	root, err := os.Open("/")
	if err != nil {
		return fmt.Errorf("%w: opening worker root: %w", ErrUnavailable, err)
	}
	defer root.Close()
	return checkWorkerRootContents(root)
}

// checkWorkerRootContents performs a bounded minimal-root inspection.
//
// Takes root (*os.File) which is an open directory positioned at its start.
//
// Returns error unless the only entry is a regular worker executable.
func checkWorkerRootContents(root *os.File) error {
	entries, err := root.ReadDir(2)
	if err != nil || len(entries) != 1 || entries[0].Name() != "worker" {
		return fmt.Errorf("%w: worker root is not minimal", ErrUnavailable)
	}
	info, err := entries[0].Info()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: worker executable is not a regular file", ErrUnavailable)
	}
	return nil
}
