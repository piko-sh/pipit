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

package sandboxbroker

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
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
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestRecoveryJournalProcessDeath$")
			command.Env = append(os.Environ(), recoveryCrashEnvironment+"="+mode, "PIPIT_TEST_RECOVERY_DIRECTORY="+directory)
			command.Stderr = os.Stderr
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = command.Process.Kill()
				_ = command.Wait()
			})
			ready, err := bufio.NewReader(output).ReadString('\n')
			if err != nil || ready != "ready\n" {
				t.Fatal("child did not reach crash point:", ready, err)
			}
			if err := command.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := command.Wait(); err == nil {
				t.Fatal("child was not killed")
			}
			owner, err := OpenLinuxRecoveryJournal(directory, recoveryRecordFixture().Namespace)
			if err != nil {
				t.Fatal("crash retained lease or damaged journal:", err)
			}
			defer owner.Close()
			records, err := owner.Records()
			if err != nil {
				t.Fatal(err)
			}
			expected := 0
			if mode == "durable" {
				expected = 1
			}
			if len(records) != expected {
				t.Fatal("unexpected crash records:", records)
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != expected {
				t.Fatal("partial publication or anonymous remnant:", entries, err)
			}
		})
	}
}

func TestRecoveryJournalPinnedDirectory(t *testing.T) {
	directory := testRecoveryDirectory(t)
	record := recoveryRecordFixture()
	owner, err := OpenLinuxRecoveryJournal(directory, record.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	moved := filepath.Join(t.TempDir(), "original")
	if err := os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := owner.appendRecord(record); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.ReadDir(directory)
	if err != nil || len(replacement) != 0 {
		t.Fatal("journal followed replacement directory:", replacement, err)
	}
	original, err := os.ReadDir(moved)
	if err != nil || len(original) != 1 {
		t.Fatal("journal lost pinned directory:", original, err)
	}
}

func recoveryJournalCrashChild(t *testing.T, mode string) {
	t.Helper()
	owner, err := OpenLinuxRecoveryJournal(os.Getenv("PIPIT_TEST_RECOVERY_DIRECTORY"), recoveryRecordFixture().Namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	switch mode {
	case "durable":
		if err := owner.appendRecord(recoveryRecordFixture()); err != nil {
			t.Fatal(err)
		}
	case "anonymous":
		temporary, err := createLinuxStagingFile(owner.directory)
		if err != nil {
			t.Fatal(err)
		}
		defer temporary.Close()
		if _, err := temporary.Write([]byte("incomplete")); err != nil {
			t.Fatal(err)
		}
		if err := temporary.Sync(); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("unknown crash point")
	}
	fmt.Println("ready")
	time.Sleep(time.Hour)
	t.Fatal("child was not killed")
}
