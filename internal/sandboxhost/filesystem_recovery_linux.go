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

package sandboxhost

import (
	"context"
	"errors"
	"path/filepath"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxlinux"
)

// filesystemRecoveryPhase tracks the current step in a recovery sequence.
type filesystemRecoveryPhase uint8

const (
	// filesystemRecoveryClaimed is the phase after claim acquisition.
	filesystemRecoveryClaimed filesystemRecoveryPhase = iota

	// filesystemRecoveryImages is the phase that reclaims orphan images.
	filesystemRecoveryImages

	// filesystemRecoveryCleanup is the phase that cleans approved writes.
	filesystemRecoveryCleanup

	// filesystemRecoveryRelease is the phase that releases the service.
	filesystemRecoveryRelease

	// filesystemRecoveryResume is the phase that resumes a prior release.
	filesystemRecoveryResume

	// filesystemRecoveryComplete marks a fully recovered record.
	filesystemRecoveryComplete
)

// linuxFilesystemRecovery holds one claim per recoverable filesystem launch record of the
// tenant.
type linuxFilesystemRecovery struct {
	// records holds each claimed recovery entry.
	records []*linuxFilesystemRecoveryRecord

	// closed is true once Close has released all claims.
	closed bool
}

// Recover advances every claimed record, keeping the failed ones for a retry.
//
// Takes a fresh attempt context while the public owner serialises lifecycle operations.
//
// Returns public error categories without discarding native failure details.
func (owner *linuxFilesystemRecovery) Recover(ctx context.Context) error {
	var failures error
	for _, entry := range owner.records {
		if entry.phase == filesystemRecoveryComplete {
			continue
		}
		if owner.closed {
			return ErrIsolatedUsed
		}
		failures = errors.Join(failures, entry.Recover(ctx))
	}
	return failures
}

// Close preserves unfinished native cleanup ownership rather than dropping its leases.
//
// Returns success only when every native claim permits closure.
func (owner *linuxFilesystemRecovery) Close() error {
	if owner.closed {
		return nil
	}
	var failures error
	for _, entry := range owner.records {
		if err := entry.claim.Close(); err != nil {
			failures = errors.Join(failures, filesystemLaunchError(err))
		}
	}
	if failures != nil {
		return failures
	}
	owner.closed = true
	return nil
}

// linuxFilesystemRecoveryRecord is the recovery of one launch record.
type linuxFilesystemRecoveryRecord struct {
	// claim is the native service recovery claim.
	claim *sandboxlinux.ServiceRecoveryClaim

	// roots holds the host-approved filesystem root grants.
	roots []sandboxbroker.LinuxRootGrant

	// record is the launch checkpoint on trusted local storage.
	record launchRecord

	// native holds the worker configuration for recovery.
	native sandboxlinux.WorkerConfig

	// phase tracks the current recovery step.
	phase filesystemRecoveryPhase
}

// Recover advances only completed phases and retains failed helper ownership for retry.
//
// Takes a fresh attempt context while the public owner serialises lifecycle operations.
//
// Returns public error categories without discarding native failure details; the launch
// record is removed once the original service has been released.
func (owner *linuxFilesystemRecoveryRecord) Recover(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, filesystemCleanupTimeout)
	defer cancel()
	if err := filesystemLaunchError(owner.recover(ctx)); err != nil {
		return err
	}
	if err := owner.claim.Close(); err != nil {
		return filesystemLaunchError(err)
	}
	return owner.record.remove()
}

// recover selects durable release resumption or the original confined cleanup sequence.
//
// Takes a bounded attempt context while exclusive public ownership is held.
//
// Returns success only after the original service has been safely released.
func (owner *linuxFilesystemRecoveryRecord) recover(ctx context.Context) error {
	if owner.phase == filesystemRecoveryClaimed {
		if err := owner.selectRecoveryPhase(ctx); err != nil {
			return err
		}
	}
	if owner.phase == filesystemRecoveryImages {
		if err := owner.claim.RecoverImages(ctx, owner.record.imagesDirectory(), owner.roots); err != nil {
			return err
		}
		owner.phase = filesystemRecoveryCleanup
	}
	if owner.phase == filesystemRecoveryCleanup {
		if err := owner.claim.RecoverFilesystem(ctx, owner.native, owner.roots); err != nil {
			return err
		}
		owner.phase = filesystemRecoveryRelease
	}
	var err error
	if owner.phase == filesystemRecoveryResume {
		err = owner.claim.ResumeRelease(ctx, owner.native.CgroupParent)
	} else {
		err = owner.claim.Release(ctx)
	}
	if err != nil {
		return err
	}
	owner.phase = filesystemRecoveryComplete
	return nil
}

