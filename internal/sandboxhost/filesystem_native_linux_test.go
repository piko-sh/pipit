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

package sandboxhost_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxhost"
)

func testIsolatedFilesystemNative(t *testing.T, workerConfig sandboxhost.IsolatedConfig) {
	t.Helper()
	executable := os.Getenv("PIPIT_TEST_FILESYSTEM_BROKER_BINARY")
	if executable == "" {
		t.Fatal("requires the approved static broker fixture")
	}
	image, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"success", "write", "denied-write", "source-approval-failure", "state-permissions", "recovery-retry"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "file"), []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			state := privateStateDirectory(t)
			config := sandboxhost.IsolatedFilesystemConfig{
				Worker: workerConfig, BrokerPath: executable, BrokerSHA256: sha256.Sum256(image),
				Roots:  []sandboxhost.FilesystemRoot{{Name: "data", HostPath: root, Rights: sandboxhost.FilesystemRead}},
				Limits: sandboxhost.FilesystemLimits{},
			}
			config.Worker.StateDirectory = state
			if mode == "source-approval-failure" {
				config.Worker.WorkerSHA256[0] ^= 1
			}
			if mode == "state-permissions" {
				if err := os.Chmod(state, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "write" || mode == "recovery-retry" {
				config.Roots[0].Rights |= sandboxhost.FilesystemWrite
			}
			before := filesystemNativeGroups(t, workerConfig.LinuxCgroupParent)
			worker, err := sandboxhost.NewIsolatedFilesystemWorker(context.Background(), config)
			if worker != nil {
				defer worker.Close()
			}
			if mode == "source-approval-failure" || mode == "state-permissions" {

				if worker != nil || !errors.Is(err, sandboxhost.ErrInvalidIsolatedConfig) {
					t.Fatalf("partial failure was not cleaned up: %v: %v", worker, err)
				}
				assertFilesystemGroupsClean(t, workerConfig.LinuxCgroupParent, before)
				assertLaunchRecordsClean(t, state)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertFilesystemAggregate(t, workerConfig.LinuxCgroupParent, before)
			records := launchRecords(t, state)
			if len(records) != 1 {
				t.Fatal("launch did not record exactly one launch:", records)
			}
			journal := filepath.Join(records[0], "journal")
			checkpoint, err := os.ReadFile(filepath.Join(records[0], "checkpoint", "checkpoint.json"))
			if err != nil {
				t.Fatal(err)
			}
			var captured struct{ Context []byte }
			if err := json.Unmarshal(checkpoint, &captured); err != nil || len(captured.Context) == 0 {
				t.Fatal("worker launched without original service identity:", err)
			}
			for _, directory := range []string{"checkpoint", "approval"} {
				entries, err := os.ReadDir(filepath.Join(records[0], directory))
				if err != nil || len(entries) != 1 {
					t.Fatal("worker launched without durable recovery metadata:", entries, err)
				}
			}
			if mode == "success" {
				assertFilesystemAdmissionHeld(t, config)
			}
			if mode == "recovery-retry" {
				testPublicRecoveryRetry(t, worker, journal, root, workerConfig.LinuxCgroupParent, before, config)
				return
			}
			config.Roots[0].Rights = sandboxhost.FilesystemWrite
			config.Roots[0].HostPath = t.TempDir()
			switch mode {
			case "denied-write":
				if _, err := worker.Eval("import \"pipit/fs\"\nfs.Write(\"data\",\"file\",[]byte(\"bad\"))"); err == nil {
					t.Fatal("write grant widened")
				}
			case "write":
				result, err := worker.EvalFile(`package main
import "pipit/fs"
func answer() string {
 if err := fs.Write("data","file",[]byte("new")); err != nil { return "failed" }
 return "written"
}`, "answer")
				if err != nil || string(result.Value) != `"written"` {
					t.Fatalf("journalled public write: %+v: %v", result, err)
				}

				assertLaunchRecordsClean(t, state)
			default:
				result, err := worker.EvalFile(`package main
import "pipit/fs"
import "math"
func answer() string {
 data,err := fs.Read("data","file",64)
 if err != nil { return "failed" }
 if math.Sqrt(49) != 7 { return "wrong helper" }
 return string(data)
}`, "answer")
				if err != nil || string(result.Value) != `"original"` {
					t.Fatalf("public result: %+v: %v", result, err)
				}
				if err := worker.Close(); err != nil {
					t.Fatal("successful cleanup:", err)
				}
				assertLaunchRecordsClean(t, state)
			}
			if _, err := worker.Eval("1"); !errors.Is(err, sandboxhost.ErrIsolatedUsed) {
				t.Fatal("owner reused:", err)
			}
			assertFilesystemGroupsClean(t, workerConfig.LinuxCgroupParent, before)
			data, err := os.ReadFile(filepath.Join(root, "file"))
			expected := "original"
			if mode == "write" {
				expected = "new"
			}
			if err != nil || string(data) != expected {
				t.Fatalf("unexpected mutation: %q: %v", data, err)
			}
		})
	}
}

