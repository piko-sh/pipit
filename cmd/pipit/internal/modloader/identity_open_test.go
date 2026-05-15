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
	if err := os.WriteFile(path, []byte("package main"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	expected, err := root.Lstat("main.go")
	if err != nil {
		t.Fatal(err)
	}
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
	if err := os.WriteFile(replacement, []byte("package main"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if err := scan.addFile(path, expected); err == nil {
		t.Fatal("same-size replacement was accepted as the original source")
	}
}

func TestIdentityReadRejectsDirectoryReplacement(t *testing.T) {
	t.Parallel()
	scan, path, expected := identityFileFixture(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := scan.addFile(path, expected); err == nil {
		t.Fatal("directory replacement was accepted as source")
	}
}

func TestIdentityReadChecksFinalMetadata(t *testing.T) {
	t.Parallel()
	scan, path, expected := identityFileFixture(t)
	scan.digest = &mutatingIdentityHash{
		Hash: sha256.New(),
		mutate: func() {
			changed := expected.ModTime().Add(time.Hour)
			if err := os.Chtimes(path, changed, changed); err != nil {
				t.Fatal(err)
			}
		},
		once: sync.Once{},
	}
	if err := scan.addFile(path, expected); err == nil {
		t.Fatal("source modification after opening was not detected")
	}
}

func TestIdentityReadStableFile(t *testing.T) {
	t.Parallel()
	scan, path, expected := identityFileFixture(t)
	if err := scan.addFile(path, expected); err != nil {
		t.Fatalf("stable regular source rejected: %v", err)
	}
	if scan.remaining != maxIdentityBytes-expected.Size() {
		t.Fatal("source bytes were not charged to the scan budget")
	}
}
