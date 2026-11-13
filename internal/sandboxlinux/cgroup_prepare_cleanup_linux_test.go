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

package sandboxlinux

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxwire"
)

func TestCgroupNativePreparationOwnership(t *testing.T) {
	for _, mode := range []string{"rollback", "blocked-removal", "missing-handle"} {
		t.Run(mode, func(t *testing.T) { exerciseGroupPreparationOwnership(t, mode) })
	}
}

func exerciseGroupPreparationOwnership(t *testing.T, mode string) {
	t.Helper()
	aggregate, ctx := aggregateGroupFixture(t, Limits{})
	parent, err := openDirectory(int(aggregate.directory.Fd()), ".")
	if err != nil {
		t.Fatal(err)
	}
	name := "partial-setup"
	if mode != "missing-handle" {
		if err := unix.Mkdirat(int(parent.Fd()), name, groupDirectoryMode); err != nil {
			_ = parent.Close()
			t.Fatal(err)
		}
	}
	group, preparationErr := prepareGroup(parent, name, map[string]string{"unavailable-control": "1"})
	if preparationErr == nil || group == nil || !errors.Is(preparationErr, ErrUnavailable) {
		t.Fatalf("failed preparation lost owner: %v: %v", group, preparationErr)
	}
	t.Cleanup(func() {
		if mode == "missing-handle" {
			if err := group.closeHandles(); err != nil {
				t.Error(err)
			}
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := group.closeGroup(cleanup); err != nil {
			t.Error(err)
		}
	})
	if err := group.start(exec.Command("/bin/true")); !errors.Is(err, errClosed) {
		t.Fatal("partial group admitted process:", err)
	}
	if _, err := group.newChild(t.Context(), Limits{}); !errors.Is(err, errClosed) {
		t.Fatal("partial group admitted child:", err)
	}
	if mode == "blocked-removal" {
		if err := unix.Mkdirat(int(group.directory.Fd()), "blocker", groupDirectoryMode); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := finishGroupPreparation(t.Context(), group, preparationErr)
	if !errors.Is(err, preparationErr) {
		t.Fatal("preparation error lost:", err)
	}
	if mode == "rollback" {
		if owner != nil || !group.closed {
			t.Fatal("successful rollback retained live group")
		}
		if _, err := parent.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("rolled-back parent handle retained:", err)
		}
		return
	}
	if owner != group || group.closed {
		t.Fatal("failed rollback discarded owner")
	}
	if _, err := parent.Stat(); err != nil {
		t.Fatal("failed rollback closed parent handle:", err)
	}
	if mode == "missing-handle" {
		if err := unix.Mkdirat(int(parent.Fd()), name, groupDirectoryMode); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR); err != nil {
				t.Error(err)
			}
		}()
		if err := group.closeGroup(ctx); err == nil || group.directory != nil || group.closed {
			t.Fatal("missing original handle adopted a later directory:", err)
		}
		replacement, err := openDirectory(int(parent.Fd()), name)
		if err != nil {
			t.Fatal("cleanup removed an unowned replacement:", err)
		}
		if err := replacement.Close(); err != nil {
			t.Fatal(err)
		}
		return
	} else {
		if err := unix.Unlinkat(int(group.directory.Fd()), "blocker", unix.AT_REMOVEDIR); err != nil {
			t.Fatal(err)
		}
	}
	if err := group.closeGroup(ctx); err != nil {
		t.Fatal("cleanup retry failed:", err)
	}
	if err := group.closeGroup(ctx); err != nil {
		t.Fatal("cleanup not idempotent:", err)
	}
	if _, err := parent.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("completed owner leaked parent handle:", err)
	}
}

func TestCgroupNativePreparationCleanupAfterCancel(t *testing.T) {
	for _, mode := range []string{"runs", "bounded"} {
		t.Run(mode, func(t *testing.T) { exercisePreparationCleanupAfterCancel(t, mode) })
	}
}

func exercisePreparationCleanupAfterCancel(t *testing.T, mode string) {
	t.Helper()
	aggregate, _ := aggregateGroupFixture(t, Limits{})
	parent, err := openDirectory(int(aggregate.directory.Fd()), ".")
	if err != nil {
		t.Fatal(err)
	}
	name := "partial-" + mode
	if err := unix.Mkdirat(int(parent.Fd()), name, groupDirectoryMode); err != nil {
		_ = parent.Close()
		t.Fatal(err)
	}
	group, preparationErr := prepareGroup(parent, name, map[string]string{"unavailable-control": "1"})
	if preparationErr == nil || group == nil || group.kill != nil {
		t.Fatalf("partial preparation did not keep an owner without a kill handle: %v: %v", group, preparationErr)
	}
	occupant := exec.Command("/bin/sleep", "60")
	t.Cleanup(func() {
		if occupant.Process != nil && occupant.ProcessState == nil {
			_ = occupant.Process.Kill()
			_ = occupant.Wait()
		}
		cleanup, cancel := sandboxwire.CleanupContext(t.Context())
		defer cancel()
		if err := group.closeGroup(cleanup); err != nil {
			t.Error(err)
		}
	})
	if mode == "bounded" {
		occupant.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(group.directory.Fd())}
		if err := occupant.Start(); err != nil {
			t.Fatal("starting a process inside the partial group:", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	started := time.Now()
	owner, err := finishGroupPreparation(ctx, group, preparationErr)
	elapsed := time.Since(started)
	if !errors.Is(err, preparationErr) {
		t.Fatal("preparation error lost:", err)
	}
	if mode == "runs" {
		if owner != nil || !group.closed {
			t.Fatal("rollback did not run after the caller's context ended:", err)
		}
		return
	}
	if owner != group || group.closed || !errors.Is(err, sandboxwire.ErrCleanupTimedOut) {
		t.Fatalf("populated rollback did not stop at the cleanup bound: owner=%v error=%v", owner, err)
	}
	if elapsed < sandboxwire.CleanupTimeout || elapsed > sandboxwire.CleanupTimeout+2*time.Second {
		t.Fatal("cleanup bound not kept:", elapsed)
	}
	if err := occupant.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = occupant.Wait()
	retry, release := sandboxwire.CleanupContext(t.Context())
	defer release()
	if err := group.closeGroup(retry); err != nil || !group.closed {
		t.Fatal("retained owner did not clean up once the group emptied:", err)
	}
}
