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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxhost"
	"pipit.sh/pipit/internal/sandboxtest"
)

func testIsolatedFilesystemNative(t *testing.T, workerConfig sandboxhost.IsolatedConfig) {
	t.Helper()
	executable := sandboxtest.RequiredBinary(t, "PIPIT_TEST_FILESYSTEM_BROKER_BINARY", "the approved static broker fixture")
	image, err := os.ReadFile(executable)
	require.NoError(t, err)
	for _, mode := range []string{"success", "write", "denied-write", "source-approval-failure", "state-permissions", "recovery-retry"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "file"), []byte("original"), 0600))
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
				require.NoError(t, os.Chmod(state, 0755))
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

				require.Nil(t, worker, "partial failure was not cleaned up")
				require.ErrorIs(t, err, sandboxhost.ErrInvalidIsolatedConfig)
				assertFilesystemGroupsClean(t, workerConfig.LinuxCgroupParent, before)
				assertLaunchRecordsClean(t, state)
				return
			}
			require.NoError(t, err)
			assertFilesystemAggregate(t, workerConfig.LinuxCgroupParent, before)
			records := launchRecords(t, state)
			require.Len(t, records, 1, "a launch records exactly one launch")
			journal := filepath.Join(records[0], "journal")
			checkpoint, err := os.ReadFile(filepath.Join(records[0], "checkpoint", "checkpoint.json"))
			require.NoError(t, err)
			var captured struct{ Context []byte }
			require.NoError(t, json.Unmarshal(checkpoint, &captured), "worker launched without original service identity")
			require.NotEmpty(t, captured.Context)
			for _, directory := range []string{"checkpoint", "approval"} {
				entries, err := os.ReadDir(filepath.Join(records[0], directory))
				require.NoError(t, err, "worker launched without durable recovery metadata")
				require.Len(t, entries, 1)
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
				_, err := worker.Eval("import \"pipit/fs\"\nfs.Write(\"data\",\"file\",[]byte(\"bad\"))")
				require.Error(t, err, "the write grant is not widened")
			case "write":
				result, err := worker.EvalFile(`package main
import "pipit/fs"
func answer() string {
 if err := fs.Write("data","file",[]byte("new")); err != nil { return "failed" }
 return "written"
}`, "answer")
				require.NoError(t, err, "journalled public write: %+v", result)
				require.Equal(t, `"written"`, string(result.Value))

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
				require.NoError(t, err, "public result: %+v", result)
				require.Equal(t, `"original"`, string(result.Value))
				require.NoError(t, worker.Close(), "successful cleanup")
				assertLaunchRecordsClean(t, state)
			}
			_, err = worker.Eval("1")
			require.ErrorIs(t, err, sandboxhost.ErrIsolatedUsed, "owner reused")
			assertFilesystemGroupsClean(t, workerConfig.LinuxCgroupParent, before)
			data, err := os.ReadFile(filepath.Join(root, "file"))
			expected := "original"
			if mode == "write" {
				expected = "new"
			}
			require.NoError(t, err, "unexpected mutation: %q", data)
			require.Equal(t, expected, string(data))
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
			require.FailNow(t, "the evaluation runs until recovery fault injection", "%v", outcome.err)
		case <-deadline.C:
			require.FailNow(t, "a journalled write reaches publication")
		case <-ticker.C:
			entries, err := os.ReadDir(journal)
			require.NoError(t, err)
			if len(entries) == 0 {
				continue
			}
			name := filepath.Join(root, strings.TrimSuffix(entries[0].Name(), ".json"))
			file, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if errors.Is(err, os.ErrExist) {
				continue
			}
			require.NoError(t, err)
			_, writeErr := file.WriteString("collision")
			require.NoError(t, errors.Join(writeErr, file.Close()))
			collision = name
		}
	}
	result := <-done
	require.Error(t, result.err, "failed recovery exposed a result: %v", result)
	require.Nil(t, result.result.Value)
	require.Empty(t, result.result.Output)
	require.Len(t, filesystemNativeGroups(t, parent), len(before)+1, "a failed recovery keeps aggregate ownership")
	assertFilesystemAdmissionHeld(t, config)
	data, err := os.ReadFile(collision)
	require.NoError(t, err, "recovery changed a colliding inode")
	require.Equal(t, "collision", string(data))
	target, err := os.ReadFile(filepath.Join(root, "file"))
	require.NoError(t, err, "the write is atomic")
	require.Contains(t, []string{"new", "original"}, string(target))
	require.NoError(t, os.Remove(collision))
	var blocker string
	for name := range filesystemNativeGroups(t, parent) {
		if !before[name] {
			blocker = filepath.Join(parent, name, "cleanup-blocker")
		}
	}
	require.NotEmpty(t, blocker, "a failed recovery keeps its aggregate")
	require.NoError(t, os.Mkdir(blocker, 0700))
	defer os.Remove(blocker)
	require.Error(t, worker.Close(), "aggregate cleanup notices a retained child")
	require.Len(t, filesystemNativeGroups(t, parent), len(before)+1, "a failed parent cleanup keeps aggregate ownership")
	assertFilesystemAdmissionHeld(t, config)
	require.NoError(t, os.Remove(blocker))
	_ = worker.Close()
	assertFilesystemGroupsClean(t, parent, before)
	config.Worker.LinuxCgroupParent = filepath.Join(t.TempDir(), "missing")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	next, launchErr := sandboxhost.NewIsolatedFilesystemWorker(ctx, config)
	if next != nil {
		_ = next.Close()
	}
	require.Nil(t, next, "a successful retry does not keep aggregate admission")
	require.ErrorIs(t, launchErr, sandboxhost.ErrIsolatedUnavailable)
	after, err := os.ReadFile(filepath.Join(root, "file"))
	require.NoError(t, err, "retry changed destination")
	require.Equal(t, string(target), string(after))
}

