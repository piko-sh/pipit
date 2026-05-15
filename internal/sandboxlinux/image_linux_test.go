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
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPrepareWorkerImage(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "approved")
	data := workerELFFixture()
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	image, err := PrepareWorkerImage(context.Background(), source, sha256.Sum256(data))
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	root, err := image.Root()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WorkerAttributes(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("source changed after approval"), 0600); err != nil {
		t.Fatal(err)
	}
	staged, err := os.ReadFile(filepath.Join(root, "worker"))
	if err != nil || !bytes.Equal(staged, data) {
		t.Fatalf("private image followed mutable source: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "worker"))
	if err != nil || info.Mode().Perm() != workerImageMode {
		t.Fatalf("unexpected staged permissions: %v", err)
	}
	var wait sync.WaitGroup
	for range 8 {
		wait.Go(func() {
			if err := image.Close(); err != nil {
				t.Errorf("concurrent cleanup: %v", err)
			}
		})
	}
	wait.Wait()
	if _, err := image.Root(); !errors.Is(err, errClosed) {
		t.Fatalf("closed image remained available: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private image root survived cleanup: %v", err)
	}
}

func TestPrepareWorkerImageRejectsUnapprovedInput(t *testing.T) {
	parent := t.TempDir()
	t.Setenv("TMPDIR", parent)
	source := filepath.Join(t.TempDir(), "source")
	data := workerELFFixture()
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareWorkerImage(context.Background(), source, sha256.Sum256([]byte("different"))); !errors.Is(err, ErrInvalidWorker) {
		t.Fatalf("approval mismatch accepted: %v", err)
	}
	for _, invalid := range []string{"source", filepath.Join(t.TempDir(), "missing")} {
		if _, err := PrepareWorkerImage(context.Background(), invalid, sha256.Sum256(data)); !errors.Is(err, ErrInvalidWorker) {
			t.Fatalf("invalid source accepted: %v", err)
		}
	}
	if _, err := PrepareWorkerImage(context.Background(), source, [sha256.Size]byte{}); !errors.Is(err, ErrInvalidWorker) {
		t.Fatalf("missing approval accepted: %v", err)
	}
	if _, err := PrepareWorkerImage(nil, source, sha256.Sum256(data)); !errors.Is(err, ErrInvalidWorker) {
		t.Fatalf("nil context accepted: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed staging leaked private roots: %v, %v", entries, err)
	}
}

func TestPrepareWorkerImageRejectsSpecialFiles(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	link := filepath.Join(parent, "link")
	if err := os.Symlink("/dev/null", link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(parent, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(parent, "large")
	file, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(file.Truncate(maximumWorkerImageBytes+1), file.Close()); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{parent, link, fifo, large, "/dev/null"} {
		if _, err := PrepareWorkerImage(context.Background(), source, sha256.Sum256([]byte("approved"))); !errors.Is(err, ErrInvalidWorker) {
			t.Fatalf("unsafe source %q accepted: %v", source, err)
		}
	}
}

func TestPrepareWorkerImageRejectsUnsupportedELF(t *testing.T) {
	t.Parallel()
	for _, variant := range []string{"malformed", "foreign", "interpreter", "writable-code", "missing-entry", "big-endian", "core"} {
		t.Run(variant, func(t *testing.T) {
			data := workerELFFixture()
			switch variant {
			case "malformed":
				data[0] = 0
			case "foreign":
				binary.LittleEndian.PutUint16(data[18:20], 0)
			case "interpreter":
				binary.LittleEndian.PutUint32(data[64:68], 3)
			case "writable-code":
				binary.LittleEndian.PutUint32(data[68:72], 7)
			case "missing-entry":
				binary.LittleEndian.PutUint64(data[24:32], 0)
			case "big-endian":
				data[5] = 2
			case "core":
				binary.LittleEndian.PutUint16(data[16:18], 4)
			}
			source := filepath.Join(t.TempDir(), "worker")
			if err := os.WriteFile(source, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := PrepareWorkerImage(context.Background(), source, sha256.Sum256(data)); !errors.Is(err, ErrInvalidWorker) {
				t.Fatalf("unsupported approved ELF accepted: %v", err)
			}
		})
	}
}

func TestWorkerImageCancellationAndZeroValue(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PrepareWorkerImage(ctx, "/not-opened", sha256.Sum256([]byte("approved"))); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled staging continued: %v", err)
	}
	reader := workerImageReader{context: ctx, source: nil}
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled copy continued reading: %v", err)
	}
	var image WorkerImage
	if err := image.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := image.Root(); !errors.Is(err, errClosed) {
		t.Fatalf("zero image provided a root: %v", err)
	}
}

func TestWorkerImageCleanupRefusesUnexpectedEntries(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "approved")
	data := workerELFFixture()
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	image, err := PrepareWorkerImage(context.Background(), source, sha256.Sum256(data))
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	root, err := image.Root()
	if err != nil {
		t.Fatal(err)
	}
	unexpected := filepath.Join(root, "unexpected")
	if err := os.WriteFile(unexpected, []byte("must not remove"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := image.Close(); err == nil {
		t.Fatal("cleanup recursively removed an unexpected entry")
	}
	if data, err := os.ReadFile(unexpected); err != nil || string(data) != "must not remove" {
		t.Fatalf("unexpected entry was altered: %v", err)
	}
	if err := os.Remove(unexpected); err != nil {
		t.Fatal(err)
	}
	if err := image.Close(); err != nil {
		t.Fatalf("cleanup retry failed: %v", err)
	}
}

func workerELFFixture() []byte {
	data := make([]byte, 128)
	copy(data, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(data[16:18], 2)
	machine := uint16(62)
	if runtime.GOARCH == "arm64" {
		machine = 183
	}
	binary.LittleEndian.PutUint16(data[18:20], machine)
	binary.LittleEndian.PutUint32(data[20:24], 1)
	binary.LittleEndian.PutUint64(data[24:32], 0x400078)
	binary.LittleEndian.PutUint64(data[32:40], 64)
	binary.LittleEndian.PutUint16(data[52:54], 64)
	binary.LittleEndian.PutUint16(data[54:56], 56)
	binary.LittleEndian.PutUint16(data[56:58], 1)
	binary.LittleEndian.PutUint32(data[64:68], 1)
	binary.LittleEndian.PutUint32(data[68:72], 5)
	binary.LittleEndian.PutUint64(data[80:88], 0x400000)
	binary.LittleEndian.PutUint64(data[96:104], 128)
	binary.LittleEndian.PutUint64(data[104:112], 128)
	binary.LittleEndian.PutUint64(data[112:120], 4096)
	return data
}
