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
)

// Release removes the original service reservation after successful filesystem recovery.
// Failure before removal retains exclusive ownership for a bounded retry.
//
// Takes a cleanup context with successful recovery and no retained process.
//
// Returns removal and metadata closure errors without deleting recovery records.
//
// Safe for concurrent use by multiple goroutines.
func (owner *ServiceRecoveryClaim) Release(ctx context.Context) error {
	if owner == nil {
		return errClosed
	}
	if ctx == nil {
		return ErrInvalidLimits
	}
	ctx, cancel := context.WithTimeout(ctx, workerCleanupTimeout)
	defer cancel()
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	return owner.release(ctx)
}

// release finalises verified cleanup while exclusive recovery ownership is held. The
// caller must hold owner.mutex.
//
// Takes a bounded context and retains ownership on failures before service removal.
//
// Returns error when removal, metadata closure or prerequisites fail.
func (owner *ServiceRecoveryClaim) release(ctx context.Context) error {
	if owner.released {
		return owner.releaseResult
	}
	if owner.closed || owner.snapshot == nil {
		return errClosed
	}
	if !owner.recovered || owner.recovering || owner.process != nil || owner.group == nil || !owner.quiesced {
		return ErrServiceBusy
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	owner.pending = true
	if err := owner.prepareFilesystemRecovery(ctx); err != nil {
		return err
	}
	if err := owner.closeRecoveryImages(); err != nil {
		return err
	}
	if err := owner.snapshot.RecordServiceReleaseReady(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	result := owner.group.closeGroup(ctx)
	owner.group.mutex.Lock()
	removed := owner.group.closed
	owner.group.mutex.Unlock()
	if !removed {
		return result
	}
	owner.group = nil
	return owner.finishRelease(result)
}

// finishRelease drops metadata leases only after the original reservation is gone.
//
// Takes result (error) which holds terminal removal errors with exclusive recovery
// ownership held.
//
// Returns a cached terminal result without deleting durable recovery records.
func (owner *ServiceRecoveryClaim) finishRelease(result error) error {
	owner.releaseResult = errors.Join(result, owner.snapshot.Close())
	owner.released, owner.closed, owner.pending = true, true, false
	return owner.releaseResult
}

// closeRecoveryImages drains image ownership before durable release readiness.
//
// Returns failure with ownership retained and helper admission still closed.
func (owner *ServiceRecoveryClaim) closeRecoveryImages() error {
	if owner.releaseStarted {
		return nil
	}
	if owner.identity.Images.Inode != 0 && owner.images == nil {
		return ErrServiceBusy
	}
	if owner.images != nil {
		if err := owner.images.Close(); err != nil {
			return err
		}
	}
	owner.releaseStarted = true
	return nil
}
