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
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxwire"
)

func openTestFilesystem(t *testing.T, root string, rights Rights, limits FilesystemLimits) *LinuxFilesystem {
	t.Helper()
	backend, err := OpenLinuxFilesystem([]LinuxRootGrant{{Name: "data", Path: root, Rights: rights}}, limits)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, backend.Close())
	})
	return backend
}

func filesystemMessage(t *testing.T, id uint64, operation Operation, path string, limit int) sandboxwire.Message {
	t.Helper()
	payload := map[string]any{"operation": operation, "root": "data", "path": path}
	switch operation {
	case ReadFile:
		payload["max_bytes"] = limit
	case ListDirectory:
		payload["max_entries"] = limit
	case WriteFile:
		payload["data"] = ""
	}
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	return sandboxwire.Message{Kind: sandboxwire.Call, ID: id, Payload: encoded}
}

func TestLinuxFilesystemReadAndList(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "file"), []byte("abcdef"), 0600))
	backend := openTestFilesystem(t, root, Read|List, FilesystemLimits{})
	result, err := backend.Execute(filesystemMessage(t, 1, ReadFile, "file", 4))
	require.NoError(t, err, "%+v", result)
	require.Equal(t, "abcd", string(result.Data))
	require.Empty(t, result.Entries)
	for _, id := range []uint64{2, 3} {
		result, err = backend.Execute(filesystemMessage(t, id, ListDirectory, ".", 1))
		require.NoError(t, err, "listing offset leaked: %+v", result)
		require.Len(t, result.Entries, 1)
		require.Equal(t, "file", result.Entries[0])
	}
	require.NoError(t, backend.Close())
	_, err = backend.Execute(filesystemMessage(t, 4, ReadFile, "file", 4))
	require.ErrorIs(t, err, ErrClosed)
}

func TestLinuxFilesystemRejectsNonRegularObjects(t *testing.T) {
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
	backend := openTestFilesystem(t, root, Read|List, FilesystemLimits{})
	for index, path := range []string{"symlink", "directory-link/secret", "hardlink", "fifo", "directory", "missing"} {
		result, err := backend.Execute(filesystemMessage(t, uint64(index+1), ReadFile, path, 64))
		require.Error(t, err, "opened %s: %+v", path, result)
		require.Empty(t, result.Data)
		require.Empty(t, result.Entries)
	}
	_, err := backend.Execute(filesystemMessage(t, 7, ListDirectory, "directory-link", 1))
	require.Error(t, err, "listed a symlink")
}

func TestLinuxFilesystemPinsRootIdentity(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	original, moved := filepath.Join(parent, "root"), filepath.Join(parent, "moved")
	require.NoError(t, os.Mkdir(original, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(original, "file"), []byte("approved"), 0600))
	backend := openTestFilesystem(t, original, Read, FilesystemLimits{})
	require.NoError(t, os.Rename(original, moved))
	require.NoError(t, os.Mkdir(original, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(original, "file"), []byte("replacement"), 0600))
	result, err := backend.Execute(filesystemMessage(t, 1, ReadFile, "file", 64))
	require.NoError(t, err, "root identity changed: %+v", result)
	require.Equal(t, "approved", string(result.Data))
}

func TestLinuxFilesystemRejectsMountCrossing(t *testing.T) {
	t.Parallel()
	backend := openTestFilesystem(t, "/", Read|List, FilesystemLimits{})
	for index, test := range []struct {
		operation Operation
		path      string
	}{
		{operation: ReadFile, path: "proc/version"}, {operation: ListDirectory, path: "proc"},
	} {
		result, err := backend.Execute(filesystemMessage(t, uint64(index+1), test.operation, test.path, 1))
		require.ErrorIs(t, err, unix.EXDEV, "mount traversal: %+v", result)
		require.Empty(t, result.Data)
		require.Empty(t, result.Entries)
	}
}

