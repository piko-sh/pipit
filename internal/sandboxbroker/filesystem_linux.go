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
	"os"
	"strconv"
	"sync"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxwire"
)

// linuxResolveFlags is the mandatory openat2 resolve mask for all broker path operations.
const linuxResolveFlags = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV

// LinuxRootGrant binds a host-selected absolute directory to an opaque grant. Root trees
// must not be concurrently rearranged by untrusted host processes.
type LinuxRootGrant struct {
	// Name is the opaque root identifier visible to the protocol.
	Name string

	// Path is the host-selected absolute directory to pin.
	Path string

	// Rights holds the approved operations for this root.
	Rights Rights
}

// LinuxFilesystem owns pinned roots and one cumulative request budget. It must run inside
// an independently supervised broker process before accepting worker input.
type LinuxFilesystem struct {
	// roots maps each named root to its pinned directory handle.
	roots map[string]*os.File

	// identities maps each named root to its kernel identity.
	identities map[string]filesystemBootstrapRoot

	// proc is the pinned /proc/self/fd directory for inode reopening.
	proc *os.File

	// budget tracks cumulative reservation quotas.
	budget *FilesystemBudget

	// namespace is the host-selected staging namespace.
	namespace string

	// recovery holds pending recovery records from journalled writes.
	recovery []RecoveryRecord

	// mutex guards all mutable backend state.
	mutex sync.Mutex

	// closed is true after Close releases all handles.
	closed bool

	// sealed is true after Landlock confinement succeeds.
	sealed bool

	// sealAttempted is true after the first SealProcess call.
	sealAttempted bool

	// protocolStarted is true after the first protocol message.
	protocolStarted bool
}

// Execute authorises one ordered request and completes its reservation exactly once.
// Results are discarded on any I/O or accounting failure.
//
// Takes message (sandboxwire.Message) after transport lifecycle validation.
//
// Returns bounded data, or an error without a partial result.
func (backend *LinuxFilesystem) Execute(message sandboxwire.Message) (FilesystemResult, error) {
	return backend.execute(message, nil)
}

