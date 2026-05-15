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
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNativeHostWatchdogBootstrap(t *testing.T) {
	if os.Getenv("PIPIT_WATCHDOG_BOOTSTRAP_CHILD") != "" {
		exerciseWatchdogBootstrapChild(t)
		return
	}
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup-v2 parent")
	}
	for _, mode := range []string{"valid", "executable", "wrong-control", "expired"} {
		t.Run(mode, func(t *testing.T) { exerciseWatchdogBootstrap(t, parent, mode) })
	}
}

func exerciseWatchdogBootstrap(t *testing.T, parent, mode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if mode == "executable" {
		executable = os.Getenv("PIPIT_TEST_HOST_WATCHDOG_BINARY")
		if executable == "" {
			t.Fatal("missing static host-watchdog fixture")
		}
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	image, err := PrepareWorkerImage(ctx, executable, sha256.Sum256(data))
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	root, err := image.Root()
	if err != nil {
		t.Fatal(err)
	}
	group, err := newGroup(parent, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer group.closeGroup(ctx)
	worker := watchdogFixtureCommand(ctx, "idle")
	if err := group.start(worker); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = worker.Process.Kill(); _ = worker.Wait() }()
	handoff, err := prepareWatchdogHandoff(group, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer handoff.Close()
	substituteWatchdogBootstrap(t, handoff, mode)
	command := exec.CommandContext(ctx, "/worker", "-test.run=^TestNativeHostWatchdogBootstrap$")
	if mode == "executable" {
		command = exec.CommandContext(ctx, "/worker")
	}
	command.Dir = "/"
	command.Env = []string{"PIPIT_WATCHDOG_BOOTSTRAP_CHILD=" + mode}
	var diagnostics bytes.Buffer
	command.Stdout, command.Stderr = &diagnostics, &diagnostics
	stream := configureWatchdogBootstrap(t, command, handoff)
	command.SysProcAttr, err = WorkerAttributes(root)
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := newGroup(parent, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.closeGroup(ctx)
	if err := supervisor.start(command); err != nil {
		t.Fatal(err)
	}
	if err := command.ExtraFiles[0].Close(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	if err := stream.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var ready [1]byte
	count, readErr := io.ReadFull(stream, ready[:])
	if mode == "valid" || mode == "executable" {
		if readErr != nil || count != 1 || ready[0] != watchdogReadyByte {
			_ = command.Wait()
			t.Fatalf("missing sealed readiness: %v %v\n%s", ready, readErr, diagnostics.String())
		}
		if err := worker.Wait(); err == nil {
			t.Fatal("sealed watchdog left worker alive")
		}
		if err := group.waitEmpty(ctx); err != nil {
			t.Fatal(err)
		}
	} else {
		if count != 0 || !errors.Is(readErr, io.EOF) {
			t.Fatalf("invalid bootstrap emitted readiness: %v %v", ready, readErr)
		}
		if err := worker.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatal("invalid authority killed target:", err)
		}
	}
	waitErr := command.Wait()
	if mode == "executable" {
		var exit *exec.ExitError
		if !errors.As(waitErr, &exit) || exit.ExitCode() != 1 || !strings.Contains(diagnostics.String(), "context deadline exceeded") {
			t.Fatalf("standalone watchdog did not enforce deadline: %v\n%s", waitErr, diagnostics.String())
		}
	} else if waitErr != nil {
		t.Fatalf("watchdog bootstrap failed: %v\n%s", waitErr, diagnostics.String())
	}
	if ctx.Err() != nil {
		t.Fatal("test timeout enforced termination instead of watchdog")
	}
}

func substituteWatchdogBootstrap(t *testing.T, handoff *watchdogHandoff, mode string) {
	t.Helper()
	switch mode {
	case "wrong-control":
		replacement, err := openControl(handoff.files[2], "memory.max", unix.O_WRONLY)
		if err != nil {
			t.Fatal(err)
		}
		_ = handoff.files[watchdogKillIndex].Close()
		handoff.files[watchdogKillIndex] = replacement
	case "expired":
		now, err := watchdogBootTime()
		if err != nil {
			t.Fatal(err)
		}
		replacement, err := sealWatchdogPolicy(now-1, handoff.files[1:])
		if err != nil {
			t.Fatal(err)
		}
		_ = handoff.files[0].Close()
		handoff.files[0] = replacement
	}
}

func configureWatchdogBootstrap(t *testing.T, command *exec.Cmd, handoff *watchdogHandoff) *os.File {
	t.Helper()
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	parent := os.NewFile(uintptr(pair[0]), "watchdog-parent")
	child := os.NewFile(uintptr(pair[1]), "watchdog-child")
	t.Cleanup(func() { _ = parent.Close(); _ = child.Close() })
	command.ExtraFiles = append([]*os.File{child}, handoff.files...)
	return parent
}

func exerciseWatchdogBootstrapChild(t *testing.T) {
	watchdog, err := BootstrapHostWatchdog()
	mode := os.Getenv("PIPIT_WATCHDOG_BOOTSTRAP_CHILD")
	if mode != "valid" {
		if err == nil || watchdog != nil {
			t.Fatal("invalid bootstrap accepted")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer watchdog.Close()
	if err := watchdog.Run(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("watchdog deadline not enforced:", err)
	}
	if err := watchdog.Run(); !errors.Is(err, errClosed) {
		t.Fatal("watchdog reused:", err)
	}
}
