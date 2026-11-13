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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxhost"
	"pipit.sh/pipit/internal/sandboxtest"
)

func testIsolatedRecoveryNative(t *testing.T, config sandboxhost.IsolatedConfig) {
	t.Helper()
	config.StateDirectory = privateStateDirectory(t)
	config.Imports = []string{"math"}
	before := filesystemNativeGroups(t, config.LinuxCgroupParent)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	crashPublicWorkerHost(t, ctx, config)
	records := launchRecords(t, config.StateDirectory)
	require.Len(t, records, 1, "a dead host leaves exactly one launch record")
	entries, err := os.ReadDir(filepath.Join(records[0], "images"))
	require.NoError(t, err, "original host left no images to recover")
	require.NotEmpty(t, entries)
	entries, err = os.ReadDir(filepath.Join(records[0], "checkpoint"))
	require.NoError(t, err, "original host left no checkpoint")
	require.Len(t, entries, 1)
	_, err = os.Stat(filepath.Join(records[0], "journal"))
	require.ErrorIs(t, err, os.ErrNotExist, "worker record carries a journal")
	changed := config
	changed.MemoryBytes = 256 << 20
	rejected, err := sandboxhost.NewIsolatedRecovery(ctx, changed)
	if rejected != nil {
		_ = rejected.Close()
	}
	require.Error(t, err, "changed worker policy accepted")
	require.Nil(t, rejected)
	recovery, err := sandboxhost.NewIsolatedRecovery(ctx, config)
	require.NoError(t, err)
	defer recovery.Close()
	assertLeasedWorkerRecordUntouched(t, ctx, config, records[0])
	require.NoError(t, recovery.Recover(ctx), "public worker recovery succeeds")
	require.NoError(t, recovery.Close())
	require.NoError(t, recovery.Recover(ctx), "a completed recovery is idempotent")
	assertFilesystemGroupsClean(t, config.LinuxCgroupParent, before)
	assertLaunchRecordsClean(t, config.StateDirectory)
	worker, err := sandboxhost.NewIsolatedWorker(ctx, config)
	if worker != nil {
		defer worker.Close(t.Context())
	}
	require.NoError(t, err, "a relaunch after recovery succeeds")
	result, err := worker.Eval(t.Context(), "import \"math\"\nmath.Sqrt(49)")
	require.NoError(t, err, "relaunched worker: %+v", result)
	require.Equal(t, "7", string(result.Value))
	require.NoError(t, worker.Close(t.Context()))
	assertLaunchRecordsClean(t, config.StateDirectory)
	assertFilesystemGroupsClean(t, config.LinuxCgroupParent, before)
}

func assertLeasedWorkerRecordUntouched(t *testing.T, ctx context.Context, config sandboxhost.IsolatedConfig, record string) {
	t.Helper()
	competing, err := sandboxhost.NewIsolatedRecovery(ctx, config)
	require.NoError(t, err, "recovery skips a leased record")
	require.NoError(t, competing.Recover(ctx), "recovery over a leased record reported work")
	require.NoError(t, competing.Close())
	_, err = os.Stat(filepath.Join(record, "images"))
	require.NoError(t, err, "recovery over a leased record removed it")
}

func crashPublicWorkerHost(t *testing.T, ctx context.Context, config sandboxhost.IsolatedConfig) {
	t.Helper()
	encoded, err := json.Marshal(config)
	require.NoError(t, err)
	command := sandboxtest.Command(ctx, "TestPublicWorkerRecoveryHostChild", "PIPIT_TEST_PUBLIC_WORKER_RECOVERY="+string(encoded))
	command.Stderr = os.Stderr
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	defer output.Close()
	require.NoError(t, command.Start())
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	sandboxtest.WaitForLine(t, bufio.NewReader(output), "ready", "the original session host starts")
	live := launchRecords(t, config.StateDirectory)
	require.Len(t, live, 1, "live host has no launch record")
	assertLeasedWorkerRecordUntouched(t, ctx, config, live[0])
	require.NoError(t, command.Process.Kill())
	require.Error(t, command.Wait(), "original session host survived SIGKILL")
}

func TestPublicWorkerRecoveryHostChild(t *testing.T) {
	encoded := os.Getenv("PIPIT_TEST_PUBLIC_WORKER_RECOVERY")
	if encoded == "" {
		return
	}
	var config sandboxhost.IsolatedConfig
	require.NoError(t, json.Unmarshal([]byte(encoded), &config))
	session, err := sandboxhost.NewIsolatedSession(t.Context(), config)
	if err != nil && session != nil {
		_ = session.Close(t.Context())
	}
	require.NoError(t, err)
	defer session.Close(t.Context())
	_, err = session.Submit(t.Context(), "40 + 2")
	require.NoError(t, err)
	fmt.Println("ready")
	_, err = session.Submit(t.Context(), "for {}")
	require.FailNow(t, "the original session host is killed before it returns", "%v", err)
}
