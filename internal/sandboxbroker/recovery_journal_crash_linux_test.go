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

package sandboxbroker

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxtest"
)

const recoveryCrashEnvironment = "PIPIT_TEST_RECOVERY_CRASH"

func TestRecoveryJournalProcessDeath(t *testing.T) {
	if mode := os.Getenv(recoveryCrashEnvironment); mode != "" {
		recoveryJournalCrashChild(t, mode)
		return
	}
	for _, mode := range []string{"durable", "anonymous"} {
		t.Run(mode, func(t *testing.T) {
			directory := testRecoveryDirectory(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			command := sandboxtest.Command(ctx, "TestRecoveryJournalProcessDeath", recoveryCrashEnvironment+"="+mode, "PIPIT_TEST_RECOVERY_DIRECTORY="+directory)
			command.Stderr = os.Stderr
			output, err := command.StdoutPipe()
			require.NoError(t, err)
			require.NoError(t, command.Start())
			t.Cleanup(func() {
				_ = command.Process.Kill()
				_ = command.Wait()
			})
			sandboxtest.WaitForLine(t, bufio.NewReader(output), "ready", "the child reaches the crash point")
			require.NoError(t, command.Process.Kill())
			require.Error(t, command.Wait(), "the child is killed before it wakes")
			owner, err := OpenLinuxRecoveryJournal(directory, recoveryRecordFixture().Namespace)
			require.NoError(t, err, "a crash keeps no lease and leaves the journal intact")
			defer owner.Close()
			records, err := owner.Records()
			require.NoError(t, err)
			expected := 0
			if mode == "durable" {
				expected = 1
			}
			require.Len(t, records, expected)
			entries, err := os.ReadDir(directory)
			require.NoError(t, err, "partial publication or anonymous remnant")
			require.Len(t, entries, expected)
		})
	}
}

func TestRecoveryJournalPinnedDirectory(t *testing.T) {
	directory := testRecoveryDirectory(t)
	record := recoveryRecordFixture()
	owner, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	require.NoError(t, err)
	defer owner.Close()
	moved := filepath.Join(t.TempDir(), "original")
	require.NoError(t, os.Rename(directory, moved))
	require.NoError(t, os.Mkdir(directory, 0700))
	require.NoError(t, owner.appendRecord(record))
	replacement, err := os.ReadDir(directory)
	require.NoError(t, err, "journal followed replacement directory")
	require.Empty(t, replacement)
	original, err := os.ReadDir(moved)
	require.NoError(t, err, "journal lost pinned directory")
	require.Len(t, original, 1)
}

func recoveryJournalCrashChild(t *testing.T, mode string) {
	t.Helper()
	owner, err := OpenLinuxRecoveryJournal(os.Getenv("PIPIT_TEST_RECOVERY_DIRECTORY"), recoveryRecordFixture().Namespace)
	require.NoError(t, err)
	defer owner.Close()
	switch mode {
	case "durable":
		require.NoError(t, owner.appendRecord(recoveryRecordFixture()))
	case "anonymous":
		temporary, err := createLinuxStagingFile(owner.directory)
		require.NoError(t, err)
		defer temporary.Close()
		_, err = temporary.Write([]byte("incomplete"))
		require.NoError(t, err)
		require.NoError(t, temporary.Sync())
	default:
		require.FailNow(t, "the crash point is known")
	}
	fmt.Println("ready")
	time.Sleep(time.Hour)
	require.FailNow(t, "the child is killed before it wakes")
}
