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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxhost"
)

func testIsolatedRecoveryNative(t *testing.T, config sandboxhost.IsolatedConfig) {
	t.Helper()
	config.StateDirectory = privateStateDirectory(t)
	config.Imports = []string{"math"}
	before := filesystemNativeGroups(t, config.LinuxCgroupParent)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	crashPublicWorkerHost(t, ctx, config)
	records := launchRecords(t, config.StateDirectory)
	if len(records) != 1 {
		t.Fatal("dead host did not leave exactly one launch record:", records)
	}
	if entries, err := os.ReadDir(filepath.Join(records[0], "images")); err != nil || len(entries) == 0 {
		t.Fatal("original host left no images to recover:", err)
	}
	if entries, err := os.ReadDir(filepath.Join(records[0], "checkpoint")); err != nil || len(entries) != 1 {
		t.Fatal("original host left no checkpoint:", err)
	}
	if _, err := os.Stat(filepath.Join(records[0], "journal")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("worker record carries a journal:", err)
	}
	changed := config
	changed.MemoryBytes = 256 << 20
	rejected, err := sandboxhost.NewIsolatedRecovery(ctx, changed)
	if rejected != nil {
		_ = rejected.Close()
	}
	if err == nil || rejected != nil {
		t.Fatal("changed worker policy accepted:", err)
	}
	recovery, err := sandboxhost.NewIsolatedRecovery(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer recovery.Close()
	assertLeasedWorkerRecordUntouched(t, ctx, config, records[0])
	if err := recovery.Recover(ctx); err != nil {
		t.Fatal("public worker recovery failed:", err)
	}
	if err := recovery.Close(); err != nil {
		t.Fatal(err)
	}
	if err := recovery.Recover(ctx); err != nil {
		t.Fatal("completed recovery was not idempotent:", err)
	}
	assertFilesystemGroupsClean(t, config.LinuxCgroupParent, before)
	assertLaunchRecordsClean(t, config.StateDirectory)
	worker, err := sandboxhost.NewIsolatedWorker(ctx, config)
	if worker != nil {
		defer worker.Close()
	}
	if err != nil {
		t.Fatal("relaunch after recovery failed:", err)
	}
	if result, err := worker.Eval("import \"math\"\nmath.Sqrt(49)"); err != nil || string(result.Value) != "7" {
		t.Fatalf("relaunched worker: %+v: %v", result, err)
	}
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	assertLaunchRecordsClean(t, config.StateDirectory)
	assertFilesystemGroupsClean(t, config.LinuxCgroupParent, before)
}

func assertLeasedWorkerRecordUntouched(t *testing.T, ctx context.Context, config sandboxhost.IsolatedConfig, record string) {
	t.Helper()
	competing, err := sandboxhost.NewIsolatedRecovery(ctx, config)
	if err != nil {
		t.Fatal("recovery over a leased record failed instead of skipping it:", err)
	}
	if err := competing.Recover(ctx); err != nil {
		t.Fatal("recovery over a leased record reported work:", err)
	}
	if err := competing.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(record, "images")); err != nil {
		t.Fatal("recovery over a leased record removed it:", err)
	}
}

func crashPublicWorkerHost(t *testing.T, ctx context.Context, config sandboxhost.IsolatedConfig) {
	t.Helper()
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPublicWorkerRecoveryHostChild$")
	command.Env = append(os.Environ(), "PIPIT_TEST_PUBLIC_WORKER_RECOVERY="+string(encoded))
	command.Stderr = os.Stderr
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	ready, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || ready != "ready\n" {
		t.Fatal("original session host did not start:", ready, err)
	}
	live := launchRecords(t, config.StateDirectory)
	if len(live) != 1 {
		t.Fatal("live host has no launch record:", live)
	}
	assertLeasedWorkerRecordUntouched(t, ctx, config, live[0])
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("original session host survived SIGKILL")
	}
}

func TestPublicWorkerRecoveryHostChild(t *testing.T) {
	encoded := os.Getenv("PIPIT_TEST_PUBLIC_WORKER_RECOVERY")
	if encoded == "" {
		return
	}
	var config sandboxhost.IsolatedConfig
	if err := json.Unmarshal([]byte(encoded), &config); err != nil {
		t.Fatal(err)
	}
	session, err := sandboxhost.NewIsolatedSession(context.Background(), config)
	if err != nil {
		if session != nil {
			_ = session.Close()
		}
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Submit(context.Background(), "40 + 2"); err != nil {
		t.Fatal(err)
	}
	fmt.Println("ready")
	_, err = session.Submit(context.Background(), "for {}")
	t.Fatal("original session host returned before SIGKILL:", err)
}
