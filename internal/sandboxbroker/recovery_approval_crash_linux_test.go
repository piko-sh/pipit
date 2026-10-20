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
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxtest"
)

func TestApprovedCheckpointProcessDeath(t *testing.T) {
	if base := os.Getenv("PIPIT_TEST_APPROVED_CHECKPOINT_DIRECTORY"); base != "" {
		publishApprovedCheckpointFixture(t, base)
		return
	}
	base := t.TempDir()
	for _, name := range []string{"root", "journal", "checkpoint", "approval"} {
		require.NoError(t, os.Mkdir(filepath.Join(base, name), 0700))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := sandboxtest.Command(ctx, "TestApprovedCheckpointProcessDeath", "PIPIT_TEST_APPROVED_CHECKPOINT_DIRECTORY="+base)
	command.Stderr = os.Stderr
	input, err := command.StdinPipe()
	require.NoError(t, err)
	defer input.Close()
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	defer output.Close()
	require.NoError(t, command.Start())
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	sandboxtest.WaitForLine(t, bufio.NewReader(output), "ready", "the publisher commits both stores")
	require.NoError(t, command.Process.Kill())
	require.Error(t, command.Wait(), "publisher survived forced termination")
	approval, err := OpenLinuxRecoveryApprovalStore(filepath.Join(base, "approval"))
	require.NoError(t, err)
	defer approval.Close()
	binding := [sha256.Size]byte{1}
	digest, err := approval.Load(binding)
	require.NoError(t, err, "durable approval unavailable")
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(filepath.Join(base, "checkpoint"))
	require.NoError(t, err)
	defer checkpoint.Close()
	data, err := checkpoint.Load(digest, binding)
	require.NoError(t, err, "approved checkpoint did not survive host death")
	require.NotEmpty(t, data)
}

func publishApprovedCheckpointFixture(t *testing.T, base string) {
	t.Helper()
	bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{{Name: "data", Path: filepath.Join(base, "root"), Rights: Read | Write}}, FilesystemLimits{})
	require.NoError(t, err)
	defer bootstrap.Close()
	authority, err := bootstrap.RecoveryAuthority()
	require.NoError(t, err)
	defer authority.Close()
	journal, err := OpenLinuxRecoveryJournal(filepath.Join(base, "journal"), authority.namespace)
	require.NoError(t, err)
	defer journal.Close()
	checkpoint, err := OpenLinuxRecoveryCheckpointStore(filepath.Join(base, "checkpoint"))
	require.NoError(t, err)
	defer checkpoint.Close()
	approval, err := OpenLinuxRecoveryApprovalStore(filepath.Join(base, "approval"))
	require.NoError(t, err)
	defer approval.Close()
	_, err = authority.PersistApprovedCheckpoint(journal, checkpoint, approval, [sha256.Size]byte{1})
	require.NoError(t, err)
	fmt.Println("ready")
	var signal [1]byte
	_, _ = io.ReadFull(os.Stdin, signal[:])
}