// selectRecoveryPhase authenticates readiness before touching reusable image storage.
//
// Takes a bounded attempt context with exclusive recovery ownership.
//
// Returns error when readiness cannot be determined.
func (owner *linuxFilesystemRecoveryRecord) selectRecoveryPhase(ctx context.Context) error {
	ready, err := owner.claim.ReleaseReady()
	if err != nil {
		return err
	}
	if ready {
		owner.phase = filesystemRecoveryResume
		return nil
	}
	if err := owner.claim.Quiesce(ctx, owner.native.CgroupParent); err != nil {
		return err
	}
	owner.phase = filesystemRecoveryImages
	return nil
}

// newIsolatedFilesystemRecovery binds current host policy before opening original stores.
//
// Takes config (*IsolatedFilesystemConfig) which is the validated private configuration
// copy.
//
// Returns no authority on changed approval or unsupported isolation; records whose lease
// another process holds are skipped, and a record that cannot be authenticated fails the
// claim closed.
func newIsolatedFilesystemRecovery(ctx context.Context, config *IsolatedFilesystemConfig) (isolatedFilesystemRecovery, error) {
	if !filepath.IsAbs(config.Worker.LinuxCgroupParent) {
		return nil, ErrInvalidIsolatedConfig
	}
	ctx, cancel := context.WithTimeout(ctx, filesystemCleanupTimeout)
	defer cancel()
	policy, err := isolatedFilesystemRecoveryPolicy(ctx, config)
	if err != nil {
		return nil, filesystemLaunchError(err)
	}
	binding, err := sandboxlinux.RecoveryHostBinding(ctx, policy)
	if err != nil {
		return nil, filesystemLaunchError(err)
	}
	records, err := listLaunchRecords(config.Worker.StateDirectory, config.Worker.Tenant)
	if err != nil {
		return nil, launchRecordError(err)
	}
	roots := make([]sandboxbroker.LinuxRootGrant, len(config.Roots))
	for index, root := range config.Roots {
		roots[index] = sandboxbroker.LinuxRootGrant{Name: root.Name, Path: root.HostPath, Rights: root.Rights}
	}
	owner := &linuxFilesystemRecovery{records: nil, closed: false}
	for _, record := range records {
		if !record.journal {
			continue
		}
		claim, err := sandboxlinux.ClaimServiceRecovery(ctx, sandboxlinux.FilesystemRecoveryStorage{
			JournalDirectory: record.journalDirectory(), CheckpointDirectory: record.checkpointDirectory(),
			ApprovalDirectory: record.approvalDirectory(), Binding: binding, ServiceContext: nil,
		})
		if errors.Is(err, sandboxlinux.ErrServiceBusy) {
			continue
		}
		if err != nil {
			return nil, errors.Join(filesystemLaunchError(err), owner.Close())
		}
		owner.records = append(owner.records, &linuxFilesystemRecoveryRecord{
			claim: claim, roots: roots, native: filesystemNativeConfig(config), record: record,
			phase: filesystemRecoveryClaimed,
		})
	}
	return owner, nil
}

// filesystemNativeConfig shares helper approvals and limits across launch and recovery.
//
// Takes config (*IsolatedFilesystemConfig) which is the validated independently approved
// complete filesystem policy.
//
// Returns the broker configuration without deriving approval from an executable path.
func filesystemNativeConfig(config *IsolatedFilesystemConfig) sandboxlinux.WorkerConfig {
	lifetime := config.Worker.Lifetime
	if lifetime == 0 {
		lifetime = defaultFilesystemLifetime
	}
	return sandboxlinux.WorkerConfig{
		ImageStore: nil, Checkpoint: nil, Cache: nil, Tenant: config.Worker.Tenant,
		Executable: config.BrokerPath, Digest: config.BrokerSHA256, CgroupParent: config.Worker.LinuxCgroupParent,
		WatchdogExecutable: config.Worker.WatchdogPath, WatchdogDigest: config.Worker.WatchdogSHA256,
		Lifetime: lifetime, OutputBytes: config.Worker.OutputBytes,
		Limits: sandboxlinux.Limits{MemoryBytes: config.Worker.MemoryBytes, CPUMilli: config.Worker.CPUMilli, Tasks: config.Worker.Tasks},
	}
}
