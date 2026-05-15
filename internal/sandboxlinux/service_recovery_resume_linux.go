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

// ResumeRelease finishes a release whose filesystem cleanup was durably recorded. It
// never launches a helper or adopts a replacement at the original service name.
//
// Takes parentPath (string) which is the independently selected original delegated
// parent.
//
// Returns failure on ambiguous identity, missing readiness or unfinished processes.
//
// Safe for concurrent use by multiple goroutines.
func (owner *ServiceRecoveryClaim) ResumeRelease(ctx context.Context, parentPath string) error {
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
	if owner.released {
		return owner.releaseResult
	}
	if owner.closed || owner.snapshot == nil {
		return errClosed
	}
	if owner.recovering || owner.process != nil {
		return ErrServiceBusy
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ready, err := owner.snapshot.ServiceReleaseReady()
	if err != nil {
		return err
	}
	if !ready {
		return ErrServiceBusy
	}
	owner.releaseStarted = true
	return owner.resumeReadyRelease(ctx, parentPath)
}

// resumeReadyRelease pins only the original empty group after readiness verification.
//
// Takes parentPath (string) which is the trusted parent path.
//
// Returns cached completion when the original reservation has already disappeared.
func (owner *ServiceRecoveryClaim) resumeReadyRelease(ctx context.Context, parentPath string) error {
	if owner.group == nil {
		group, err := pinReadyRecoveryService(parentPath, owner.identity)
		if err != nil {
			return err
		}
		if group == nil {
			if err := ctx.Err(); err != nil {
				return err
			}
			return owner.finishRelease(nil)
		}
		owner.group, owner.parentPath = group, parentPath
	} else if owner.parentPath != parentPath {
		return ErrInvalidLimits
	}
	owner.pending = true
	if err := owner.prepareFilesystemRecovery(ctx); err != nil {
		return err
	}
	owner.recovered = true
	return owner.release(ctx)
}

// pinReadyRecoveryService opens only an empty original reservation, never its
// replacement. A trusted delegated parent must not be concurrently renamed or rearranged.
//
// Takes parentPath (string) which is the trusted delegated parent path.
// Takes identity (serviceRecoveryIdentity) which holds the identities authenticated by
// the original checkpoint and a durable readiness record.
//
// Returns nil for a removed original, including a different group at the same name.
func pinReadyRecoveryService(parentPath string, identity serviceRecoveryIdentity) (group *Group, result error) {
	parent, err := openDirectory(unix.AT_FDCWD, parentPath)
	if err != nil {
		return nil, err
	}
	candidate := &Group{
		parent: parent, directory: nil, kill: nil, name: identity.Name, mutex: sync.Mutex{},
		closed: false, closing: true, started: false, parentOnly: true,
	}
	defer func() {
		if group == nil {
			result = errors.Join(result, candidate.closeHandles())
		}
	}()
	original, err := matchReadyRecoveryService(candidate, identity)
	if err != nil || !original {
		return nil, err
	}
	if err := recoveryDirectoryEmpty(candidate.directory); err != nil {
		return nil, err
	}
	candidate.kill, err = openControl(candidate.directory, "cgroup.kill", unix.O_WRONLY)
	if err != nil {
		return nil, err
	}
	if err := validateWatchdogKill(candidate.kill); err != nil {
		return nil, err
	}
	return candidate, nil
}

// matchReadyRecoveryService distinguishes an absent original from unverifiable state.
//
// Takes group (*Group) which is the partially opened owner.
// Takes identity (serviceRecoveryIdentity) which holds the independently authenticated
// kernel identities.
//
// Returns false only for absence or a distinct cgroup on the same verified mount.
func matchReadyRecoveryService(group *Group, identity serviceRecoveryIdentity) (bool, error) {
	parent, err := cgroupDirectoryIdentity(group.parent)
	if err != nil {
		return false, err
	}
	if parent != identity.Parent {
		return false, ErrUnavailable
	}
	group.directory, err = openDirectory(int(group.parent.Fd()), identity.Name)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	current, err := cgroupDirectoryIdentity(group.directory)
	if err != nil {
		return false, err
	}
	if current.Mount != parent.Mount || current.Major != parent.Major || current.Minor != parent.Minor {
		return false, ErrUnavailable
	}
	if current != identity.Group {
		return false, nil
	}
	return true, group.verifyDirectoryEntry()
}
