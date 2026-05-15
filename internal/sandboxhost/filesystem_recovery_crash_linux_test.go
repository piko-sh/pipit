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
	"os/exec"
	"path/filepath"
	"testing"

	"pipit.sh/pipit/internal/sandboxhost"
)

func crashPublicRecoveryAttempts(t *testing.T, ctx context.Context, config sandboxhost.IsolatedFilesystemConfig, staging, images string) {
	t.Helper()
	unexpected := filepath.Join(images, "unexpected")
	if err := os.WriteFile(unexpected, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(images)
	if err != nil {
		t.Fatal(err)
	}
	crashPublicRecoveryClaimant(t, ctx, config, images)
	after, err := os.ReadDir(images)
	if err != nil || len(after) != len(before) {
		t.Fatal("failed image planning changed orphan storage:", err)
	}
	for index, entry := range before {
		if after[index].Name() != entry.Name() {
			t.Fatal("failed image planning replaced an entry")
		}
	}
	if data, err := os.ReadFile(unexpected); err != nil || string(data) != "preserve" {
		t.Fatal("failed image cleanup changed unexpected contents:", err)
	}
	if err := os.Remove(unexpected); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(config.BrokerPath, config.BrokerPath+".held"); err != nil {
		t.Fatal(err)
	}
	crashPublicRecoveryClaimant(t, ctx, config, images)
	if err := os.Rename(config.BrokerPath+".held", config.BrokerPath); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(images); err != nil || len(entries) != 0 {
		t.Fatal("replacement host did not finish original image cleanup:", err)
	}
	if data, err := os.ReadFile(staging); err != nil || string(data) != "new" {
		t.Fatal("failed helper launch modified original staging data:", err)
	}
	if data, err := os.ReadFile(filepath.Join(config.Roots[0].HostPath, "file")); err != nil || string(data) != "published" {
		t.Fatal("replacement-host crashes changed the published destination:", err)
	}
}

func crashPublicRecoveryClaimant(t *testing.T, ctx context.Context, config sandboxhost.IsolatedFilesystemConfig, images string) {
	t.Helper()
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPublicFilesystemRecoveryClaimantChild$")
	command.Env = append(os.Environ(), "PIPIT_TEST_PUBLIC_RECOVERY_CLAIMANT="+string(encoded))
	command.Stderr = os.Stderr
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	ready, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || ready != "ready\n" {
		t.Fatal("replacement host did not retain failed recovery:", ready, err)
	}
	assertLeasedRecordUntouched(t, ctx, config, filepath.Dir(images))
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("replacement recovery host survived SIGKILL")
	}
}

func TestPublicFilesystemRecoveryClaimantChild(t *testing.T) {
	encoded := os.Getenv("PIPIT_TEST_PUBLIC_RECOVERY_CLAIMANT")
	if encoded == "" {
		return
	}
	var config sandboxhost.IsolatedFilesystemConfig
	if err := json.Unmarshal([]byte(encoded), &config); err != nil {
		t.Fatal(err)
	}
	recovery, err := sandboxhost.NewIsolatedFilesystemRecovery(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer recovery.Close()
	if err := recovery.Recover(context.Background()); err == nil {
		t.Fatal("recovery unexpectedly succeeded with a blocked cleanup phase")
	}
	if err := recovery.Close(); err == nil {
		t.Fatal("failed replacement recovery released its ownership")
	}
	fmt.Println("ready")
	var input [1]byte
	_, err = os.Stdin.Read(input[:])
	t.Fatal("replacement recovery host returned before SIGKILL:", err)
}
