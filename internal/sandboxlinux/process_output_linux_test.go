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
	"sync"
	"sync/atomic"
	"testing"
)

func TestWorkerSharedOutputBudget(t *testing.T) {
	t.Parallel()
	for _, scriptFirst := range []bool{false, true} {
		var cancelled atomic.Int32
		var process WorkerProcess
		process.output = &workerOutput{
			cancel: func() { cancelled.Add(1) }, data: nil, limit: 8, used: 0,
			mutex: sync.Mutex{}, exceeded: false,
		}
		if scriptFirst {
			if err := process.ChargeOutput(5); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := process.output.Write([]byte("abc")); err != nil {
			t.Fatal(err)
		}
		if !scriptFirst {
			if err := process.ChargeOutput(5); err != nil {
				t.Fatal(err)
			}
		}
		if process.Output() != "abc" || cancelled.Load() != 0 {
			t.Fatal("script bytes entered diagnostic storage or exact limit failed")
		}
		if err := process.ChargeOutput(1); !errors.Is(err, ErrWorkerOutput) {
			t.Fatalf("combined overflow accepted: %v", err)
		}
		if _, err := process.output.Write([]byte("more")); !errors.Is(err, ErrWorkerOutput) {
			t.Fatalf("native output ignored terminal overflow: %v", err)
		}
		if cancelled.Load() != 1 {
			t.Fatalf("overflow cancellation count=%d", cancelled.Load())
		}
	}
}

func TestWorkerSharedOutputConcurrent(t *testing.T) {
	t.Parallel()
	var cancellations atomic.Int32
	var process WorkerProcess
	process.output = &workerOutput{
		cancel: func() { cancellations.Add(1) }, data: nil, limit: 1024, used: 0,
		mutex: sync.Mutex{}, exceeded: false,
	}
	var writers sync.WaitGroup
	for range 16 {
		writers.Go(func() { _ = process.ChargeOutput(128) })
		writers.Go(func() { _, _ = process.output.Write(make([]byte, 128)) })
	}
	writers.Wait()
	if process.output.used > 1024 || len(process.Output()) > 1024 || cancellations.Load() != 1 {
		t.Fatalf("used=%d captured=%d cancellations=%d", process.output.used, len(process.Output()), cancellations.Load())
	}
}

func TestWorkerOutputChargeAfterExit(t *testing.T) {
	t.Parallel()
	var process WorkerProcess
	process.context, process.cancel = context.WithCancel(context.Background())
	process.done = make(chan struct{})
	process.output = &workerOutput{
		cancel: process.cancel, data: nil, limit: 8, used: 0,
		mutex: sync.Mutex{}, exceeded: false,
	}
	process.finish(nil)
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := process.ChargeOutput(-1); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("negative accounting accepted")
	}
	if err := process.ChargeOutput(9); !errors.Is(err, ErrWorkerOutput) {
		t.Fatal("buffered result bypassed accounting after process exit")
	}
	if !errors.Is(process.Wait(), ErrWorkerOutput) || !errors.Is(process.Close(), ErrWorkerOutput) {
		t.Fatal("terminal methods lost late output overflow")
	}
}
