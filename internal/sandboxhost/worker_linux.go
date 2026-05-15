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

// linuxIsolatedProcess is a top-level worker launch with the launch record and image
// store it owns; a clean close removes the record, a failed one leaves it for recovery.
type linuxIsolatedProcess struct {
	*sandboxlinux.WorkerProcess

	// images holds verified executable images for this launch.
	images *sandboxbroker.LinuxImageStore

	// record is the launch checkpoint on trusted local storage.
	record launchRecord

	// recorded is true once a launch record has been created.
	recorded bool
}

// Close stops the worker and releases its resources. Once the native cleanup has released
// everything, whatever the run's own outcome, the image store is closed and the launch
// record removed; a failed cleanup keeps the record for NewIsolatedRecovery.
//
// Returns error which reports the run's failure and any cleanup failure.
func (process *linuxIsolatedProcess) Close() error {
	worker := process.WorkerProcess
	closeErr := worker.Close()
	if !process.recorded || !worker.Released() {
		return closeErr
	}
	if err := process.images.Close(); err != nil {
		return errors.Join(closeErr, err)
	}
	process.recorded = false
	return errors.Join(closeErr, process.record.remove())
}

// newIsolatedProcess constructs the mandatory Linux boundary.
//
// Takes config (IsolatedConfig) which is copied host-private policy.
//
// Returns isolatedProcess owning all native resources and any launch error.
func newIsolatedProcess(ctx context.Context, config IsolatedConfig) (isolatedProcess, error) {
	if !filepath.IsAbs(config.LinuxCgroupParent) {
		return nil, ErrInvalidIsolatedConfig
	}
	policy, err := isolatedLaunchPolicy(ctx, &config)
	if err != nil {
		return nil, err
	}
	binding, err := sandboxlinux.RecoveryHostBinding(ctx, policy)
	if err != nil {
		return nil, isolatedLaunchError(err)
	}
	record, err := createLaunchRecord(config.StateDirectory, config.Tenant, false)
	if err != nil {
		return nil, launchRecordError(err)
	}
	images, err := sandboxbroker.OpenLinuxImageStore(record.imagesDirectory(), nil, record.storeMetadata())
	if err != nil {
		return nil, errors.Join(isolatedLaunchError(err), record.remove())
	}
	process, err := sandboxlinux.LaunchWorker(ctx, sandboxlinux.WorkerConfig{
		ImageStore: images, Tenant: config.Tenant,
		Checkpoint: &sandboxlinux.WorkerCheckpoint{
			CheckpointDirectory: record.checkpointDirectory(), ApprovalDirectory: record.approvalDirectory(), Binding: binding,
		},
		Cache:      workerImageCache(config.Images),
		Executable: config.WorkerPath, CgroupParent: config.LinuxCgroupParent,
		WatchdogExecutable: config.WatchdogPath, WatchdogDigest: config.WatchdogSHA256,
		Digest: config.WorkerSHA256, Lifetime: config.Lifetime, OutputBytes: config.OutputBytes,
		Limits: sandboxlinux.Limits{MemoryBytes: config.MemoryBytes, CPUMilli: config.CPUMilli, Tasks: config.Tasks},
	})
	if process == nil {
		return nil, errors.Join(isolatedLaunchError(err), images.Close(), record.remove())
	}
	return &linuxIsolatedProcess{WorkerProcess: process, images: images, record: record, recorded: true}, isolatedLaunchError(err)
}

// isolatedLaunchError maps a native launch error onto the public sentinels.
//
// Takes err (error) which may be nil.
//
// Returns error which wraps ErrIsolatedUnavailable or ErrInvalidIsolatedConfig where the
// native cause says so.
func isolatedLaunchError(err error) error {
	if errors.Is(err, sandboxlinux.ErrUnavailable) {
		return errors.Join(ErrIsolatedUnavailable, err)
	}
	if errors.Is(err, sandboxlinux.ErrInvalidLimits) || errors.Is(err, sandboxlinux.ErrInvalidWorker) ||
		errors.Is(err, sandboxbroker.ErrInvalidPolicy) || errors.Is(err, sandboxbroker.ErrDenied) {
		return errors.Join(ErrInvalidIsolatedConfig, err)
	}
	return err
}
