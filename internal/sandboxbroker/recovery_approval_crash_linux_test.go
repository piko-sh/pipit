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
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestApprovedCheckpointProcessDeath(t *testing.T) {
	if base := os.Getenv("PIPIT_TEST_APPROVED_CHECKPOINT_DIRECTORY"); base != "" {
		publishApprovedCheckpointFixture(t, base)
		return
	}
	base := t.TempDir()
	for _, name := range []string{"root", "journal", "checkpoint", "approval"} {
		if err := os.Mkdir(filepath.Join(base, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestApprovedCheckpointProcessDeath$")
	command.Env = append(os.Environ(), "PIPIT_TEST_APPROVED_CHECKPOINT_DIRECTORY="+base)
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
	if err != nil || ready != "ready\n" {
		t.Fatal("publisher did not commit both stores:", ready, err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("publisher survived forced termination")
	}
	approval, err := OpenLinuxRecoveryApprovalStore(filepath.Join(base, "approval"))
	if err != nil {
		t.Fatal(err)
	}
	defer approval.Close()
	binding := [sha256.Size]byte{1}
	digest, err := approval.Load(binding)
	if err != nil {
		t.Fatal("durable approval unavailable:", err)
	}
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(filepath.Join(base, "checkpoint"))
	if err != nil {
		t.Fatal(err)
	}
	defer checkpoint.Close()
	if data, err := checkpoint.Load(digest, binding); err != nil || len(data) == 0 {
		t.Fatal("approved checkpoint did not survive host death:", err)
	}
}

func publishApprovedCheckpointFixture(t *testing.T, base string) {
	t.Helper()
	bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{{Name: "data", Path: filepath.Join(base, "root"), Rights: Read | Write}}, FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	authority, err := bootstrap.RecoveryAuthority()
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	journal, err := OpenLinuxRecoveryJournal(filepath.Join(base, "journal"), authority.namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(filepath.Join(base, "checkpoint"))
	if err != nil {
		t.Fatal(err)
	}
	defer checkpoint.Close()
	approval, err := OpenLinuxRecoveryApprovalStore(filepath.Join(base, "approval"))
	if err != nil {
		t.Fatal(err)
	}
	defer approval.Close()
	if _, err := authority.PersistApprovedCheckpoint(journal, checkpoint, approval, [sha256.Size]byte{1}); err != nil {
		t.Fatal(err)
	}
	fmt.Println("ready")
	var signal [1]byte
	_, _ = io.ReadFull(os.Stdin, signal[:])
}
