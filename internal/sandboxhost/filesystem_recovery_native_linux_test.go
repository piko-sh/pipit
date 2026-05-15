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
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxhost"
)

func testIsolatedFilesystemRecoveryNative(t *testing.T, worker sandboxhost.IsolatedConfig) {
	t.Helper()
	config := publicRecoveryConfig(t, worker)
	root := config.Roots[0].HostPath
	before := filesystemNativeGroups(t, worker.LinuxCgroupParent)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	crashPublicFilesystemHost(t, ctx, config)
	records := launchRecords(t, config.Worker.StateDirectory)
	if len(records) != 1 {
		t.Fatal("dead host did not leave exactly one launch record:", records)
	}
	images := filepath.Join(records[0], "images")
	if entries, err := os.ReadDir(images); err != nil || len(entries) == 0 {
		t.Fatal("original host left no images to recover:", err)
	}
	intents, err := os.ReadDir(filepath.Join(records[0], "journal"))
	if err != nil || len(intents) != 1 {
		t.Fatal("original write did not leave one durable intent:", err)
	}
	staging := filepath.Join(root, strings.TrimSuffix(intents[0].Name(), ".json"))
	if err := os.Rename(filepath.Join(root, "file"), staging); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("published"), 0600); err != nil {
		t.Fatal(err)
	}
	changed := config
	changed.Limits.Calls = 32
	rejected, err := sandboxhost.NewIsolatedFilesystemRecovery(ctx, changed)
	if rejected != nil {
		_ = rejected.Close()
	}
	if err == nil || rejected != nil {
		t.Fatal("changed public recovery policy accepted:", err)
	}
	changed = config
	changed.Worker.MemoryBytes = 256 << 20
	rejected, err = sandboxhost.NewIsolatedFilesystemRecovery(ctx, changed)
	if rejected != nil {
		_ = rejected.Close()
	}
	if err == nil || rejected != nil {
		t.Fatal("changed worker policy accepted:", err)
	}
	crashPublicRecoveryAttempts(t, ctx, config, staging, images)
	recovery, err := sandboxhost.NewIsolatedFilesystemRecovery(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer recovery.Close()
	assertLeasedRecordUntouched(t, ctx, config, records[0])
	if err := os.Rename(config.BrokerPath, config.BrokerPath+".held"); err != nil {
		t.Fatal(err)
	}
	if err := recovery.Recover(ctx); err == nil {
		t.Fatal("missing recovery helper accepted")
	}
	if err := recovery.Close(); err == nil {
		t.Fatal("unfinished public recovery dropped ownership")
	}
	if err := os.Rename(config.BrokerPath+".held", config.BrokerPath); err != nil {
		t.Fatal(err)
	}
	saved := config.Roots[0]
	config.Roots[0].HostPath = t.TempDir()
	if err := recovery.Recover(ctx); err != nil {
		t.Fatal("public recovery retry failed:", err)
	}
	config.Roots[0] = saved
	if err := recovery.Close(); err != nil {
		t.Fatal(err)
	}
	if err := recovery.Recover(ctx); err != nil {
		t.Fatal("completed recovery was not idempotent:", err)
	}
	assertFilesystemGroupsClean(t, worker.LinuxCgroupParent, before)
	assertLaunchRecordsClean(t, config.Worker.StateDirectory)
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "file" {
		t.Fatal("public restart left staging data:", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "file"))
	if err != nil || string(data) != "published" {
		t.Fatal("public recovery changed the published destination:", err)
	}
	resumed, err := sandboxhost.NewIsolatedFilesystemRecovery(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if err := resumed.Recover(ctx); err != nil {
		t.Fatal("fresh public claim over a clean tenant failed:", err)
	}
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertLeasedRecordUntouched(t *testing.T, ctx context.Context, config sandboxhost.IsolatedFilesystemConfig, record string) {
	t.Helper()
	competing, err := sandboxhost.NewIsolatedFilesystemRecovery(ctx, config)
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

func publicRecoveryConfig(t *testing.T, worker sandboxhost.IsolatedConfig) sandboxhost.IsolatedFilesystemConfig {
	t.Helper()
	image, err := os.ReadFile(os.Getenv("PIPIT_TEST_FILESYSTEM_BROKER_BINARY"))
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "broker")
	if err := os.WriteFile(executable, image, 0500); err != nil {
		t.Fatal(err)
	}
	config := sandboxhost.IsolatedFilesystemConfig{
		Worker: worker, BrokerPath: executable, BrokerSHA256: sha256.Sum256(image),
		Roots:  []sandboxhost.FilesystemRoot{{Name: "data", HostPath: t.TempDir(), Rights: sandboxhost.FilesystemRead | sandboxhost.FilesystemWrite}},
		Limits: sandboxhost.FilesystemLimits{},
	}
	config.Worker.StateDirectory = privateStateDirectory(t)
	return config
}

func crashPublicFilesystemHost(t *testing.T, ctx context.Context, config sandboxhost.IsolatedFilesystemConfig) {
	t.Helper()
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPublicFilesystemRecoveryHostChild$")
	command.Env = append(os.Environ(), "PIPIT_TEST_PUBLIC_RECOVERY="+string(encoded))
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
		t.Fatal("original public worker did not start:", ready, err)
	}
	live := launchRecords(t, config.Worker.StateDirectory)
	if len(live) != 1 {
		t.Fatal("live host has no launch record:", live)
	}
	assertLeasedRecordUntouched(t, ctx, config, live[0])
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		data, err := os.ReadFile(filepath.Join(config.Roots[0].HostPath, "file"))
		if err == nil && string(data) == "new" {
			break
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("original write did not complete:", ctx.Err())
		case <-ticker.C:
		}
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("original public host survived SIGKILL")
	}
}

func TestPublicFilesystemRecoveryHostChild(t *testing.T) {
	encoded := os.Getenv("PIPIT_TEST_PUBLIC_RECOVERY")
	if encoded == "" {
		return
	}
	var config sandboxhost.IsolatedFilesystemConfig
	if err := json.Unmarshal([]byte(encoded), &config); err != nil {
		t.Fatal(err)
	}
	worker, err := sandboxhost.NewIsolatedFilesystemWorker(context.Background(), config)
	if err != nil {
		if worker != nil {
			_ = worker.Close()
		}
		t.Fatal(err)
	}
	defer worker.Close()
	fmt.Println("ready")
	_, err = worker.EvalFile("package main\nimport \"pipit/fs\"\nfunc main() { fs.Write(\"data\", \"file\", []byte(\"new\")); for {} }", "main")
	t.Fatal("original host returned before SIGKILL:", err)
}
