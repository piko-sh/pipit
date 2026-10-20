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
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxhost"
	"pipit.sh/pipit/internal/sandboxtest"
)

func TestIsolatedEmbeddingNative(t *testing.T) {
	parent := sandboxtest.CgroupParent(t)
	executable := sandboxtest.RequiredBinary(t, "PIPIT_TEST_WORKER_BINARY", "the static worker fixture")
	source, err := os.Open(executable)
	require.NoError(t, err)
	digest := sha256.New()
	_, readErr := io.Copy(digest, source)
	require.NoError(t, errors.Join(readErr, source.Close()))
	var config sandboxhost.IsolatedConfig
	config.WorkerPath = executable
	config.WorkerSHA256 = [sha256.Size]byte(digest.Sum(nil))
	config.WatchdogPath = os.Getenv("PIPIT_TEST_HOST_WATCHDOG_BINARY")
	watchdogImage, err := os.ReadFile(config.WatchdogPath)
	require.NoError(t, err)
	config.WatchdogSHA256 = sha256.Sum256(watchdogImage)
	config.LinuxCgroupParent = parent
	config.Imports = []string{"math"}
	config.Tenant = "native-test"
	config.StateDirectory = privateStateDirectory(t)
	worker, err := sandboxhost.NewIsolatedWorker(context.Background(), config)
	if worker != nil {
		defer worker.Close()
	}
	require.NoError(t, err)
	config.Imports[0] = "os"
	result, err := worker.Eval("import \"math\"\nmath.Sqrt(49)")
	if err != nil || string(result.Value) != "7" {
		require.Failf(t, "the native worker returns 7", "result=%+v error=%v diagnostics=%s", result, err, worker.Diagnostics())
	}
	_, err = worker.Eval("1")
	require.ErrorIs(t, err, sandboxhost.ErrIsolatedUsed, "native worker was reused")
	sessionConfig := config
	sessionConfig.Imports = []string{"math"}
	t.Run("persistent session", func(t *testing.T) { testIsolatedSessionNative(t, sessionConfig) })
	sessionConfig.Imports = []string{"math"}
	t.Run("filesystem capabilities", func(t *testing.T) { testIsolatedFilesystemNative(t, sessionConfig) })
	t.Run("filesystem host restart", func(t *testing.T) { testIsolatedFilesystemRecoveryNative(t, sessionConfig) })
	t.Run("worker host restart", func(t *testing.T) { testIsolatedRecoveryNative(t, sessionConfig) })
	assertLaunchRecordsClean(t, config.StateDirectory)
	config.Imports = nil
	config.LinuxCgroupParent = t.TempDir()
	unavailable, err := sandboxhost.NewIsolatedWorker(context.Background(), config)
	if unavailable != nil {
		defer unavailable.Close()
	}
	require.ErrorIs(t, err, sandboxhost.ErrIsolatedUnavailable, "a missing cgroup boundary fails closed")
}

func privateStateDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	require.NoError(t, os.Chmod(directory, 0700))
	return directory
}

func launchRecords(t *testing.T, stateDirectory string) []string {
	t.Helper()
	records, err := filepath.Glob(filepath.Join(stateDirectory, "*", "*"))
	require.NoError(t, err)
	return records
}

func assertLaunchRecordsClean(t *testing.T, stateDirectory string) {
	t.Helper()
	require.Empty(t, launchRecords(t, stateDirectory), "clean close left launch records")
}
