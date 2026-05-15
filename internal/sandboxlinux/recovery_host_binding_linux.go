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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"runtime"
	"slices"

	"golang.org/x/sys/unix"
)

const (
	// recoveryHostBindingProfile is the versioned profile name for host bindings.
	recoveryHostBindingProfile = "linux-recovery-host-v1"

	// recoveryBootIDBytes is the expected length of a kernel boot UUID line.
	recoveryBootIDBytes = 37

	// maximumRecoveryGroups is the upper bound on supplementary group entries.
	maximumRecoveryGroups = 65536
)

// recoveryHostContext holds the canonical snapshot of host identity for recovery binding.
type recoveryHostContext struct {
	// Security holds the kernel privilege and LSM status fields.
	Security map[string]string

	// Boot holds the kernel boot UUID.
	Boot string

	// Groups holds the sorted supplementary group identifiers.
	Groups []int

	// Namespaces holds the measured namespace identities.
	Namespaces []recoveryNamespaceIdentity

	// UID holds the real user identifier.
	UID int

	// EUID holds the effective user identifier.
	EUID int

	// GID holds the real group identifier.
	GID int

	// EGID holds the effective group identifier.
	EGID int
}

// recoveryNamespaceIdentity holds the kernel identity of one measured namespace.
type recoveryNamespaceIdentity struct {
	// Name holds the namespace type name.
	Name string

	// Device holds the namespace filesystem device number.
	Device uint64

	// Inode holds the namespace inode number.
	Inode uint64
}

// recoveryHostApproval holds the complete binding input for JSON encoding.
type recoveryHostApproval struct {
	// Profile holds the versioned binding profile name.
	Profile string

	// Context holds the serialised host identity snapshot.
	Context json.RawMessage

	// Executable holds the running host executable digest.
	Executable [sha256.Size]byte

	// Policy holds the independently selected policy digest.
	Policy [sha256.Size]byte
}

