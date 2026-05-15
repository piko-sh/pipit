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
	"os"
	"time"
)

// Read consumes private protocol bytes, including buffered bytes after normal exit.
//
// Takes data ([]byte) which receives bytes from the worker channel.
//
// Returns int which is the number of bytes read.
// Returns error when the channel is unavailable or transport reading fails.
func (process *WorkerProcess) Read(data []byte) (int, error) {
	if process.stream == nil {
		return 0, os.ErrClosed
	}
	return process.stream.Read(data)
}

// Write sends private protocol bytes to the confined worker.
//
// Takes data ([]byte) which contains host-validated protocol bytes.
//
// Returns int which is the number of bytes written.
// Returns error when the channel is unavailable or transport writing fails.
func (process *WorkerProcess) Write(data []byte) (int, error) {
	if process.stream == nil {
		return 0, os.ErrClosed
	}
	return process.stream.Write(data)
}

// SetDeadline bounds both transport I/O and the remaining protocol phase. Expiry cancels
// the supervised process and cannot be revived by a later deadline.
//
// Takes deadline (time.Time) which is a non-zero, host-selected phase deadline.
//
// Returns error when the phase has ended or the deadline cannot be enforced.
//
// Safe for concurrent use by multiple goroutines.
func (process *WorkerProcess) SetDeadline(deadline time.Time) error {
	process.phaseMutex.Lock()
	defer process.phaseMutex.Unlock()
	if process.phaseFinished || process.stream == nil {
		return os.ErrClosed
	}
	if err := process.context.Err(); err != nil {
		return err
	}
	if deadline.IsZero() {
		return ErrInvalidLimits
	}
	now := time.Now()
	if !now.Before(deadline) || !process.phaseDeadline.IsZero() && !now.Before(process.phaseDeadline) {
		process.phaseErr = context.DeadlineExceeded
		process.cancel()
		return process.phaseErr
	}
	if err := process.stream.SetDeadline(deadline); err != nil {
		process.cancel()
		return err
	}
	if process.phaseTimer != nil {
		process.phaseTimer.Stop()
	}
	process.phaseDeadline = deadline
	process.phaseTimer = time.AfterFunc(time.Until(deadline), func() { process.expirePhase(deadline) })
	return nil
}

// expirePhase ignores superseded timers but never extends an expired phase.
//
// Takes deadline (time.Time) which identifies the timer's original phase.
//
// Safe for concurrent use by multiple goroutines.
func (process *WorkerProcess) expirePhase(deadline time.Time) {
	process.phaseMutex.Lock()
	defer process.phaseMutex.Unlock()
	if process.phaseFinished || !process.phaseDeadline.Equal(deadline) {
		return
	}
	process.phaseErr = context.DeadlineExceeded
	process.cancel()
}
