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

	"github.com/stretchr/testify/require"
)

func TestApprovalReloadDiscardsStaleGrants(t *testing.T) {
	t.Parallel()
	for _, condition := range []string{"removed", "malformed", "newer-schema", "oversized", "directory"} {
		t.Run(condition, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), LockfileName)
			store := NewStore(path)
			store.Upsert(LockedModule{Path: "example.com/module", Version: "v1", ApprovedCapabilities: []string{"network"}})
			require.NoError(t, store.Save())
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
				require.NoError(t, os.Remove(path))
				err = os.Mkdir(path, 0o700)
			}
			require.NoError(t, err)
			err = store.Load()
			if condition == "removed" {
				require.NoError(t, err)
			} else {
				require.Error(t, err, "an invalid approval file is rejected")
			}
			_, exists := store.Lookup("example.com/module", "v1")
			require.False(t, exists, "a reload drops stale grants")
			require.False(t, store.dirty, "a reload resets the approval state")
			require.Empty(t, store.lockfile.ScriptHash)
			require.Equal(t, LockfileSchema, store.lockfile.Schema)
		})
	}
}

func TestApprovalOversizedSavePreservesPriorFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), LockfileName)
	original := []byte("original")
	require.NoError(t, os.WriteFile(path, original, 0o600))
	store := NewStore(path)
	store.SetScript(strings.Repeat("x", maximumApprovalFileBytes), "hash")
	require.Error(t, store.Save(), "an oversized approval store is not published")
	data, err := os.ReadFile(path)
	require.NoError(t, err, "a rejected save keeps the existing file")
	require.Equal(t, string(original), string(data))
	require.True(t, store.dirty, "a failed save keeps pending changes")
}
