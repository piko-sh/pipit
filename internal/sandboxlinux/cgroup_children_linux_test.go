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
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCgroupNativeAggregateTasks(t *testing.T) {
	aggregate, ctx := aggregateGroupFixture(t, Limits{MemoryBytes: 0, CPUMilli: 100, Tasks: 1})
	first, err := aggregate.newChild(Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.closeGroup(ctx)
	second, err := aggregate.newChild(Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.closeGroup(ctx)
	if err := aggregate.start(exec.Command("/bin/true")); !errors.Is(err, errClosed) {
		t.Fatal("aggregate admitted a direct process:", err)
	}
	command := exec.CommandContext(ctx, "/bin/sleep", "30")
	if err := first.start(command); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		if command.ProcessState == nil {
			_ = command.Wait()
		}
	}()
	rejected := exec.CommandContext(ctx, "/bin/true")
	if err := second.start(rejected); err == nil {
		_ = rejected.Wait()
		t.Fatal("second child bypassed combined task limit")
	}
	if current, err := readControl(aggregate.directory, "pids.current"); err != nil || strings.TrimSpace(current) != "1" {
		t.Fatalf("aggregate task count %q: %v", current, err)
	}
	if control, err := readControl(aggregate.directory, "cpu.max"); err != nil || strings.TrimSpace(control) != "10000 100000" {
		t.Fatalf("aggregate CPU quota %q: %v", control, err)
	}
	if err := aggregate.closeGroup(ctx); err == nil {
		t.Fatal("parent removed while child owners still exist")
	}
	if err := command.Wait(); err == nil {
		t.Fatal("parent close did not kill descendant")
	}
	if _, err := aggregate.newChild(Limits{}); !errors.Is(err, errClosed) {
		t.Fatal("closing aggregate admitted child:", err)
	}
	if err := first.closeGroup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := second.closeGroup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.closeGroup(ctx); err != nil {
		t.Fatal("parent cleanup retry:", err)
	}
}

func TestCgroupNativeAggregateMemory(t *testing.T) {
	if os.Getenv("PIPIT_AGGREGATE_MEMORY_CHILD") == "1" {
		allocation := make([]byte, 40<<20)
		for offset := 0; offset < len(allocation); offset += os.Getpagesize() {
			allocation[offset] = 1
		}
		fmt.Println("ready")
		time.Sleep(30 * time.Second)
		runtime.KeepAlive(allocation)
		return
	}
	aggregate, ctx := aggregateGroupFixture(t, Limits{MemoryBytes: 64 << 20, CPUMilli: 0, Tasks: 0})
	first, err := aggregate.newChild(Limits{MemoryBytes: 128 << 20, CPUMilli: 0, Tasks: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer first.closeGroup(ctx)
	second, err := aggregate.newChild(Limits{MemoryBytes: 128 << 20, CPUMilli: 0, Tasks: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer second.closeGroup(ctx)
	command := aggregateMemoryCommand(t, ctx, first, true)
	other := aggregateMemoryCommand(t, ctx, second, false)
	if err := other.Wait(); err == nil {
		t.Fatal("combined memory limit did not kill second child")
	}
	if err := command.Wait(); err == nil {
		t.Fatal("group OOM did not kill first child")
	}
	if ctx.Err() != nil {
		t.Fatal("watchdog rather than memory controller terminated children:", ctx.Err())
	}
	events, err := readControl(aggregate.directory, "memory.events")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for line := range strings.SplitSeq(events, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "oom_kill" {
			count, err := strconv.ParseUint(fields[1], 10, 64)
			found = err == nil && count >= 2
		}
	}
	if !found {
		t.Fatal("missing aggregate OOM evidence:", events)
	}
}

func aggregateMemoryCommand(t *testing.T, ctx context.Context, group *Group, awaitReady bool) *exec.Cmd {
	t.Helper()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCgroupNativeAggregateMemory$")
	command.Env = append(os.Environ(), "PIPIT_AGGREGATE_MEMORY_CHILD=1")
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := group.start(command); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	if awaitReady {
		reader := bufio.NewScanner(output)
		if !reader.Scan() || reader.Text() != "ready" {
			t.Fatal("first child not ready:", reader.Err())
		}
	}
	return command
}

func aggregateGroupFixture(t *testing.T, limits Limits) (*Group, context.Context) {
	t.Helper()
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup v2")
	}
	aggregate, err := newGroup(parent, limits)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(func() {
		defer cancel()
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := aggregate.closeGroup(cleanup); err != nil {
			t.Error("aggregate cleanup:", err)
		}
	})
	return aggregate, ctx
}

func TestGroupChildAdmission(t *testing.T) {
	var absent *Group
	if _, err := absent.newChild(Limits{}); !errors.Is(err, errClosed) {
		t.Fatal(err)
	}
	for _, group := range []*Group{{closed: true}, {closing: true}, {started: true}} {
		if _, err := group.newChild(Limits{}); !errors.Is(err, errClosed) {
			t.Fatal("invalid parent accepted:", err)
		}
	}
}
