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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApprovalReloadDiscardsStaleGrants(t *testing.T) {
	t.Parallel()
	for _, condition := range []string{"removed", "malformed", "newer-schema", "oversized", "directory"} {
		t.Run(condition, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), LockfileName)
			store := NewStore(path)
			store.Upsert(LockedModule{Path: "example.com/module", Version: "v1", ApprovedCapabilities: []string{"network"}})
			if err := store.Save(); err != nil {
				t.Fatal(err)
			}
			var err error
			switch condition {
			case "removed":
				err = os.Remove(path)
			case "malformed":
				err = os.WriteFile(path, []byte("{"), 0o600)
			case "newer-schema":
				err = os.WriteFile(path, []byte("{\"schema\":999}"), 0o600)
			case "oversized":
				err = os.Truncate(path, maximumApprovalFileBytes+1)
			case "directory":
				if removeErr := os.Remove(path); removeErr != nil {
					t.Fatal(removeErr)
				}
				err = os.Mkdir(path, 0o700)
			}
			if err != nil {
				t.Fatal(err)
			}
			err = store.Load()
			if condition == "removed" && err != nil {
				t.Fatal(err)
			}
			if condition != "removed" && err == nil {
				t.Fatal("invalid approval file accepted")
			}
			if _, exists := store.Lookup("example.com/module", "v1"); exists {
				t.Fatal("stale grant survived reload")
			}
			if store.dirty || store.lockfile.ScriptHash != "" || store.lockfile.Schema != LockfileSchema {
				t.Fatal("reload did not reset approval state")
			}
		})
	}
}

func TestApprovalOversizedSavePreservesPriorFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), LockfileName)
	original := []byte("original")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	store.SetScript(strings.Repeat("x", maximumApprovalFileBytes), "hash")
	if err := store.Save(); err == nil {
		t.Fatal("oversized approval store published")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(original) {
		t.Fatalf("rejected save replaced existing file: %v", err)
	}
	if !store.dirty {
		t.Fatal("failed save discarded pending changes")
	}
}
