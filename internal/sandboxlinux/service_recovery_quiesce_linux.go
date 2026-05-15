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
	"context"
	"errors"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

// Quiesce terminates tasks only in the independently approved original service subtree.
// Failed termination retains handles and leases for retry with a fresh bounded context.
//
// Takes parentPath (string) which is the host-selected original delegated parent path.
//
// Returns success only after the kernel reports recursive emptiness.
//
// Safe for concurrent use by multiple goroutines.
func (owner *ServiceRecoveryClaim) Quiesce(ctx context.Context, parentPath string) error {
	if owner == nil {
		return errClosed
	}
	if ctx == nil || !filepath.IsAbs(parentPath) {
		return ErrInvalidLimits
	}
	ctx, cancel := context.WithTimeout(ctx, workerCleanupTimeout)
	defer cancel()
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed || owner.snapshot == nil {
		return errClosed
	}
	if owner.process != nil {
		return ErrServiceBusy
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if owner.group == nil {
		group, err := pinRecoveryService(parentPath, owner.identity)
		if err != nil {
			return err
		}
		owner.group, owner.parentPath = group, parentPath
	} else if parentPath != owner.parentPath {
		return ErrInvalidLimits
	}
	owner.group.mutex.Lock()
	defer owner.group.mutex.Unlock()
	owner.pending = true
	owner.quiesced = false
	if err := owner.group.prepareCleanup(); err != nil {
		return err
	}
	if err := owner.group.waitEmpty(ctx); err != nil {
		return err
	}
	owner.pending = false
	owner.quiesced = true
	return ctx.Err()
}

// pinRecoveryService matches both directory identities before opening termination
// authority.
//
// Takes parentPath (string) which is the trusted delegated parent path.
// Takes identity (serviceRecoveryIdentity) which is the independently approved original
// identity.
//
// Returns a cleanup-only group, never a fresh reservation or executable resource owner.
func pinRecoveryService(parentPath string, identity serviceRecoveryIdentity) (*Group, error) {
	parent, err := openDirectory(unix.AT_FDCWD, parentPath)
	if err != nil {
		return nil, err
	}
	group := &Group{
		parent: parent, directory: nil, kill: nil, name: identity.Name, mutex: sync.Mutex{},
		closed: false, closing: true, started: false, parentOnly: true,
	}
	if err := matchRecoveryService(group, identity); err != nil {
		return nil, errors.Join(err, group.closeHandles())
	}
	group.kill, err = openControl(group.directory, "cgroup.kill", unix.O_WRONLY)
	if err != nil {
		return nil, errors.Join(err, group.closeHandles())
	}
	if err := validateWatchdogKill(group.kill); err != nil {
		return nil, errors.Join(err, group.closeHandles())
	}
	return group, nil
}

// matchRecoveryService validates the original delegated parent and service inode.
//
// Takes group (*Group) which is the partially opened owner.
// Takes identity (serviceRecoveryIdentity) which holds the approved kernel identities,
// not directory names.
//
// Returns failure before a termination handle is opened for any replacement group.
func matchRecoveryService(group *Group, identity serviceRecoveryIdentity) error {
	parent, err := cgroupDirectoryIdentity(group.parent)
	if err != nil {
		return err
	}
	if parent != identity.Parent {
		return ErrUnavailable
	}
	group.directory, err = openDirectory(int(group.parent.Fd()), identity.Name)
	if err != nil {
		return err
	}
	current, err := cgroupDirectoryIdentity(group.directory)
	if err != nil {
		return err
	}
	if current != identity.Group {
		return ErrUnavailable
	}
	return group.verifyDirectoryEntry()
}
