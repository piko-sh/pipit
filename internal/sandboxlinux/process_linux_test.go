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
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerOutputBound(t *testing.T) {
	t.Parallel()
	var cancellations atomic.Int32
	output := workerOutput{
		cancel: func() { cancellations.Add(1) }, data: nil, limit: 8, used: 0,
		mutex: sync.Mutex{}, exceeded: false,
	}
	for _, data := range []string{"abc", "defgh"} {
		if count, err := output.Write([]byte(data)); count != len(data) || err != nil {
			t.Fatalf("within budget: count=%d error=%v", count, err)
		}
	}
	if cancellations.Load() != 0 {
		t.Fatal("exact limit cancelled the worker")
	}
	if count, err := output.Write([]byte("excess")); count != 0 || !errors.Is(err, ErrWorkerOutput) {
		t.Fatalf("overflow: count=%d error=%v", count, err)
	}
	if count, err := output.Write([]byte("more")); count != 0 || !errors.Is(err, ErrWorkerOutput) {
		t.Fatalf("terminal overflow: count=%d error=%v", count, err)
	}
	if cancellations.Load() != 1 || string(output.data) != "abcdefgh" {
		t.Fatalf("cancellations=%d retained=%q", cancellations.Load(), output.data)
	}
}

func TestWorkerOutputConcurrent(t *testing.T) {
	t.Parallel()
	var cancellations atomic.Int32
	output := workerOutput{
		cancel: func() { cancellations.Add(1) }, data: nil, limit: 1024, used: 0,
		mutex: sync.Mutex{}, exceeded: false,
	}
	var writers sync.WaitGroup
	for range 16 {
		writers.Go(func() {
			_, _ = output.Write(make([]byte, 1024))
		})
	}
	writers.Wait()
	if len(output.data) != output.limit || cancellations.Load() != 1 {
		t.Fatalf("bytes=%d cancellations=%d", len(output.data), cancellations.Load())
	}
}

func TestLaunchWorkerRejectsInvalidPolicy(t *testing.T) {
	t.Parallel()
	for _, config := range []WorkerConfig{
		{Tenant: testTenant, Lifetime: -time.Second},
		{Tenant: testTenant, OutputBytes: -1},
		{Tenant: testTenant, OutputBytes: maximumWorkerOutput + 1},
		{Tenant: ""},
	} {
		process, err := LaunchWorker(context.Background(), config)
		if process != nil || !errors.Is(err, ErrInvalidLimits) {
			t.Fatalf("process=%v error=%v", process, err)
		}
	}
	if process, err := LaunchWorker(nil, WorkerConfig{Tenant: testTenant}); process != nil || err == nil {
		t.Fatalf("nil context: process=%v error=%v", process, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if process, err := LaunchWorker(ctx, WorkerConfig{Tenant: testTenant}); process != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context: process=%v error=%v", process, err)
	}
}

func TestLaunchWorkerFailureRetainsCloseableOwner(t *testing.T) {
	t.Parallel()
	process, err := LaunchWorker(context.Background(), WorkerConfig{Tenant: testTenant})
	if err == nil || process == nil {
		t.Fatalf("process=%v error=%v", process, err)
	}
	if process.Stream() != nil || process.Output() != "" {
		t.Fatal("invalid executable acquired execution resources")
	}
	if process.Wait() == nil || process.Close() == nil || process.Close() == nil {
		t.Fatal("launch failure was lost")
	}
}

func TestWorkerProcessHostile(t *testing.T) {
	skipUnderCoverage(t)
	if mode := os.Getenv("PIPIT_TEST_PROCESS_HOSTILE"); mode != "" {
		if mode == "exit" {
			return
		}
		if mode == "flood" {
			for {
				_, _ = os.Stdout.Write(make([]byte, 8192))
			}
		}
		time.Sleep(time.Hour)
		return
	}
	for _, mode := range []string{"deadline", "phase_deadline", "flood", "close", "exit"} {
		t.Run(mode, func(t *testing.T) {
			lifetime := 3 * time.Second
			if mode == "deadline" {
				lifetime = 200 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), lifetime)
			defer cancel()
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			var process WorkerProcess
			process.context = ctx
			process.cancel = cancel
			process.stream = reader
			process.done = make(chan struct{})
			process.output = &workerOutput{
				cancel: cancel, data: nil, limit: 1024, used: 0,
				mutex: sync.Mutex{}, exceeded: false,
			}
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWorkerProcessHostile$")
			command.Env = append(os.Environ(), "PIPIT_TEST_PROCESS_HOSTILE="+mode)
			command.Stdout = process.output
			command.Stderr = process.output
			command.WaitDelay = workerPipeTimeout
			if err := command.Start(); err != nil {
				_ = reader.Close()
				t.Fatal(err)
			}
			process.watch(command)
			defer process.Close()
			if mode == "phase_deadline" {
				if err := process.SetDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "close" {
				_ = process.Close()
			}
			err = process.Wait()
			expected := context.DeadlineExceeded
			switch mode {
			case "flood":
				expected = ErrWorkerOutput
			case "close":
				expected = context.Canceled
			case "exit":
				expected = nil
			}
			if !errors.Is(err, expected) {
				t.Fatalf("expected %v, got %v", expected, err)
			}
			if mode == "phase_deadline" && !errors.Is(ctx.Err(), context.Canceled) {
				t.Fatalf("whole-process lifetime, not the phase deadline, stopped execution: %v", ctx.Err())
			}
			if command.ProcessState == nil {
				t.Fatal("worker was not reaped")
			}
			if len(process.Output()) > 1024 {
				t.Fatal("diagnostic capture exceeded its bound")
			}
			if mode == "exit" {
				if _, err := writer.Write([]byte("x")); err != nil {
					t.Fatal(err)
				}
				if _, err := reader.Read(make([]byte, 1)); err != nil {
					t.Fatalf("successful exit discarded pending protocol bytes: %v", err)
				}
				return
			}
			if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) && !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("protocol endpoint remained open: %v", err)
			}
		})
	}
}
