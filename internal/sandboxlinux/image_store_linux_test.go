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

	"pipit.sh/pipit/internal/sandboxbroker"
)

func TestWorkerImageExplicitStore(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := sandboxbroker.OpenLinuxImageStore(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := filepath.Join(t.TempDir(), "source")
	data := workerELFFixture()
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	image, err := PrepareWorkerImageInStore(context.Background(), source, sha256.Sum256(data), store)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	root, err := image.Root()
	if err != nil || filepath.Dir(root) != directory {
		t.Fatal("explicit image escaped private storage:", err)
	}
	if err := store.Close(); err == nil {
		t.Fatal("image did not retain store ownership")
	}
	if err := image.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if image, err := PrepareWorkerImageInStore(context.Background(), source, sha256.Sum256(data), store); image != nil || !errors.Is(err, sandboxbroker.ErrClosed) {
		t.Fatal("closed store fell back to temporary storage:", err)
	}
	if image, err := PrepareWorkerImageInStore(context.Background(), source, sha256.Sum256(data), nil); image != nil || !errors.Is(err, ErrInvalidWorker) {
		t.Fatal("missing explicit store accepted:", err)
	}
}

func TestWorkerImageStoreRetainsFailedCleanup(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := sandboxbroker.OpenLinuxImageStore(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	image, err := newWorkerImageInStore(store)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	blocker := filepath.Join(image.directory, "unexpected")
	if err := os.WriteFile(blocker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := image.Close(); err == nil {
		t.Fatal("unexpected entry accepted")
	}
	if err := store.Close(); err == nil {
		t.Fatal("failed image cleanup dropped storage ownership")
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := image.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}
