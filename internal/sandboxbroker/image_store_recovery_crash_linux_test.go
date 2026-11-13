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

	"pipit.sh/pipit/internal/sandboxtest"
)

func TestImageStoreRecoveryAfterHostDeath(t *testing.T) {
	if directory := os.Getenv("PIPIT_TEST_IMAGE_STORE_CRASH"); directory != "" {
		store, err := OpenLinuxImageStore(directory, nil, nil)
		require.NoError(t, err)
		defer store.Close()
		_, _, err = store.AcquireDirectory()
		require.NoError(t, err)
		writeOrphanImage(t, directory, "A", true)
		fmt.Println("ready")
		var signal [1]byte
		_, _ = io.ReadFull(os.Stdin, signal[:])
		return
	}
	directory, identity := orphanStoreFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := sandboxtest.Command(ctx, "TestImageStoreRecoveryAfterHostDeath", "PIPIT_TEST_IMAGE_STORE_CRASH="+directory)
	command.Stderr = os.Stderr
	input, err := command.StdinPipe()
	require.NoError(t, err)
	defer input.Close()
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	defer output.Close()
	require.NoError(t, command.Start())
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	sandboxtest.WaitForLine(t, bufio.NewReader(output), "ready", "the original image owner becomes ready")
	competing, err := ReopenLinuxImageStore(directory, identity, nil, nil)
	if competing != nil {
		_ = competing.closeHandles()
	}
	require.Error(t, err, "live image owner lost its exclusive lease")
	require.Nil(t, competing)
	require.NoError(t, command.Process.Kill())
	require.Error(t, command.Wait(), "image owner survived SIGKILL")
	recovered, err := ReopenLinuxImageStore(directory, identity, nil, nil)
	require.NoError(t, err, "a terminated owner keeps no image storage")
	defer recovered.closeHandles()
	require.NoError(t, recovered.CleanupOrphans(ctx), "orphan cleanup after host death succeeds")
	require.NoError(t, recovered.Close())
}
