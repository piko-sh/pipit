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
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxwire"
)

func filesystemWriteMessage(t *testing.T, id uint64, path string, data []byte) sandboxwire.Message {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"operation": WriteFile, "root": "data", "path": path, "data": data})
	require.NoError(t, err)
	return sandboxwire.Message{Kind: sandboxwire.Call, ID: id, Payload: encoded}
}

func TestLinuxFilesystemStagedWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "file")
	require.NoError(t, os.WriteFile(target, []byte("original"), 0644))
	original, err := os.Open(target)
	require.NoError(t, err)
	defer original.Close()
	before, err := original.Stat()
	require.NoError(t, err)
	backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
	result, err := backend.Execute(filesystemWriteMessage(t, 1, "file", []byte("replacement")))
	require.NoError(t, err, "%+v", result)
	require.Equal(t, 11, result.Written)
	require.Empty(t, result.Data)
	require.Empty(t, result.Entries)
	after, err := os.Stat(target)
	require.NoError(t, err)
	require.False(t, os.SameFile(before, after), "write was not a new private inode")
	require.Equal(t, os.FileMode(0600), after.Mode().Perm())
	data, err := os.ReadFile(target)
	require.NoError(t, err, "%q", data)
	require.Equal(t, "replacement", string(data))
	buffer := make([]byte, 8)
	_, err = original.Read(buffer)
	require.NoError(t, err, "original inode modified: %q", buffer)
	require.Equal(t, "original", string(buffer))
	entries, err := os.ReadDir(root)
	require.NoError(t, err, "staging remnant")
	require.Len(t, entries, 1)
	require.Equal(t, "file", entries[0].Name())
	_, err = backend.Execute(filesystemWriteMessage(t, 2, "empty", []byte{}))
	require.NoError(t, err)
	empty, err := os.Stat(filepath.Join(root, "empty"))
	require.NoError(t, err, "empty write: %v", empty)
	require.Equal(t, int64(0), empty.Size())
}

func TestLinuxFilesystemWriteRejectsEscapes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret")
	require.NoError(t, os.WriteFile(target, []byte("secret"), 0600))
	require.NoError(t, os.Symlink(target, filepath.Join(root, "symlink")))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "directory-link")))
	require.NoError(t, os.Link(target, filepath.Join(root, "hardlink")))
	require.NoError(t, unix.Mkfifo(filepath.Join(root, "fifo"), 0600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "directory"), 0700))
	backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
	for index, path := range []string{"symlink", "directory-link/secret", "hardlink", "fifo", "directory", "missing/file"} {
		result, err := backend.Execute(filesystemWriteMessage(t, uint64(index+1), path, []byte("changed")))
		require.Error(t, err, "write accepted %s: %+v", path, result)
		require.Equal(t, 0, result.Written)
	}
	data, err := os.ReadFile(target)
	require.NoError(t, err, "outside target changed: %q", data)
	require.Equal(t, "secret", string(data))
	entries, err := os.ReadDir(root)
	require.NoError(t, err, "failed write left files")
	require.Len(t, entries, 5)
}

func TestLinuxFilesystemStagingCleanup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "directory"), 0700))
	backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
	parent, err := openLinuxBeneath(backend.roots["data"], ".", unix.O_RDONLY|unix.O_DIRECTORY)
	require.NoError(t, err)
	defer parent.Close()
	temporary, err := createLinuxStagingFile(parent)
	require.NoError(t, err)
	defer temporary.Close()
	_, err = temporary.WriteString("staged")
	require.NoError(t, err)
	require.Error(t, backend.publish(parent, temporary, "directory", 1), "replaced directory")
	entries, err := os.ReadDir(root)
	require.NoError(t, err, "failed publication leaked staging entry")
	require.Len(t, entries, 1)
	require.Equal(t, "directory", entries[0].Name())
}

func TestLinuxFilesystemStagingCollision(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
	staging, err := StagingName(backend.namespace, 1)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, staging), []byte("not ours"), 0600))
	_, err = backend.Execute(filesystemWriteMessage(t, 1, "file", []byte("replacement")))
	require.Error(t, err, "publication replaced an existing staging entry")
	data, err := os.ReadFile(filepath.Join(root, staging))
	require.NoError(t, err, "collision altered existing file: %q", data)
	require.Equal(t, "not ours", string(data))
	_, err = os.Stat(filepath.Join(root, "file"))
	require.True(t, os.IsNotExist(err), "collision published destination: %v", err)
}

func TestLinuxFilesystemWriteNested(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "directory"), 0700))
	backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
	_, err := backend.Execute(filesystemWriteMessage(t, 1, "directory/file", []byte("nested")))
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(root, "directory", "file"))
	require.NoError(t, err, "%q", data)
	require.Equal(t, "nested", string(data))
}
