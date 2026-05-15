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

	"pipit.sh/pipit/internal/sandboxbroker"
)

// reapForRecovery separates execution failure from proof of safe cleanup. A crashed
// broker is recoverable, but an unreaped process or retained cgroup is not.
//
// Takes a recovery context without extending original process lifetime.
//
// Returns only after original reaping and a successful descendant-cleanup retry.
func (process *WorkerProcess) reapForRecovery(ctx context.Context) error {
	process.cancel()
	if process.stream != nil {
		_ = process.stream.Close()
	}
	select {
	case <-process.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return process.cleanup()
}

// LaunchFilesystemRecovery reaps the original broker before launching sealed cleanup. It
// accepts only a recovery bootstrap bound to that broker's original namespace.
//
// Takes config (WorkerConfig) which is the approved image configuration.
// Takes original (*WorkerProcess) which is the retained original owner.
// Takes bootstrap (*sandboxbroker.LinuxBootstrap) which holds the retained bootstrap
// handles.
//
// Returns a recovery process, including partial failure ownership, or an error. Always
// close returned processes. Retain the original owner if cleanup fails.
func LaunchFilesystemRecovery(ctx context.Context, config WorkerConfig, original *WorkerProcess,
	bootstrap *sandboxbroker.LinuxBootstrap,
) (*WorkerProcess, error) {
	return launchFilesystemRecovery(ctx, config, original, bootstrap, nil)
}

// launchFilesystemRecoveryInGroup keeps recovery beneath the original aggregate. The
// caller retains that parent until all recovery ownership is closed.
//
// Takes config (WorkerConfig) which is the approved configuration.
// Takes original (*WorkerProcess) which is the original owner.
// Takes bootstrap (*sandboxbroker.LinuxBootstrap) which holds the recovery policy.
// Takes parent (*Group) which is the pinned aggregate parent.
//
// Returns a recovery owner without a standalone or unconfined fallback.
func launchFilesystemRecoveryInGroup(ctx context.Context, config WorkerConfig, original *WorkerProcess,
	bootstrap *sandboxbroker.LinuxBootstrap, parent *Group,
) (*WorkerProcess, error) {
	if parent == nil {
		return nil, ErrInvalidLimits
	}
	return launchFilesystemRecovery(ctx, config, original, bootstrap, parent)
}

// launchFilesystemRecovery validates role binding before cancelling the original.
//
// Takes config (WorkerConfig) which holds the host-owned lifecycle configuration.
// Takes original (*WorkerProcess) which is the original process to be recovered.
// Takes bootstrap (*sandboxbroker.LinuxBootstrap) which holds the recovery bootstrap
// handles.
// Takes parent (*Group) which is the optional pinned aggregate parent.
//
// Returns no new process unless original reaping and descendant cleanup succeed.
func launchFilesystemRecovery(ctx context.Context, config WorkerConfig, original *WorkerProcess,
	bootstrap *sandboxbroker.LinuxBootstrap, parent *Group,
) (*WorkerProcess, error) {
	if ctx == nil || original == nil || original.brokerNamespace == "" || !bootstrap.RecoveryOnly() ||
		bootstrap.StagingNamespace() != original.brokerNamespace || original.brokerParent != parent {
		return nil, ErrInvalidLimits
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	files, err := bootstrap.Files()
	if err != nil {
		return nil, err
	}
	if err := original.reapForRecovery(ctx); err != nil {
		return nil, err
	}
	return launchConfinedProcess(ctx, config, &filesystemBrokerAdmission, files, parent)
}
