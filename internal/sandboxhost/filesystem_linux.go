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
	"time"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxlinux"
	"pipit.sh/pipit/internal/sandboxworker"
)

const (
	// defaultFilesystemLifetime is the default worker lifetime.
	defaultFilesystemLifetime = 17 * time.Second

	// filesystemCleanupTimeout is the timeout for post-execution cleanup.
	filesystemCleanupTimeout = 5 * time.Second
)

// linuxFilesystemProcess is the Linux implementation of the combined source and broker
// boundary.
type linuxFilesystemProcess struct {
	// context is the shared lifetime context for both processes.
	context context.Context

	// cancel terminates the shared lifetime.
	cancel context.CancelFunc

	// worker is the source-evaluation native process.
	worker *sandboxlinux.WorkerProcess

	// broker is the filesystem broker native process.
	broker *sandboxlinux.FilesystemBroker

	// aggregate is the shared cgroup parent for both processes.
	aggregate *sandboxlinux.FilesystemAggregate

	// images holds verified executable images for this launch.
	images *sandboxbroker.LinuxImageStore

	// policy holds the filesystem configuration for the worker.
	policy sandboxworker.FilesystemConfiguration

	// record is the launch checkpoint on trusted local storage.
	record launchRecord

	// recorded is true once a launch record has been created.
	recorded bool
}

// Close cancels the shared lifetime and retains both native owners for cleanup retry.
//
// Returns any execution or cleanup error.
func (process *linuxFilesystemProcess) Close() error {
	process.cancel()
	var workerErr error
	if process.worker != nil {
		workerErr = process.worker.Close()
	}
	brokerErr := process.broker.Close()
	if process.broker.RecoveryPending() {
		return errors.Join(workerErr, brokerErr)
	}
	if process.images != nil {
		if err := process.images.Close(); err != nil {
			return errors.Join(workerErr, brokerErr, err)
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(process.context), filesystemCleanupTimeout)
	defer cancel()
	if err := process.aggregate.Close(cleanup); err != nil {
		return errors.Join(workerErr, brokerErr, err)
	}
	if !process.recorded || !process.worker.Released() || !process.broker.Released() {
		return errors.Join(workerErr, brokerErr)
	}
	process.recorded = false
	return errors.Join(workerErr, brokerErr, process.record.remove())
}

// Diagnostics returns source diagnostics without exposing native process objects.
//
// Returns string containing bounded, untrusted worker output.
func (process *linuxFilesystemProcess) Diagnostics() string {
	if process.worker == nil {
		return ""
	}
	return process.worker.Output()
}

// evaluate delegates one submission to the host-validating filesystem relay.
//
// Takes request (sandboxworker.Request) which is the source-only request.
//
// Returns a response after both native owners have completed cleanup.
func (process *linuxFilesystemProcess) evaluate(request sandboxworker.Request) (sandboxworker.Response, error) {
	response, err := sandboxworker.ExchangeFilesystem(process.context, process.worker, process.policy, request, process.broker)
	if err := errors.Join(err, process.Close()); err != nil {
		return sandboxworker.Response{}, err
	}
	return response, nil
}

// newIsolatedFilesystemProcess creates both native owners from the same copied policy.
//
// Takes config (*IsolatedFilesystemConfig) which is the validated filesystem
// configuration.
//
// Returns cleanup ownership even if the second launch fails.
func newIsolatedFilesystemProcess(ctx context.Context, config *IsolatedFilesystemConfig) (isolatedFilesystemProcess, error) {
	if !filepath.IsAbs(config.Worker.LinuxCgroupParent) {
		return nil, ErrInvalidIsolatedConfig
	}
	lifetime := config.Worker.Lifetime
	if lifetime == 0 {
		lifetime = defaultFilesystemLifetime
	}
	ctx, cancel := context.WithTimeout(ctx, lifetime)
	policy, err := isolatedFilesystemRecoveryPolicy(ctx, config)
	if err != nil {
		cancel()
		return nil, filesystemLaunchError(err)
	}
	native := filesystemNativeConfig(config)
	roots := make([]sandboxbroker.LinuxRootGrant, len(config.Roots))
	grants := make([]sandboxbroker.RootGrant, len(config.Roots))
	for index, root := range config.Roots {
		roots[index] = sandboxbroker.LinuxRootGrant{Name: root.Name, Path: root.HostPath, Rights: root.Rights}
		grants[index] = sandboxbroker.RootGrant{Name: root.Name, Rights: root.Rights}
	}
	aggregate, err := sandboxlinux.NewFilesystemAggregate(ctx, config.Worker.LinuxCgroupParent, native.Limits, config.Worker.Tenant)
	if aggregate == nil {
		cancel()
		return nil, filesystemLaunchError(err)
	}
	owner := &linuxFilesystemProcess{
		context: ctx, cancel: cancel, worker: nil, broker: nil, aggregate: aggregate, images: nil,
		policy: sandboxworker.FilesystemConfiguration{
			Profile: sandboxworker.FilesystemProfile, Imports: append(config.Worker.Imports, "pipit/fs"),
			Roots: grants, Limits: &config.Limits,
		},
		record: launchRecord{directory: "", journal: false}, recorded: false,
	}
	if err != nil {
		return owner, errors.Join(filesystemLaunchError(err), owner.Close())
	}
	owner.record, err = createLaunchRecord(config.Worker.StateDirectory, config.Worker.Tenant, true)
	if err != nil {
		return owner, errors.Join(launchRecordError(err), owner.Close())
	}
	owner.recorded = true
	owner.images, err = sandboxbroker.OpenLinuxImageStore(owner.record.imagesDirectory(), roots, owner.record.storeMetadata())
	if err != nil {
		return owner, errors.Join(filesystemLaunchError(err), owner.Close())
	}
	native.ImageStore = owner.images
	storage, err := owner.captureRecoveryStorage(policy)
	if err != nil {
		return owner, errors.Join(filesystemLaunchError(err), owner.Close())
	}
	owner.broker, err = sandboxlinux.OpenCheckpointedFilesystemBrokerInGroup(ctx, native, roots, config.Limits, storage, aggregate.Group)
	if err != nil {
		return owner, errors.Join(filesystemLaunchError(err), owner.Close())
	}
	native.Executable, native.Digest = config.Worker.WorkerPath, config.Worker.WorkerSHA256
	owner.worker, err = sandboxlinux.LaunchWorkerInGroup(ctx, native, aggregate.Group)
	if err != nil {
		return owner, errors.Join(filesystemLaunchError(err), owner.Close())
	}
	return owner, nil
}

// filesystemLaunchError preserves platform failure categories for the public API.
//
// Takes err (error) which is the native launch error.
//
// Returns joined public and native error identities without enabling fallback.
func filesystemLaunchError(err error) error {
	if errors.Is(err, sandboxlinux.ErrUnavailable) || errors.Is(err, sandboxbroker.ErrConfinement) {
		return errors.Join(ErrIsolatedUnavailable, err)
	}
	if errors.Is(err, sandboxlinux.ErrInvalidLimits) || errors.Is(err, sandboxlinux.ErrInvalidWorker) ||
		errors.Is(err, sandboxbroker.ErrInvalidPolicy) || errors.Is(err, sandboxbroker.ErrDenied) {
		return errors.Join(ErrInvalidIsolatedConfig, err)
	}
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return errors.Join(ErrIsolatedUnavailable, err)
	}
	return err
}
