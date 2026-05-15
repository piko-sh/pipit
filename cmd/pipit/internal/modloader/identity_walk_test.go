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
	"strings"
	"testing"
)

func TestScriptIdentityBoundsIgnoredEntries(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	for index := range maxIdentityEntries + 1 {
		if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("%05d.txt", index)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := ScriptIdentity(directory); err == nil || !strings.Contains(err.Error(), "directory entry limit") {
		t.Fatalf("irrelevant files escaped traversal budget: %v", err)
	}
}

func TestScriptIdentityBoundsDirectoryDepth(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	nested := directory
	for range maxIdentityDepth + 1 {
		nested = filepath.Join(nested, "d")
	}
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ScriptIdentity(directory); err == nil || !strings.Contains(err.Error(), "directory depth limit") {
		t.Fatalf("empty directories escaped depth budget: %v", err)
	}
}

func TestScriptIdentityPreservesLexicalHash(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	for _, name := range []string{"z.go", "b/helper.go", "a.go", "b/ignored.txt", "go.mod", ".git/deep/ignored.go"} {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	_, actual, err := ScriptIdentity(directory)
	expected := fmt.Sprintf("sha256:%x", reference.digest.Sum(nil))
	if err != nil || actual != expected {
		t.Fatalf("bounded traversal changed stable identity: actual=%q expected=%q error=%v", actual, expected, err)
	}
}

func TestIdentityDirectoryReadsAreRootedAndRejectSymlinks(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var scan scriptIdentityScan
	scan.rootHandle = root
	if _, err := scan.readDirectory("../outside"); err == nil {
		t.Fatal("directory traversal escaped the pinned root")
	}
	if err := os.Mkdir(filepath.Join(directory, "real"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(directory, "link")); err != nil {
		t.Skipf("cannot create symlink on this host: %v", err)
	}
	if _, err := scan.readDirectory("link"); err == nil {
		t.Fatal("directory symlink was followed")
	}
}
