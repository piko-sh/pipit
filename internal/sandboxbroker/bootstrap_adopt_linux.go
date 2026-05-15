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
	"os"
	"sync"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxwire"
)

// adopt validates independent copies rather than retaining borrowed descriptors.
//
// Takes roots ([]*os.File) which are independent copies of trusted bootstrap root
// handles.
// Takes grants ([]filesystemBootstrapRoot) which hold approved identities after schema
// validation.
// Takes proc (*os.File) which is a trusted handle to the broker's own procfs fd
// directory.
//
// Returns error while the caller retains responsibility for closing partial state.
func (backend *LinuxFilesystem) adopt(roots []*os.File, grants []filesystemBootstrapRoot, proc *os.File) error {
	copied, err := duplicateLinuxBootstrapFile(proc)
	if err != nil {
		return err
	}
	backend.proc = copied
	var filesystem unix.Statfs_t
	var status unix.Stat_t
	if err := unix.Fstatfs(int(copied.Fd()), &filesystem); err != nil {
		return err
	}
	if err := unix.Fstat(int(copied.Fd()), &status); err != nil {
		return err
	}
	if filesystem.Type != unix.PROC_SUPER_MAGIC || status.Mode&unix.S_IFMT != unix.S_IFDIR {
		return ErrInvalidPolicy
	}
	for index, root := range roots {
		copied, err := duplicateLinuxBootstrapFile(root)
		if err != nil {
			return err
		}
		backend.roots[grants[index].Name] = copied
		identity, err := linuxBootstrapRootIdentity(copied)
		if err != nil {
			return err
		}
		approved := grants[index]
		if identity.Device != approved.Device || identity.Inode != approved.Inode || identity.MountID != approved.MountID {
			return ErrInvalidPolicy
		}
		backend.identities[approved.Name] = approved
	}
	return nil
}

// adoptLinuxFilesystem duplicates and validates a host-created descriptor handoff. Input
// handles remain caller-owned and must be closed by the bootstrap audit.
//
// Takes files ([]*os.File) with sealed policy first and then ordered O_PATH roots.
// Takes proc (*os.File) which is a trusted handle to the broker's own procfs fd
// directory.
//
// Returns an independent backend owner, or error after closing partial copies.
func adoptLinuxFilesystem(files []*os.File, proc *os.File) (*LinuxFilesystem, error) {
	if len(files) < 1 || len(files) > maximumRoots+1 {
		return nil, ErrInvalidPolicy
	}
	policy, err := readLinuxBootstrap(files[0])
	if err != nil {
		return nil, err
	}
	if len(policy.Roots) != len(files)-1 {
		return nil, ErrInvalidPolicy
	}
	grants := make([]RootGrant, len(policy.Roots))
	for index, root := range policy.Roots {
		grants[index] = RootGrant{Name: root.Name, Rights: root.Rights}
	}
	budget, err := NewFilesystemBudget(grants, *policy.Limits)
	if err != nil {
		return nil, err
	}
	recovery, err := decodeBootstrapRecovery(policy)
	if err != nil {
		return nil, err
	}
	backend := &LinuxFilesystem{roots: make(map[string]*os.File, len(grants)),
		identities: make(map[string]filesystemBootstrapRoot, len(grants)), proc: nil,
		recovery: recovery,
		budget:   budget, namespace: policy.Namespace, mutex: sync.Mutex{}, closed: false, sealed: false, sealAttempted: false, protocolStarted: false}
	if err := backend.adopt(files[1:], policy.Roots, proc); err != nil {
		return nil, errors.Join(err, backend.Close())
	}
	return backend, nil
}

