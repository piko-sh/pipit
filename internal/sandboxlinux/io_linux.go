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
	// maximumWakeDescriptors is the upper bound on runtime eventfd handles.
	maximumWakeDescriptors = 8

	// descriptorNumberBits is the bit width for parsing descriptor numbers.
	descriptorNumberBits = 31

	// bootstrapUmask is the restrictive umask every bootstrapped process installs before
	// sealing, so created files never carry group or other permission bits.
	bootstrapUmask = 0o077
)

// WorkerIO owns the prepared IPC stream and privately verified runtime I/O roles.
// Construct it before SealWorkerDescriptors consumes the bootstrap procfs handle.
type WorkerIO struct {
	// stream holds the private IPC transport socket.
	stream *os.File

	// wakeDescriptors holds the verified runtime eventfd numbers.
	wakeDescriptors []uint32
}

// Stream returns the private IPC transport for the trusted worker loop. The worker must
// not expose this handle or the bootstrap object to scripts.
//
// Returns *os.File which supports deadlines and concurrent close.
func (workerIO *WorkerIO) Stream() *os.File {
	return workerIO.stream
}

// installSyscallFilter installs the runtime-aware profile after all other sealing. The
// caller must close Stream when the worker exits or its protocol terminates.
//
// Returns error unless the prepared runtime roles and filter can be installed.
func (workerIO *WorkerIO) installSyscallFilter() error {
	if workerIO.stream == nil || len(workerIO.wakeDescriptors) == 0 {
		return fmt.Errorf("%w: missing prepared worker I/O", ErrUnavailable)
	}
	filter, err := workerFilterWithWakeDescriptors(unix.Getpid(), workerIO.wakeDescriptors)
	if err != nil {
		return err
	}
	return installWorkerFilter(filter)
}

// prepareWorkerIO initialises Go's poller before enumerating its wake-up handles. Only
// locally created, close-on-exec eventfds qualify for extra syscall access.
//
// Returns *WorkerIO for protocol framing and the runtime-aware syscall profile.
// Returns error unless namespace identity, channels and runtime handles validate.
func prepareWorkerIO() (*WorkerIO, error) {
	if os.Getpid() != 1 || os.Geteuid() != 0 {
		return nil, fmt.Errorf("%w: runtime I/O requires namespace init", ErrUnavailable)
	}

	unix.Umask(bootstrapUmask)
	if err := checkWorkerChannels(); err != nil {
		return nil, err
	}
	if err := unix.SetNonblock(workerIPCFD, true); err != nil {
		return nil, fmt.Errorf("%w: preparing nonblocking IPC: %w", ErrUnavailable, err)
	}
	stream := os.NewFile(workerIPCFD, "worker-ipc")
	wakeDescriptors, err := findWakeDescriptors()
	if err != nil {
		return nil, errors.Join(err, stream.Close())
	}
	return &WorkerIO{stream: stream, wakeDescriptors: wakeDescriptors}, nil
}

// findWakeDescriptors discovers kernel-labelled runtime eventfds through procfs.
//
// Returns only close-on-exec local runtime eventfd handles, or an error when discovery
// fails or the runtime profile cannot be bounded.
func findWakeDescriptors() (result []uint32, failure error) {
	directory, err := openOwnProcSelfFd()
	if err != nil {
		return nil, err
	}
	descriptor := int(directory.Fd())
	defer func() { failure = errors.Join(failure, directory.Close()) }()
	count := 0
	for {
		names, err := directory.Readdirnames(descriptorBatchSize)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%w: reading runtime descriptor table: %w", ErrUnavailable, err)
		}
		count += len(names)
		if count > maximumBootstrapDescriptors {
			return nil, fmt.Errorf("%w: too many runtime descriptors", ErrUnavailable)
		}
		batch, scanErr := scanWakeDescriptors(descriptor, names)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, batch...)
		if errors.Is(err, io.EOF) {
			break
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%w: runtime wake-up descriptor missing", ErrUnavailable)
	}
	return result, validateWakeDescriptors(result)
}

// scanWakeDescriptors checks one bounded batch of kernel descriptor entries.
//
// Takes directory (int) which is the worker descriptor table.
// Takes names ([]string) which contains one enumeration batch.
//
// Returns []uint32 containing only verified local eventfd handles.
// Returns error if any entry cannot be inspected safely.
func scanWakeDescriptors(directory int, names []string) ([]uint32, error) {
	var result []uint32
	for _, name := range names {
		wake, found, err := runtimeWakeDescriptor(directory, name)
		if err != nil {
			return nil, err
		}
		if found {
			result = append(result, wake)
		}
	}
	return result, nil
}

// runtimeWakeDescriptor checks one kernel descriptor-table entry.
//
// Takes directory (int) which is the open worker descriptor table.
// Takes name (string) which is a kernel descriptor number.
//
// Returns uint32 which is the validated eventfd number.
// Returns bool which reports whether this entry is a local runtime eventfd.
// Returns error when an entry is invalid or descriptor inspection fails.
func runtimeWakeDescriptor(directory int, name string) (uint32, bool, error) {
	descriptor, err := strconv.ParseUint(name, 10, descriptorNumberBits)
	if err != nil {
		return 0, false, fmt.Errorf("%w: invalid runtime descriptor", ErrUnavailable)
	}
	flags, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0)
	if errors.Is(err, unix.EBADF) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("%w: checking runtime descriptor: %w", ErrUnavailable, err)
	}
	if flags&unix.FD_CLOEXEC == 0 || descriptor <= workerIPCFD {
		return 0, false, nil
	}
	var target [64]byte
	length, err := unix.Readlinkat(directory, name, target[:])
	if errors.Is(err, unix.ENOENT) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("%w: checking runtime descriptor target: %w", ErrUnavailable, err)
	}
	return uint32(descriptor), string(target[:length]) == "anon_inode:[eventfd]", nil
}

// validateWakeDescriptors rejects unsafe or excessive runtime descriptor lists.
//
// Takes descriptors ([]uint32) which are private bootstrap discoveries.
//
// Returns error for reserved, duplicate, oversized or excessive descriptors.
func validateWakeDescriptors(descriptors []uint32) error {
	if len(descriptors) > maximumWakeDescriptors {
		return fmt.Errorf("%w: too many runtime wake-up descriptors", ErrUnavailable)
	}
	seen := make(map[uint32]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		if descriptor <= workerIPCFD || descriptor > 1<<descriptorNumberBits-1 {
			return fmt.Errorf("%w: invalid runtime wake-up descriptor", ErrUnavailable)
		}
		if _, exists := seen[descriptor]; exists {
			return fmt.Errorf("%w: duplicate runtime wake-up descriptor", ErrUnavailable)
		}
		seen[descriptor] = struct{}{}
	}
	return nil
}
