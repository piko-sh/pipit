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

package sandboxhost

import (
	"context"
	"errors"
	"path/filepath"

	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/sandboxlinux"
)

// linuxIsolatedRecovery holds the claims over a tenant's service-only launch records.
type linuxIsolatedRecovery struct {
	// parent is the delegated cgroup-v2 parent path.
	parent string

	// claims holds one entry per claimed launch record.
	claims []*linuxRecoveryClaim

	// closed is true once Close has released all claims.
	closed bool
}

// Recover quiesces, prunes and releases every claimed record, removing each recovered
// record.
//
// Returns error which joins the failures; a failed record keeps its claim until Close.
func (owner *linuxIsolatedRecovery) Recover(ctx context.Context) error {
	if owner.closed {
		if len(owner.claims) == 0 {
			return nil
		}
		return ErrIsolatedUsed
	}
	var failures error
	remaining := owner.claims[:0]
	for _, entry := range owner.claims {
		if err := owner.recoverRecord(ctx, entry); err != nil {
			failures = errors.Join(failures, err)
			remaining = append(remaining, entry)
			continue
		}
	}
	owner.claims = remaining
	return failures
}

// Close releases the claims that were not recovered.
//
// Returns error which joins the close failures.
func (owner *linuxIsolatedRecovery) Close() error {
	if owner.closed {
		return nil
	}
	var failures error
	remaining := owner.claims[:0]
	for _, entry := range owner.claims {
		if err := entry.claim.Close(); err != nil {
			failures = errors.Join(failures, isolatedLaunchError(err))
			remaining = append(remaining, entry)
		}
	}
	owner.claims = remaining
	if failures != nil {
		return failures
	}
	owner.closed = true
	return nil
}

// recoverRecord runs one record through the recovery sequence and removes it.
//
// Takes entry (*linuxRecoveryClaim) which is the claimed record.
//
// Returns error when a step fails; the claim stays open for a retry or Close.
func (owner *linuxIsolatedRecovery) recoverRecord(ctx context.Context, entry *linuxRecoveryClaim) error {
	logging.LoggerFrom(ctx).Info("isolated.recovery.claim", "record", entry.record.directory)
	if err := entry.claim.Quiesce(ctx, owner.parent); err != nil {
		return isolatedLaunchError(err)
	}
	if err := entry.claim.Prune(ctx); err != nil {
		return isolatedLaunchError(err)
	}
	if err := entry.claim.RecoverImages(ctx, entry.record.imagesDirectory(), nil); err != nil {
		return isolatedLaunchError(err)
	}
	if err := entry.claim.Release(ctx); err != nil {
		return isolatedLaunchError(err)
	}
	if err := entry.claim.Close(); err != nil {
		return isolatedLaunchError(err)
	}
	logging.LoggerFrom(ctx).Info("isolated.recovery.released", "record", entry.record.directory)
	return entry.record.remove()
}

// linuxRecoveryClaim pairs a claimed record with its claim.
type linuxRecoveryClaim struct {
	// claim is the native service recovery claim.
	claim *sandboxlinux.ServiceRecoveryClaim

	// record is the launch checkpoint on trusted local storage.
	record launchRecord
}

// newIsolatedRecovery claims the tenant's recoverable launch records.
//
// Takes config (*IsolatedConfig) naming the state directory and tenant.
//
// Returns isolatedRecovery and any claim error.
func newIsolatedRecovery(ctx context.Context, config *IsolatedConfig) (isolatedRecovery, error) {
	if !filepath.IsAbs(config.LinuxCgroupParent) {
		return nil, ErrInvalidIsolatedConfig
	}
	policy, err := isolatedLaunchPolicy(ctx, config)
	if err != nil {
		return nil, err
	}
	binding, err := sandboxlinux.RecoveryHostBinding(ctx, policy)
	if err != nil {
		return nil, isolatedLaunchError(err)
	}
	records, err := listLaunchRecords(config.StateDirectory, config.Tenant)
	if err != nil {
		return nil, launchRecordError(err)
	}
	owner := &linuxIsolatedRecovery{parent: config.LinuxCgroupParent, claims: nil, closed: false}
	for _, record := range records {
		if record.journal {
			continue
		}
		claim, err := sandboxlinux.ClaimServiceRecovery(ctx, sandboxlinux.FilesystemRecoveryStorage{
			JournalDirectory: "", CheckpointDirectory: record.checkpointDirectory(),
			ApprovalDirectory: record.approvalDirectory(), Binding: binding, ServiceContext: nil,
		})
		if errors.Is(err, sandboxlinux.ErrServiceBusy) {
			continue
		}
		if err != nil {
			return nil, errors.Join(isolatedLaunchError(err), owner.Close())
		}
		owner.claims = append(owner.claims, &linuxRecoveryClaim{claim: claim, record: record})
	}
	return owner, nil
}
