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

	"golang.org/x/sys/unix"
)

func TestRecoveryJournalCloseReleasesInheritedLease(t *testing.T) {
	if os.Getenv("PIPIT_TEST_JOURNAL_LEASE_CHILD") == "1" {
		inherited := os.NewFile(3, "inherited-journal-directory")
		defer inherited.Close()
		if _, err := inherited.Stat(); err != nil {
			t.Fatal(err)
		}
		fmt.Println("ready")
		var signal [1]byte
		_, _ = io.ReadFull(os.Stdin, signal[:])
		return
	}
	directory := testRecoveryDirectory(t)
	namespace := recoveryRecordFixture().Namespace
	owner, err := OpenLinuxRecoveryJournal(directory, namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecoveryJournalCloseReleasesInheritedLease$")
	command.Env = append(os.Environ(), "PIPIT_TEST_JOURNAL_LEASE_CHILD=1")
	command.ExtraFiles = []*os.File{owner.directory}
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
		t.Fatal("inheriting child did not become ready:", ready, err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := OpenLinuxRecoveryJournal(directory, namespace)
	if err != nil {
		t.Fatal("live child retained a closed owner's lease:", err)
	}
	defer replacement.Close()
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	if concurrent, err := OpenLinuxRecoveryJournal(directory, namespace); concurrent != nil || err == nil {
		if concurrent != nil {
			_ = concurrent.Close()
		}
		t.Fatal("child exit released replacement owner's lease:", err)
	}
}

func TestRecoveryJournalCloseReleasesDuplicatedLease(t *testing.T) {
	directory := testRecoveryDirectory(t)
	namespace := recoveryRecordFixture().Namespace
	owner, err := OpenLinuxRecoveryJournal(directory, namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	descriptor, err := unix.FcntlInt(owner.directory.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	inherited := os.NewFile(uintptr(descriptor), "inherited-journal-directory")
	defer inherited.Close()
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := OpenLinuxRecoveryJournal(directory, namespace)
	if err != nil {
		t.Fatal("closed owner retained lease through duplicated descriptor:", err)
	}
	defer replacement.Close()
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if concurrent, err := OpenLinuxRecoveryJournal(directory, namespace); concurrent != nil || err == nil {
		if concurrent != nil {
			_ = concurrent.Close()
		}
		t.Fatal("repeated closure released replacement owner's lease:", err)
	}
	if err := inherited.Close(); err != nil {
		t.Fatal(err)
	}
	if concurrent, err := OpenLinuxRecoveryJournal(directory, namespace); concurrent != nil || err == nil {
		if concurrent != nil {
			_ = concurrent.Close()
		}
		t.Fatal("old descriptor closure released replacement owner's lease:", err)
	}
}