// readLinuxBootstrap checks mutation seals and size before allocating or decoding.
//
// Takes file (*os.File) which must contain immutable, non-executable policy.
//
// Returns the strict bootstrap schema or an error without granting authority.
func readLinuxBootstrap(file *os.File) (filesystemBootstrap, error) {
	var policy filesystemBootstrap
	if file == nil {
		return policy, ErrInvalidPolicy
	}
	seals, err := unix.FcntlInt(file.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&requiredBootstrapSeals != requiredBootstrapSeals {
		return policy, ErrInvalidPolicy
	}
	status, err := file.Stat()
	if err != nil {
		return policy, err
	}
	if !status.Mode().IsRegular() || status.Mode().Perm()&bootstrapExecutableBits != 0 ||
		status.Size() <= 0 || status.Size() > maximumRecoveryBootstrapBytes {
		return policy, ErrInvalidPolicy
	}
	encoded := make([]byte, int(status.Size()))
	if _, err := file.ReadAt(encoded, 0); err != nil {
		return policy, err
	}
	message := sandboxwire.Message{Kind: sandboxwire.Configure, ID: 0, Payload: encoded}
	if err := message.DecodePayload(&policy); err != nil {
		return policy, errors.Join(ErrInvalidPolicy, err)
	}
	if !validStagingNamespace(policy.Namespace) ||
		policy.Roots == nil || policy.Limits == nil || len(policy.Roots) > maximumRoots {
		return policy, ErrInvalidPolicy
	}
	if policy.Profile == filesystemBootstrapProfile && len(encoded) > maximumBootstrapBytes {
		return policy, ErrInvalidPolicy
	}
	if _, err := decodeBootstrapRecovery(policy); err != nil {
		return policy, err
	}
	return policy, nil
}

// duplicateLinuxBootstrapFile copies an existing trusted descriptor with close-on-exec.
//
// Takes source (*os.File) borrowed only for the duration of trusted bootstrap.
//
// Returns an independently owned descriptor, never the caller's original handle.
func duplicateLinuxBootstrapFile(source *os.File) (*os.File, error) {
	if source == nil {
		return nil, ErrInvalidPolicy
	}
	descriptor, err := unix.FcntlInt(source.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), "broker-adopted"), nil
}

// checkLinuxBootstrapRoot requires an O_PATH directory with kernel beneath resolution.
//
// Takes root (*os.File) already duplicated into backend ownership.
//
// Returns error for an I/O handle, non-directory or unsupported resolution policy.
func checkLinuxBootstrapRoot(root *os.File) error {
	flags, err := unix.FcntlInt(root.Fd(), unix.F_GETFL, 0)
	if err != nil {
		return err
	}
	var status unix.Stat_t
	if err := unix.Fstat(int(root.Fd()), &status); err != nil {
		return err
	}
	if flags&unix.O_PATH == 0 || status.Mode&unix.S_IFMT != unix.S_IFDIR {
		return ErrInvalidPolicy
	}
	probe, err := openLinuxBeneath(root, ".", unix.O_PATH|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	return probe.Close()
}

// linuxBootstrapRootIdentity records the pinned directory's inode and mount identity.
// Missing kernel identity fields fail closed rather than falling back to path strings.
//
// Takes root (*os.File) owned by trusted bootstrap.
//
// Returns an identity with authority fields left empty, or an error.
func linuxBootstrapRootIdentity(root *os.File) (filesystemBootstrapRoot, error) {
	if err := checkLinuxBootstrapRoot(root); err != nil {
		return filesystemBootstrapRoot{}, err
	}
	const required = unix.STATX_INO | unix.STATX_TYPE | unix.STATX_MNT_ID
	var status unix.Statx_t
	if err := unix.Statx(int(root.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, required, &status); err != nil {
		return filesystemBootstrapRoot{}, err
	}
	if status.Mask&required != required || status.Mode&unix.S_IFMT != unix.S_IFDIR {
		return filesystemBootstrapRoot{}, ErrInvalidPolicy
	}
	return filesystemBootstrapRoot{Name: "", Rights: 0,
		Device: unix.Mkdev(status.Dev_major, status.Dev_minor), Inode: status.Ino, MountID: status.Mnt_id}, nil
}
