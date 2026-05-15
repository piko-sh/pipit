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
	"fmt"
	"math"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	// minimumBrokerLandlockABI is the lowest kernel ABI that supports thread-synchronised
	// confinement.
	minimumBrokerLandlockABI = 8

	// brokerFilesystemAccess is the deny-by-default filesystem access mask.
	brokerFilesystemAccess = unix.LANDLOCK_ACCESS_FS_EXECUTE |
		unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_READ_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_DIR | unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE | unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
		unix.LANDLOCK_ACCESS_FS_MAKE_DIR | unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK | unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK | unix.LANDLOCK_ACCESS_FS_MAKE_SYM |
		unix.LANDLOCK_ACCESS_FS_REFER | unix.LANDLOCK_ACCESS_FS_TRUNCATE |
		unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
)

var (
	// ErrConfinement reports missing or failed mandatory broker kernel protections.
	ErrConfinement = errors.New("filesystem broker confinement unavailable")
)

// SealProcess restricts every thread of a disposable broker process using Landlock. Call
// only during trusted bootstrap, before any request is admitted.
//
// Returns error unless ABI 8 thread-synchronised confinement succeeds.
//
// Safe for concurrent use by multiple goroutines.
func (backend *LinuxFilesystem) SealProcess() error {
	if backend == nil {
		return ErrClosed
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	if backend.closed {
		return ErrClosed
	}
	if backend.sealAttempted {
		return ErrInvalidPolicy
	}
	backend.sealAttempted = true
	if backend.budget.calls != 0 {
		return ErrInvalidPolicy
	}
	if _, err := brokerLandlockABI(); err != nil {
		return err
	}
	ruleset, err := backend.landlockRuleset()
	if err != nil {
		return err
	}
	defer unix.Close(ruleset)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("%w: no_new_privs: %w", ErrConfinement, err)
	}
	_, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(ruleset), unix.LANDLOCK_RESTRICT_SELF_TSYNC, 0)
	if errno != 0 {
		return fmt.Errorf("%w: thread-synchronised rules: %w", ErrConfinement, errno)
	}
	backend.sealed = true
	return nil
}

// landlockRuleset creates deny-by-default filesystem and TCP rules. Rules grant only the
// operation rights needed beneath previously pinned roots.
//
// Returns an owned ruleset descriptor, or an error after closing partial state.
func (backend *LinuxFilesystem) landlockRuleset() (int, error) {
	attributes := unix.LandlockRulesetAttr{
		Access_fs:  brokerFilesystemAccess,
		Access_net: unix.LANDLOCK_ACCESS_NET_BIND_TCP | unix.LANDLOCK_ACCESS_NET_CONNECT_TCP,
		Scoped:     0,
	}
	descriptor, _, errno := syscall.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attributes)), unsafe.Sizeof(attributes), 0)
	runtime.KeepAlive(&attributes)
	if errno != 0 {
		return -1, fmt.Errorf("%w: creating Landlock policy: %w", ErrConfinement, errno)
	}
	for name, root := range backend.roots {
		allowed := brokerLandlockRights(backend.budget.roots[name])
		if backend.recovery != nil {
			allowed = unix.LANDLOCK_ACCESS_FS_READ_DIR | unix.LANDLOCK_ACCESS_FS_REMOVE_FILE
		}
		if err := addBrokerLandlockRule(descriptor, root.Fd(), allowed); err != nil {
			return -1, errors.Join(err, unix.Close(int(descriptor)))
		}
	}
	return int(descriptor), nil
}

// brokerLandlockABI checks support without silently weakening the required policy.
//
// Returns the kernel ABI or a confinement error below the TSYNC-capable ABI.
func brokerLandlockABI() (uintptr, error) {
	version, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return 0, fmt.Errorf("%w: querying Landlock: %w", ErrConfinement, errno)
	}
	if version < minimumBrokerLandlockABI {
		return 0, fmt.Errorf("%w: Landlock ABI %d requires at least %d", ErrConfinement, version, minimumBrokerLandlockABI)
	}
	return version, nil
}

// addBrokerLandlockRule binds native rights to one host-pinned directory. Writes also
// need directory-open authority to synchronise publication metadata.
//
// Takes ruleset (uintptr) which is the Landlock ruleset descriptor.
// Takes root (uintptr) which is the host-pinned directory descriptor.
// Takes allowed (uint64) which is the native access mask selected by immutable host
// policy.
//
// Returns error without falling back to a broader or incomplete rule.
func addBrokerLandlockRule(ruleset, root uintptr, allowed uint64) error {
	if root > math.MaxInt32 {
		return ErrInvalidPolicy
	}
	attributes := unix.LandlockPathBeneathAttr{
		Allowed_access: allowed, Parent_fd: int32(root),
	}
	_, _, errno := syscall.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, ruleset,
		unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&attributes)), 0, 0, 0)
	runtime.KeepAlive(&attributes)
	if errno != 0 {
		return fmt.Errorf("%w: adding root rule: %w", ErrConfinement, errno)
	}
	return nil
}

// brokerLandlockRights maps independent protocol grants onto required kernel rights. A
// Write grant carries READ_DIR because staged writes open the parent directory for the
// anonymous inode.
//
// Takes rights (Rights) selected by host policy.
//
// Returns the minimum native mask used by the current backend implementation.
func brokerLandlockRights(rights Rights) uint64 {
	var allowed uint64
	if rights&Read != 0 {
		allowed |= unix.LANDLOCK_ACCESS_FS_READ_FILE
	}
	if rights&List != 0 {
		allowed |= unix.LANDLOCK_ACCESS_FS_READ_DIR
	}
	if rights&Write != 0 {
		allowed |= unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR |
			unix.LANDLOCK_ACCESS_FS_MAKE_REG | unix.LANDLOCK_ACCESS_FS_REMOVE_FILE
	}
	return allowed
}
