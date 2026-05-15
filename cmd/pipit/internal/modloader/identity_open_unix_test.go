//go:build unix

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

package modloader

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestIdentityReadRejectsFIFOWithoutBlocking(t *testing.T) {
	if os.Getenv("PIPIT_IDENTITY_FIFO_FIXTURE") == "1" {
		scan, path, expected := identityFileFixture(t)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
		if err := scan.addFile(path, expected); err == nil {
			t.Fatal("FIFO replacement was accepted as regular source")
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestIdentityReadRejectsFIFOWithoutBlocking$")
	command.Env = append(os.Environ(), "PIPIT_IDENTITY_FIFO_FIXTURE=1")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil || err != nil {
		t.Fatalf("source FIFO blocked or escaped rejection: context=%v error=%v output=%s", ctx.Err(), err, output)
	}
}

func TestIdentityReadRefusesSymlinkEvenToOriginal(t *testing.T) {
	t.Parallel()
	scan, path, expected := identityFileFixture(t)
	if err := os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+".original", path); err != nil {
		t.Fatal(err)
	}
	if err := scan.addFile(path, expected); err == nil {
		t.Fatal("final-component symlink was followed during source hashing")
	}
}
