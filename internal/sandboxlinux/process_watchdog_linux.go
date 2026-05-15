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
	"io"
	"math"
	"time"
)

const (
	// watchdogStartupTimeout is the maximum time for watchdog readiness.
	watchdogStartupTimeout = 10 * time.Second

	// watchdogDiagnosticBytes is the output limit for the watchdog process.
	watchdogDiagnosticBytes = 4 << 10
)

// launchHostWatchdog starts an approved supervisor inside a retained aggregate.
//
// Takes config (WorkerConfig) which holds the executable approval.
// Takes parent (*Group) which is the pinned parent, retained until cleanup.
//
// Returns ownership on partial failure. Caller cancellation does not disarm the watchdog
// after readiness, so close it only after protected descendants are reaped.
func launchHostWatchdog(ctx context.Context, config WorkerConfig, parent *Group) (*WorkerProcess, error) {
	if ctx == nil || parent == nil {
		return nil, ErrInvalidLimits
	}
	boot, err := watchdogBootTime()
	if err != nil {
		return nil, err
	}
	lifetime, err := watchdogLaunchLifetime(ctx, config.Lifetime)
	if err != nil || boot > math.MaxInt64-int64(lifetime) {
		return nil, errors.Join(ErrInvalidLimits, err)
	}
	startupDeadline := time.Now().Add(min(lifetime, watchdogStartupTimeout))
	handoff, err := prepareWatchdogHandoffAt(parent, boot+int64(lifetime))
	if err != nil {
		return nil, err
	}
	config.Lifetime = lifetime + workerCleanupTimeout
	config.OutputBytes = watchdogDiagnosticBytes

	config.Checkpoint = nil
	var gate admissionTable
	process, launchErr := launchNativeProcess(context.WithoutCancel(ctx), config, &gate, handoff.files, parent, false)
	closeErr := handoff.Close()
	if err := errors.Join(launchErr, closeErr); err != nil {
		return process, err
	}
	if process == nil {
		return nil, ErrUnavailable
	}
	if err := awaitWatchdogReady(ctx, process, startupDeadline); err != nil {
		return process, err
	}
	return process, nil
}

// watchdogLaunchLifetime binds the kernel deadline to the caller's remaining budget.
//
// Takes lifetime (time.Duration) which is the optional finite lifetime, with zero
// selecting defaults.
//
// Returns a positive capped duration without granting a fresh budget after queueing.
func watchdogLaunchLifetime(ctx context.Context, lifetime time.Duration) (time.Duration, error) {
	if ctx == nil {
		return 0, ErrInvalidLimits
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if lifetime == 0 {
		lifetime = defaultWorkerLifetime
	}
	if lifetime <= 0 || lifetime > maximumHostWatchdogLifetime {
		return 0, ErrInvalidLimits
	}
	if deadline, exists := ctx.Deadline(); exists {
		lifetime = min(lifetime, time.Until(deadline))
	}
	if lifetime <= 0 {
		return 0, context.DeadlineExceeded
	}
	return lifetime, nil
}

// awaitWatchdogReady accepts only the sealed supervisor's one-byte readiness marker.
//
// Takes process (*WorkerProcess) which is the owned process.
// Takes deadline (time.Time) which is the bounded startup deadline.
//
// Returns error on cancellation, premature exit, malformed readiness or expired startup.
func awaitWatchdogReady(ctx context.Context, process *WorkerProcess, deadline time.Time) error {
	if ctx == nil || process == nil || process.stream == nil || process.context == nil {
		return ErrInvalidLimits
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := process.context.Err(); err != nil {
		return err
	}
	if deadline.IsZero() {
		return ErrInvalidLimits
	}
	if !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	if err := process.stream.SetReadDeadline(deadline); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = process.stream.Close() })
	defer stop()
	var ready [1]byte
	_, err := io.ReadFull(process.stream, ready[:])
	if err := errors.Join(err, ctx.Err(), process.context.Err()); err != nil {
		return err
	}
	if ready[0] != watchdogReadyByte {
		return ErrUnavailable
	}
	if !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return process.stream.SetReadDeadline(time.Time{})
}
