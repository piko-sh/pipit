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
	"io"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

const (
	// workerProcFD is the inherited descriptor number for the host procfs root.
	workerProcFD = 4

	// descriptorBatchSize is the number of entries read per readdir batch.
	descriptorBatchSize = 128

	// maximumBootstrapDescriptors is the upper bound on descriptors before the audit aborts.
	maximumBootstrapDescriptors = 4096
)

// OpenWorkerProc opens the host procfs root solely for trusted bootstrap.
//
// Returns *os.File which is the procfs root descriptor.
// Returns error when procfs cannot be opened or verified.
func OpenWorkerProc() (*os.File, error) {
	descriptor, err := unix.Open("/proc", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: opening bootstrap procfs: %w", ErrUnavailable, err)
	}
	if err := checkWorkerProc(descriptor); err != nil {
		_ = unix.Close(descriptor)
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), "worker-bootstrap-proc"), nil
}

// sealWorkerDescriptors removes inherited authority before untrusted input. It validates
// fixed channels, enumerates the descriptor table and closes every non-close-on-exec
// descriptor above the IPC slot.
//
// Returns error which reports any validation, enumeration or close failure.
func sealWorkerDescriptors() (result error) {
	if os.Getpid() != 1 || os.Geteuid() != 0 {
		return fmt.Errorf("%w: descriptor sealing requires namespace init", ErrUnavailable)
	}
	if err := checkWorkerChannels(); err != nil {
		return err
	}
	directory, err := openOwnProcSelfFd()
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, directory.Close()) }()
	if err := closeInheritedDescriptors(directory); err != nil {
		return err
	}
	if err := unix.Close(0); err != nil && !errors.Is(err, unix.EBADF) {
		return fmt.Errorf("%w: closing worker stdin: %w", ErrUnavailable, err)
	}
	return nil
}

// openOwnProcSelfFd mounts a fresh procfs and returns its self/fd directory so the
// process can enumerate descriptors without inheriting a host procfs handle.
//
// Returns *os.File which is the readable self/fd directory.
// Returns error when procfs cannot be mounted or opened.
func openOwnProcSelfFd() (*os.File, error) {
	filesystem, err := unix.Fsopen("proc", unix.FSOPEN_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("%w: opening procfs: %w", ErrUnavailable, err)
	}
	defer unix.Close(filesystem)
	if err := unix.FsconfigCreate(filesystem); err != nil {
		return nil, fmt.Errorf("%w: creating procfs: %w", ErrUnavailable, err)
	}
	mount, err := unix.Fsmount(filesystem, unix.FSMOUNT_CLOEXEC,
		unix.MOUNT_ATTR_RDONLY|unix.MOUNT_ATTR_NOSUID|unix.MOUNT_ATTR_NODEV|unix.MOUNT_ATTR_NOEXEC)
	if err != nil {
		return nil, fmt.Errorf("%w: mounting procfs: %w", ErrUnavailable, err)
	}
	defer unix.Close(mount)
	descriptor, err := unix.Openat(mount, "self/fd", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: opening descriptor table: %w", ErrUnavailable, err)
	}
	return os.NewFile(uintptr(descriptor), "worker-descriptors"), nil
}

// checkWorkerProc ensures a bootstrap handle refers to a procfs directory.
//
// Takes descriptor (int) which is the bootstrap procfs handle.
//
// Returns error for any other filesystem or descriptor type.
func checkWorkerProc(descriptor int) error {
	var filesystem unix.Statfs_t
	var status unix.Stat_t
	if err := unix.Fstatfs(descriptor, &filesystem); err != nil || filesystem.Type != unix.PROC_SUPER_MAGIC {
		return fmt.Errorf("%w: bootstrap descriptor is not procfs", ErrUnavailable)
	}
	if err := unix.Fstat(descriptor, &status); err != nil || status.Mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("%w: bootstrap procfs is not a directory", ErrUnavailable)
	}
	return nil
}

// checkWorkerChannels verifies the fixed output and protocol descriptor roles.
//
// Returns error unless output is write-only pipes and IPC a connected Unix stream.
func checkWorkerChannels() error {
	for _, descriptor := range []int{1, 2} {
		if err := checkWorkerOutput(descriptor); err != nil {
			return err
		}
	}
	return checkWorkerIPC(workerIPCFD)
}

// checkWorkerOutput checks one output descriptor without consuming any bytes.
//
// Takes descriptor (int) which must be a write-only pipe.
//
// Returns error for a different descriptor type or access mode.
func checkWorkerOutput(descriptor int) error {
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil || status.Mode&unix.S_IFMT != unix.S_IFIFO {
		return fmt.Errorf("%w: worker output must use pipes", ErrUnavailable)
	}
	flags, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_WRONLY {
		return fmt.Errorf("%w: worker output pipe is not write-only", ErrUnavailable)
	}
	return nil
}

// checkWorkerIPC checks the private protocol transport without reading it.
//
// Takes descriptor (int) which must be a connected Unix stream socket.
//
// Returns error for an unsupported protocol transport.
func checkWorkerIPC(descriptor int) error {
	domain, err := unix.GetsockoptInt(descriptor, unix.SOL_SOCKET, unix.SO_DOMAIN)
	if err != nil || domain != unix.AF_UNIX {
		return fmt.Errorf("%w: worker IPC must use a Unix socket", ErrUnavailable)
	}
	kind, err := unix.GetsockoptInt(descriptor, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || kind != unix.SOCK_STREAM {
		return fmt.Errorf("%w: worker IPC must use a stream socket", ErrUnavailable)
	}
	if _, err := unix.Getpeername(descriptor); err != nil {
		return fmt.Errorf("%w: worker IPC must be connected: %w", ErrUnavailable, err)
	}
	return nil
}

// closeInheritedDescriptors scans the actual kernel table with a bounded budget.
//
// Takes directory (*os.File) which enumerates the current worker's descriptors.
//
// Returns error if any unexpected inherited descriptor cannot be removed.
func closeInheritedDescriptors(directory *os.File) error {
	count := 0
	for {
		names, err := directory.Readdirnames(descriptorBatchSize)
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: enumerating descriptors: %w", ErrUnavailable, err)
		}
		count += len(names)
		if count > maximumBootstrapDescriptors {
			return fmt.Errorf("%w: too many bootstrap descriptors", ErrUnavailable)
		}
		for _, name := range names {
			if err := closeInheritedDescriptor(name); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

// closeInheritedDescriptor removes one unexpected descriptor inherited by exec.
//
// Takes name (string) which is a numeric entry from the worker descriptor table.
//
// Returns error for an invalid entry or a failed descriptor operation.
func closeInheritedDescriptor(name string) error {
	descriptor, err := strconv.Atoi(name)
	if err != nil || descriptor < 0 {
		return fmt.Errorf("%w: invalid kernel descriptor entry", ErrUnavailable)
	}
	if descriptor <= workerIPCFD {
		return nil
	}
	flags, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0)
	if errors.Is(err, unix.EBADF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: checking inherited descriptor: %w", ErrUnavailable, err)
	}
	if flags&unix.FD_CLOEXEC == 0 {
		if err := unix.Close(descriptor); err != nil {
			return fmt.Errorf("%w: closing inherited descriptor: %w", ErrUnavailable, err)
		}
	}
	return nil
}
