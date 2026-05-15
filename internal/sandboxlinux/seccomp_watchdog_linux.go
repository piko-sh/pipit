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

//go:build linux && (amd64 || arm64)

package sandboxlinux

import (
	"math"
	"slices"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/safeconv"
)

const (
	// watchdogPollMaskArgument is the syscall argument index for ppoll signal mask.
	watchdogPollMaskArgument = 3

	// watchdogWriteOffsetArgument is the syscall argument index for pwrite64 offset.
	watchdogWriteOffsetArgument = 3

	// watchdogSendFlagsArgument is the syscall argument index for sendto flags.
	watchdogSendFlagsArgument = 3

	// watchdogDestinationArgument is the syscall argument index for sendto destination.
	watchdogDestinationArgument = 4

	// watchdogDestinationLengthArgument is the syscall argument index for sendto destination
	// length.
	watchdogDestinationLengthArgument = 5

	// watchdogReadySendFlags is the flag set for the one-byte readiness send.
	watchdogReadySendFlags = unix.MSG_NOSIGNAL | unix.MSG_DONTWAIT
)

// hostWatchdogFilter grants only the observation and termination descriptor roles.
//
// Takes processID (int) which is the process identity.
// Takes host (uint32) which is the trusted host observation descriptor.
// Takes kill (uint32) which is the trusted cgroup termination descriptor.
// Takes wakeDescriptors ([]uint32) which holds the verified runtime wake fds.
//
// Returns a default-deny, architecture-checked filter without general filesystem,
// networking, process signalling or descriptor-transfer authority.
func hostWatchdogFilter(processID int, host, kill uint32, wakeDescriptors []uint32) ([]unix.SockFilter, error) {
	rules, err := hostWatchdogRules(processID, host, kill, wakeDescriptors)
	if err != nil {
		return nil, err
	}
	return compileSyscallRules(rules, killRules())
}

// hostWatchdogRules validates descriptor roles and builds the monitor's syscall rules.
//
// Takes processID (int) which is the trusted process identity.
// Takes host (uint32) which is the trusted host observation descriptor.
// Takes kill (uint32) which is the trusted cgroup termination descriptor.
// Takes wakeDescriptors ([]uint32) which holds the runtime wake identities.
//
// Returns uncompiled rules for the fixed monitor or its sealed bootstrap profile.
func hostWatchdogRules(processID int, host, kill uint32, wakeDescriptors []uint32) ([]syscallRule, error) {
	if processID <= 0 || host <= 2 || kill <= 2 || host == kill || host > math.MaxInt32 || kill > math.MaxInt32 {
		return nil, ErrUnavailable
	}
	if err := validateWakeDescriptors(wakeDescriptors); err != nil {
		return nil, err
	}
	if slices.Contains(wakeDescriptors, host) || slices.Contains(wakeDescriptors, kill) {
		return nil, ErrUnavailable
	}
	runtime := runtimeRules(safeconv.IntToUint32(processID), wakeDescriptors)
	rules := make([]syscallRule, 0, len(runtime))
	for _, rule := range runtime {
		switch rule.number {
		case unix.SYS_READ, unix.SYS_WRITE, unix.SYS_WRITEV:
			continue
		default:
			rules = append(rules, rule)
		}
	}
	return append(rules, watchdogOperationRules(host, kill, wakeDescriptors)...), nil
}

// watchdogReadyFilter adds one-byte readiness and a preconfigured kernel timer.
//
// Takes processID (int) which is the independently adopted process identity.
// Takes host (uint32) which is the adopted host observation descriptor.
// Takes kill (uint32) which is the adopted cgroup termination descriptor.
// Takes timer (uint32) which is the locally created close-on-exec timer descriptor.
// Takes wakes ([]uint32) which holds the verified runtime wake descriptors.
//
// Returns a profile that cannot create, reset or read the timer after sealing.
func watchdogReadyFilter(processID int, host, kill, timer uint32, wakes []uint32) ([]unix.SockFilter, error) {
	if host <= workerIPCFD || kill <= workerIPCFD || timer <= workerIPCFD ||
		timer > math.MaxInt32 || timer == host || timer == kill || slices.Contains(wakes, timer) {
		return nil, ErrUnavailable
	}
	rules, err := hostWatchdogRules(processID, host, kill, wakes)
	if err != nil {
		return nil, err
	}
	rules = slices.DeleteFunc(rules, func(rule syscallRule) bool { return rule.number == unix.SYS_PPOLL })
	rules = append(rules,
		syscallRule{number: unix.SYS_SENDTO, arguments: []argumentRule{
			{index: 0, mask: allArgumentBits, allowed: []uint32{workerIPCFD}},
			{index: 2, mask: allArgumentBits, allowed: []uint32{1}},
			{index: watchdogSendFlagsArgument, mask: allArgumentBits, allowed: []uint32{watchdogReadySendFlags}},
			{index: watchdogDestinationArgument, mask: allArgumentBits, allowed: []uint32{0}},
			{index: watchdogDestinationLengthArgument, mask: allArgumentBits, allowed: []uint32{0}},
		}},
		syscallRule{number: unix.SYS_PPOLL, arguments: []argumentRule{
			{index: 1, mask: allArgumentBits, allowed: []uint32{2}},
			{index: watchdogPollMaskArgument, mask: allArgumentBits, allowed: []uint32{0}},
		}},
	)
	return compileSyscallRules(rules, killRules())
}

// watchdogOperationRules restricts cgroup termination to one byte at offset zero.
//
// Takes host (uint32) which is the validated host observation descriptor.
// Takes kill (uint32) which is the validated cgroup termination descriptor.
// Takes wakeDescriptors ([]uint32) which holds the runtime wake descriptors.
//
// Returns only the operations required by the bounded host-lifetime monitor.
func watchdogOperationRules(host, kill uint32, wakeDescriptors []uint32) []syscallRule {
	exact := func(index, value uint32) argumentRule {
		return argumentRule{index: index, mask: allArgumentBits, allowed: []uint32{value}}
	}
	writes := append([]uint32{1, 2}, wakeDescriptors...)
	rules := []syscallRule{
		{number: unix.SYS_WRITE, arguments: []argumentRule{{index: 0, mask: allArgumentBits, allowed: writes}}},
		{number: unix.SYS_WRITEV, arguments: []argumentRule{{index: 0, mask: allArgumentBits, allowed: []uint32{1, 2}}}},
		{number: unix.SYS_FSTATFS, arguments: []argumentRule{{index: 0, mask: allArgumentBits, allowed: []uint32{host, kill}}}},
		{number: unix.SYS_FCNTL, arguments: []argumentRule{
			{index: 0, mask: allArgumentBits, allowed: []uint32{host, kill}}, exact(1, unix.F_GETFL),
		}},
		{number: unix.SYS_PPOLL, arguments: []argumentRule{exact(1, 1), exact(watchdogPollMaskArgument, 0)}},
		{number: unix.SYS_PWRITE64, arguments: []argumentRule{exact(0, kill), exact(2, 1), exact(watchdogWriteOffsetArgument, 0)}},
	}
	if len(wakeDescriptors) != 0 {
		rules = append(rules, syscallRule{number: unix.SYS_READ,
			arguments: []argumentRule{{index: 0, mask: allArgumentBits, allowed: slices.Clone(wakeDescriptors)}}})
	}
	return rules
}
