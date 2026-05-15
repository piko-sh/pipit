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
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func orphanStoreFixture(t *testing.T) (string, LinuxImageStoreIdentity) {
	t.Helper()
	directory := testRecoveryDirectory(t)
	store, err := OpenLinuxImageStore(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.InitialIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CleanupOrphans(context.Background()); !errors.Is(err, ErrDenied) {
		t.Fatal("fresh store acquired recovery authority:", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return directory, identity
}

func writeOrphanImage(t *testing.T, directory, suffix string, executable bool) string {
	t.Helper()
	root := filepath.Join(directory, orphanImagePrefix+strings.Repeat("A", orphanImageTokenBytes-1)+suffix)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if executable {
		if err := os.WriteFile(filepath.Join(root, orphanImageExecutable), []byte("partial"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestImageStoreReopensApprovedOriginal(t *testing.T) {
	directory, identity := orphanStoreFixture(t)
	writeOrphanImage(t, directory, "A", true)
	writeOrphanImage(t, directory, "B", false)
	altered := identity
	altered.Inode ^= 1
	if owner, err := ReopenLinuxImageStore(directory, altered, nil, nil); owner != nil || err == nil {
		t.Fatal("changed image-store identity accepted:", err)
	}
	if owner, err := ReopenLinuxImageStore(directory, identity, []LinuxRootGrant{{Name: "exposed", Path: directory, Rights: Read}}, nil); owner != nil || err == nil {
		t.Fatal("exposed orphan storage accepted:", err)
	}
	store, err := ReopenLinuxImageStore(directory, identity, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.closeHandles()
	if file, _, err := store.AcquireDirectory(); file != nil || !errors.Is(err, ErrDenied) {
		t.Fatal("orphan storage admitted new staging:", err)
	}
	if _, err := store.InitialIdentity(); !errors.Is(err, ErrDenied) {
		t.Fatal("recovery store captured fresh initial authority:", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.CleanupOrphans(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled cleanup accepted:", err)
	}
	if entries, err := os.ReadDir(directory); err != nil || len(entries) != 2 {
		t.Fatal("cancelled cleanup mutated storage:", err)
	}
	if err := store.CleanupOrphans(context.Background()); err != nil {
		t.Fatal("orphan cleanup failed:", err)
	}
	if err := store.CleanupOrphans(context.Background()); err != nil {
		t.Fatal("repeated cleanup failed:", err)
	}
	file, _, err := store.AcquireDirectory()
	if err != nil {
		t.Fatal("cleaned store did not reopen helper admission:", err)
	}
	if err := store.CleanupOrphans(context.Background()); !errors.Is(err, ErrDenied) {
		t.Fatal("borrowed image handle allowed cleanup:", err)
	}
	if err := store.ReleaseDirectory(file); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(directory); err != nil || len(entries) != 0 {
		t.Fatal("orphan entries survived cleanup:", err)
	}
}

func TestOrphanImagePlanningRejectsUnsafeEntries(t *testing.T) {
	for _, mode := range []string{"name", "extra", "directory", "symlink", "fifo", "hardlink", "mode", "root-mode", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			directory, identity := orphanStoreFixture(t)
			original := writeOrphanImage(t, directory, "A", true)
			bad := writeOrphanImage(t, directory, "B", true)
			corruptOrphanImage(t, directory, bad, mode)
			store, err := ReopenLinuxImageStore(directory, identity, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer store.closeHandles()
			if err := store.CleanupOrphans(context.Background()); err == nil {
				t.Fatal("unsafe orphan layout accepted")
			}
			if data, err := os.ReadFile(filepath.Join(original, orphanImageExecutable)); err != nil || string(data) != "partial" {
				t.Fatal("planning failure deleted an earlier valid image:", err)
			}
			if file, _, err := store.AcquireDirectory(); file != nil || err == nil {
				t.Fatal("failed cleanup opened helper admission")
			}
			if err := store.Close(); err == nil {
				t.Fatal("failed cleanup discarded private storage ownership")
			}
		})
	}
}

func corruptOrphanImage(t *testing.T, directory, root, mode string) {
	t.Helper()
	executable := filepath.Join(root, orphanImageExecutable)
	var err error
	switch mode {
	case "name":
		err = os.Rename(root, filepath.Join(directory, "unexpected"))
	case "extra":
		err = os.WriteFile(filepath.Join(root, "extra"), []byte("keep"), 0600)
	case "hardlink":
		err = os.Link(executable, filepath.Join(t.TempDir(), "alias"))
	case "mode":
		err = os.Chmod(executable, 0777)
	case "root-mode":
		err = os.Chmod(root, 0755)
	case "oversize":
		err = os.Truncate(executable, maximumOrphanImageBytes+1)
	case "directory", "symlink", "fifo":
		if err := os.Remove(executable); err != nil {
			t.Fatal(err)
		}
		switch mode {
		case "directory":
			err = os.Mkdir(executable, 0700)
		case "symlink":
			err = os.Symlink(filepath.Join(directory, "outside"), executable)
		case "fifo":
			err = unix.Mkfifo(executable, 0600)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestOrphanImageCountBound(t *testing.T) {
	directory, identity := orphanStoreFixture(t)
	for index := range maximumImageStoreLeases + 1 {
		writeOrphanImage(t, directory, string(rune('A'+index)), false)
	}
	store, err := ReopenLinuxImageStore(directory, identity, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.closeHandles()
	if err := store.CleanupOrphans(context.Background()); !errors.Is(err, errLimit) {
		t.Fatal("oversized orphan plan accepted:", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != maximumImageStoreLeases+1 {
		t.Fatal("oversized planning mutated storage:", err)
	}
	if err := os.Remove(filepath.Join(directory, entries[0].Name())); err != nil {
		t.Fatal(err)
	}
	if err := store.CleanupOrphans(context.Background()); err != nil {
		t.Fatal("bounded retry failed:", err)
	}
}

func TestOrphanImagePlanRejectsReplacement(t *testing.T) {
	for _, target := range []string{"root", "executable"} {
		t.Run(target, func(t *testing.T) {
			directory, identity := orphanStoreFixture(t)
			root := writeOrphanImage(t, directory, "A", true)
			store, err := ReopenLinuxImageStore(directory, identity, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer store.closeHandles()
			plan, err := store.orphanPlan(context.Background())
			if err != nil || len(plan) != 1 {
				t.Fatal(err)
			}
			defer plan[0].close()
			selected := root
			if target == "executable" {
				selected = filepath.Join(root, orphanImageExecutable)
			}
			held := filepath.Join(t.TempDir(), "held")
			if err := os.Rename(selected, held); err != nil {
				t.Fatal(err)
			}
			if target == "root" {
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
			}
			replacement := filepath.Join(root, orphanImageExecutable)
			if err := os.WriteFile(replacement, []byte("replacement"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := store.removeOrphan(plan[0]); !errors.Is(err, ErrDenied) {
				t.Fatal("stale plan accepted replacement:", err)
			}
			if data, err := os.ReadFile(replacement); err != nil || string(data) != "replacement" {
				t.Fatal("stale plan changed replacement:", err)
			}
			if err := os.Remove(replacement); err != nil {
				t.Fatal(err)
			}
			if target == "root" {
				if err := os.Remove(root); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Rename(held, selected); err != nil {
				t.Fatal(err)
			}
			if err := store.CleanupOrphans(context.Background()); err != nil {
				t.Fatal("restored original cleanup failed:", err)
			}
		})
	}
}
