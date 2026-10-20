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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScriptIdentityBoundsIgnoredEntries(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	for index := range maxIdentityEntries + 1 {
		require.NoError(t, os.WriteFile(filepath.Join(directory, fmt.Sprintf("%05d.txt", index)), nil, 0600))
	}
	_, _, err := ScriptIdentity(directory)
	require.Error(t, err, "irrelevant files count towards the traversal budget")
	require.Contains(t, err.Error(), "directory entry limit")
}

func TestScriptIdentityBoundsDirectoryDepth(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	nested := directory
	for range maxIdentityDepth + 1 {
		nested = filepath.Join(nested, "d")
	}
	require.NoError(t, os.MkdirAll(nested, 0700))
	_, _, err := ScriptIdentity(directory)
	require.Error(t, err, "empty directories count towards the depth budget")
	require.Contains(t, err.Error(), "directory depth limit")
}

func TestScriptIdentityPreservesLexicalHash(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	for _, name := range []string{"z.go", "b/helper.go", "a.go", "b/ignored.txt", "go.mod", ".git/deep/ignored.go"} {
		path := filepath.Join(directory, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, []byte(name), 0600))
	}
	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	defer root.Close()
	var reference scriptIdentityScan
	reference.digest = sha256.New()
	reference.rootHandle = root
	reference.root = directory
	reference.target = directory
	reference.remaining = maxIdentityBytes
	fmt.Fprintf(reference.digest, "pipit-local-source-v2\x00%s\x00", directory)
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == ".pipit" {
				return filepath.SkipDir
			}
			return nil
		}
		return reference.visit(path, entry)
	})
	require.NoError(t, err)
	_, actual, err := ScriptIdentity(directory)
	expected := fmt.Sprintf("sha256:%x", reference.digest.Sum(nil))
	require.NoError(t, err, "bounded traversal keeps a stable identity")
	require.Equal(t, expected, actual)
}

func TestIdentityDirectoryReadsAreRootedAndRejectSymlinks(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	defer root.Close()
	var scan scriptIdentityScan
	scan.rootHandle = root
	_, err = scan.readDirectory("../outside")
	require.Error(t, err, "directory traversal stays inside the pinned root")
	require.NoError(t, os.Mkdir(filepath.Join(directory, "real"), 0700))
	if err := os.Symlink("real", filepath.Join(directory, "link")); err != nil {
		t.Skipf("cannot create symlink on this host: %v", err)
	}
	_, err = scan.readDirectory("link")
	require.Error(t, err, "a directory symlink is not followed")
}