func assertFilesystemAdmissionHeld(t *testing.T, config sandboxhost.IsolatedFilesystemConfig) {
	t.Helper()
	config.Worker.LinuxCgroupParent = filepath.Join(t.TempDir(), "missing")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	next, err := sandboxhost.NewIsolatedFilesystemWorker(ctx, config)
	if next != nil {
		_ = next.Close()
	}
	require.Nil(t, next, "an occupied aggregate refuses native preparation")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func filesystemNativeGroups(t *testing.T, parent string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
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
	require.Equal(t, before, filesystemNativeGroups(t, parent), "aggregate ownership does not leak")
}

func assertFilesystemAggregate(t *testing.T, parent string, before map[string]bool) {
	t.Helper()
	current := filesystemNativeGroups(t, parent)
	for name := range before {
		delete(current, name)
	}
	require.Len(t, current, 1, "the workers share one resource parent")
	for name := range current {
		root := filepath.Join(parent, name)
		children := filesystemNativeGroups(t, root)
		require.Len(t, children, 2, "source and broker are not both beneath parent")
		for child := range children {
			container := filepath.Join(root, child)
			members, err := os.ReadFile(filepath.Join(container, "cgroup.procs"))
			require.NoError(t, err, "protected aggregate process membership: %q", members)
			require.Empty(t, strings.Fields(string(members)))
			roles := filesystemNativeGroups(t, container)
			require.Len(t, roles, 2, "protected process and watchdog must share aggregate")
			for role := range roles {
				members, err := os.ReadFile(filepath.Join(container, role, "cgroup.procs"))
				require.NoError(t, err, "protected role process membership: %q", members)
				require.Len(t, strings.Fields(string(members)), 1)
			}
		}
		for control, expected := range map[string]string{
			"memory.max": "268435456", "memory.swap.max": "0", "memory.oom.group": "1",
			"cpu.max": "100000 100000", "pids.max": "64",
		} {
			data, err := os.ReadFile(filepath.Join(root, control))
			require.NoError(t, err, "aggregate %s: %q", control, data)
			require.Equal(t, expected, strings.TrimSpace(string(data)))
		}
	}
}
