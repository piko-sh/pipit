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
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxtest"
)

func TestRecoveryCheckpointStoreProcessDeath(t *testing.T) {
	if directory := os.Getenv("PIPIT_TEST_CHECKPOINT_CRASH_DIRECTORY"); directory != "" {
		encoded, approved, binding := checkpointStoreFixture(t)
		owner, err := OpenLinuxRecoveryCheckpointStore(directory)
		require.NoError(t, err)
		defer owner.Close()
		require.NoError(t, owner.store(encoded, approved, binding))
		fmt.Println(hex.EncodeToString(approved[:]))
		var signal [1]byte
		_, _ = io.ReadFull(os.Stdin, signal[:])
		return
	}
	directory := testRecoveryDirectory(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := sandboxtest.Command(ctx, "TestRecoveryCheckpointStoreProcessDeath", "PIPIT_TEST_CHECKPOINT_CRASH_DIRECTORY="+directory, "TMPDIR="+t.TempDir())
	command.Stderr = os.Stderr
	input, err := command.StdinPipe()
	require.NoError(t, err)
	defer input.Close()
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	defer output.Close()
	require.NoError(t, command.Start())
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	ready, err := bufio.NewReader(output).ReadString('\n')
	require.NoError(t, err, "the checkpoint host publishes")
	digest, err := hex.DecodeString(strings.TrimSpace(ready))
	require.NoError(t, err, "missing independent fixture approval: %v", ready)
	require.Len(t, digest, sha256.Size)
	approved := [sha256.Size]byte(digest)
	require.NoError(t, command.Process.Kill())
	require.Error(t, command.Wait(), "checkpoint host survived SIGKILL")
	status, ok := command.ProcessState.Sys().(syscall.WaitStatus)
	require.True(t, ok, "the checkpoint host reports a wait status")
	require.True(t, status.Signaled(), "the checkpoint host dies by SIGKILL")
	require.Equal(t, syscall.SIGKILL, status.Signal())
	owner, err := OpenLinuxRecoveryCheckpointStore(directory)
	require.NoError(t, err, "host death keeps no checkpoint lease")
	defer owner.Close()
	data, err := owner.Load(approved, [sha256.Size]byte{1})
	require.NoError(t, err, "host death lost approved checkpoint")
	require.NotEmpty(t, data)
}
