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
	"sync"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxbroker"
)

// ServiceRecoveryClaim retains exclusive metadata ownership after original-host exit.
// Quiesce can pin the approved original service and prove recursive task termination.
type ServiceRecoveryClaim struct {
	// snapshot holds the durable metadata claim.
	snapshot *sandboxbroker.LinuxRecoveryClaim

	// group holds the adopted cgroup for recovery.
	group *Group

	// process holds the active recovery helper process.
	process *WorkerProcess

	// images holds the claimed image store, if any.
	images *sandboxbroker.LinuxImageStore

	// imagePath holds the image store directory path.
	imagePath string

	// metadata holds the directories the image store is bound to.
	metadata []string

	// releaseResult caches the terminal release error.
	releaseResult error

	// parentPath holds the delegated cgroup parent path.
	parentPath string

	// identity holds the decoded original service identity.
	identity serviceRecoveryIdentity

	// mutex guards mutable state during recovery and close.
	mutex sync.Mutex

	// pending is true while a recovery attempt is in progress.
	pending bool

	// quiesced is true once the original service has been proven empty.
	quiesced bool

	// recovering is true while filesystem recovery is active.
	recovering bool

	// recovered is true once filesystem recovery has succeeded.
	recovered bool

	// closed is true once metadata leases have been released.
	closed bool

	// released is true once native resources are fully removed.
	released bool

	// releaseStarted is true once release has been attempted.
	releaseStarted bool
}

// ReleaseReady reads durable cleanup progress without granting native removal authority.
//
// Returns no progress after metadata ownership has been released.
//
// Safe for concurrent use by multiple goroutines.
func (owner *ServiceRecoveryClaim) ReleaseReady() (bool, error) {
	if owner == nil {
		return false, errClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed || owner.snapshot == nil {
		return false, errClosed
	}
	return owner.snapshot.ServiceReleaseReady()
}

// Close releases metadata leases without deleting or reclaiming original resources.
//
// Failed cleanup retains every lease until Quiesce or Prune succeeds on retry.
//
// Returns error when storage closure fails; repeated successful closure is harmless.
//
// Safe for concurrent use by multiple goroutines.
func (owner *ServiceRecoveryClaim) Close() error {
	if owner == nil {
		return nil
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed {
		return owner.releaseResult
	}
	if owner.pending || owner.recovering || owner.process != nil {
		return ErrServiceBusy
	}
	if owner.images != nil {
		if err := owner.images.Close(); err != nil {
			return err
		}
	}
	owner.closed = true
	var result error
	if owner.group != nil {
		result = owner.group.closeHandles()
	}
	return errors.Join(result, owner.snapshot.Close())
}

// ClaimServiceRecovery acquires every original store before verifying host termination. A
// record whose lease another process holds reports ErrServiceBusy.
//
// Takes storage (FilesystemRecoveryStorage) which is the independently approved original
// storage selection.
//
// Returns read-only ownership, without signalling processes or modifying cgroups.
func ClaimServiceRecovery(ctx context.Context, storage FilesystemRecoveryStorage) (*ServiceRecoveryClaim, error) {
	if ctx == nil {
		return nil, ErrInvalidLimits
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot, err := sandboxbroker.OpenLinuxRecoveryClaim(
		storage.JournalDirectory, storage.CheckpointDirectory, storage.ApprovalDirectory, storage.Binding,
	)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return nil, errors.Join(ErrServiceBusy, err)
	}
	if err != nil {
		return nil, err
	}
	checkpoint, approved, err := snapshot.Checkpoint()
	if err == nil {
		err = verifyServiceRecoveryHost(ctx, checkpoint, approved, storage.Binding)
	}
	if err != nil {
		return nil, errors.Join(err, snapshot.Close())
	}
	metadata, err := sandboxbroker.RecoveryCheckpointContext(checkpoint, approved, storage.Binding)
	if err != nil {
		return nil, errors.Join(err, snapshot.Close())
	}
	identity, err := decodeServiceRecoveryIdentity(metadata)
	if err != nil {
		return nil, errors.Join(err, snapshot.Close())
	}
	if snapshot.Service() != (storage.JournalDirectory == "") {
		return nil, errors.Join(ErrInvalidLimits, snapshot.Close())
	}
	return &ServiceRecoveryClaim{
		snapshot: snapshot, group: nil, process: nil, images: nil, imagePath: "",
		metadata:       recoveryStorageMetadata(storage),
		releaseStarted: false, releaseResult: nil, parentPath: "", identity: identity,
		mutex: sync.Mutex{}, pending: false, quiesced: false, recovering: false, recovered: false, closed: false, released: false,
	}, nil
}

// recoveryStorageMetadata lists the metadata directories an image store is bound to: the
// journal, checkpoint and approval directories of a filesystem launch, or the checkpoint
// and approval directories of a service-only one.
//
// Takes storage (FilesystemRecoveryStorage) which names the directories.
//
// Returns []string which the launch used when it opened its image store.
func recoveryStorageMetadata(storage FilesystemRecoveryStorage) []string {
	if storage.JournalDirectory == "" {
		return []string{storage.CheckpointDirectory, storage.ApprovalDirectory}
	}
	return []string{storage.JournalDirectory, storage.CheckpointDirectory, storage.ApprovalDirectory}
}
