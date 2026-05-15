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
	"path/filepath"

	"pipit.sh/pipit/internal/sandboxbroker"
)

// RecoverImages reclaims approved orphan images only after original service quiescence.
// Failed cleanup retains image and metadata leases for a bounded retry.
//
// Takes directory (string) which is the independently selected original storage path.
// Takes grants ([]sandboxbroker.LinuxRootGrant) which holds the current approved
// filesystem grants.
//
// Returns success with the original store retained for confined recovery helpers.
//
// Safe for concurrent use by multiple goroutines.
func (owner *ServiceRecoveryClaim) RecoverImages(ctx context.Context, directory string, grants []sandboxbroker.LinuxRootGrant) error {
	if owner == nil {
		return errClosed
	}
	if ctx == nil || !filepath.IsAbs(directory) {
		return ErrInvalidLimits
	}
	ctx, cancel := context.WithTimeout(ctx, workerCleanupTimeout)
	defer cancel()
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if err := owner.checkImageRecoveryAdmission(ctx); err != nil {
		return err
	}
	if owner.images != nil && owner.imagePath != directory {
		return ErrInvalidLimits
	}
	if err := owner.prepareFilesystemRecovery(ctx); err != nil {
		return err
	}
	if owner.images == nil {
		store, err := sandboxbroker.ReopenLinuxImageStore(directory, owner.identity.Images, grants, owner.metadata[:])
		if err != nil {
			return err
		}
		owner.images, owner.imagePath = store, directory
	}
	if err := owner.images.ValidatePolicy(grants, owner.metadata[:]); err != nil {
		return err
	}
	owner.pending = true
	if err := owner.images.CleanupOrphans(ctx); err != nil {
		return err
	}
	owner.pending = false
	if owner.snapshot.Service() {
		owner.recovered = true
	}
	return nil
}

// checkImageRecoveryAdmission rejects cleanup without exclusive original ownership.
//
// Returns failure before opening image storage or changing cleanup state.
func (owner *ServiceRecoveryClaim) checkImageRecoveryAdmission(ctx context.Context) error {
	if owner.closed || owner.snapshot == nil {
		return errClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if owner.releaseStarted || owner.recovering || owner.recovered || owner.process != nil || owner.group == nil || !owner.quiesced {
		return ErrServiceBusy
	}
	if owner.identity.Images.Inode == 0 || owner.identity.Images.MountID == 0 {
		return ErrInvalidLimits
	}
	ready, err := owner.snapshot.ServiceReleaseReady()
	if err != nil {
		return err
	}
	if ready {
		return ErrServiceBusy
	}
	return nil
}
