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

//go:build linux

package modloader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApprovalAncestorRejectsOtherWriters(t *testing.T) {
	t.Parallel()
	for _, mode := range []os.FileMode{0o770, 0o707, os.ModeSticky | 0o777} {
		ancestor := t.TempDir()
		directory := filepath.Join(ancestor, "private")
		store := NewStore(filepath.Join(directory, LockfileName))
		store.Upsert(LockedModule{Path: "module", ApprovedCapabilities: []string{"network"}})
		require.NoError(t, store.Save())
		require.NoError(t, os.Chmod(ancestor, mode))
		require.Error(t, store.Load(), "loading refuses a writable ancestor")
		_, exists := store.Lookup("module", "")
		require.False(t, exists, "prior grants are dropped")
		store.SetScript("script.go", "replacement")
		require.Error(t, store.Save(), "saving refuses a writable ancestor")
		missing := filepath.Join(ancestor, "missing", "nested")
		fresh := NewStore(filepath.Join(missing, LockfileName))
		fresh.SetScript("script.go", "new")
		require.Error(t, fresh.Save(), "no approval is created through a writable ancestor")
		_, err := os.Stat(filepath.Join(ancestor, "missing"))
		require.True(t, os.IsNotExist(err), "no directory is created before the ancestor check: %v", err)
		require.NoError(t, os.Chmod(ancestor, 0o700))
	}
}

func TestApprovalDirectoryRejectsSymlinkAncestors(t *testing.T) {
	t.Parallel()
	for _, relative := range []bool{false, true} {
		parent := t.TempDir()
		target := filepath.Join(parent, "target")
		store := NewStore(filepath.Join(target, "private", LockfileName))
		store.SetScript("script.go", "original")
		require.NoError(t, store.Save())
		linkTarget := target
		if relative {
			linkTarget = "target"
		}
		alias := filepath.Join(parent, "alias")
		require.NoError(t, os.Symlink(linkTarget, alias))
		aliasStore := NewStore(filepath.Join(alias, "private", LockfileName))
		require.Error(t, aliasStore.Load(), "loading refuses a symlinked ancestor")
		aliasStore.SetScript("script.go", "replacement")
		require.Error(t, aliasStore.Save(), "saving refuses a symlinked ancestor")
		require.NoError(t, store.Load())
		require.Equal(t, "original", store.lockfile.ScriptHash, "the original approval is left alone")
	}
}

func TestApprovalDirectoryDepthAndTraversal(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/" + strings.Repeat("child/", maximumApprovalDirectoryDepth+1),
		t.TempDir() + "/../child",
	} {
		root, err := openApprovalDirectory(path, true)
		if root != nil {
			root.Close()
		}
		require.Error(t, err, "an invalid directory path %q is rejected", path)
	}
}

func TestApprovalDirectoryCreatesProtectedChain(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "first", "second")
	root, err := openApprovalDirectory(path, true)
	require.NoError(t, err)
	require.NoError(t, root.Close())
	root, err = openApprovalDirectory(path, false)
	require.NoError(t, err)
	require.NoError(t, root.Close())
}
