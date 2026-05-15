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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRecoveryCheckpointStoreProcessDeath(t *testing.T) {
	if directory := os.Getenv("PIPIT_TEST_CHECKPOINT_CRASH_DIRECTORY"); directory != "" {
		encoded, approved, binding := checkpointStoreFixture(t)
		owner, err := OpenLinuxRecoveryCheckpointStore(directory)
		if err != nil {
			t.Fatal(err)
		}
		defer owner.Close()
		if err := owner.store(encoded, approved, binding); err != nil {
			t.Fatal(err)
		}
		fmt.Println(hex.EncodeToString(approved[:]))
		var signal [1]byte
		_, _ = io.ReadFull(os.Stdin, signal[:])
		return
	}
	directory := testRecoveryDirectory(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecoveryCheckpointStoreProcessDeath$")
	command.Env = append(os.Environ(), "PIPIT_TEST_CHECKPOINT_CRASH_DIRECTORY="+directory, "TMPDIR="+t.TempDir())
	command.Stderr = os.Stderr
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
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
	if err != nil {
		t.Fatal("checkpoint host did not publish:", err)
	}
	digest, err := hex.DecodeString(strings.TrimSpace(ready))
	if err != nil || len(digest) != sha256.Size {
		t.Fatal("missing independent fixture approval:", ready, err)
	}
	approved := [sha256.Size]byte(digest)
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("checkpoint host survived SIGKILL")
	}
	status, ok := command.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("unexpected checkpoint host exit:", command.ProcessState)
	}
	owner, err := OpenLinuxRecoveryCheckpointStore(directory)
	if err != nil {
		t.Fatal("host death retained checkpoint lease:", err)
	}
	defer owner.Close()
	data, err := owner.Load(approved, [sha256.Size]byte{1})
	if err != nil || len(data) == 0 {
		t.Fatal("host death lost approved checkpoint:", err)
	}
}
