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
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestPreparedWriteDoesNotPublish(t *testing.T) {
	t.Parallel()
	for _, commit := range []bool{false, true} {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "file"), []byte("original"), 0600))
		backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
		prepared, err := prepareLinuxWrite(backend.roots["data"], "file", []byte("new"), 1)
		if prepared != nil {
			t.Cleanup(func() { _ = prepared.Close() })
		}
		require.NoError(t, err)
		entries, err := os.ReadDir(root)
		require.NoError(t, err, "preparation exposed a name")
		require.Len(t, entries, 1)
		require.Equal(t, "file", entries[0].Name())
		data, err := os.ReadFile(filepath.Join(root, "file"))
		require.NoError(t, err, "preparation changed target")
		require.Equal(t, "original", string(data))
		require.EqualValues(t, 3, prepared.identity.Size, "the prepared identity is complete")
		require.NotZero(t, prepared.identity.Inode)
		require.NotZero(t, prepared.identity.ParentInode)
		expected := "original"
		if commit {
			require.NoError(t, prepared.commit(backend))
			expected = "new"
		}
		require.NoError(t, prepared.Close())
		require.NoError(t, prepared.Close())
		require.ErrorIs(t, prepared.commit(backend), ErrClosed, "closed prepared write reused")
		data, err = os.ReadFile(filepath.Join(root, "file"))
		require.NoError(t, err, "unexpected final target")
		require.Equal(t, expected, string(data))
	}
}

func TestPreparedWriteRejectsChangedInode(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"size", "mode", "link", "target"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "file"), []byte("original"), 0600))
			backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
			prepared, err := prepareLinuxWrite(backend.roots["data"], "file", []byte("new"), 1)
			if prepared != nil {
				defer prepared.Close()
			}
			require.NoError(t, err)
			switch mode {
			case "size":
				_, err = prepared.temporary.WriteAt([]byte("larger"), 0)
			case "mode":
				err = prepared.temporary.Chmod(0644)
			case "link":
				err = unix.Linkat(int(backend.proc.Fd()), strconv.FormatUint(uint64(prepared.temporary.Fd()), 10),
					int(prepared.parent.Fd()), "unexpected-link", unix.AT_SYMLINK_FOLLOW)
			case "target":
				err = os.Link(filepath.Join(root, "file"), filepath.Join(root, "target-link"))
			}
			require.NoError(t, err)
			require.ErrorIs(t, prepared.commit(backend), ErrDenied, "changed prepared state accepted")
			require.True(t, prepared.closed, "a failed commit keeps no handles")
			data, err := os.ReadFile(filepath.Join(root, "file"))
			require.NoError(t, err, "failed commit changed target")
			require.Equal(t, "original", string(data))
		})
	}
}

func TestPreparedWritePartialOwnership(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "directory"), 0700))
	backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
	prepared, err := prepareLinuxWrite(backend.roots["data"], "directory", nil, 1)
	require.Error(t, err, "partial failure lost cleanup ownership")
	require.NotNil(t, prepared)
	require.NoError(t, prepared.Close())
	_, err = prepared.parent.Stat()
	require.ErrorIs(t, err, os.ErrClosed, "partial owner leaked parent")
	for _, name := range []string{"../escape", ".pipit-stage-private", ""} {
		prepared, err := prepareLinuxWrite(backend.roots["data"], name, nil, 1)
		require.Error(t, err, "invalid path admitted: %v", name)
		require.Nil(t, prepared)
	}
}
