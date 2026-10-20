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
	"crypto/sha256"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mutatingIdentityHash struct {
	hash.Hash
	mutate func()
	once   sync.Once
}

func (digest *mutatingIdentityHash) Write(data []byte) (int, error) {
	digest.once.Do(digest.mutate)
	return digest.Hash.Write(data)
}

func identityFileFixture(t *testing.T) (*scriptIdentityScan, string, fs.FileInfo) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "main.go")
	require.NoError(t, os.WriteFile(path, []byte("package main"), 0600))
	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	expected, err := root.Lstat("main.go")
	require.NoError(t, err)
	var scan scriptIdentityScan
	scan.digest = sha256.New()
	scan.rootHandle = root
	scan.root = directory
	scan.remaining = maxIdentityBytes
	return &scan, path, expected
}

func TestIdentityReadRejectsReplacedFile(t *testing.T) {
	t.Parallel()
	scan, path, expected := identityFileFixture(t)
	replacement := filepath.Join(scan.root, "replacement")
	require.NoError(t, os.WriteFile(replacement, []byte("package main"), 0600))
	require.NoError(t, os.Rename(path, path+".original"))
	require.NoError(t, os.Rename(replacement, path))
	require.Error(t, scan.addFile(path, expected), "a same-size replacement is not the original source")
}

func TestIdentityReadRejectsDirectoryReplacement(t *testing.T) {
	t.Parallel()
	scan, path, expected := identityFileFixture(t)
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Mkdir(path, 0700))
	require.Error(t, scan.addFile(path, expected), "a directory replacement is not source")
}

func TestIdentityReadChecksFinalMetadata(t *testing.T) {
	t.Parallel()
	scan, path, expected := identityFileFixture(t)
	scan.digest = &mutatingIdentityHash{
		Hash: sha256.New(),
		mutate: func() {
			changed := expected.ModTime().Add(time.Hour)
			assert.NoError(t, os.Chtimes(path, changed, changed))
		},
		once: sync.Once{},
	}
	require.Error(t, scan.addFile(path, expected), "a source change after opening is detected")
}

func TestIdentityReadStableFile(t *testing.T) {
	t.Parallel()
	scan, path, expected := identityFileFixture(t)
	require.NoError(t, scan.addFile(path, expected), "a stable regular source is accepted")
	require.Equal(t, maxIdentityBytes-expected.Size(), scan.remaining, "source bytes count towards the scan budget")
}
