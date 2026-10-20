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
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxtest"
)

func TestApprovalConcurrentCreationHasOneWinner(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), LockfileName)
	const writers = 8
	start := make(chan struct{})
	results := make(chan error, writers)
	var waiting sync.WaitGroup
	for index := range writers {
		waiting.Go(func() {
			store := NewStore(path)
			store.SetScript("script.go", fmt.Sprint(index))
			<-start
			results <- store.Save()
		})
	}
	close(start)
	waiting.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes, "concurrent stores published revisions instead of one")
}

func TestApprovalSaveRefusesStaleRevision(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), LockfileName)
	original := NewStore(path)
	original.SetScript("script.go", "first")
	require.NoError(t, original.Save())
	stale := NewStore(path)
	require.NoError(t, stale.Load())
	original.SetScript("script.go", "second")
	require.NoError(t, original.Save())
	stale.SetScript("script.go", "stale")
	err := stale.Save()
	require.ErrorIs(t, err, errApprovalChanged, "a stale overwrite is refused")
	fresh := NewStore(path)
	fresh.SetScript("script.go", "not loaded")
	err = fresh.Save()
	require.ErrorIs(t, err, errApprovalChanged, "an existing file that was not observed is kept")
	require.NoError(t, stale.Load())
	require.Equal(t, "second", stale.lockfile.ScriptHash, "a stale save keeps the approved identity")
	stale.SetScript("script.go", "third")
	require.NoError(t, stale.Save())
	require.NoError(t, os.Remove(path))
	stale.SetScript("script.go", "resurrected")
	err = stale.Save()
	require.ErrorIs(t, err, errApprovalChanged, "a deleted approval stays deleted")
}

func TestApprovalGuardCoordinatesProcessesAndReleasesAfterExit(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), LockfileName)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := sandboxtest.Command(ctx, "TestApprovalGuardChild", "PIPIT_APPROVAL_GUARD_CHILD="+path)
	stdout, err := command.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	reader := bufio.NewScanner(stdout)
	require.True(t, reader.Scan(), "the guard helper reports its state: %v", reader.Err())
	require.Equal(t, "locked", reader.Text(), "the guard helper acquires the lock")
	guard, err := acquireApprovalGuard(path)
	if guard != nil {
		_ = guard.Close()
	}
	require.Error(t, err, "another process's lock holds")
	require.NoError(t, command.Process.Kill())
	require.Error(t, command.Wait(), "the killed helper fails")
	guard, err = acquireApprovalGuard(path)
	require.NoError(t, err, "an exited process releases the lock")
	require.NoError(t, guard.Close())
}

func TestApprovalGuardChild(t *testing.T) {
	path := os.Getenv("PIPIT_APPROVAL_GUARD_CHILD")
	if path == "" {
		t.Skip("subprocess fixture")
	}
	guard, err := acquireApprovalGuard(path)
	require.NoError(t, err)
	defer guard.Close()
	fmt.Println("locked")
	time.Sleep(time.Minute)
}
