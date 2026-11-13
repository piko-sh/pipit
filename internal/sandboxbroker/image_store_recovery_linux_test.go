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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func orphanStoreFixture(t *testing.T) (string, LinuxImageStoreIdentity) {
	t.Helper()
	directory := testRecoveryDirectory(t)
	store, err := OpenLinuxImageStore(directory, nil, nil)
	require.NoError(t, err)
	identity, err := store.InitialIdentity()
	require.NoError(t, err)
	require.ErrorIs(t, store.CleanupOrphans(t.Context()), ErrDenied, "fresh store acquired recovery authority")
	require.NoError(t, store.Close())
	return directory, identity
}

func writeOrphanImage(t *testing.T, directory, suffix string, executable bool) string {
	t.Helper()
	root := filepath.Join(directory, orphanImagePrefix+strings.Repeat("A", orphanImageTokenBytes-1)+suffix)
	require.NoError(t, os.Mkdir(root, 0700))
	if executable {
		require.NoError(t, os.WriteFile(filepath.Join(root, orphanImageExecutable), []byte("partial"), 0600))
	}
	return root
}

func TestImageStoreReopensApprovedOriginal(t *testing.T) {
	t.Parallel()
	directory, identity := orphanStoreFixture(t)
	writeOrphanImage(t, directory, "A", true)
	writeOrphanImage(t, directory, "B", false)
	altered := identity
	altered.Inode ^= 1
	owner, err := ReopenLinuxImageStore(directory, altered, nil, nil)
	require.Nil(t, owner, "changed image-store identity accepted")
	require.Error(t, err)
	owner, err = ReopenLinuxImageStore(directory, identity, []LinuxRootGrant{{Name: "exposed", Path: directory, Rights: Read}}, nil)
	require.Nil(t, owner, "exposed orphan storage accepted")
	require.Error(t, err)
	store, err := ReopenLinuxImageStore(directory, identity, nil, nil)
	require.NoError(t, err)
	defer store.closeHandles()
	staging, _, err := store.AcquireDirectory()
	require.Nil(t, staging, "orphan storage admits no new staging")
	require.ErrorIs(t, err, ErrDenied)
	_, err = store.InitialIdentity()
	require.ErrorIs(t, err, ErrDenied, "recovery store captured fresh initial authority")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, store.CleanupOrphans(ctx), context.Canceled, "cancelled cleanup accepted")
	remaining, err := os.ReadDir(directory)
	require.NoError(t, err, "a cancelled cleanup leaves the storage unchanged")
	require.Len(t, remaining, 2)
	require.NoError(t, store.CleanupOrphans(t.Context()), "orphan cleanup succeeds")
	require.NoError(t, store.CleanupOrphans(t.Context()), "a repeated cleanup succeeds")
	file, _, err := store.AcquireDirectory()
	require.NoError(t, err, "a cleaned store reopens helper admission")
	require.ErrorIs(t, store.CleanupOrphans(t.Context()), ErrDenied, "borrowed image handle allowed cleanup")
	require.NoError(t, store.ReleaseDirectory(file))
	require.NoError(t, store.Close())
	entries, err := os.ReadDir(directory)
	require.NoError(t, err, "orphan entries survived cleanup")
	require.Empty(t, entries)
}

func TestOrphanImagePlanningRejectsUnsafeEntries(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"name", "extra", "directory", "symlink", "fifo", "hardlink", "mode", "root-mode", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			directory, identity := orphanStoreFixture(t)
			original := writeOrphanImage(t, directory, "A", true)
			bad := writeOrphanImage(t, directory, "B", true)
			corruptOrphanImage(t, directory, bad, mode)
			store, err := ReopenLinuxImageStore(directory, identity, nil, nil)
			require.NoError(t, err)
			defer store.closeHandles()
			require.Error(t, store.CleanupOrphans(t.Context()), "unsafe orphan layout accepted")
			data, err := os.ReadFile(filepath.Join(original, orphanImageExecutable))
			require.NoError(t, err, "planning failure deleted an earlier valid image")
			require.Equal(t, "partial", string(data))
			file, _, err := store.AcquireDirectory()
			require.Nil(t, file, "failed cleanup opened helper admission")
			require.Error(t, err)
			require.Error(t, store.Close(), "a failed cleanup keeps private storage ownership")
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
		require.NoError(t, os.Remove(executable))
		switch mode {
		case "directory":
			err = os.Mkdir(executable, 0700)
		case "symlink":
			err = os.Symlink(filepath.Join(directory, "outside"), executable)
		case "fifo":
			err = unix.Mkfifo(executable, 0600)
		}
	}
	require.NoError(t, err)
}

func TestOrphanImageCountBound(t *testing.T) {
	t.Parallel()
	directory, identity := orphanStoreFixture(t)
	for index := range maximumImageStoreLeases + 1 {
		writeOrphanImage(t, directory, string(rune('A'+index)), false)
	}
	store, err := ReopenLinuxImageStore(directory, identity, nil, nil)
	require.NoError(t, err)
	defer store.closeHandles()
	err = store.CleanupOrphans(t.Context())
	require.ErrorIs(t, err, errLimit, "oversized orphan plan accepted")
	entries, err := os.ReadDir(directory)
	require.NoError(t, err, "oversized planning mutated storage")
	require.Len(t, entries, maximumImageStoreLeases+1)
	require.NoError(t, os.Remove(filepath.Join(directory, entries[0].Name())))
	require.NoError(t, store.CleanupOrphans(t.Context()), "the bounded retry succeeds")
}

func TestOrphanImagePlanRejectsReplacement(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"root", "executable"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			directory, identity := orphanStoreFixture(t)
			root := writeOrphanImage(t, directory, "A", true)
			store, err := ReopenLinuxImageStore(directory, identity, nil, nil)
			require.NoError(t, err)
			defer store.closeHandles()
			plan, err := store.orphanPlan(t.Context())
			require.NoError(t, err)
			require.Len(t, plan, 1)
			defer plan[0].close()
			selected := root
			if target == "executable" {
				selected = filepath.Join(root, orphanImageExecutable)
			}
			held := filepath.Join(t.TempDir(), "held")
			require.NoError(t, os.Rename(selected, held))
			if target == "root" {
				require.NoError(t, os.Mkdir(root, 0700))
			}
			replacement := filepath.Join(root, orphanImageExecutable)
			require.NoError(t, os.WriteFile(replacement, []byte("replacement"), 0600))
			require.ErrorIs(t, store.removeOrphan(plan[0]), ErrDenied, "stale plan accepted replacement")
			data, err := os.ReadFile(replacement)
			require.NoError(t, err, "stale plan changed replacement")
			require.Equal(t, "replacement", string(data))
			require.NoError(t, os.Remove(replacement))
			if target == "root" {
				require.NoError(t, os.Remove(root))
			}
			require.NoError(t, os.Rename(held, selected))
			require.NoError(t, store.CleanupOrphans(t.Context()), "the restored original cleanup succeeds")
		})
	}
}
