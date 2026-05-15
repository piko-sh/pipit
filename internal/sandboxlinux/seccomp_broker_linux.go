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
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/safeconv"
)

const (
	// brokerDescriptorSignBit is the mask for the sign bit of a broker descriptor argument.
	brokerDescriptorSignBit = uint32(1 << 31)

	// openModeArgument is the syscall argument index for open mode.
	openModeArgument = 3

	// openHowSizeArgument is the syscall argument index for openat2 size.
	openHowSizeArgument = 3

	// linkFlagsArgument is the syscall argument index for linkat flags.
	linkFlagsArgument = 4

	// renameFlagsArgument is the syscall argument index for renameat2 flags.
	renameFlagsArgument = 4
)

// installFilesystemBrokerFilter seals the syscall surface of a dedicated broker. Call
// only after verified descriptor adoption, filesystem and privilege sealing.
//
// Returns error unless the reviewed profile is installed on every thread.
func (workerIO *WorkerIO) installFilesystemBrokerFilter() error {
	if workerIO == nil || workerIO.stream == nil || len(workerIO.wakeDescriptors) == 0 {
		return fmt.Errorf("%w: missing prepared broker I/O", ErrUnavailable)
	}
	filter, err := filesystemBrokerFilter(unix.Getpid(), workerIO.wakeDescriptors)
	if err != nil {
		return err
	}
	return installWorkerFilter(filter)
}

// filesystemBrokerFilter keeps runtime restrictions and adds bounded operation forms.
//
// Takes processID (int) which is the broker's verified native process identifier.
// Takes wakeDescriptors ([]uint32) which are the verified bootstrap wake descriptors.
//
// Returns an architecture-checked default-deny profile, or an error.
func filesystemBrokerFilter(processID int, wakeDescriptors []uint32) ([]unix.SockFilter, error) {
	if processID <= 0 {
		return nil, ErrUnavailable
	}
	if err := validateWakeDescriptors(wakeDescriptors); err != nil {
		return nil, err
	}
	runtime := runtimeRules(safeconv.IntToUint32(processID), wakeDescriptors)
	operations := filesystemBrokerRules()
	rules := make([]syscallRule, 0, len(runtime)+len(operations))
	for _, rule := range runtime {
		switch rule.number {
		case unix.SYS_READ, unix.SYS_WRITE, unix.SYS_WRITEV:
			continue
		default:
			rules = append(rules, rule)
		}
	}
	return compileSyscallRules(append(rules, operations...), killRules())
}

// filesystemBrokerRules permits the reviewed pinned-root backend's kernel operations.
// Landlock checks pathname authority; seccomp rejects all unlisted operation families.
//
// Returns operation forms without exec, sockets, mounts or descriptor duplication.
func filesystemBrokerRules() []syscallRule {
	descriptor := func(index uint32) argumentRule {
		return argumentRule{index: index, mask: brokerDescriptorSignBit, allowed: []uint32{0}}
	}
	exact := func(index, value uint32) argumentRule {
		return argumentRule{index: index, mask: allArgumentBits, allowed: []uint32{value}}
	}
	return []syscallRule{
		{number: unix.SYS_OPENAT, arguments: []argumentRule{
			descriptor(0), exact(2, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOCTTY), exact(openModeArgument, 0),
		}},
		{number: unix.SYS_OPENAT2, arguments: []argumentRule{
			descriptor(0), exact(openHowSizeArgument, uint32(unsafe.Sizeof(unix.OpenHow{Flags: 0, Mode: 0, Resolve: 0}))),
		}},
		{number: unix.SYS_FCNTL, arguments: []argumentRule{
			descriptor(0), {index: 1, mask: allArgumentBits, allowed: []uint32{unix.F_GETFL, unix.F_SETFL, unix.F_GETFD}},
		}},
		{number: unix.SYS_LINKAT, arguments: []argumentRule{
			descriptor(0), descriptor(2), exact(linkFlagsArgument, unix.AT_SYMLINK_FOLLOW),
		}},
		{number: unix.SYS_RENAMEAT2, arguments: []argumentRule{
			descriptor(0), descriptor(2), exact(renameFlagsArgument, 0),
		}},
		{number: unix.SYS_UNLINKAT, arguments: []argumentRule{
			descriptor(0), exact(2, 0),
		}},
		{number: unix.SYS_READ, arguments: []argumentRule{descriptor(0)}},
		{number: unix.SYS_WRITE, arguments: []argumentRule{descriptor(0)}},
		{number: unix.SYS_WRITEV, arguments: []argumentRule{descriptor(0)}},
		{number: unix.SYS_FSTAT, arguments: []argumentRule{descriptor(0)}},
		{number: unix.SYS_GETDENTS64, arguments: []argumentRule{descriptor(0)}},
		{number: unix.SYS_FSYNC, arguments: []argumentRule{descriptor(0)}},
	}
}
