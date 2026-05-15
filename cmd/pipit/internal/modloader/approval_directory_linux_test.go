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
)

func TestApprovalAncestorRejectsOtherWriters(t *testing.T) {
	t.Parallel()
	for _, mode := range []os.FileMode{0o770, 0o707, os.ModeSticky | 0o777} {
		ancestor := t.TempDir()
		directory := filepath.Join(ancestor, "private")
		store := NewStore(filepath.Join(directory, LockfileName))
		store.Upsert(LockedModule{Path: "module", ApprovedCapabilities: []string{"network"}})
		if err := store.Save(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(ancestor, mode); err != nil {
			t.Fatal(err)
		}
		if err := store.Load(); err == nil {
			t.Fatal("loaded through writable ancestor")
		}
		if _, exists := store.Lookup("module", ""); exists {
			t.Fatal("retained prior grants")
		}
		store.SetScript("script.go", "replacement")
		if err := store.Save(); err == nil {
			t.Fatal("saved through writable ancestor")
		}
		missing := filepath.Join(ancestor, "missing", "nested")
		fresh := NewStore(filepath.Join(missing, LockfileName))
		fresh.SetScript("script.go", "new")
		if err := fresh.Save(); err == nil {
			t.Fatal("created approval through writable ancestor")
		}
		if _, err := os.Stat(filepath.Join(ancestor, "missing")); !os.IsNotExist(err) {
			t.Fatalf("created directories before validating ancestor: %v", err)
		}
		if err := os.Chmod(ancestor, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func TestApprovalDirectoryRejectsSymlinkAncestors(t *testing.T) {
	t.Parallel()
	for _, relative := range []bool{false, true} {
		parent := t.TempDir()
		target := filepath.Join(parent, "target")
		store := NewStore(filepath.Join(target, "private", LockfileName))
		store.SetScript("script.go", "original")
		if err := store.Save(); err != nil {
			t.Fatal(err)
		}
		linkTarget := target
		if relative {
			linkTarget = "target"
		}
		alias := filepath.Join(parent, "alias")
		if err := os.Symlink(linkTarget, alias); err != nil {
			t.Fatal(err)
		}
		aliasStore := NewStore(filepath.Join(alias, "private", LockfileName))
		if err := aliasStore.Load(); err == nil {
			t.Fatal("loaded through symlink ancestor")
		}
		aliasStore.SetScript("script.go", "replacement")
		if err := aliasStore.Save(); err == nil {
			t.Fatal("saved through symlink ancestor")
		}
		if err := store.Load(); err != nil {
			t.Fatal(err)
		}
		if store.lockfile.ScriptHash != "original" {
			t.Fatal("modified original approval")
		}
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
		if err == nil {
			t.Fatalf("accepted invalid directory path: %q", path)
		}
	}
}

func TestApprovalDirectoryCreatesProtectedChain(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "first", "second")
	root, err := openApprovalDirectory(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	root, err = openApprovalDirectory(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
}
