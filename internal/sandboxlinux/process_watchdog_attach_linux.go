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

package sandboxlinux

import (
	"context"
	"errors"

	"pipit.sh/pipit/internal/sandboxwire"
)

// prepareWatchdog reserves a per-process aggregate and requires sealed supervisor
// readiness.
//
// Takes config (WorkerConfig) which is the approved worker/watchdog configuration.
// Takes parent (*Group) which is the retained outer aggregate.
//
// Returns the aggregate for source or broker placement only while its watchdog is live.
func (process *WorkerProcess) prepareWatchdog(ctx context.Context, config WorkerConfig, parent *Group) (*Group, error) {
	var err error
	process.container, err = parent.newChild(ctx, config.Limits)
	if err != nil {
		return nil, err
	}
	supervisor := config
	supervisor.Executable, supervisor.Digest = config.WatchdogExecutable, config.WatchdogDigest
	process.watchdog, err = launchHostWatchdog(ctx, supervisor, process.container)
	if err != nil {
		return nil, err
	}
	process.stopWatchdogExit = process.watchdog.onLifetimeEnd(process.cancel)
	if err := errors.Join(ctx.Err(), process.watchdog.lifetimeErr()); err != nil {
		return nil, err
	}
	return process.container, nil
}

// cleanupWatchdog releases protection only after protected process and image cleanup.
//
// Returns error while supervisor reaping, its resources or the aggregate remain owned.
func (process *WorkerProcess) cleanupWatchdog(ctx context.Context) error {
	if process.watchdog != nil {
		if process.stopWatchdogExit != nil {
			process.stopWatchdogExit()
			process.stopWatchdogExit = nil
		}
		stopCtx, cancel := sandboxwire.CleanupContext(ctx)
		err := process.watchdog.stopSupervisor(stopCtx)
		cancel()
		if err != nil {
			return err
		}
	}
	if process.container != nil {
		closeCtx, cancel := sandboxwire.CleanupContext(ctx)
		err := process.container.closeGroup(closeCtx)
		cancel()
		return err
	}
	return nil
}

// stopSupervisor reaps a stopped watchdog without treating SIGKILL as failure.
//
// Protected descendants must be confirmed gone before cleanup begins.
//
// Returns cleanup errors while ignoring the expected execution cancellation.
func (process *WorkerProcess) stopSupervisor(ctx context.Context) error {
	process.cancel()
	if process.stream != nil {
		_ = process.stream.Close()
	}
	select {
	case <-process.done:
		return process.cleanup(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}