func TestLinuxFilesystemLimitsAndRights(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "file"), []byte("a"), 0600))
	backend := openTestFilesystem(t, root, Read, FilesystemLimits{ReadBytes: 4})
	_, err := backend.Execute(filesystemMessage(t, 1, ListDirectory, ".", 1))
	require.ErrorIs(t, err, ErrDenied)
	_, err = backend.Execute(filesystemMessage(t, 2, WriteFile, "file", 0))
	require.ErrorIs(t, err, ErrDenied)
	_, err = backend.Execute(filesystemMessage(t, 3, ReadFile, "missing", 4))
	require.Error(t, err, "missing read succeeded")
	_, err = backend.Execute(filesystemMessage(t, 4, ReadFile, "file", 1))
	require.ErrorIs(t, err, errLimit, "failed I/O refunded quota")
}

func TestLinuxFilesystemSkipsInvalidListing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"invalid\nname", "valid", "CON"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), nil, 0600))
	}
	backend := openTestFilesystem(t, root, List, FilesystemLimits{})
	result, err := backend.Execute(filesystemMessage(t, 1, ListDirectory, ".", 3))
	require.NoError(t, err, "mixed names: %+v", result)
	require.Len(t, result.Entries, 1)
	require.Equal(t, "valid", result.Entries[0])
	require.Equal(t, 2, result.skipped)

	result, err = backend.Execute(filesystemMessage(t, 2, ListDirectory, ".", 1))
	require.NoError(t, err, "bounded mixed listing: %+v", result)
	require.Equal(t, 1, len(result.Entries)+result.skipped)
}

func TestLinuxFilesystemInvalidConfiguration(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(root, link))
	for _, grants := range [][]LinuxRootGrant{
		{{Name: "data", Path: root, Rights: 0}},
		{{Name: "data", Path: "relative", Rights: Read}},
		{{Name: "data", Path: link, Rights: Read}},
		{{Name: "data", Path: root, Rights: Read}, {Name: "missing", Path: filepath.Join(root, "absent"), Rights: Read}},
	} {
		backend, err := OpenLinuxFilesystem(grants, FilesystemLimits{})
		require.Error(t, err, "invalid grant accepted: %+v", grants)
		require.Nil(t, backend)
	}
	var backend *LinuxFilesystem
	require.NoError(t, backend.Close())
	_, err := backend.Execute(brokerMessage(1, readPayload))
	require.ErrorIs(t, err, ErrClosed)
}

func TestLinuxFilesystemCloseRace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "file"), []byte("data"), 0600))
	backend := openTestFilesystem(t, root, Read, FilesystemLimits{})
	message := filesystemMessage(t, 1, ReadFile, "file", 4)
	var group sync.WaitGroup
	group.Go(func() {
		result, err := backend.Execute(message)
		if err != nil {
			assert.ErrorIs(t, err, ErrClosed)
		} else {
			assert.Equal(t, "data", string(result.Data), "a concurrent close leaves no partial result")
		}
	})
	group.Go(func() {
		assert.NoError(t, backend.Close())
	})
	group.Wait()
}

func TestLinuxFilesystemSymlinkReplacementRace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(secret, []byte("outside"), 0600))
	target := filepath.Join(root, "file")
	backend := openTestFilesystem(t, root, Read|Write, FilesystemLimits{})
	var group sync.WaitGroup
	group.Go(func() {
		for range 100 {
			candidate := filepath.Join(root, "candidate")
			if !assert.NoError(t, os.Symlink(secret, candidate)) {
				return
			}
			if !assert.NoError(t, os.Rename(candidate, target)) {
				return
			}
		}
	})
	for index := range 48 {
		write := filesystemWriteMessage(t, uint64(index*2+1), "file", []byte("inside"))
		if result, err := backend.Execute(write); err == nil {
			assert.Equal(t, 6, result.Written, "a successful write counts every byte")
		}
		read := filesystemMessage(t, uint64(index*2+2), ReadFile, "file", 64)
		if result, err := backend.Execute(read); err == nil {
			assert.Equal(t, "inside", string(result.Data), "the read stays inside the root")
		}
	}
	group.Wait()
	data, err := os.ReadFile(secret)
	require.NoError(t, err, "write followed a raced symlink: %q", data)
	require.Equal(t, "outside", string(data))
}
