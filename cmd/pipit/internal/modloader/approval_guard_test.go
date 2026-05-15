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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
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
	if successes != 1 {
		t.Fatalf("concurrent stores published %d revisions instead of one", successes)
	}
}

func TestApprovalSaveRefusesStaleRevision(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), LockfileName)
	original := NewStore(path)
	original.SetScript("script.go", "first")
	if err := original.Save(); err != nil {
		t.Fatal(err)
	}
	stale := NewStore(path)
	if err := stale.Load(); err != nil {
		t.Fatal(err)
	}
	original.SetScript("script.go", "second")
	if err := original.Save(); err != nil {
		t.Fatal(err)
	}
	stale.SetScript("script.go", "stale")
	if err := stale.Save(); !errors.Is(err, errApprovalChanged) {
		t.Fatalf("stale overwrite accepted: %v", err)
	}
	fresh := NewStore(path)
	fresh.SetScript("script.go", "not loaded")
	if err := fresh.Save(); !errors.Is(err, errApprovalChanged) {
		t.Fatalf("unobserved existing file replaced: %v", err)
	}
	if err := stale.Load(); err != nil {
		t.Fatal(err)
	}
	if stale.lockfile.ScriptHash != "second" {
		t.Fatal("stale save changed the approved identity")
	}
	stale.SetScript("script.go", "third")
	if err := stale.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	stale.SetScript("script.go", "resurrected")
	if err := stale.Save(); !errors.Is(err, errApprovalChanged) {
		t.Fatalf("deleted approval was resurrected: %v", err)
	}
}

func TestApprovalGuardCoordinatesProcessesAndReleasesAfterExit(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), LockfileName)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestApprovalGuardChild$")
	command.Env = append(os.Environ(), "PIPIT_APPROVAL_GUARD_CHILD="+path)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	reader := bufio.NewScanner(stdout)
	if !reader.Scan() || reader.Text() != "locked" {
		t.Fatalf("guard helper did not acquire lock: %q %v", reader.Text(), reader.Err())
	}
	guard, err := acquireApprovalGuard(path)
	if guard != nil {
		_ = guard.Close()
	}
	if err == nil {
		t.Fatal("another process's lock was bypassed")
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("killed helper unexpectedly succeeded")
	}
	guard, err = acquireApprovalGuard(path)
	if err != nil {
		t.Fatalf("exited process retained the lock: %v", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestApprovalGuardChild(t *testing.T) {
	path := os.Getenv("PIPIT_APPROVAL_GUARD_CHILD")
	if path == "" {
		t.Skip("subprocess fixture")
	}
	guard, err := acquireApprovalGuard(path)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	fmt.Println("locked")
	time.Sleep(time.Minute)
}
