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
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestImageStoreRecoveryAfterHostDeath(t *testing.T) {
	if directory := os.Getenv("PIPIT_TEST_IMAGE_STORE_CRASH"); directory != "" {
		store, err := OpenLinuxImageStore(directory, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		if _, _, err := store.AcquireDirectory(); err != nil {
			t.Fatal(err)
		}
		writeOrphanImage(t, directory, "A", true)
		fmt.Println("ready")
		var signal [1]byte
		_, _ = io.ReadFull(os.Stdin, signal[:])
		return
	}
	directory, identity := orphanStoreFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestImageStoreRecoveryAfterHostDeath$")
	command.Env = append(os.Environ(), "PIPIT_TEST_IMAGE_STORE_CRASH="+directory)
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
		t.Fatal("original image owner did not become ready:", ready, err)
	}
	competing, err := ReopenLinuxImageStore(directory, identity, nil, nil)
	if competing != nil {
		_ = competing.closeHandles()
	}
	if err == nil || competing != nil {
		t.Fatal("live image owner lost its exclusive lease:", err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("image owner survived SIGKILL")
	}
	recovered, err := ReopenLinuxImageStore(directory, identity, nil, nil)
	if err != nil {
		t.Fatal("terminated owner retained image storage:", err)
	}
	defer recovered.closeHandles()
	if err := recovered.CleanupOrphans(ctx); err != nil {
		t.Fatal("orphan cleanup after host death failed:", err)
	}
	if err := recovered.Close(); err != nil {
		t.Fatal(err)
	}
}
