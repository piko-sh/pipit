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
	"testing"
	"time"

	"golang.org/x/sys/unix"
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
	if _, err := group.newChild(Limits{}); !errors.Is(err, errClosed) {
		t.Fatal("partial group admitted child:", err)
	}
	if mode == "blocked-removal" {
		if err := unix.Mkdirat(int(group.directory.Fd()), "blocker", groupDirectoryMode); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := finishGroupPreparation(group, preparationErr)
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
