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
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxtest"
)

func TestRecoveryJournalCloseReleasesInheritedLease(t *testing.T) {
	if os.Getenv("PIPIT_TEST_JOURNAL_LEASE_CHILD") == "1" {
		inherited := os.NewFile(3, "inherited-journal-directory")
		defer inherited.Close()
		_, err := inherited.Stat()
		require.NoError(t, err)
		fmt.Println("ready")
		var signal [1]byte
		_, _ = io.ReadFull(os.Stdin, signal[:])
		return
	}
	directory := testRecoveryDirectory(t)
	namespace := recoveryRecordFixture().Namespace
	owner, err := OpenLinuxRecoveryJournal(directory, namespace)
	require.NoError(t, err)
	defer owner.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := sandboxtest.Command(ctx, "TestRecoveryJournalCloseReleasesInheritedLease", "PIPIT_TEST_JOURNAL_LEASE_CHILD=1")
	command.ExtraFiles = []*os.File{owner.directory}
	command.Stderr = os.Stderr
	input, err := command.StdinPipe()
	require.NoError(t, err)
	defer input.Close()
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	defer output.Close()
	require.NoError(t, command.Start())
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	sandboxtest.WaitForLine(t, bufio.NewReader(output), "ready", "the inheriting child becomes ready")
	require.NoError(t, owner.Close())
	replacement, err := OpenLinuxRecoveryJournal(directory, namespace)
	require.NoError(t, err, "a live child keeps no closed owner's lease")
	defer replacement.Close()
	require.NoError(t, input.Close())
	require.NoError(t, command.Wait())
	requireJournalLocked(t, directory, namespace, "the child's exit keeps the replacement owner's lease")
}

func TestRecoveryJournalCloseReleasesDuplicatedLease(t *testing.T) {
	directory := testRecoveryDirectory(t)
	namespace := recoveryRecordFixture().Namespace
	owner, err := OpenLinuxRecoveryJournal(directory, namespace)
	require.NoError(t, err)
	defer owner.Close()
	descriptor, err := unix.FcntlInt(owner.directory.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	require.NoError(t, err)
	inherited := os.NewFile(uintptr(descriptor), "inherited-journal-directory")
	defer inherited.Close()
	require.NoError(t, owner.Close())
	replacement, err := OpenLinuxRecoveryJournal(directory, namespace)
	require.NoError(t, err, "a closed owner keeps no lease through a duplicated descriptor")
	defer replacement.Close()
	require.NoError(t, owner.Close())
	requireJournalLocked(t, directory, namespace, "a repeated close keeps the replacement owner's lease")
	require.NoError(t, inherited.Close())
	requireJournalLocked(t, directory, namespace, "closing the old descriptor keeps the replacement owner's lease")
}
