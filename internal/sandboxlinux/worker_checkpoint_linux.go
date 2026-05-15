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
	"crypto/sha256"
	"errors"

	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/sandboxbroker"
)

// WorkerCheckpoint names the stores a top-level launch records its recovery identity in:
// the service cgroup's identity, the host binding and the launch's image store, so a host
// that dies mid-run leaves a record a later ClaimServiceRecovery can act on.
type WorkerCheckpoint struct {
	// CheckpointDirectory is an empty, private checkpoint store directory.
	CheckpointDirectory string

	// ApprovalDirectory is an empty, private approval store directory, on storage distinct
	// from the checkpoint store's.
	ApprovalDirectory string

	// Binding is the host binding RecoveryHostBinding computed for the launch policy.
	Binding [sha256.Size]byte
}

// persistLaunchCheckpoint records the service group's identity before any child exists.
// The checkpoint store stays leased until cleanup so a recovery claim requires host
// death.
//
// Takes config (WorkerConfig) whose Checkpoint is nil when nothing is recorded.
//
// Returns error which fails the launch closed when the record cannot be written.
func (process *WorkerProcess) persistLaunchCheckpoint(config WorkerConfig) (result error) {
	if config.Checkpoint == nil {
		return nil
	}
	if config.ImageStore == nil || process.serviceGroup == nil || config.Checkpoint.Binding == ([sha256.Size]byte{}) {
		return ErrInvalidLimits
	}
	identity, err := process.serviceGroup.RecoveryIdentityWithImages(config.ImageStore)
	if err != nil {
		return err
	}
	checkpoint, err := sandboxbroker.OpenLinuxRecoveryCheckpointStore(config.Checkpoint.CheckpointDirectory)
	if err != nil {
		return err
	}
	defer func() {
		if result != nil {
			result = errors.Join(result, checkpoint.Close())
		}
	}()
	approval, err := sandboxbroker.OpenLinuxRecoveryApprovalStore(config.Checkpoint.ApprovalDirectory)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, approval.Close()) }()
	if _, err := sandboxbroker.PersistServiceCheckpoint(checkpoint, approval, config.Checkpoint.Binding, identity); err != nil {
		return err
	}
	process.checkpoint = checkpoint
	logging.LoggerFrom(process.context).Info("isolated.checkpoint")
	return nil
}
