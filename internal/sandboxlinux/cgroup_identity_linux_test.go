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
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestGroupIdentityRejectsOrdinaryStorage(t *testing.T) {
	parent, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if identity, err := cgroupDirectoryIdentity(parent); !errors.Is(err, ErrUnavailable) || identity != (cgroupDirectoryID{}) {
		t.Fatal("ordinary storage accepted as cgroup identity:", identity, err)
	}
	if identity, err := cgroupDirectoryIdentity(nil); !errors.Is(err, ErrUnavailable) || identity != (cgroupDirectoryID{}) {
		t.Fatal("missing handle accepted:", identity, err)
	}
}

func TestGroupCleanupNeverAdoptsMissingHandle(t *testing.T) {
	path := t.TempDir()
	parent, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := os.Mkdir(filepath.Join(path, "replacement"), 0700); err != nil {
		t.Fatal(err)
	}
	group := &Group{parent: parent, name: "replacement"}
	if err := group.prepareCleanup(); err == nil || group.directory != nil {
		if group.directory != nil {
			_ = group.directory.Close()
		}
		t.Fatal("cleanup adopted a directory without original identity:", err)
	}
	if _, err := os.Stat(filepath.Join(path, "replacement")); err != nil {
		t.Fatal("unowned directory was removed:", err)
	}
}

func TestCgroupNativeCleanupRejectsReplacement(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup-v2 parent")
	}
	group, err := newServiceGroup(parent, Limits{}, testTenant)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	originalName := group.name
	original, err := cgroupDirectoryIdentity(group.directory)
	if err != nil {
		_ = group.closeGroup(ctx)
		t.Fatal(err)
	}
	if err := unix.Unlinkat(int(group.parent.Fd()), originalName, unix.AT_REMOVEDIR); err != nil {
		_ = group.closeGroup(ctx)
		t.Fatal(err)
	}
	defer func() {
		if err := group.closeHandles(); err != nil {
			t.Error(err)
		}
	}()
	if err := group.closeGroup(ctx); !errors.Is(err, ErrUnavailable) || group.closed {
		t.Fatal("missing original name did not retain ownership:", err)
	}
	replacement, err := newServiceGroup(parent, Limits{}, testTenant)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := replacement.closeGroup(ctx); err != nil {
			t.Error(err)
		}
	}()
	current, err := cgroupDirectoryIdentity(replacement.directory)
	if err != nil || current == original {
		t.Fatal("replacement did not have distinct identity:", err)
	}
	if err := group.closeGroup(ctx); !errors.Is(err, ErrUnavailable) || group.closed {
		t.Fatal("cleanup accepted replacement reservation:", err)
	}
	if _, err := os.Stat(filepath.Join(parent, originalName)); err != nil {
		t.Fatal("cleanup removed replacement reservation:", err)
	}
	if err := replacement.verifyDirectoryEntry(); err != nil {
		t.Fatal("replacement lost its own identity:", err)
	}
}
