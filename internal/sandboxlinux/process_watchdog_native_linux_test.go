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

//go:build linux && (amd64 || arm64) && !race

package sandboxlinux

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestNativeSupervisedHostWatchdog(t *testing.T) {
	parent, executable := os.Getenv("PIPIT_TEST_CGROUP_PARENT"), os.Getenv("PIPIT_TEST_HOST_WATCHDOG_BINARY")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup-v2 parent")
	}
	if executable == "" {
		t.Fatal("missing static watchdog fixture")
	}
	for _, mode := range []string{"deadline", "caller-cancelled", "bad-digest", "wrong-role"} {
		t.Run(mode, func(t *testing.T) { exerciseSupervisedWatchdog(t, parent, executable, mode) })
	}
}

func TestNativeWorkerWatchdogAttachment(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup-v2 parent")
	}
	workerPath := os.Getenv("PIPIT_TEST_WORKER_BINARY")
	watchdogPath := os.Getenv("PIPIT_TEST_HOST_WATCHDOG_BINARY")
	workerImage, err := os.ReadFile(workerPath)
	if err != nil {
		t.Fatal(err)
	}
	watchdogImage, err := os.ReadFile(watchdogPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"missing-approval", "bad-digest", "supervisor-exit", "caller-cancelled"} {
		t.Run(mode, func(t *testing.T) {
			config := WorkerConfig{Tenant: testTenant,
				Executable: workerPath, Digest: sha256.Sum256(workerImage), CgroupParent: parent,
				WatchdogExecutable: watchdogPath, WatchdogDigest: sha256.Sum256(watchdogImage),
				Lifetime: 10 * time.Second,
			}
			if mode == "missing-approval" {
				config.WatchdogExecutable = ""
				config.WatchdogDigest = [sha256.Size]byte{}
			}
			if mode == "bad-digest" {
				config.WatchdogDigest[0] ^= 1
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			process, err := LaunchWorker(ctx, config)
			if process != nil {
				defer process.Close()
			}
			if mode == "missing-approval" || mode == "bad-digest" {
				if !errors.Is(err, ErrInvalidWorker) || process == nil || process.image != nil {
					t.Fatalf("worker prepared without approved watchdog: process=%+v error=%v", process, err)
				}
				if err := process.cleanup(); err != nil {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if process.watchdog == nil || process.container == nil {
				t.Fatal("worker admitted without watchdog ownership")
			}
			if mode == "supervisor-exit" {
				process.watchdog.cancel()
			} else {
				cancel()
			}
			select {
			case <-process.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("protected worker outlived cancellation or supervisor exit")
			}
			if !errors.Is(process.Wait(), context.Canceled) {
				t.Fatal("protected worker did not report cancellation")
			}
			if err := process.cleanup(); err != nil {
				t.Fatal("protected resources retained:", err)
			}
			select {
			case <-process.watchdog.Done():
			default:
				t.Fatal("supervisor not reaped after protected worker cleanup")
			}
		})
	}
}

func exerciseSupervisedWatchdog(t *testing.T, parent, executable, mode string) {
	t.Helper()
	if mode == "wrong-role" {
		executable = os.Getenv("PIPIT_TEST_WORKER_BINARY")
	}
	image, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	config := WorkerConfig{Tenant: testTenant, Executable: executable, Digest: sha256.Sum256(image),
		CgroupParent: parent, Limits: Limits{}, Lifetime: time.Second, OutputBytes: 0}
	if mode == "bad-digest" {
		config.Digest[0] ^= 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	aggregate, err := newGroup(parent, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer aggregate.closeGroup(ctx)
	target, err := aggregate.newChild(Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer target.closeGroup(ctx)
	worker := watchdogFixtureCommand(ctx, "idle")
	if err := target.start(worker); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = worker.Process.Kill(); _ = worker.Wait() }()
	startup, cancelStartup := context.WithCancel(ctx)
	defer cancelStartup()
	watchdog, launchErr := launchHostWatchdog(startup, config, aggregate)
	if watchdog != nil {
		defer watchdog.Close()
	}
	if mode == "bad-digest" || mode == "wrong-role" {
		if launchErr == nil {
			t.Fatal("unapproved or wrong-role supervisor accepted")
		}
		if mode == "bad-digest" && !errors.Is(launchErr, ErrInvalidWorker) {
			t.Fatal(launchErr)
		}
		if err := worker.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatal("failed supervisor killed unadmitted target:", err)
		}
		return
	}
	if launchErr != nil {
		diagnostics := ""
		if watchdog != nil {
			diagnostics = watchdog.Output()
		}
		t.Fatalf("watchdog launch: %v\n%s", launchErr, diagnostics)
	}
	if mode == "caller-cancelled" {
		cancelStartup()
		select {
		case <-watchdog.context.Done():
			t.Fatal("caller cancellation disarmed independent watchdog")
		case <-time.After(25 * time.Millisecond):
		}
	}
	if err := worker.Wait(); err == nil {
		t.Fatal("supervised watchdog left target alive")
	}
	if err := watchdog.Wait(); err == nil {
		t.Fatal("watchdog did not terminate with its protected aggregate")
	}
	if err := watchdog.cleanup(); err != nil {
		t.Fatal("watchdog retained resources:", err)
	}
	if err := target.waitEmpty(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("fixture timeout rather than watchdog killed target")
	}
}
