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

package sandboxlinux

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func ownedImageFixture(t *testing.T) (*WorkerImage, string) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "source")
	data := workerELFFixture()
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	image, err := PrepareWorkerImage(context.Background(), source, sha256.Sum256(data))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := image.Close(); err != nil {
			t.Error(err)
		}
	})
	root, err := image.Root()
	if err != nil {
		t.Fatal(err)
	}
	return image, root
}

func TestWorkerImageRejectsRootReplacement(t *testing.T) {
	image, root := ownedImageFixture(t)
	original := root + ".original"
	if err := os.Rename(root, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(root, "worker")
	if err := os.WriteFile(replacement, []byte("replacement"), 0500); err != nil {
		t.Fatal(err)
	}
	if err := image.Close(); err == nil || image.closed {
		t.Fatal("replacement root accepted for cleanup:", err)
	}
	if _, err := image.Root(); err == nil {
		t.Fatal("replacement root accepted for launch")
	}
	data, err := os.ReadFile(replacement)
	if err != nil || string(data) != "replacement" {
		t.Fatal("replacement executable changed:", err)
	}
	if err := os.Remove(replacement); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original, root); err != nil {
		t.Fatal(err)
	}
	if err := image.Close(); err != nil {
		t.Fatal("restored root could not be cleaned:", err)
	}
}

func TestWorkerImageCleanupUsesPinnedParent(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "temporary")
	moved := filepath.Join(base, "moved")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", parent)
	image, root := ownedImageFixture(t)
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(root, "worker")
	if err := os.WriteFile(replacement, []byte("replacement"), 0500); err != nil {
		t.Fatal(err)
	}
	if _, err := image.Root(); err == nil {
		t.Fatal("moved parent accepted for launch")
	}
	if err := image.Close(); err != nil {
		t.Fatal("pinned parent cleanup failed:", err)
	}
	data, err := os.ReadFile(replacement)
	if err != nil || string(data) != "replacement" {
		t.Fatal("cleanup followed the replaced parent:", err)
	}
	if _, err := os.Stat(filepath.Join(moved, filepath.Base(root))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("original pinned root remains:", err)
	}
}

func TestWorkerImageRejectsExecutableReplacement(t *testing.T) {
	for _, mode := range []string{"regular", "symlink", "directory", "fifo", "hardlink", "permissions", "root-permissions"} {
		t.Run(mode, func(t *testing.T) {
			image, root := ownedImageFixture(t)
			executable := filepath.Join(root, "worker")
			held := filepath.Join(t.TempDir(), "held")
			restore := replaceImageFixture(t, root, executable, held, mode)
			if err := image.Close(); err == nil || image.closed {
				t.Fatal("changed executable accepted for cleanup:", err)
			}
			if _, err := image.Root(); err == nil {
				t.Fatal("changed executable accepted for launch")
			}
			if _, err := os.Lstat(executable); err != nil {
				t.Fatal("cleanup removed unowned entry:", err)
			}
			restore()
			if err := image.Close(); err != nil {
				t.Fatal("restored executable could not be cleaned:", err)
			}
		})
	}
}

func replaceImageFixture(t *testing.T, root, executable, held, mode string) func() {
	t.Helper()
	switch mode {
	case "hardlink":
		if err := os.Link(executable, held); err != nil {
			t.Fatal(err)
		}
		return func() {
			if err := os.Remove(held); err != nil {
				t.Fatal(err)
			}
		}
	case "permissions", "root-permissions":
		target, original := executable, os.FileMode(0500)
		if mode == "root-permissions" {
			target, original = root, 0700
		}
		if err := os.Chmod(target, 0777); err != nil {
			t.Fatal(err)
		}
		return func() {
			if err := os.Chmod(target, original); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.Rename(executable, held); err != nil {
		t.Fatal(err)
	}
	var err error
	switch mode {
	case "regular":
		err = os.WriteFile(executable, []byte("replacement"), 0500)
	case "symlink":
		err = os.Symlink(held, executable)
	case "directory":
		err = os.Mkdir(executable, 0700)
	case "fifo":
		err = unix.Mkfifo(executable, 0600)
	}
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Remove(executable); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(held, executable); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkerImageMissingPinsCannotAdoptDirectory(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "worker")
	if err := os.WriteFile(executable, []byte("unowned"), 0500); err != nil {
		t.Fatal(err)
	}
	image := &WorkerImage{directory: root}
	if err := image.Close(); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing pins accepted for deletion:", err)
	}
	data, err := os.ReadFile(executable)
	if err != nil || string(data) != "unowned" {
		t.Fatal("missing pins removed unowned data:", err)
	}
}
