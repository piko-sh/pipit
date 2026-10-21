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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxtest"
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
			require.NoError(t, err, "partial deletion prevented approved reopening")
			defer store.closeHandles()
			_, _, err = store.AcquireDirectory()
			require.Error(t, err, "partial deletion admitted new images before cleanup")
			require.NoError(t, store.CleanupOrphans(t.Context()), "partial deletion recovery succeeds")
			require.NoError(t, store.Close())
			entries, err := os.ReadDir(directory)
			require.NoError(t, err, "partial deletion recovery left orphan entries")
			require.Empty(t, entries)
		})
	}
}

func crashPartialImageCleanup(t *testing.T, directory string, identity LinuxImageStoreIdentity, phase string) {
	t.Helper()
	encoded, err := json.Marshal(identity)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := sandboxtest.Command(ctx, "TestImageStoreRecoveryPartialDeletionCrash", "PIPIT_TEST_IMAGE_PARTIAL_CRASH="+string(encoded),
		"PIPIT_TEST_IMAGE_PARTIAL_DIRECTORY="+directory, "PIPIT_TEST_IMAGE_PARTIAL_PHASE="+phase)
	command.Stderr = os.Stderr
	input, err := command.StdinPipe()
	require.NoError(t, err)
	defer input.Close()
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	defer output.Close()
	require.NoError(t, command.Start())
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	sandboxtest.WaitForLine(t, bufio.NewReader(output), "ready", "image cleanup reaches the selected crash boundary")
	competing, err := ReopenLinuxImageStore(directory, identity, nil, nil)
	if competing != nil {
		_ = competing.closeHandles()
	}
	require.Error(t, err, "partial cleanup released exclusive storage ownership")
	require.Nil(t, competing)
	require.NoError(t, command.Process.Kill())
	require.Error(t, command.Wait(), "partial image cleanup owner survived SIGKILL")
}

func partialImageCleanupChild(t *testing.T, encoded string) {
	t.Helper()
	var identity LinuxImageStoreIdentity
	require.NoError(t, json.Unmarshal([]byte(encoded), &identity))
	store, err := ReopenLinuxImageStore(os.Getenv("PIPIT_TEST_IMAGE_PARTIAL_DIRECTORY"), identity, nil, nil)
	require.NoError(t, err)
	defer store.closeHandles()
	plan, err := store.orphanPlan(t.Context())
	require.NoError(t, err, "partial cleanup lacks a complete original plan")
	require.Len(t, plan, 2)
	defer func() {
		for _, root := range plan {
			_ = root.close()
		}
	}()
	root := plan[0]
	require.NoError(t, unix.Unlinkat(int(root.directory.Fd()), orphanImageExecutable, 0))
	phase := os.Getenv("PIPIT_TEST_IMAGE_PARTIAL_PHASE")
	if phase != "executable-unlinked" {
		require.NoError(t, root.directory.Sync())
	}
	if phase == "root-unlinked" || phase == "store-synchronised" {
		require.NoError(t, unix.Unlinkat(int(store.storage.directory.Fd()), root.name, unix.AT_REMOVEDIR))
	}
	if phase == "store-synchronised" {
		require.NoError(t, store.storage.directory.Sync())
	}
	fmt.Println("ready")
	var signal [1]byte
	_, err = io.ReadFull(os.Stdin, signal[:])
	require.FailNow(t, "the partial cleanup is killed before it returns", "%v", err)
}
