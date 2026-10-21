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

package sandboxhost_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxhost"
	"pipit.sh/pipit/internal/sandboxtest"
)

func testIsolatedFilesystemRecoveryNative(t *testing.T, worker sandboxhost.IsolatedConfig) {
	t.Helper()
	config := publicRecoveryConfig(t, worker)
	root := config.Roots[0].HostPath
	before := filesystemNativeGroups(t, worker.LinuxCgroupParent)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	crashPublicFilesystemHost(t, ctx, config)
	records := launchRecords(t, config.Worker.StateDirectory)
	require.Len(t, records, 1, "a dead host leaves exactly one launch record")
	images := filepath.Join(records[0], "images")
	imageEntries, err := os.ReadDir(images)
	require.NoError(t, err, "the original host leaves images to recover")
	require.NotEmpty(t, imageEntries)
	intents, err := os.ReadDir(filepath.Join(records[0], "journal"))
	require.NoError(t, err, "original write did not leave one durable intent")
	require.Len(t, intents, 1)
	staging := filepath.Join(root, strings.TrimSuffix(intents[0].Name(), ".json"))
	require.NoError(t, os.Rename(filepath.Join(root, "file"), staging))
	require.NoError(t, os.WriteFile(filepath.Join(root, "file"), []byte("published"), 0600))
	changed := config
	changed.Limits.Calls = 32
	rejected, err := sandboxhost.NewIsolatedFilesystemRecovery(ctx, changed)
	if rejected != nil {
		_ = rejected.Close()
	}
	require.Error(t, err, "changed public recovery policy accepted")
	require.Nil(t, rejected)
	changed = config
	changed.Worker.MemoryBytes = 256 << 20
	rejected, err = sandboxhost.NewIsolatedFilesystemRecovery(ctx, changed)
	if rejected != nil {
		_ = rejected.Close()
	}
	require.Error(t, err, "changed worker policy accepted")
	require.Nil(t, rejected)
	crashPublicRecoveryAttempts(t, ctx, config, staging, images)
	recovery, err := sandboxhost.NewIsolatedFilesystemRecovery(ctx, config)
	require.NoError(t, err)
	defer recovery.Close()
	assertLeasedRecordUntouched(t, ctx, config, records[0])
	require.NoError(t, os.Rename(config.BrokerPath, config.BrokerPath+".held"))
	require.Error(t, recovery.Recover(ctx), "missing recovery helper accepted")
	require.Error(t, recovery.Close(), "unfinished public recovery dropped ownership")
	require.NoError(t, os.Rename(config.BrokerPath+".held", config.BrokerPath))
	saved := config.Roots[0]
	config.Roots[0].HostPath = t.TempDir()
	require.NoError(t, recovery.Recover(ctx), "the public recovery retry succeeds")
	config.Roots[0] = saved
	require.NoError(t, recovery.Close())
	require.NoError(t, recovery.Recover(ctx), "a completed recovery is idempotent")
	assertFilesystemGroupsClean(t, worker.LinuxCgroupParent, before)
	assertLaunchRecordsClean(t, config.Worker.StateDirectory)
	entries, err := os.ReadDir(root)
	require.NoError(t, err, "public restart left staging data")
	require.Len(t, entries, 1)
	require.Equal(t, "file", entries[0].Name())
	data, err := os.ReadFile(filepath.Join(root, "file"))
	require.NoError(t, err, "public recovery changed the published destination")
	require.Equal(t, "published", string(data))
	resumed, err := sandboxhost.NewIsolatedFilesystemRecovery(ctx, config)
	require.NoError(t, err)
	defer resumed.Close()
	require.NoError(t, resumed.Recover(ctx), "a fresh public claim over a clean tenant succeeds")
	require.NoError(t, resumed.Close())
}

func assertLeasedRecordUntouched(t *testing.T, ctx context.Context, config sandboxhost.IsolatedFilesystemConfig, record string) {
	t.Helper()
	competing, err := sandboxhost.NewIsolatedFilesystemRecovery(ctx, config)
	require.NoError(t, err, "recovery skips a leased record")
	require.NoError(t, competing.Recover(ctx), "recovery over a leased record reported work")
	require.NoError(t, competing.Close())
	_, err = os.Stat(filepath.Join(record, "images"))
	require.NoError(t, err, "recovery over a leased record removed it")
}

func publicRecoveryConfig(t *testing.T, worker sandboxhost.IsolatedConfig) sandboxhost.IsolatedFilesystemConfig {
	t.Helper()
	image, err := os.ReadFile(os.Getenv("PIPIT_TEST_FILESYSTEM_BROKER_BINARY"))
	require.NoError(t, err)
	executable := filepath.Join(t.TempDir(), "broker")
	require.NoError(t, os.WriteFile(executable, image, 0500))
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
	require.NoError(t, err)
	command := sandboxtest.Command(ctx, "TestPublicFilesystemRecoveryHostChild", "PIPIT_TEST_PUBLIC_RECOVERY="+string(encoded))
	command.Stderr = os.Stderr
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	defer output.Close()
	require.NoError(t, command.Start())
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	sandboxtest.WaitForLine(t, bufio.NewReader(output), "ready", "the original public worker starts")
	live := launchRecords(t, config.Worker.StateDirectory)
	require.Len(t, live, 1, "live host has no launch record")
	assertLeasedRecordUntouched(t, ctx, config, live[0])
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		data, err := os.ReadFile(filepath.Join(config.Roots[0].HostPath, "file"))
		if err == nil && string(data) == "new" {
			break
		}
		if err != nil {
			require.ErrorIs(t, err, os.ErrNotExist)
		}
		select {
		case <-ctx.Done():
			require.FailNow(t, "the original write completes", "%v", ctx.Err())
		case <-ticker.C:
		}
	}
	require.NoError(t, command.Process.Kill())
	require.Error(t, command.Wait(), "original public host survived SIGKILL")
}

func TestPublicFilesystemRecoveryHostChild(t *testing.T) {
	encoded := os.Getenv("PIPIT_TEST_PUBLIC_RECOVERY")
	if encoded == "" {
		return
	}
	var config sandboxhost.IsolatedFilesystemConfig
	require.NoError(t, json.Unmarshal([]byte(encoded), &config))
	worker, err := sandboxhost.NewIsolatedFilesystemWorker(t.Context(), config)
	if err != nil && worker != nil {
		_ = worker.Close(t.Context())
	}
	require.NoError(t, err)
	defer worker.Close(t.Context())
	fmt.Println("ready")
	_, err = worker.EvalFile(t.Context(), "package main\nimport \"pipit/fs\"\nfunc main() { fs.Write(\"data\", \"file\", []byte(\"new\")); for {} }", "main")
	require.FailNow(t, "the original host is killed before it returns", "%v", err)
}
