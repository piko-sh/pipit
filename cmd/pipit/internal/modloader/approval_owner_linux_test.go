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
	"testing"
)

func TestApprovalLoadRejectsUnsafeInodes(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"group-read", "group-write", "world-read", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			path := filepath.Join(directory, LockfileName)
			store := NewStore(path)
			store.Upsert(LockedModule{Path: "module", ApprovedCapabilities: []string{"network"}})
			if err := store.Save(); err != nil {
				t.Fatal(err)
			}
			if err := store.Load(); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "group-read":
				err = os.Chmod(path, 0o640)
			case "group-write":
				err = os.Chmod(path, 0o620)
			case "world-read":
				err = os.Chmod(path, 0o604)
			case "symlink":
				target := filepath.Join(directory, "target")
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink("target", path)
			case "hardlink":
				err = os.Link(path, filepath.Join(directory, "alias"))
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Load(); err == nil {
				t.Fatal("unsafe approval file loaded")
			}
			if _, exists := store.Lookup("module", ""); exists {
				t.Fatal("unsafe file retained prior grants")
			}
		})
	}
}

func TestApprovalLoadAcceptsPrivateReadOnlyFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), LockfileName)
	if err := os.WriteFile(path, []byte(`{"schema":2,"modules":[]}`), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := NewStore(path).Load(); err != nil {
		t.Fatal(err)
	}
}

func TestApprovalDirectoryRejectsOtherWriters(t *testing.T) {
	t.Parallel()
	for _, mode := range []os.FileMode{0o770, 0o707, os.ModeSticky | 0o777} {
		directory := t.TempDir()
		path := filepath.Join(directory, LockfileName)
		store := NewStore(path)
		store.SetScript("script.go", "original")
		if err := store.Save(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory, mode); err != nil {
			t.Fatal(err)
		}
		if err := store.Load(); err == nil {
			t.Fatal("approval loaded from writable directory")
		}
		store.SetScript("script.go", "replacement")
		if err := store.Save(); err == nil {
			t.Fatal("approval saved to writable directory")
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := store.Load(); err != nil {
			t.Fatal(err)
		}
		if store.lockfile.ScriptHash != "original" {
			t.Fatal("rejected save replaced approval")
		}
	}
}

func TestApprovalPublicationUsesPinnedDirectory(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	path := filepath.Join(parent, "original")
	moved := filepath.Join(parent, "moved")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	guard, err := acquireApprovalGuardRoot(root, LockfileName)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"schema":2,"modules":[]}`)
	if err := publishApprovalFile(root, LockfileName, data); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(moved, LockfileName))
	if err != nil || string(actual) != string(data) {
		t.Fatalf("publication lost the pinned directory: %q %v", actual, err)
	}
	if _, err := os.Stat(filepath.Join(path, LockfileName)); !os.IsNotExist(err) {
		t.Fatalf("publication followed a replaced pathname: %v", err)
	}
	entries, err := os.ReadDir(moved)
	if err != nil || len(entries) != 2 {
		t.Fatalf("publication left temporary files: %v %v", entries, err)
	}
}
