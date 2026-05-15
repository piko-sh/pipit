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
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestResourceSettings(t *testing.T) {
	t.Parallel()
	settings, err := resourceSettings(Limits{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"memory.max": "268435456", "memory.swap.max": "0", "memory.oom.group": "1",
		"cpu.max": "100000 100000", "pids.max": "64",
	}
	if !reflect.DeepEqual(settings, want) {
		t.Fatalf("settings=%v", settings)
	}
	for _, limits := range []Limits{
		{MemoryBytes: -1}, {MemoryBytes: 1}, {MemoryBytes: defaultMemoryBytes + 1},
		{CPUMilli: -1}, {CPUMilli: 1}, {CPUMilli: maximumCPUMilli + 1},
		{Tasks: -1}, {Tasks: maximumTasks + 1},
	} {
		if _, err := resourceSettings(limits); !errors.Is(err, ErrInvalidLimits) {
			t.Fatalf("invalid limits accepted: %+v err=%v", limits, err)
		}
	}
}

func TestNewGroupRejectsOrdinaryFilesystem(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	group, err := newGroup(parent, Limits{})
	if group != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("group=%v err=%v", group, err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("refusal changed parent: entries=%v err=%v", entries, err)
	}
}

func TestNewGroupRejectsSymlinkParent(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	link := filepath.Join(t.TempDir(), "parent")
	if err := os.Symlink(parent, link); err != nil {
		t.Fatal(err)
	}
	if _, err := newGroup(link, Limits{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestParseEmpty(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input string
		empty bool
		valid bool
	}{
		{input: "populated 0\nfrozen 0\n", empty: true, valid: true},
		{input: "populated 1\n", empty: false, valid: true},
		{input: "frozen 0\n", empty: false, valid: false},
		{input: "populated 0\npopulated 1\n", empty: false, valid: false},
		{input: "populated 2\n", empty: false, valid: false},
		{input: "populated\n", empty: false, valid: false},
		{input: "populated 0 extra\n", empty: false, valid: false},
	} {
		empty, err := parseEmpty(test.input)
		if (err == nil) != test.valid || empty != test.empty {
			t.Fatalf("input=%q empty=%v err=%v", test.input, empty, err)
		}
	}
}

func TestGroupRejectsStartAfterTerminalTransition(t *testing.T) {
	t.Parallel()
	for _, group := range []*Group{
		{closed: true}, {closing: true}, {started: true},
	} {
		if err := group.start(nil); !errors.Is(err, errClosed) {
			t.Fatalf("terminal group accepted another start: %v", err)
		}
	}
}

func TestCgroupNativeLifecycle(t *testing.T) {
	if os.Getenv("PIPIT_CGROUP_TEST_CHILD") == "1" {
		data, err := os.ReadFile("/proc/self/cgroup")
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout.Write(data)
		time.Sleep(30 * time.Second)
		return
	}
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires an explicitly delegated cgroup-v2 parent")
	}
	group, err := newGroup(parent, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := group.closeGroup(ctx); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(cleanup)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestCgroupNativeLifecycle")
	command.Env = append(os.Environ(), "PIPIT_CGROUP_TEST_CHILD=1")
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := group.start(command); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	scanner := bufio.NewScanner(output)
	if !scanner.Scan() || !strings.HasSuffix(scanner.Text(), "/"+group.name) {
		t.Fatalf("child was not born in its own cgroup: %q err=%v", scanner.Text(), scanner.Err())
	}
	cleanup()
	if err := group.start(exec.CommandContext(ctx, os.Args[0])); !errors.Is(err, errClosed) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(parent, group.name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("resource group survived cleanup: %v", err)
	}
}

func TestCgroupNativeDescendants(t *testing.T) {
	if mode := os.Getenv("PIPIT_CGROUP_DESCENDANT_CHILD"); mode != "" {
		if mode == "leader" {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCgroupNativeDescendants$")
			child.Env = append(os.Environ(), "PIPIT_CGROUP_DESCENDANT_CHILD=descendant")
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stdout.WriteString(strconv.Itoa(child.Process.Pid) + "\n"); err != nil {
				t.Fatal(err)
			}
			_ = child.Wait()
			return
		}
		time.Sleep(30 * time.Second)
		return
	}
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires an explicitly delegated cgroup-v2 parent")
	}
	group, err := newGroup(parent, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := group.closeGroup(ctx); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(cleanup)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCgroupNativeDescendants$")
	command.Env = append(os.Environ(), "PIPIT_CGROUP_DESCENDANT_CHILD=leader")
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := group.start(command); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	scanner := bufio.NewScanner(output)
	if !scanner.Scan() {
		t.Fatalf("descendant did not start: %v", scanner.Err())
	}
	descendant, err := strconv.Atoi(scanner.Text())
	if err != nil || descendant <= 0 {
		t.Fatalf("invalid descendant PID: %q", scanner.Text())
	}
	processes, err := readControl(group.directory, "cgroup.procs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains("\n"+processes, "\n"+strconv.Itoa(descendant)+"\n") {
		t.Fatalf("native descendant escaped its resource group: %q", processes)
	}
	cleanup()
	if ctx.Err() != nil {
		t.Fatalf("deadline, not group cleanup, terminated descendants: %v", ctx.Err())
	}
	if _, err := os.Stat(filepath.Join(parent, group.name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("populated descendant group survived cleanup: %v", err)
	}
}

func TestCgroupNativeMemory(t *testing.T) {
	if os.Getenv("PIPIT_CGROUP_MEMORY_CHILD") == "1" {
		allocations := make([][]byte, 0, 32)
		for range 32 {
			allocation := make([]byte, 4<<20)
			for offset := 0; offset < len(allocation); offset += os.Getpagesize() {
				allocation[offset] = 1
			}
			allocations = append(allocations, allocation)
		}
		runtime.KeepAlive(allocations)
		return
	}
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires an explicitly delegated cgroup-v2 parent")
	}
	group, err := newGroup(parent, Limits{MemoryBytes: 32 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := group.closeGroup(ctx); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestCgroupNativeMemory")
	command.Env = append(os.Environ(), "PIPIT_CGROUP_MEMORY_CHILD=1")
	if err := group.start(command); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("native allocation exceeded its hard memory limit")
	}
	if ctx.Err() != nil {
		t.Fatalf("watchdog, not the memory controller, killed the child: %v", ctx.Err())
	}
	events, err := readControl(group.directory, "memory.events")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(events, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "oom_kill" {
			continue
		}
		count, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || count == 0 {
			t.Fatalf("no kernel-confirmed OOM kill: %q", events)
		}
		return
	}
	t.Fatalf("missing kernel OOM evidence: %q", events)
}
