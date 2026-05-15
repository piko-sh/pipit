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

	"pipit.sh/pipit/internal/sandboxbroker"
)

// RecoverFilesystem runs approved original journal cleanup in a confined helper. Original
// quiescence is mandatory and failed attempts retain ownership until retry.
//
// Takes config (WorkerConfig) which is the approved helper configuration.
// Takes grants ([]sandboxbroker.LinuxRootGrant) which holds the current host roots.
//
// Returns success after protocol completion, process reaping and recursive emptiness.
//
// Safe for concurrent use by multiple goroutines.
func (owner *ServiceRecoveryClaim) RecoverFilesystem(ctx context.Context, config WorkerConfig, grants []sandboxbroker.LinuxRootGrant) error {
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
	if err := owner.checkFilesystemRecoveryAdmission(ctx); err != nil {
		return err
	}
	if config.ImageStore != nil && config.ImageStore != owner.images {
		return ErrInvalidLimits
	}
	config.ImageStore = owner.images
	if err := owner.checkRecoveryImageStore(config.ImageStore, grants); err != nil {
		return err
	}
	owner.recovering, owner.pending, owner.recovered = true, true, false
	if err := owner.prepareFilesystemRecovery(ctx); err != nil {
		return err
	}
	bootstrap, err := owner.snapshot.PrepareRecovery(grants)
	if err != nil {
		return err
	}
	if bootstrap != nil {
		if err := owner.runFilesystemRecovery(ctx, config, bootstrap); err != nil {
			return err
		}
	}
	if err := recoveryDirectoryEmpty(owner.group.directory); err != nil {
		return err
	}
	owner.recovering, owner.pending, owner.recovered, owner.quiesced = false, false, true, true
	return nil
}

// checkFilesystemRecoveryAdmission rejects fresh work after durable release readiness.
//
// Takes a bounded context while exclusive recovery ownership is held.
//
// Returns failure without changing retained recovery or cleanup state.
func (owner *ServiceRecoveryClaim) checkFilesystemRecoveryAdmission(ctx context.Context) error {
	if owner.closed || owner.snapshot == nil {
		return errClosed
	}
	if owner.releaseStarted || owner.group == nil || !owner.recovering && (!owner.quiesced || owner.pending) {
		return ErrServiceBusy
	}
	if err := ctx.Err(); err != nil {
		return err
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

// checkRecoveryImageStore requires the original approved image-store identity and policy.
//
// Takes store (*sandboxbroker.LinuxImageStore) which is the claimed image store.
// Takes grants ([]sandboxbroker.LinuxRootGrant) which are the current host roots.
//
// Returns failure before helper admission instead of falling back to temporary storage.
func (owner *ServiceRecoveryClaim) checkRecoveryImageStore(store *sandboxbroker.LinuxImageStore, grants []sandboxbroker.LinuxRootGrant) error {
	var empty sandboxbroker.LinuxImageStoreIdentity
	if owner.identity.Images == empty {
		if store != nil {
			return ErrInvalidLimits
		}
		return nil
	}
	if store == nil {
		return ErrInvalidLimits
	}
	identity, err := store.Identity()
	if err != nil {
		return err
	}
	if identity != owner.identity.Images {
		return ErrInvalidLimits
	}
	return store.ValidatePolicy(grants, nil)
}

// prepareFilesystemRecovery reaps previous attempts before touching their cgroups. The
// caller must hold owner.mutex.
//
// Takes a fresh bounded retry context with exclusive recovery ownership held.
//
// Returns only after old process resources and empty descendant directories are gone.
func (owner *ServiceRecoveryClaim) prepareFilesystemRecovery(ctx context.Context) error {
	if owner.process != nil {
		if err := owner.process.reapForRecovery(ctx); err != nil {
			return err
		}
		owner.process = nil
	}
	owner.group.mutex.Lock()
	defer owner.group.mutex.Unlock()
	if err := owner.group.verifyDirectoryEntry(); err != nil {
		return err
	}
	if err := recoveryDirectoryEmpty(owner.group.directory); err != nil {
		return err
	}
	if err := pruneRecoveryDescendants(ctx, owner.group.directory); err != nil {
		return err
	}
	owner.quiesced = true
	return nil
}

// runFilesystemRecovery retains the helper even if launch or protocol exchange fails. The
// caller must hold owner.mutex.
//
// Takes config (WorkerConfig) which is the independent approved native configuration.
// Takes bootstrap (*sandboxbroker.LinuxBootstrap) which is the sealed recovery-only
// bootstrap.
//
// Returns failure without dropping cleanup ownership or granting normal broker admission.
func (owner *ServiceRecoveryClaim) runFilesystemRecovery(ctx context.Context, config WorkerConfig, bootstrap *sandboxbroker.LinuxBootstrap) (result error) {
	defer func() { result = errors.Join(result, bootstrap.Close()) }()
	if !bootstrap.RecoveryOnly() {
		return ErrInvalidLimits
	}
	files, err := bootstrap.Files()
	if err != nil {
		return err
	}
	if err := owner.allowRecoveryChildren(); err != nil {
		return err
	}
	owner.quiesced = false
	owner.process, err = launchConfinedProcess(ctx, config, &filesystemBrokerAdmission, files, owner.group)
	owner.group.mutex.Lock()
	owner.group.closing = true
	owner.group.mutex.Unlock()
	if err != nil {
		return err
	}
	if err := sandboxbroker.ExchangeFilesystemRecovery(ctx, owner.process); err != nil {
		return err
	}
	if err := owner.process.reapForRecovery(ctx); err != nil {
		return err
	}
	owner.process = nil
	return nil
}

// allowRecoveryChildren enables only the private launch window under retained ownership.
// The caller must hold owner.mutex.
//
// Returns failure before launch when original identity or subtree controls are
// unavailable.
func (owner *ServiceRecoveryClaim) allowRecoveryChildren() error {
	owner.group.mutex.Lock()
	defer owner.group.mutex.Unlock()
	if err := owner.group.verifyDirectoryEntry(); err != nil {
		return err
	}
	if err := recoveryDirectoryEmpty(owner.group.directory); err != nil {
		return err
	}
	if err := enableChildControllers(owner.group.directory); err != nil {
		return err
	}
	owner.group.closing = false
	return nil
}
