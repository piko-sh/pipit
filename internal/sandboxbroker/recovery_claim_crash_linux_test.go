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
	"testing"
	"time"
)

func TestRecoveryClaimProcessDeath(t *testing.T) {
	if journal := os.Getenv("PIPIT_TEST_CLAIM_JOURNAL"); journal != "" {
		claim, err := OpenLinuxRecoveryClaim(journal, os.Getenv("PIPIT_TEST_CLAIM_CHECKPOINT"),
			os.Getenv("PIPIT_TEST_CLAIM_APPROVAL"), [sha256.Size]byte{1})
		if err != nil {
			t.Fatal(err)
		}
		defer claim.Close()
		if err := claim.RecordServiceReleaseReady(); err != nil {
			t.Fatal(err)
		}
		fmt.Println("ready")
		var signal [1]byte
		_, _ = io.ReadFull(os.Stdin, signal[:])
		return
	}
	directories, binding := recoveryClaimFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecoveryClaimProcessDeath$")
	command.Env = append(os.Environ(), "PIPIT_TEST_CLAIM_JOURNAL="+directories[0],
		"PIPIT_TEST_CLAIM_CHECKPOINT="+directories[1], "PIPIT_TEST_CLAIM_APPROVAL="+directories[2])
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
		t.Fatal("claim owner did not become ready:", ready, err)
	}
	claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	if claim != nil {
		_ = claim.Close()
	}
	if err == nil || claim != nil {
		t.Fatal("another process bypassed the live claim:", err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("killed claimant exited successfully")
	}
	claim, err = OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	if err != nil {
		t.Fatal("terminated claimant retained leases:", err)
	}
	if ready, err := claim.ServiceReleaseReady(); err != nil || !ready {
		t.Fatal("claimant death lost durable release readiness:", err)
	}
	if err := claim.Close(); err != nil {
		t.Fatal(err)
	}
}
