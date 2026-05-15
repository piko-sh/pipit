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
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxwire"
)

func TestNativeAttachedWatchdogHostDeath(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup-v2 parent")
	}
	for _, role := range []string{"source", "filesystem-broker"} {
		t.Run(role, func(t *testing.T) { exerciseAttachedWatchdogHostDeath(t, parent, role) })
	}
}

func exerciseAttachedWatchdogHostDeath(t *testing.T, parent, role string) {
	t.Helper()
	temporary := t.TempDir()
	aggregate, err := newGroup(parent, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, aggregate.name)
	defer cleanupCrashFixture(t, aggregate, root)
	if err := os.WriteFile(filepath.Join(root, "cgroup.subtree_control"), []byte("+cpu +memory +pids"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeAttachedWatchdogCrashChild$")
	command.Env = append(os.Environ(), "PIPIT_WATCHDOG_CRASH_ROLE="+role,
		"PIPIT_WATCHDOG_CRASH_PARENT="+root, "TMPDIR="+temporary)
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	reader := bufio.NewReader(output)
	ready, err := reader.ReadString('\n')
	if err != nil || ready != "protected\n" {
		_ = command.Process.Kill()
		remaining, _ := io.ReadAll(reader)
		_ = command.Wait()
		t.Fatalf("host failed before protected readiness: %q %v %s %s", ready, err, remaining, diagnostics.String())
	}
	populated, err := os.ReadFile(filepath.Join(root, "cgroup.events"))
	if err != nil || !strings.Contains(string(populated), "populated 1\n") {
		t.Fatalf("protected aggregate was not populated: %q %v", populated, err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("embedding host survived SIGKILL")
	}
	status, ok := command.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("embedding host did not die from SIGKILL:", command.ProcessState)
	}
	cleanup, cancelCleanup := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelCleanup()
	if err := aggregate.waitEmpty(cleanup); err != nil {
		t.Fatal("watchdog left protected processes alive after host death:", err)
	}
	if owner, err := newServiceGroup(root, Limits{}, testTenant); owner != nil || !errors.Is(err, ErrServiceBusy) {
		t.Fatalf("host crash released unconfirmed service ownership: %v", err)
	}
}

func TestNativeAttachedWatchdogCrashChild(t *testing.T) {
	role := os.Getenv("PIPIT_WATCHDOG_CRASH_ROLE")
	if role == "" {
		return
	}
	config := crashFixtureConfiguration(t, role)
	var process *WorkerProcess
	var err error
	if role == "source" {
		process, err = LaunchWorker(context.Background(), config)
	} else {
		root, rootErr := os.MkdirTemp("", "broker-root-")
		if rootErr != nil {
			t.Fatal(rootErr)
		}
		bootstrap, bootstrapErr := sandboxbroker.PrepareLinuxBootstrap([]sandboxbroker.LinuxRootGrant{
			{Name: "data", Path: root, Rights: sandboxbroker.Read},
		}, sandboxbroker.FilesystemLimits{})
		if bootstrapErr != nil {
			t.Fatal(bootstrapErr)
		}
		process, err = LaunchFilesystemBroker(context.Background(), config, bootstrap)
		err = errors.Join(err, bootstrap.Close())
	}
	if process != nil {
		defer process.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Stream().SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	codec, err := sandboxwire.New(process.Stream(), process.Stream(), sandboxwire.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	readBrokerFrame(t, codec, sandboxwire.Hello, 0)
	if err := process.Stream().SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	stopCrashFixtureProcess(t, process)
	fmt.Println("protected")
	var signal [1]byte
	_, _ = io.ReadFull(os.Stdin, signal[:])
}

func crashFixtureConfiguration(t *testing.T, role string) WorkerConfig {
	t.Helper()
	var config WorkerConfig
	config.Executable = os.Getenv("PIPIT_TEST_WORKER_BINARY")
	if role == "filesystem-broker" {
		config.Executable = os.Getenv("PIPIT_TEST_FILESYSTEM_BROKER_BINARY")
	}
	config.WatchdogExecutable = os.Getenv("PIPIT_TEST_HOST_WATCHDOG_BINARY")
	image, err := os.ReadFile(config.Executable)
	if err != nil {
		t.Fatal(err)
	}
	watchdog, err := os.ReadFile(config.WatchdogExecutable)
	if err != nil {
		t.Fatal(err)
	}
	config.Digest = sha256.Sum256(image)
	config.WatchdogDigest = sha256.Sum256(watchdog)
	config.CgroupParent = os.Getenv("PIPIT_WATCHDOG_CRASH_PARENT")
	config.Tenant = testTenant
	config.Lifetime = 30 * time.Second
	return config
}

func stopCrashFixtureProcess(t *testing.T, process *WorkerProcess) {
	t.Helper()
	members, err := os.ReadFile(filepath.Join("/proc/self/fd", strconv.Itoa(int(process.group.directory.Fd())), "cgroup.procs"))
	if err != nil || len(strings.Fields(string(members))) != 1 {
		t.Fatalf("expected one protected process: %q %v", members, err)
	}
	identity, err := strconv.Atoi(strings.TrimSpace(string(members)))
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := unix.PidfdOpen(identity, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(descriptor)
	if err := unix.PidfdSendSignal(descriptor, unix.SIGSTOP, nil, 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(identity), "status"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(status), "State:\tT (stopped)") {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("protected process did not stop before host termination")
}

func cleanupCrashFixture(t *testing.T, aggregate *Group, root string) {
	t.Helper()
	if _, err := aggregate.kill.WriteAt([]byte("1"), 0); err != nil {
		t.Error(err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := aggregate.waitEmpty(ctx); err != nil {
		t.Error(err)
		return
	}
	var directories []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != root {
			directories = append(directories, path)
		}
		return nil
	}); err != nil {
		t.Error(err)
		return
	}
	for _, directory := range slices.Backward(directories) {
		if err := os.Remove(directory); err != nil {
			t.Error(err)
			return
		}
	}
	if err := aggregate.closeGroup(ctx); err != nil {
		t.Error(err)
	}
}
