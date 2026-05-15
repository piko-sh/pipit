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
	"bytes"
	"context"
	"crypto/sha256"
	"path/filepath"

	"pipit.sh/pipit/internal/sandboxbroker"
)

// FilesystemRecoveryStorage selects independently provisioned private recovery stores.
// Each directory must be empty, durable and disjoint from grants and the other stores.
type FilesystemRecoveryStorage struct {
	// JournalDirectory holds the broker's journal; empty selects the service-only profile of
	// a worker launch without a filesystem broker.
	JournalDirectory string

	// CheckpointDirectory holds persisted recovery checkpoints.
	CheckpointDirectory string

	// ApprovalDirectory holds persisted recovery approvals.
	ApprovalDirectory string

	// ServiceContext is an opaque host-captured identity covering the execution policy. It
	// must not be derived from a checkpoint or approval loaded from these directories.
	ServiceContext []byte

	// Binding covers the host, tenant and complete execution policy.
	Binding [sha256.Size]byte
}

// persistCheckpoint retains storage leases through publication and subsequent cleanup.
//
// Takes storage (FilesystemRecoveryStorage) which is the independently approved recovery
// policy before any child has been launched.
//
// Returns failure without admitting filesystem operations or discarding partial state.
func (state *filesystemBrokerRecovery) persistCheckpoint(storage FilesystemRecoveryStorage) error {
	var err error
	state.checkpoint, err = sandboxbroker.OpenLinuxRecoveryCheckpointStore(storage.CheckpointDirectory)
	if err != nil {
		return err
	}
	state.approval, err = sandboxbroker.OpenLinuxRecoveryApprovalStore(storage.ApprovalDirectory)
	if err != nil {
		return err
	}
	_, err = state.authority.PersistApprovedCheckpointWithContext(state.journal, state.checkpoint, state.approval, storage.Binding, storage.ServiceContext)
	return err
}

// OpenCheckpointedFilesystemBrokerInGroup persists recovery approval before launch. All
// three storage leases remain owned until execution and recovery cleanup finish.
//
// Takes config (WorkerConfig) which is the approved policy.
// Takes grants ([]sandboxbroker.LinuxRootGrant) which holds the host-approved root
// grants.
// Takes limits (sandboxbroker.FilesystemLimits) which selects the broker's cumulative
// quotas.
// Takes storage (FilesystemRecoveryStorage) which holds the separate private recovery
// stores.
// Takes parent (*Group) which is the mandatory aggregate parent.
//
// Returns partial ownership on failure. Close every non-nil owner and retry cleanup.
func OpenCheckpointedFilesystemBrokerInGroup(ctx context.Context, config WorkerConfig, grants []sandboxbroker.LinuxRootGrant,
	limits sandboxbroker.FilesystemLimits, storage FilesystemRecoveryStorage, parent *Group,
) (*FilesystemBroker, error) {
	if parent == nil || storage.Binding == ([sha256.Size]byte{}) ||
		!filepath.IsAbs(storage.JournalDirectory) || !filepath.IsAbs(storage.CheckpointDirectory) ||
		!filepath.IsAbs(storage.ApprovalDirectory) {
		return nil, ErrInvalidLimits
	}
	if err := validateCheckpointImageStorage(parent, config.ImageStore, grants, storage); err != nil {
		return nil, err
	}
	return openJournalledFilesystemBroker(ctx, config, grants, limits, storage.JournalDirectory, parent, &storage)
}

// validateCheckpointImageStorage matches explicit image storage before publishing
// approval.
//
// Takes parent (*Group) which is the aggregate parent.
// Takes store (*sandboxbroker.LinuxImageStore) which is the optional private image store.
// Takes grants ([]sandboxbroker.LinuxRootGrant) which holds the current root grants.
// Takes storage (FilesystemRecoveryStorage) which is the original storage selection.
//
// Returns failure before launch or metadata publication on missing or changed image
// binding.
func validateCheckpointImageStorage(parent *Group, store *sandboxbroker.LinuxImageStore, grants []sandboxbroker.LinuxRootGrant, storage FilesystemRecoveryStorage) error {
	metadata := storage.ServiceContext
	if len(metadata) == 0 {
		if store != nil {
			return ErrInvalidLimits
		}
		return nil
	}
	identity, err := decodeServiceRecoveryIdentity(metadata)
	if err != nil {
		return err
	}
	if store == nil {
		var empty sandboxbroker.LinuxImageStoreIdentity
		if identity.Images != empty {
			return ErrInvalidLimits
		}
		return nil
	}
	if err := store.ValidatePolicy(grants, []string{storage.JournalDirectory, storage.CheckpointDirectory, storage.ApprovalDirectory}); err != nil {
		return err
	}
	expected, err := parent.RecoveryIdentityWithImages(store)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, metadata) {
		return ErrInvalidLimits
	}
	return nil
}

// checkBrokerImageStorage validates current exclusions before preparing a broker.
//
// Takes store (*sandboxbroker.LinuxImageStore) which is the optional private image
// storage.
// Takes grants ([]sandboxbroker.LinuxRootGrant) which holds the current root grants.
// Takes metadata ([]string) which holds the metadata directories to exclude.
//
// Returns failure before bootstrap or staging if private image exclusion is incomplete.
func checkBrokerImageStorage(ctx context.Context, store *sandboxbroker.LinuxImageStore, grants []sandboxbroker.LinuxRootGrant, metadata []string) error {
	if ctx == nil {
		return ErrInvalidLimits
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if store != nil {
		return store.ValidatePolicy(grants, metadata)
	}
	return nil
}
