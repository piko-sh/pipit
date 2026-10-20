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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxhost"
	"pipit.sh/pipit/internal/sandboxtest"
)

func crashPublicRecoveryAttempts(t *testing.T, ctx context.Context, config sandboxhost.IsolatedFilesystemConfig, staging, images string) {
	t.Helper()
	unexpected := filepath.Join(images, "unexpected")
	require.NoError(t, os.WriteFile(unexpected, []byte("preserve"), 0600))
	before, err := os.ReadDir(images)
	require.NoError(t, err)
	crashPublicRecoveryClaimant(t, ctx, config, images)
	after, err := os.ReadDir(images)
	require.NoError(t, err, "failed image planning changed orphan storage")
	require.Len(t, after, len(before))
	for index, entry := range before {
		require.Equal(t, entry.Name(), after[index].Name(), "failed image planning replaces no entry")
	}
	data, err := os.ReadFile(unexpected)
	require.NoError(t, err, "failed image cleanup changed unexpected contents")
	require.Equal(t, "preserve", string(data))
	require.NoError(t, os.Remove(unexpected))
	require.NoError(t, os.Rename(config.BrokerPath, config.BrokerPath+".held"))
	crashPublicRecoveryClaimant(t, ctx, config, images)
	require.NoError(t, os.Rename(config.BrokerPath+".held", config.BrokerPath))
	entries, err := os.ReadDir(images)
	require.NoError(t, err, "replacement host did not finish original image cleanup")
	require.Empty(t, entries)
	data, err = os.ReadFile(staging)
	require.NoError(t, err, "failed helper launch modified original staging data")
	require.Equal(t, "new", string(data))
	data, err = os.ReadFile(filepath.Join(config.Roots[0].HostPath, "file"))
	require.NoError(t, err, "replacement-host crashes changed the published destination")
	require.Equal(t, "published", string(data))
}

func crashPublicRecoveryClaimant(t *testing.T, ctx context.Context, config sandboxhost.IsolatedFilesystemConfig, images string) {
	t.Helper()
	encoded, err := json.Marshal(config)
	require.NoError(t, err)
	command := sandboxtest.Command(ctx, "TestPublicFilesystemRecoveryClaimantChild", "PIPIT_TEST_PUBLIC_RECOVERY_CLAIMANT="+string(encoded))
	command.Stderr = os.Stderr
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	defer output.Close()
	input, err := command.StdinPipe()
	require.NoError(t, err)
	defer input.Close()
	require.NoError(t, command.Start())
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	sandboxtest.WaitForLine(t, bufio.NewReader(output), "ready", "the replacement host keeps the failed recovery")
	assertLeasedRecordUntouched(t, ctx, config, filepath.Dir(images))
	require.NoError(t, command.Process.Kill())
	require.Error(t, command.Wait(), "replacement recovery host survived SIGKILL")
}

func TestPublicFilesystemRecoveryClaimantChild(t *testing.T) {
	encoded := os.Getenv("PIPIT_TEST_PUBLIC_RECOVERY_CLAIMANT")
	if encoded == "" {
		return
	}
	var config sandboxhost.IsolatedFilesystemConfig
	require.NoError(t, json.Unmarshal([]byte(encoded), &config))
	recovery, err := sandboxhost.NewIsolatedFilesystemRecovery(context.Background(), config)
	require.NoError(t, err)
	defer recovery.Close()
	require.Error(t, recovery.Recover(context.Background()), "recovery fails while a cleanup phase is blocked")
	require.Error(t, recovery.Close(), "a failed replacement recovery keeps its ownership")
	fmt.Println("ready")
	var input [1]byte
	_, err = os.Stdin.Read(input[:])
	require.FailNow(t, "the replacement recovery host is killed before it returns", "read: %v", err)
}