// Close prevents further calls and closes owned descriptors. It waits for an active
// operation.
//
// Returns error when closing any descriptor fails.
//
// Safe for concurrent use by multiple goroutines.
func (backend *LinuxFilesystem) Close() error {
	if backend == nil {
		return nil
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	if backend.closed {
		return nil
	}
	backend.closed = true
	backend.budget.Close()
	var result error
	for _, root := range backend.roots {
		result = errors.Join(result, root.Close())
	}
	if backend.proc != nil {
		result = errors.Join(result, backend.proc.Close())
	}
	return result
}

// execute admits one operation with an optional negotiated publication handshake.
//
// Takes message (sandboxwire.Message) which is the validated request after transport
// lifecycle validation.
// Takes connection (*filesystemConnection) which is the broker-owned private protocol
// connection, or nil for direct execution.
//
// Returns bounded data only after reservation accounting succeeds.
//
// Safe for concurrent use by multiple goroutines.
func (backend *LinuxFilesystem) execute(message sandboxwire.Message, connection *filesystemConnection) (FilesystemResult, error) {
	var result FilesystemResult
	if backend == nil {
		return result, ErrClosed
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	if backend.closed {
		return result, ErrClosed
	}
	if backend.recovery != nil {
		return result, ErrDenied
	}
	if backend.sealAttempted && !backend.sealed {
		return result, ErrConfinement
	}
	call, err := backend.budget.Admit(message)
	if err != nil {
		return result, err
	}
	root := backend.roots[call.root()]
	used := 0
	switch call.Operation() {
	case ReadFile:
		result.Data, err = backend.read(root, call.path(), call.limit())
		used = len(result.Data)
	case ListDirectory:
		result.Entries, result.skipped, err = listLinuxDirectory(root, call.path(), call.limit())
		used = len(result.Entries) + result.skipped
	case WriteFile:
		if connection != nil && connection.journalled {
			used, err = backend.writeJournalled(call, connection)
		} else {
			used, err = backend.write(root, call.path(), call.request.data, message.ID)
		}
		result.Written = used
	}
	err = errors.Join(err, call.Finish(used))
	if err != nil {
		return FilesystemResult{}, err
	}
	return result, nil
}

// pin opens roots and a kernel-owned descriptor reopening directory.
//
// Takes grants ([]LinuxRootGrant) which passed host policy validation.
//
// Returns error without making partially opened roots available to callers.
func (backend *LinuxFilesystem) pin(grants []LinuxRootGrant) error {
	procPath := "/proc/" + strconv.Itoa(os.Getpid()) + "/fd"
	proc, err := openLinuxDirectory(procPath)
	if err != nil {
		return fmt.Errorf("pin broker descriptor directory: %w", err)
	}
	backend.proc = proc
	var stat unix.Statfs_t
	if err := unix.Fstatfs(int(proc.Fd()), &stat); err != nil {
		return err
	}
	if stat.Type != unix.PROC_SUPER_MAGIC {
		return ErrDenied
	}
	for _, grant := range grants {
		root, err := openLinuxDirectory(grant.Path)
		if err != nil {
			return fmt.Errorf("pin broker root: %w", err)
		}
		backend.roots[grant.Name] = root
		identity, err := linuxBootstrapRootIdentity(root)
		if err != nil {
			return err
		}
		identity.Name, identity.Rights = grant.Name, grant.Rights
		backend.identities[grant.Name] = identity
		probe, err := openLinuxBeneath(root, ".", unix.O_PATH|unix.O_DIRECTORY)
		if err != nil {
			return fmt.Errorf("verify broker path confinement: %w", err)
		}
		if err := probe.Close(); err != nil {
			return err
		}
	}
	return nil
}

// FilesystemResult contains only bounded bytes or names, never native handles. Reads
// return a prefix of at most the reserved size.
type FilesystemResult struct {
	// Data holds the read bytes, at most the reserved limit.
	Data []byte

	// Entries holds the listed directory names in filesystem order.
	Entries []string

	// Written counts the staged bytes for a write operation.
	Written int

	// skipped counts directory names a listing left out because the protocol cannot carry
	// them; they are charged against the entry budget like listed names.
	skipped int
}

// OpenLinuxFilesystem pins host-approved roots without following symlinks. Unsupported
// openat2 protections fail closed.
//
// Takes grants ([]LinuxRootGrant) which are the host-approved pinned root directories.
// Takes limits (FilesystemLimits) which are the finite cumulative quotas selected by the
// host.
//
// Returns *LinuxFilesystem which must be closed after use.
// Returns error which is non-nil after closing partial state.
func OpenLinuxFilesystem(grants []LinuxRootGrant, limits FilesystemLimits) (*LinuxFilesystem, error) {
	budget, err := newLinuxGrantBudget(grants, limits)
	if err != nil {
		return nil, err
	}
	namespace, err := newStagingNamespace()
	if err != nil {
		return nil, err
	}
	backend := &LinuxFilesystem{roots: make(map[string]*os.File, len(grants)), identities: make(map[string]filesystemBootstrapRoot, len(grants)),
		proc: nil, budget: budget, namespace: namespace, recovery: nil,
		mutex: sync.Mutex{}, closed: false, sealed: false, sealAttempted: false, protocolStarted: false}
	if err := backend.pin(grants); err != nil {
		return nil, errors.Join(err, backend.Close())
	}
	return backend, nil
}

// openLinuxDirectory opens an absolute host path with no symlink traversal.
//
// Takes name (string) selected by the host, never by a worker.
//
// Returns an owned directory descriptor or an error.
func openLinuxDirectory(name string) (*os.File, error) {
	descriptor, err := unix.Openat2(unix.AT_FDCWD, name, &unix.OpenHow{
		Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Mode:  0, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), "broker-root"), nil
}

// openLinuxBeneath resolves a validated relative name beneath a pinned root.
//
// Takes root (*os.File) which is the pinned grant directory owned by the backend.
// Takes name (string) which is the validated relative path owned by the backend.
// Takes flags (uint64) which are the open flags owned by the backend.
//
// Returns an owned descriptor without following links or crossing mount points.
func openLinuxBeneath(root *os.File, name string, flags uint64) (*os.File, error) {
	descriptor, err := unix.Openat2(int(root.Fd()), name, &unix.OpenHow{
		Flags: flags | unix.O_CLOEXEC, Mode: 0, Resolve: linuxResolveFlags,
	})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), "broker-object"), nil
}
