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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLinuxImageStoreOwnership(t *testing.T) {
	t.Parallel()
	directory := testRecoveryDirectory(t)
	grant := testRecoveryDirectory(t)
	metadata := []string{testRecoveryDirectory(t), testRecoveryDirectory(t), testRecoveryDirectory(t)}
	store, err := OpenLinuxImageStore(directory, []LinuxRootGrant{{Name: "data", Path: grant, Rights: Read | Write}}, metadata)
	require.NoError(t, err)
	defer store.Close()
	identity, err := store.Identity()
	require.NoError(t, err, "missing original image-store identity")
	require.NotEqual(t, uint64(0), identity.Inode)
	require.NotEqual(t, uint64(0), identity.MountID)
	initial, err := store.InitialIdentity()
	require.NoError(t, err, "empty store could not capture initial identity")
	require.Equal(t, identity, initial)
	require.NoError(t, store.ValidatePolicy([]LinuxRootGrant{{Name: "data", Path: grant, Rights: Read}}, metadata), "original exclusion policy rejected")
	require.Error(t, store.ValidatePolicy([]LinuxRootGrant{{Name: "other", Path: t.TempDir(), Rights: Read}}, metadata), "uncovered grant accepted")
	require.Error(t, store.ValidatePolicy(nil, []string{t.TempDir()}), "uncovered metadata accepted")
	competing, err := OpenLinuxImageStore(directory, nil, nil)
	if competing != nil {
		_ = competing.Close()
	}
	require.Error(t, err, "image store lease was not exclusive")
	require.Nil(t, competing)
	leases := make([]*os.File, 0, maximumImageStoreLeases)
	for range maximumImageStoreLeases {
		file, path, err := store.AcquireDirectory()
		require.NoError(t, err, "private directory acquisition failed")
		require.Equal(t, directory, path)
		leases = append(leases, file)
	}
	file, _, err := store.AcquireDirectory()
	require.Nil(t, file, "image lease bound bypassed")
	require.ErrorIs(t, err, errLimit)
	_, err = store.InitialIdentity()
	require.Error(t, err, "active staging accepted as initial image identity")
	require.Error(t, store.Close(), "live images keep storage ownership")
	require.ErrorIs(t, store.ReleaseDirectory(nil), ErrDenied, "foreign image lease accepted")
	for _, file := range leases {
		require.NoError(t, store.ReleaseDirectory(file))
	}
	require.ErrorIs(t, store.ReleaseDirectory(leases[0]), ErrDenied, "returned lease accepted twice")
	unexpected := filepath.Join(directory, "unexpected")
	require.NoError(t, os.WriteFile(unexpected, []byte("retain"), 0600))
	require.Error(t, store.Close(), "non-empty storage was released")
	_, err = store.InitialIdentity()
	require.Error(t, err, "non-empty storage accepted as initial image identity")
	data, err := os.ReadFile(unexpected)
	require.NoError(t, err, "unexpected storage entry changed")
	require.Equal(t, "retain", string(data))
	require.NoError(t, os.Remove(unexpected))
	require.NoError(t, store.Close())
	_, err = store.Identity()
	require.ErrorIs(t, err, ErrClosed, "closed store exposed identity")
	file, _, err = store.AcquireDirectory()
	require.Nil(t, file, "closed store granted staging")
	require.ErrorIs(t, err, ErrClosed)
	reopened, err := OpenLinuxImageStore(directory, nil, nil)
	require.NoError(t, err)
	defer reopened.Close()
	current, err := reopened.Identity()
	require.NoError(t, err, "storage identity changed on reopen")
	require.Equal(t, identity, current)
}

func TestLinuxImageStoreRejectsOverlap(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"same-grant", "grant-ancestor", "grant-descendant", "same-metadata", "metadata-ancestor", "metadata-descendant"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ancestor := testRecoveryDirectory(t)
			descendant := filepath.Join(ancestor, "nested")
			require.NoError(t, os.Mkdir(descendant, 0700))
			directory, excluded := descendant, ancestor
			switch mode {
			case "same-grant", "same-metadata":
				excluded = directory
			case "grant-descendant", "metadata-descendant":
				directory, excluded = ancestor, descendant
			}
			var grants []LinuxRootGrant
			var metadata []string
			switch mode {
			case "same-grant", "grant-ancestor", "grant-descendant":
				grants = []LinuxRootGrant{{Name: "data", Path: excluded, Rights: Read}}
			default:
				metadata = []string{excluded}
			}
			store, err := OpenLinuxImageStore(directory, grants, metadata)
			if store != nil {
				_ = store.Close()
			}
			require.Error(t, err, "overlapping image storage accepted")
			require.Nil(t, store)
			var probe LinuxRecoveryJournal
			require.NoError(t, probe.open(directory), "a failed image claim keeps no lease")
			require.NoError(t, probe.Close())
		})
	}
}

func TestLinuxImageStoreRejectsChangedPaths(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"storage", "grant", "mode"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			directory, grant := testRecoveryDirectory(t), testRecoveryDirectory(t)
			store, err := OpenLinuxImageStore(directory, []LinuxRootGrant{{Name: "data", Path: grant, Rights: Read}}, nil)
			require.NoError(t, err)
			defer store.Close()
			target := directory
			if change == "grant" {
				target = grant
			}
			original := target + ".original"
			if change == "mode" {
				require.NoError(t, os.Chmod(target, 0755))
			} else {
				require.NoError(t, os.Rename(target, original))
				require.NoError(t, os.Mkdir(target, 0700))
			}
			file, _, err := store.AcquireDirectory()
			require.Nil(t, file, "changed storage policy admitted staging")
			require.Error(t, err)
			if change == "mode" {
				require.NoError(t, os.Chmod(target, 0700))
			} else {
				require.NoError(t, os.Remove(target))
				require.NoError(t, os.Rename(original, target))
			}
			require.NoError(t, store.Close(), "restored storage could not close")
		})
	}
}
