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
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	// secureNoRoot is the securebits flag disabling root privilege grants.
	secureNoRoot = 1 << 0

	// secureNoRootLocked is the flag locking the no-root securebit.
	secureNoRootLocked = 1 << 1

	// secureNoSetUIDFixup is the flag disabling setuid capability fixups.
	secureNoSetUIDFixup = 1 << 2

	// secureNoSetUIDFixupLocked is the flag locking the no-fixup securebit.
	secureNoSetUIDFixupLocked = 1 << 3

	// secureKeepCapabilities is the flag preserving capabilities across uid changes.
	secureKeepCapabilities = 1 << 4

	// secureKeepCapabilitiesLocked is the flag locking the keep-caps securebit.
	secureKeepCapabilitiesLocked = 1 << 5

	// secureNoAmbientRaise is the flag preventing ambient capability additions.
	secureNoAmbientRaise = 1 << 6

	// secureNoAmbientRaiseLocked is the flag locking the no-ambient securebit.
	secureNoAmbientRaiseLocked = 1 << 7

	// workerSecurebits is the combined securebit mask for a sealed worker.
	workerSecurebits = secureNoRoot | secureNoRootLocked |
		secureNoSetUIDFixup | secureNoSetUIDFixupLocked |
		secureKeepCapabilitiesLocked | secureNoAmbientRaise | secureNoAmbientRaiseLocked

	// maximumCapabilityBits is the upper bound on kernel capability indices.
	maximumCapabilityBits = 64
)

// sealWorkerPrivileges locks securebits and removes every capability set on every Go OS
// thread. Call once after privileged setup but before syscall filtering and untrusted
// input.
//
// Returns error which reports any restriction that could not be installed.
func sealWorkerPrivileges() error {
	securebits, _, errno := syscall.AllThreadsSyscall6(unix.SYS_PRCTL,
		unix.PR_GET_SECUREBITS, 0, 0, 0, 0, 0)
	if errno != 0 {
		return fmt.Errorf("%w: reading securebits: %w", ErrUnavailable, errno)
	}
	securebits = (securebits | workerSecurebits) &^ secureKeepCapabilities
	if _, _, errno := syscall.AllThreadsSyscall6(unix.SYS_PRCTL,
		unix.PR_SET_SECUREBITS, securebits, 0, 0, 0, 0); errno != 0 {
		return fmt.Errorf("%w: locking securebits: %w", ErrUnavailable, errno)
	}
	if err := clearCapabilityBoundingSet(); err != nil {
		return err
	}

	if _, _, errno := syscall.AllThreadsSyscall6(unix.SYS_PRCTL,
		unix.PR_SET_DUMPABLE, 0, 0, 0, 0, 0); errno != 0 {
		return fmt.Errorf("%w: clearing dumpable: %w", ErrUnavailable, errno)
	}
	return restrictWorkerPrivileges()
}

// clearCapabilityBoundingSet removes all kernel-supported capabilities from every
// thread's bounding set while CAP_SETPCAP is still effective.
//
// Returns error if capability discovery or any bounding-set removal fails.
func clearCapabilityBoundingSet() error {
	for capability := uintptr(0); capability <= maximumCapabilityBits; capability++ {
		_, _, errno := syscall.AllThreadsSyscall6(unix.SYS_PRCTL,
			unix.PR_CAPBSET_READ, capability, 0, 0, 0, 0)
		if errno == unix.EINVAL && capability > 0 {
			return nil
		}
		if errno != 0 {
			return fmt.Errorf("%w: reading capability %d: %w", ErrUnavailable, capability, errno)
		}
		if capability == maximumCapabilityBits {
			return fmt.Errorf("%w: kernel capabilities exceed the supported ABI", ErrUnavailable)
		}
		if _, _, errno := syscall.AllThreadsSyscall6(unix.SYS_PRCTL,
			unix.PR_CAPBSET_DROP, capability, 0, 0, 0, 0); errno != 0 {
			return fmt.Errorf("%w: dropping bounding capability %d: %w", ErrUnavailable, capability, errno)
		}
	}
	return ErrUnavailable
}

// restrictWorkerPrivileges sets no_new_privs and removes effective, permitted,
// inheritable and ambient capabilities from every Go thread.
//
// Returns error which reports any capability that could not be cleared.
func restrictWorkerPrivileges() error {
	if _, _, errno := syscall.AllThreadsSyscall6(unix.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0, 0); errno != 0 {
		return fmt.Errorf("%w: process-wide no_new_privs: %w", ErrUnavailable, errno)
	}
	_, _, errno := syscall.AllThreadsSyscall6(unix.SYS_PRCTL,
		unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0, 0)
	if errno != 0 {
		return fmt.Errorf("%w: clearing ambient capabilities: %w", ErrUnavailable, errno)
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3, Pid: 0}
	capabilities := [2]unix.CapUserData{}
	_, _, errno = syscall.AllThreadsSyscall(unix.SYS_CAPSET,
		uintptr(unsafe.Pointer(&header)), uintptr(unsafe.Pointer(&capabilities[0])), 0)
	runtime.KeepAlive(&header)
	runtime.KeepAlive(&capabilities)
	if errno != 0 {
		return fmt.Errorf("%w: clearing thread capabilities: %w", ErrUnavailable, errno)
	}
	return nil
}