// RecoveryHostBinding binds private recovery policy to the current Linux host identity.
// It measures the running executable, not its replaceable original pathname.
//
// Takes policy ([sha256.Size]byte) which is the nonzero independently selected policy
// digest.
//
// Returns a binding for this boot, namespace set, Unix principal and executable.
func RecoveryHostBinding(ctx context.Context, policy [sha256.Size]byte) (binding [sha256.Size]byte, result error) {
	var empty [sha256.Size]byte
	if ctx == nil || policy == empty {
		return empty, ErrInvalidLimits
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	proc, err := OpenWorkerProc()
	if err != nil {
		return empty, err
	}
	defer func() {
		result = errors.Join(result, proc.Close())
		if result != nil {
			binding = empty
		}
	}()
	before, err := captureRecoveryHostContext(proc)
	if err != nil {
		return empty, err
	}
	executable, err := recoveryHostExecutable(ctx, proc)
	if err != nil {
		return empty, err
	}
	after, err := captureRecoveryHostContext(proc)
	if err != nil {
		return empty, err
	}
	if !bytes.Equal(before, after) {
		return empty, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	encoded, err := json.Marshal(recoveryHostApproval{
		Profile: recoveryHostBindingProfile, Context: before, Executable: executable, Policy: policy,
	})
	if err != nil {
		return empty, err
	}
	return sha256.Sum256(encoded), nil
}

// captureRecoveryHostContext reads a canonical bounded snapshot from verified procfs.
//
// Takes proc (*os.File) which is the pinned procfs while the caller stays on the same
// operating-system thread.
//
// Returns boot, namespace and Unix principal metadata without volatile process IDs.
func captureRecoveryHostContext(proc *os.File) ([]byte, error) {
	boot, err := recoveryBootIdentity(proc)
	if err != nil {
		return nil, err
	}
	groups, err := os.Getgroups()
	if err != nil || len(groups) > maximumRecoveryGroups {
		return nil, errors.Join(ErrUnavailable, err)
	}
	slices.Sort(groups)
	groups = slices.Compact(groups)
	security, err := recoveryHostSecurity(proc)
	if err != nil {
		return nil, err
	}
	names := []string{"user", "mnt", "pid", "cgroup", "net", "ipc", "uts", "time"}
	namespaces := make([]recoveryNamespaceIdentity, 0, len(names))
	for _, name := range names {
		identity, err := recoveryNamespace(proc, name)
		if err != nil {
			return nil, err
		}
		namespaces = append(namespaces, identity)
	}
	return json.Marshal(recoveryHostContext{
		Boot: boot, Groups: groups, Namespaces: namespaces,
		Security: security,
		UID:      os.Getuid(), EUID: os.Geteuid(), GID: os.Getgid(), EGID: os.Getegid(),
	})
}

// recoveryBootIdentity reads the current boot UUID through the verified procfs root.
//
// Takes proc (*os.File) which is the pinned host procfs descriptor, never a
// script-selected file.
//
// Returns an exact bounded kernel UUID or an error without a pathname fallback.
func recoveryBootIdentity(proc *os.File) (identity string, result error) {
	descriptor, err := unix.Openat(int(proc.Fd()), "sys/kernel/random/boot_id", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	file := os.NewFile(uintptr(descriptor), "recovery-boot")
	defer func() { result = errors.Join(result, file.Close()) }()
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(descriptor, &filesystem); err != nil || filesystem.Type != unix.PROC_SUPER_MAGIC {
		return "", errors.Join(ErrUnavailable, err)
	}
	data, err := io.ReadAll(io.LimitReader(file, recoveryBootIDBytes+1))
	if err != nil {
		return "", err
	}
	if !validRecoveryBootIdentity(data) {
		return "", ErrUnavailable
	}
	return string(data), nil
}

// validRecoveryBootIdentity requires the complete canonical kernel UUID line.
//
// Takes data ([]byte) which is the bounded kernel metadata rather than a caller-provided
// boot identifier.
//
// Returns false for truncation, extra bytes or malformed UUID characters.
func validRecoveryBootIdentity(data []byte) bool {
	if len(data) != recoveryBootIDBytes || data[len(data)-1] != '\n' {
		return false
	}
	for index, character := range data[:len(data)-1] {
		if "00000000-0000-0000-0000-000000000000"[index] == '-' {
			if character != '-' {
				return false
			}
		} else {
			if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
				return false
			}
		}
	}
	return true
}

// recoveryNamespace verifies and measures a kernel namespace handle.
//
// Takes proc (*os.File) which is the verified procfs.
// Takes name (string) which is the internally selected namespace name.
//
// Returns descriptor identity, failing closed when the namespace is unavailable.
func recoveryNamespace(proc *os.File, name string) (identity recoveryNamespaceIdentity, result error) {
	descriptor, err := unix.Openat(int(proc.Fd()), "thread-self/ns/"+name, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return identity, err
	}
	file := os.NewFile(uintptr(descriptor), "recovery-namespace")
	defer func() { result = errors.Join(result, file.Close()) }()
	var filesystem unix.Statfs_t
	var status unix.Stat_t
	if err := unix.Fstatfs(descriptor, &filesystem); err != nil || filesystem.Type != unix.NSFS_MAGIC {
		return identity, errors.Join(ErrUnavailable, err)
	}
	if err := unix.Fstat(descriptor, &status); err != nil {
		return identity, err
	}
	if status.Ino == 0 {
		return identity, ErrUnavailable
	}
	return recoveryNamespaceIdentity{Name: name, Device: status.Dev, Inode: status.Ino}, nil
}

// recoveryHostExecutable measures exact bounded bytes of the running host image.
//
// Takes proc (*os.File) which is the pinned procfs while staying on one host thread.
//
// Returns a digest without reopening the executable's original filesystem pathname.
func recoveryHostExecutable(ctx context.Context, proc *os.File) (digest [sha256.Size]byte, result error) {
	descriptor, err := unix.Openat(int(proc.Fd()), "thread-self/exe", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return digest, err
	}
	file := os.NewFile(uintptr(descriptor), "recovery-host-image")
	defer func() { result = errors.Join(result, file.Close()) }()
	before, err := file.Stat()
	if err != nil {
		return digest, err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maximumWorkerImageBytes {
		return digest, ErrInvalidWorker
	}
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(workerImageReader{context: ctx, source: file}, maximumWorkerImageBytes+1))
	if err != nil {
		return digest, err
	}
	after, err := file.Stat()
	if err != nil {
		return digest, err
	}
	if count != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || !os.SameFile(before, after) {
		return digest, ErrInvalidWorker
	}
	return [sha256.Size]byte(hash.Sum(nil)), nil
}
