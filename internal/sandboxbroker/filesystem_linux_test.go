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
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxwire"
)

func openTestFilesystem(t *testing.T, root string, rights Rights, limits FilesystemLimits) *LinuxFilesystem {
	t.Helper()
	backend, err := OpenLinuxFilesystem([]LinuxRootGrant{{Name: "data", Path: root, Rights: rights}}, limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
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
	if err != nil {
		t.Fatal(err)
	}
	return sandboxwire.Message{Kind: sandboxwire.Call, ID: id, Payload: encoded}
}

func TestLinuxFilesystemReadAndList(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := openTestFilesystem(t, root, Read|List, FilesystemLimits{})
	result, err := backend.Execute(filesystemMessage(t, 1, ReadFile, "file", 4))
	if err != nil || string(result.Data) != "abcd" || len(result.Entries) != 0 {
		t.Fatalf("%+v: %v", result, err)
	}
	for _, id := range []uint64{2, 3} {
		result, err = backend.Execute(filesystemMessage(t, id, ListDirectory, ".", 1))
		if err != nil || len(result.Entries) != 1 || result.Entries[0] != "file" {
			t.Fatalf("listing offset leaked: %+v: %v", result, err)
		}
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Execute(filesystemMessage(t, 4, ReadFile, "file", 4)); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestLinuxFilesystemRejectsNonRegularObjects(t *testing.T) {
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
	backend := openTestFilesystem(t, root, Read|List, FilesystemLimits{})
	for index, path := range []string{"symlink", "directory-link/secret", "hardlink", "fifo", "directory", "missing"} {
		result, err := backend.Execute(filesystemMessage(t, uint64(index+1), ReadFile, path, 64))
		if err == nil || len(result.Data) != 0 || len(result.Entries) != 0 {
			t.Fatalf("opened %s: %+v, %v", path, result, err)
		}
	}
	if _, err := backend.Execute(filesystemMessage(t, 7, ListDirectory, "directory-link", 1)); err == nil {
		t.Fatal("listed a symlink")
	}
}

func TestLinuxFilesystemPinsRootIdentity(t *testing.T) {
	parent := t.TempDir()
	original, moved := filepath.Join(parent, "root"), filepath.Join(parent, "moved")
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original, "file"), []byte("approved"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := openTestFilesystem(t, original, Read, FilesystemLimits{})
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original, "file"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := backend.Execute(filesystemMessage(t, 1, ReadFile, "file", 64))
	if err != nil || string(result.Data) != "approved" {
		t.Fatalf("root identity changed: %+v: %v", result, err)
	}
}

func TestLinuxFilesystemRejectsMountCrossing(t *testing.T) {
	backend := openTestFilesystem(t, "/", Read|List, FilesystemLimits{})
	for index, test := range []struct {
		operation Operation
		path      string
	}{
		{ReadFile, "proc/version"}, {ListDirectory, "proc"},
	} {
		result, err := backend.Execute(filesystemMessage(t, uint64(index+1), test.operation, test.path, 1))
		if !errors.Is(err, unix.EXDEV) || len(result.Data) != 0 || len(result.Entries) != 0 {
			t.Fatalf("mount traversal: %+v, %v", result, err)
		}
	}
}

func TestLinuxFilesystemLimitsAndRights(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := openTestFilesystem(t, root, Read, FilesystemLimits{ReadBytes: 4})
	if _, err := backend.Execute(filesystemMessage(t, 1, ListDirectory, ".", 1)); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if _, err := backend.Execute(filesystemMessage(t, 2, WriteFile, "file", 0)); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if _, err := backend.Execute(filesystemMessage(t, 3, ReadFile, "missing", 4)); err == nil {
		t.Fatal("missing read succeeded")
	}
	if _, err := backend.Execute(filesystemMessage(t, 4, ReadFile, "file", 1)); !errors.Is(err, errLimit) {
		t.Fatal("failed I/O refunded quota:", err)
	}
}

func TestLinuxFilesystemSkipsInvalidListing(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"invalid\nname", "valid", "CON"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	backend := openTestFilesystem(t, root, List, FilesystemLimits{})
	result, err := backend.Execute(filesystemMessage(t, 1, ListDirectory, ".", 3))
	if err != nil || len(result.Entries) != 1 || result.Entries[0] != "valid" || result.skipped != 2 {
		t.Fatalf("mixed names: %+v: %v", result, err)
	}

	result, err = backend.Execute(filesystemMessage(t, 2, ListDirectory, ".", 1))
	if err != nil || len(result.Entries)+result.skipped != 1 {
		t.Fatalf("bounded mixed listing: %+v: %v", result, err)
	}
}

func TestLinuxFilesystemInvalidConfiguration(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	for _, grants := range [][]LinuxRootGrant{
		{{Name: "data", Path: root, Rights: 0}},
		{{Name: "data", Path: "relative", Rights: Read}},
		{{Name: "data", Path: link, Rights: Read}},
		{{Name: "data", Path: root, Rights: Read}, {Name: "missing", Path: filepath.Join(root, "absent"), Rights: Read}},
	} {
		backend, err := OpenLinuxFilesystem(grants, FilesystemLimits{})
		if err == nil || backend != nil {
			t.Fatalf("invalid grant accepted: %+v: %v", grants, err)
		}
	}
	var backend *LinuxFilesystem
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Execute(brokerMessage(1, readPayload)); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestLinuxFilesystemCloseRace(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := openTestFilesystem(t, root, Read, FilesystemLimits{})
	message := filesystemMessage(t, 1, ReadFile, "file", 4)
	var group sync.WaitGroup
	group.Go(func() {
		result, err := backend.Execute(message)
		if err != nil && !errors.Is(err, ErrClosed) {
			t.Error(err)
		}
		if err == nil && string(result.Data) != "data" {
			t.Error("partial result")
		}
	})
	group.Go(func() {
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	group.Wait()
}

func TestLinuxFilesystemSymlinkReplacementRace(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "file")
	backend := openTestFilesystem(t, root, Read|Write, FilesystemLimits{})
	var group sync.WaitGroup
	group.Go(func() {
		for range 100 {
			candidate := filepath.Join(root, "candidate")
			if err := os.Symlink(secret, candidate); err != nil {
				t.Error(err)
				return
			}
			if err := os.Rename(candidate, target); err != nil {
				t.Error(err)
				return
			}
		}
	})
	for index := range 48 {
		write := filesystemWriteMessage(t, uint64(index*2+1), "file", []byte("inside"))
		if result, err := backend.Execute(write); err == nil && result.Written != 6 {
			t.Error("incorrect successful write count")
		}
		read := filesystemMessage(t, uint64(index*2+2), ReadFile, "file", 64)
		if result, err := backend.Execute(read); err == nil && string(result.Data) != "inside" {
			t.Errorf("read escaped the root: %q", result.Data)
		}
	}
	group.Wait()
	data, err := os.ReadFile(secret)
	if err != nil || string(data) != "outside" {
		t.Fatalf("write followed a raced symlink: %q: %v", data, err)
	}
}
