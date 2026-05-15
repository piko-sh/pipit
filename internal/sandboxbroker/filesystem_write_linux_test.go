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

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxwire"
)

func filesystemWriteMessage(t *testing.T, id uint64, path string, data []byte) sandboxwire.Message {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"operation": WriteFile, "root": "data", "path": path, "data": data})
	if err != nil {
		t.Fatal(err)
	}
	return sandboxwire.Message{Kind: sandboxwire.Call, ID: id, Payload: encoded}
}

func TestLinuxFilesystemStagedWrite(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "file")
	if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	original, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	before, err := original.Stat()
	if err != nil {
		t.Fatal(err)
	}
	backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
	result, err := backend.Execute(filesystemWriteMessage(t, 1, "file", []byte("replacement")))
	if err != nil || result.Written != 11 || len(result.Data) != 0 || len(result.Entries) != 0 {
		t.Fatalf("%+v: %v", result, err)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) || after.Mode().Perm() != 0600 {
		t.Fatal("write was not a new private inode")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "replacement" {
		t.Fatalf("%q: %v", data, err)
	}
	buffer := make([]byte, 8)
	if _, err := original.Read(buffer); err != nil || string(buffer) != "original" {
		t.Fatalf("original inode modified: %q: %v", buffer, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "file" {
		t.Fatalf("staging remnant: %v: %v", entries, err)
	}
	if _, err := backend.Execute(filesystemWriteMessage(t, 2, "empty", []byte{})); err != nil {
		t.Fatal(err)
	}
	empty, err := os.Stat(filepath.Join(root, "empty"))
	if err != nil || empty.Size() != 0 {
		t.Fatalf("empty write: %v: %v", empty, err)
	}
}

func TestLinuxFilesystemWriteRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret")
	if err := os.WriteFile(target, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "symlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "directory-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, filepath.Join(root, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
	for index, path := range []string{"symlink", "directory-link/secret", "hardlink", "fifo", "directory", "missing/file"} {
		result, err := backend.Execute(filesystemWriteMessage(t, uint64(index+1), path, []byte("changed")))
		if err == nil || result.Written != 0 {
			t.Fatalf("write accepted %s: %+v: %v", path, result, err)
		}
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "secret" {
		t.Fatalf("outside target changed: %q: %v", data, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 5 {
		t.Fatalf("failed write left files: %v: %v", entries, err)
	}
}

func TestLinuxFilesystemStagingCleanup(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
	parent, err := openLinuxBeneath(backend.roots["data"], ".", unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	temporary, err := createLinuxStagingFile(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer temporary.Close()
	if _, err := temporary.WriteString("staged"); err != nil {
		t.Fatal(err)
	}
	if err := backend.publish(parent, temporary, "directory", 1); err == nil {
		t.Fatal("replaced directory")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "directory" {
		t.Fatalf("failed publication leaked staging entry: %v: %v", entries, err)
	}
}

func TestLinuxFilesystemStagingCollision(t *testing.T) {
	root := t.TempDir()
	backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
	staging, err := StagingName(backend.namespace, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, staging), []byte("not ours"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Execute(filesystemWriteMessage(t, 1, "file", []byte("replacement"))); err == nil {
		t.Fatal("publication replaced an existing staging entry")
	}
	data, err := os.ReadFile(filepath.Join(root, staging))
	if err != nil || string(data) != "not ours" {
		t.Fatalf("collision altered existing file: %q: %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(root, "file")); !os.IsNotExist(err) {
		t.Fatal("collision published destination:", err)
	}
}

func TestLinuxFilesystemWriteNested(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
	if _, err := backend.Execute(filesystemWriteMessage(t, 1, "directory/file", []byte("nested"))); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "directory", "file"))
	if err != nil || string(data) != "nested" {
		t.Fatalf("%q: %v", data, err)
	}
}
