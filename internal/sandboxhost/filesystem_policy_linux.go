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
	"crypto/sha256"
	"encoding/json"
	"runtime"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxlinux"
	"pipit.sh/pipit/internal/sandboxworker"
)

// captureRecoveryStorage measures original host and service ownership before launch.
//
// Takes policy ([sha256.Size]byte) which is the validated policy while this process holds
// the exclusive service reservation.
//
// Returns metadata for durable publication, not replacement-host recovery authority.
func (process *linuxFilesystemProcess) captureRecoveryStorage(policy [sha256.Size]byte) (sandboxlinux.FilesystemRecoveryStorage, error) {
	var empty sandboxlinux.FilesystemRecoveryStorage
	binding, err := sandboxlinux.RecoveryHostBinding(process.context, policy)
	if err != nil {
		return empty, err
	}
	service, err := process.aggregate.Group.RecoveryIdentityWithImages(process.images)
	if err != nil {
		return empty, err
	}
	return sandboxlinux.FilesystemRecoveryStorage{
		JournalDirectory: process.record.journalDirectory(), CheckpointDirectory: process.record.checkpointDirectory(),
		ApprovalDirectory: process.record.approvalDirectory(), Binding: binding, ServiceContext: service,
	}, nil
}

// isolatedFilesystemRecoveryPolicy binds the complete validated constructor policy into a
// domain-separated digest.
//
// Takes policy (*IsolatedFilesystemConfig) which is the host-selected policy, never
// approval reconstructed from persisted bytes.
//
// Returns a domain-separated digest including executable approvals and protocol roles.
func isolatedFilesystemRecoveryPolicy(ctx context.Context, policy *IsolatedFilesystemConfig) ([sha256.Size]byte, error) {
	if policy == nil {
		return [sha256.Size]byte{}, ErrInvalidIsolatedConfig
	}
	config := *policy
	if err := validateIsolatedFilesystemConfig(ctx, &config); err != nil {
		return [sha256.Size]byte{}, err
	}
	encoded, err := json.Marshal(struct {
		Profile      string
		Platform     string
		Architecture string
		Source       string
		Filesystem   string
		Broker       string
		Recovery     string
		Config       IsolatedFilesystemConfig
	}{
		Profile:  "isolated-filesystem-approval-v1",
		Platform: runtime.GOOS, Architecture: runtime.GOARCH,
		Source: sandboxworker.Profile, Filesystem: sandboxworker.FilesystemProfile,
		Broker: sandboxbroker.JournalledFilesystemProfile, Recovery: sandboxbroker.FilesystemRecoveryProfile,
		Config: config,
	})
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	if err := ctx.Err(); err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}
