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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeCacheSource(t *testing.T, data []byte) string {
	t.Helper()
	source := filepath.Join(t.TempDir(), "approved")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	return source
}

func TestStageWorkerImageCacheReuse(t *testing.T) {
	t.Parallel()
	data := workerELFFixture()
	digest := sha256.Sum256(data)
	cache, err := StageWorkerImageCache(context.Background(), t.TempDir(),
		[]WorkerImageRequest{{Executable: writeCacheSource(t, data), Digest: digest}})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	if !cache.has(digest) {
		t.Fatal("staged digest is not cached")
	}
	target := filepath.Join(t.TempDir(), "out")
	handle, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	hit, err := cache.copyInto(handle, digest)
	if err := errors.Join(err, handle.Close()); err != nil || !hit {
		t.Fatalf("copyInto hit=%v err=%v", hit, err)
	}
	produced, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(produced, data) {
		t.Fatalf("cached copy differs: %d bytes, err %v", len(produced), err)
	}

	repeat := filepath.Join(t.TempDir(), "again")
	handle, err = os.OpenFile(repeat, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	hit, err = cache.copyInto(handle, digest)
	if err := errors.Join(err, handle.Close()); err != nil || !hit {
		t.Fatalf("second copyInto hit=%v err=%v", hit, err)
	}
	if again, err := os.ReadFile(repeat); err != nil || !bytes.Equal(again, data) {
		t.Fatalf("repeat copy differs: %d bytes, err %v", len(again), err)
	}
}

func TestStageWorkerImageCacheAbsentDigestMisses(t *testing.T) {
	t.Parallel()
	cache, err := StageWorkerImageCache(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	handle, err := os.OpenFile(filepath.Join(t.TempDir(), "out"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if hit, err := cache.copyInto(handle, sha256.Sum256([]byte("unstaged"))); hit || err != nil {
		t.Fatalf("absent digest hit=%v err=%v", hit, err)
	}
}

func TestStageWorkerImageCacheRejectsMismatch(t *testing.T) {
	t.Parallel()
	data := workerELFFixture()
	if _, err := StageWorkerImageCache(context.Background(), t.TempDir(),
		[]WorkerImageRequest{{Executable: writeCacheSource(t, data), Digest: sha256.Sum256([]byte("wrong"))}}); !errors.Is(err, ErrInvalidWorker) {
		t.Fatalf("cache accepted an image that failed its approved digest: %v", err)
	}
}

func TestStageWorkerImageCacheRejectsTamperedEntry(t *testing.T) {
	t.Parallel()
	data := workerELFFixture()
	digest := sha256.Sum256(data)
	cacheDir := t.TempDir()
	cache, err := StageWorkerImageCache(context.Background(), cacheDir,
		[]WorkerImageRequest{{Executable: writeCacheSource(t, data), Digest: digest}})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	name := hex.EncodeToString(digest[:])
	if err := os.Chmod(filepath.Join(cacheDir, name, workerImageName), 0600); err != nil {
		t.Fatal(err)
	}
	handle, err := os.OpenFile(filepath.Join(t.TempDir(), "out"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if _, err := cache.copyInto(handle, digest); !errors.Is(err, ErrInvalidWorker) {
		t.Fatalf("reuse accepted a tampered cache image: %v", err)
	}
}