func testPublicRecoveryRetry(t *testing.T, worker *sandboxhost.IsolatedFilesystemWorker, journal, root, parent string, before map[string]bool,
	config sandboxhost.IsolatedFilesystemConfig,
) {
	t.Helper()
	type outcome struct {
		result sandboxhost.RestrictedResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := worker.EvalFile(`package main
import "pipit/fs"
func main() {
 fs.Write("data","file",[]byte("new"))
 for {}
}`, "main")
		done <- outcome{result: result, err: err}
	}()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	var collision string
	for collision == "" {
		select {
		case outcome := <-done:
			t.Fatal("evaluation ended before recovery fault injection:", outcome.err)
		case <-deadline.C:
			t.Fatal("no journalled write reached publication")
		case <-ticker.C:
			entries, err := os.ReadDir(journal)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) == 0 {
				continue
			}
			name := filepath.Join(root, strings.TrimSuffix(entries[0].Name(), ".json"))
			file, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if errors.Is(err, os.ErrExist) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			_, writeErr := file.WriteString("collision")
			if err := errors.Join(writeErr, file.Close()); err != nil {
				t.Fatal(err)
			}
			collision = name
		}
	}
	result := <-done
	if result.err == nil || result.result.Value != nil || result.result.Output != "" {
		t.Fatal("failed recovery exposed a result:", result)
	}
	if current := filesystemNativeGroups(t, parent); len(current) != len(before)+1 {
		t.Fatal("failed recovery discarded aggregate ownership:", current)
	}
	assertFilesystemAdmissionHeld(t, config)
	data, err := os.ReadFile(collision)
	if err != nil || string(data) != "collision" {
		t.Fatal("recovery changed a colliding inode:", string(data), err)
	}
	target, err := os.ReadFile(filepath.Join(root, "file"))
	if err != nil || string(target) != "new" && string(target) != "original" {
		t.Fatal("write was not atomic:", string(target), err)
	}
	if err := os.Remove(collision); err != nil {
		t.Fatal(err)
	}
	var blocker string
	for name := range filesystemNativeGroups(t, parent) {
		if !before[name] {
			blocker = filepath.Join(parent, name, "cleanup-blocker")
		}
	}
	if blocker == "" {
		t.Fatal("failed recovery lost its aggregate")
	}
	if err := os.Mkdir(blocker, 0700); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(blocker)
	if err := worker.Close(); err == nil {
		t.Fatal("aggregate cleanup ignored a retained child")
	}
	if current := filesystemNativeGroups(t, parent); len(current) != len(before)+1 {
		t.Fatal("failed parent cleanup discarded aggregate ownership:", current)
	}
	assertFilesystemAdmissionHeld(t, config)
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	_ = worker.Close()
	assertFilesystemGroupsClean(t, parent, before)
	config.Worker.LinuxCgroupParent = filepath.Join(t.TempDir(), "missing")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	next, launchErr := sandboxhost.NewIsolatedFilesystemWorker(ctx, config)
	if next != nil || !errors.Is(launchErr, sandboxhost.ErrIsolatedUnavailable) {
		if next != nil {
			_ = next.Close()
		}
		t.Fatal("successful retry retained aggregate admission:", launchErr)
	}
	after, err := os.ReadFile(filepath.Join(root, "file"))
	if err != nil || string(after) != string(target) {
		t.Fatal("retry changed destination:", string(after), err)
	}
}

func assertFilesystemAdmissionHeld(t *testing.T, config sandboxhost.IsolatedFilesystemConfig) {
	t.Helper()
	config.Worker.LinuxCgroupParent = filepath.Join(t.TempDir(), "missing")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	next, err := sandboxhost.NewIsolatedFilesystemWorker(ctx, config)
	if next != nil || !errors.Is(err, context.DeadlineExceeded) {
		if next != nil {
			_ = next.Close()
		}
		t.Fatal("occupied aggregate allowed native preparation:", err)
	}
}

func filesystemNativeGroups(t *testing.T, parent string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	groups := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "pipit-") {
			groups[entry.Name()] = true
		}
	}
	return groups
}

func assertFilesystemGroupsClean(t *testing.T, parent string, before map[string]bool) {
	t.Helper()
	if after := filesystemNativeGroups(t, parent); !reflect.DeepEqual(before, after) {
		t.Fatalf("aggregate ownership leaked: before=%v after=%v", before, after)
	}
}

func assertFilesystemAggregate(t *testing.T, parent string, before map[string]bool) {
	t.Helper()
	current := filesystemNativeGroups(t, parent)
	for name := range before {
		delete(current, name)
	}
	if len(current) != 1 {
		t.Fatalf("expected one shared resource parent: %v", current)
	}
	for name := range current {
		root := filepath.Join(parent, name)
		children := filesystemNativeGroups(t, root)
		if len(children) != 2 {
			t.Fatalf("source and broker are not both beneath parent: %v", children)
		}
		for child := range children {
			container := filepath.Join(root, child)
			members, err := os.ReadFile(filepath.Join(container, "cgroup.procs"))
			if err != nil || len(strings.Fields(string(members))) != 0 {
				t.Fatalf("protected aggregate process membership: %q: %v", members, err)
			}
			roles := filesystemNativeGroups(t, container)
			if len(roles) != 2 {
				t.Fatalf("protected process and watchdog must share aggregate: %v", roles)
			}
			for role := range roles {
				members, err := os.ReadFile(filepath.Join(container, role, "cgroup.procs"))
				if err != nil || len(strings.Fields(string(members))) != 1 {
					t.Fatalf("protected role process membership: %q: %v", members, err)
				}
			}
		}
		for control, expected := range map[string]string{
			"memory.max": "268435456", "memory.swap.max": "0", "memory.oom.group": "1",
			"cpu.max": "100000 100000", "pids.max": "64",
		} {
			data, err := os.ReadFile(filepath.Join(root, control))
			if err != nil || strings.TrimSpace(string(data)) != expected {
				t.Fatalf("aggregate %s: %q: %v", control, data, err)
			}
		}
	}
}
