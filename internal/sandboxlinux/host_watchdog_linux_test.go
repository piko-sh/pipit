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
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestHostWatchdogLifetimeValidation(t *testing.T) {
	t.Parallel()
	descriptor, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		t.Fatal(err)
	}
	host := os.NewFile(uintptr(descriptor), "host-pidfd")
	defer host.Close()
	for _, lifetime := range []time.Duration{0, -1, maximumHostWatchdogLifetime + 1} {
		if err := waitHostLifetime(host, lifetime); !errors.Is(err, ErrInvalidLimits) {
			t.Fatalf("invalid watchdog lifetime accepted: %v", err)
		}
	}
	if err := waitHostLifetime(nil, time.Second); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("missing host accepted: %v", err)
	}
	if err := waitHostLifetime(host, time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("live host bypassed watchdog deadline: %v", err)
	}
}

func TestHostWatchdogRejectsOrdinaryFiles(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "not-cgroup-kill")
	if err := os.WriteFile(path, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := waitHostLifetime(file, time.Second); err == nil {
		t.Fatal("ordinary file accepted as host pidfd")
	}
	if err := monitorHostLifetime(nil, file, time.Second); err == nil {
		t.Fatal("ordinary file accepted as termination authority")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("invalid termination handle modified: %q %v", data, err)
	}
}

func TestNativeHostWatchdog(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup-v2 parent")
	}
	for _, mode := range []string{"host-death", "namespaced-host-death", "already-dead", "deadline", "invalid-host", "invalid-lifetime"} {
		t.Run(mode, func(t *testing.T) { exerciseHostWatchdog(t, parent, mode) })
	}
}

func exerciseHostWatchdog(t *testing.T, parent, mode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	group, err := newGroup(parent, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer group.closeGroup(ctx)
	host := watchdogFixtureCommand(ctx, "idle")
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = host.Process.Kill(); _ = host.Wait() }()
	descriptor, err := unix.PidfdOpen(host.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	hostIdentity := os.NewFile(uintptr(descriptor), "host-pidfd")
	defer hostIdentity.Close()
	if mode == "invalid-host" {
		invalid, err := os.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer invalid.Close()
		hostIdentity = invalid
	}
	workerGroup, err := group.newChild(Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer workerGroup.closeGroup(ctx)
	worker := watchdogFixtureCommand(ctx, "idle")
	if err := workerGroup.start(worker); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = worker.Process.Kill(); _ = worker.Wait() }()
	otherGroup, err := group.newChild(Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer otherGroup.closeGroup(ctx)
	other := watchdogFixtureCommand(ctx, "idle")
	if err := otherGroup.start(other); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Process.Kill(); _ = other.Wait() }()
	if mode == "already-dead" {
		if err := host.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		_ = host.Wait()
	}
	watchdog := watchdogFixtureCommand(ctx, mode)
	if mode == "namespaced-host-death" {
		watchdog.SysProcAttr = &syscall.SysProcAttr{
			Cloneflags:                 unix.CLONE_NEWUSER | unix.CLONE_NEWPID,
			UidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Geteuid(), Size: 1}},
			GidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getegid(), Size: 1}},
			GidMappingsEnableSetgroups: false,
		}
	}
	watchdog.ExtraFiles = []*os.File{hostIdentity, group.kill}
	output, err := watchdog.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watchdog.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watchdog.Process.Kill(); _ = watchdog.Wait() }()
	reader := bufio.NewReader(output)
	if line, err := reader.ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("watchdog did not start: %q %v", line, err)
	}
	if mode == "host-death" || mode == "namespaced-host-death" {
		if err := host.Process.Kill(); err != nil {
			t.Fatal(err)
		}
	}
	if err := watchdog.Wait(); err != nil {
		t.Fatal("watchdog failed:", err)
	}
	if err := worker.Wait(); err == nil {
		t.Fatal("worker survived independent watchdog")
	}
	if err := other.Wait(); err == nil {
		t.Fatal("other descendant survived independent watchdog")
	}
	if err := group.waitEmpty(ctx); err != nil {
		t.Fatal("watchdog left tasks:", err)
	}
	if mode != "host-death" && mode != "namespaced-host-death" && mode != "already-dead" {
		if err := host.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatal("watchdog killed host:", err)
		}
	}
	if ctx.Err() != nil {
		t.Fatal("test watchdog rather than native monitor enforced termination")
	}
}

func TestNativeHostWatchdogChild(t *testing.T) {
	mode := os.Getenv("PIPIT_HOST_WATCHDOG_CHILD")
	if mode == "" {
		return
	}
	if mode == "idle" {
		time.Sleep(time.Minute)
		return
	}
	host := os.NewFile(3, "host-pidfd")
	kill := os.NewFile(4, "cgroup-kill")
	defer host.Close()
	defer kill.Close()
	filter, err := hostWatchdogFilter(os.Getpid(), 3, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := installWorkerFilter(filter); err != nil {
		t.Fatal(err)
	}
	verifyHostWatchdogDenials(t)
	lifetime := 5 * time.Second
	if mode == "deadline" {
		lifetime = 100 * time.Millisecond
	}
	if mode == "invalid-lifetime" {
		lifetime = 0
	}
	fmt.Println("ready")
	err = monitorHostLifetime(host, kill, lifetime)
	switch mode {
	case "invalid-lifetime":
		if !errors.Is(err, ErrInvalidLimits) {
			t.Fatal("invalid lifetime accepted:", err)
		}
	case "deadline":
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("missing watchdog deadline:", err)
		}
	case "invalid-host":
		if err == nil {
			t.Fatal("invalid host identity accepted")
		}
	default:
		if err != nil {
			t.Fatal(err)
		}
	}
}

func watchdogFixtureCommand(ctx context.Context, mode string) *exec.Cmd {
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeHostWatchdogChild$")
	command.Env = append(os.Environ(), "PIPIT_HOST_WATCHDOG_CHILD="+mode)
	return command
}

func verifyHostWatchdogDenials(t *testing.T) {
	t.Helper()
	descriptor, err := unix.Open("/dev/null", unix.O_RDONLY, 0)
	if err == nil {
		_ = unix.Close(descriptor)
	}
	if !errors.Is(err, unix.EPERM) {
		t.Fatal("watchdog retained file opens:", err)
	}
	descriptor, err = unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
	if err == nil {
		_ = unix.Close(descriptor)
	}
	if !errors.Is(err, unix.EPERM) {
		t.Fatal("watchdog retained sockets:", err)
	}
	if err := unix.PidfdSendSignal(3, unix.SIGCONT, nil, 0); !errors.Is(err, unix.EPERM) {
		t.Fatal("watchdog retained host signalling:", err)
	}
	if _, err := unix.Write(4, nil); !errors.Is(err, unix.EPERM) {
		t.Fatal("watchdog retained general cgroup writes:", err)
	}
	if _, err := unix.FcntlInt(4, unix.F_DUPFD_CLOEXEC, 0); !errors.Is(err, unix.EPERM) {
		t.Fatal("watchdog retained descriptor duplication:", err)
	}
}
