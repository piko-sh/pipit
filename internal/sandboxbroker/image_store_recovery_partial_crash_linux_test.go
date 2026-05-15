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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestImageStoreRecoveryPartialDeletionCrash(t *testing.T) {
	if encoded := os.Getenv("PIPIT_TEST_IMAGE_PARTIAL_CRASH"); encoded != "" {
		partialImageCleanupChild(t, encoded)
		return
	}
	for _, phase := range []string{"executable-unlinked", "root-synchronised", "root-unlinked", "store-synchronised"} {
		t.Run(phase, func(t *testing.T) {
			directory, identity := orphanStoreFixture(t)
			writeOrphanImage(t, directory, "A", true)
			writeOrphanImage(t, directory, "B", true)
			crashPartialImageCleanup(t, directory, identity, phase)
			store, err := ReopenLinuxImageStore(directory, identity, nil, nil)
			if err != nil {
				t.Fatal("partial deletion prevented approved reopening:", err)
			}
			defer store.closeHandles()
			if _, _, err := store.AcquireDirectory(); err == nil {
				t.Fatal("partial deletion admitted new images before cleanup")
			}
			if err := store.CleanupOrphans(context.Background()); err != nil {
				t.Fatal("partial deletion recovery failed:", err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			if entries, err := os.ReadDir(directory); err != nil || len(entries) != 0 {
				t.Fatal("partial deletion recovery left orphan entries:", err)
			}
		})
	}
}

func crashPartialImageCleanup(t *testing.T, directory string, identity LinuxImageStoreIdentity, phase string) {
	t.Helper()
	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestImageStoreRecoveryPartialDeletionCrash$")
	command.Env = append(os.Environ(), "PIPIT_TEST_IMAGE_PARTIAL_CRASH="+string(encoded),
		"PIPIT_TEST_IMAGE_PARTIAL_DIRECTORY="+directory, "PIPIT_TEST_IMAGE_PARTIAL_PHASE="+phase)
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
		t.Fatal("image cleanup did not reach selected crash boundary:", ready, err)
	}
	competing, err := ReopenLinuxImageStore(directory, identity, nil, nil)
	if competing != nil {
		_ = competing.closeHandles()
	}
	if err == nil || competing != nil {
		t.Fatal("partial cleanup released exclusive storage ownership:", err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("partial image cleanup owner survived SIGKILL")
	}
}

func partialImageCleanupChild(t *testing.T, encoded string) {
	t.Helper()
	var identity LinuxImageStoreIdentity
	if err := json.Unmarshal([]byte(encoded), &identity); err != nil {
		t.Fatal(err)
	}
	store, err := ReopenLinuxImageStore(os.Getenv("PIPIT_TEST_IMAGE_PARTIAL_DIRECTORY"), identity, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.closeHandles()
	plan, err := store.orphanPlan(context.Background())
	if err != nil || len(plan) != 2 {
		t.Fatal("partial cleanup lacks a complete original plan:", err)
	}
	defer func() {
		for _, root := range plan {
			_ = root.close()
		}
	}()
	root := plan[0]
	if err := unix.Unlinkat(int(root.directory.Fd()), orphanImageExecutable, 0); err != nil {
		t.Fatal(err)
	}
	phase := os.Getenv("PIPIT_TEST_IMAGE_PARTIAL_PHASE")
	if phase != "executable-unlinked" {
		if err := root.directory.Sync(); err != nil {
			t.Fatal(err)
		}
	}
	if phase == "root-unlinked" || phase == "store-synchronised" {
		if err := unix.Unlinkat(int(store.storage.directory.Fd()), root.name, unix.AT_REMOVEDIR); err != nil {
			t.Fatal(err)
		}
	}
	if phase == "store-synchronised" {
		if err := store.storage.directory.Sync(); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println("ready")
	var signal [1]byte
	_, err = io.ReadFull(os.Stdin, signal[:])
	t.Fatal("partial cleanup returned before SIGKILL:", err)
}
