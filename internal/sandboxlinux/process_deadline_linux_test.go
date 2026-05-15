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
	"errors"
	"os"
	"testing"
	"time"
)

func TestWorkerPhaseDeadlineReplacement(t *testing.T) {
	t.Parallel()
	process := phaseFixture(t)
	first := time.Now().Add(time.Hour)
	second := first.Add(time.Hour)
	if err := process.SetDeadline(first); err != nil {
		t.Fatal(err)
	}
	if err := process.SetDeadline(second); err != nil {
		t.Fatal(err)
	}
	process.expirePhase(first)
	if err := process.context.Err(); err != nil {
		t.Fatalf("superseded timer cancelled a new phase: %v", err)
	}
	process.expirePhase(second)
	if !errors.Is(process.context.Err(), context.Canceled) {
		t.Fatal("phase expiry did not cancel the process")
	}
	if err := process.SetDeadline(second.Add(time.Hour)); !errors.Is(err, context.Canceled) {
		t.Fatalf("expired worker was revived: %v", err)
	}
}

func TestWorkerPhaseCannotExtendExpiredDeadline(t *testing.T) {
	t.Parallel()
	process := phaseFixture(t)
	process.phaseDeadline = time.Now().Add(-time.Second)
	if err := process.SetDeadline(time.Now().Add(time.Hour)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired phase extended before its callback ran: %v", err)
	}
	if !errors.Is(process.context.Err(), context.Canceled) {
		t.Fatal("expired phase did not cancel execution")
	}
}

func TestWorkerPhaseRejectsMissingDeadline(t *testing.T) {
	t.Parallel()
	process := phaseFixture(t)
	if err := process.SetDeadline(time.Time{}); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("disabled phase deadline accepted: %v", err)
	}
	if err := process.SetDeadline(time.Now().Add(-time.Second)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("past deadline accepted: %v", err)
	}
}

func TestWorkerPhaseIgnoresTimerAfterExit(t *testing.T) {
	t.Parallel()
	process := phaseFixture(t)
	deadline := time.Now().Add(time.Hour)
	if err := process.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	process.phaseFinished = true
	process.expirePhase(deadline)
	if err := process.context.Err(); err != nil {
		t.Fatalf("finished phase accepted a stale timeout: %v", err)
	}
	if err := process.SetDeadline(deadline); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("finished phase accepted a new deadline: %v", err)
	}
}

func phaseFixture(t *testing.T) *WorkerProcess {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	var process WorkerProcess
	process.context, process.cancel = context.WithCancel(context.Background())
	process.stream = reader
	t.Cleanup(func() {
		process.phaseMutex.Lock()
		process.phaseFinished = true
		if process.phaseTimer != nil {
			process.phaseTimer.Stop()
		}
		process.phaseMutex.Unlock()
		process.cancel()
		_ = reader.Close()
		_ = writer.Close()
	})
	return &process
}
