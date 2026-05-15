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
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPreparedWriteDoesNotPublish(t *testing.T) {
	for _, commit := range []bool{false, true} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "file"), []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
		prepared, err := prepareLinuxWrite(backend.roots["data"], "file", []byte("new"), 1)
		if prepared != nil {
			t.Cleanup(func() { _ = prepared.Close() })
		}
		if err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 1 || entries[0].Name() != "file" {
			t.Fatal("preparation exposed a name:", entries, err)
		}
		data, err := os.ReadFile(filepath.Join(root, "file"))
		if err != nil || string(data) != "original" {
			t.Fatal("preparation changed target:", string(data), err)
		}
		if prepared.identity.Size != 3 || prepared.identity.Inode == 0 || prepared.identity.ParentInode == 0 {
			t.Fatal("missing prepared identity:", prepared.identity)
		}
		expected := "original"
		if commit {
			if err := prepared.commit(backend); err != nil {
				t.Fatal(err)
			}
			expected = "new"
		}
		if err := prepared.Close(); err != nil {
			t.Fatal(err)
		}
		if err := prepared.Close(); err != nil {
			t.Fatal(err)
		}
		if err := prepared.commit(backend); !errors.Is(err, ErrClosed) {
			t.Fatal("closed prepared write reused:", err)
		}
		data, err = os.ReadFile(filepath.Join(root, "file"))
		if err != nil || string(data) != expected {
			t.Fatal("unexpected final target:", string(data), err)
		}
	}
}

func TestPreparedWriteRejectsChangedInode(t *testing.T) {
	for _, mode := range []string{"size", "mode", "link", "target"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "file"), []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
			prepared, err := prepareLinuxWrite(backend.roots["data"], "file", []byte("new"), 1)
			if prepared != nil {
				defer prepared.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
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
			if err != nil {
				t.Fatal(err)
			}
			if err := prepared.commit(backend); !errors.Is(err, ErrDenied) {
				t.Fatal("changed prepared state accepted:", err)
			}
			if !prepared.closed {
				t.Fatal("failed commit retained handles")
			}
			data, err := os.ReadFile(filepath.Join(root, "file"))
			if err != nil || string(data) != "original" {
				t.Fatal("failed commit changed target:", string(data), err)
			}
		})
	}
}

func TestPreparedWritePartialOwnership(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	backend := openTestFilesystem(t, root, Write, FilesystemLimits{})
	prepared, err := prepareLinuxWrite(backend.roots["data"], "directory", nil, 1)
	if err == nil || prepared == nil {
		t.Fatal("partial failure lost cleanup ownership:", prepared, err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.parent.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("partial owner leaked parent:", err)
	}
	for _, name := range []string{"../escape", ".pipit-stage-private", ""} {
		if prepared, err := prepareLinuxWrite(backend.roots["data"], name, nil, 1); err == nil || prepared != nil {
			t.Fatal("invalid path admitted:", name, prepared, err)
		}
	}
}
