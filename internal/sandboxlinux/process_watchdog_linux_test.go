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
	"os"
	"testing"
	"time"
)

func TestWatchdogLaunchLifetime(t *testing.T) {
	t.Parallel()
	if _, err := watchdogLaunchLifetime(nil, time.Second); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal(err)
	}
	for _, lifetime := range []time.Duration{-1, maximumHostWatchdogLifetime + 1} {
		if _, err := watchdogLaunchLifetime(context.Background(), lifetime); !errors.Is(err, ErrInvalidLimits) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	remaining, err := watchdogLaunchLifetime(ctx, maximumHostWatchdogLifetime)
	if err != nil || remaining <= 0 || remaining > time.Second {
		t.Fatalf("caller budget not applied: %v %v", remaining, err)
	}
	cancel()
	if _, err := watchdogLaunchLifetime(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if duration, err := watchdogLaunchLifetime(context.Background(), 0); err != nil || duration != defaultWorkerLifetime {
		t.Fatalf("invalid watchdog default: %v %v", duration, err)
	}
}

func TestWatchdogReadinessValidation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"valid", "wrong-byte", "eof", "caller-cancelled", "supervisor-exited", "zero-deadline"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			defer writer.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if mode == "caller-cancelled" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
				defer cancel()
			}
			supervisor, stop := context.WithCancel(context.Background())
			defer stop()
			var process WorkerProcess
			process.stream, process.context = reader, supervisor
			deadline := time.Now().Add(time.Second)
			switch mode {
			case "valid", "supervisor-exited", "zero-deadline":
				if _, err := writer.Write([]byte{watchdogReadyByte}); err != nil {
					t.Fatal(err)
				}
			case "wrong-byte":
				if _, err := writer.Write([]byte{0}); err != nil {
					t.Fatal(err)
				}
			case "eof":
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "supervisor-exited" {
				stop()
			}
			if mode == "zero-deadline" {
				deadline = time.Time{}
			}
			err = awaitWatchdogReady(ctx, &process, deadline)
			if mode == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid supervisor readiness accepted")
			}
			if mode == "caller-cancelled" && (!errors.Is(err, context.DeadlineExceeded) || supervisor.Err() != nil) {
				t.Fatalf("startup cancellation changed independent supervisor lifetime: %v %v", err, supervisor.Err())
			}
		})
	}
}
