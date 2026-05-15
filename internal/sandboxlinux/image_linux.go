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
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxbroker"
)

const (
	// maximumWorkerImageBytes is the largest allowed worker executable size.
	maximumWorkerImageBytes = 128 << 20

	// workerImageMode is the sealed permission mode for a staged worker.
	workerImageMode = 0500

	// workerStagingMode is the temporary permission mode during staging.
	workerStagingMode = 0600
)

var (
	// ErrInvalidWorker reports an unapproved or unsupported worker executable.
	ErrInvalidWorker = errors.New("invalid isolated worker image")
)

// WorkerImage owns one private copy of an explicitly host-approved executable.
type WorkerImage struct {
	// store holds the optional private image store.
	store *sandboxbroker.LinuxImageStore

	// parent holds the pinned parent directory handle.
	parent *os.File

	// root holds the pinned private root directory handle.
	root *os.File

	// worker holds the pinned staged executable handle.
	worker *os.File

	// directory holds the absolute path to the private root.
	directory string

	// mutex guards mutable state during root access and close.
	mutex sync.Mutex

	// closed is true once cleanup has completed successfully.
	closed bool

	// prepared is true once staging and verification have succeeded.
	prepared bool
}

// Root returns the host-private directory for namespace launch configuration.
//
// Returns string which is the private root path.
// Returns error when the image has been closed or verification fails.
//
// Safe for concurrent use by multiple goroutines.
func (image *WorkerImage) Root() (string, error) {
	if image == nil {
		return "", errClosed
	}
	image.mutex.Lock()
	defer image.mutex.Unlock()
	if image.closed || !image.prepared || image.directory == "" {
		return "", errClosed
	}
	if err := image.verifyLaunchRoot(); err != nil {
		return "", err
	}
	return image.directory, nil
}

// Close removes the staged executable and its root after worker termination.
//
// Returns error if cleanup fails. Successful repeated calls return nil.
//
// Safe for concurrent use by multiple goroutines.
func (image *WorkerImage) Close() error {
	if image == nil {
		return nil
	}
	image.mutex.Lock()
	defer image.mutex.Unlock()
	if image.closed || image.directory == "" {
		return nil
	}
	if err := image.verifyRootEntry(); err != nil {
		return err
	}
	if err := image.verifyCleanupEntries(); err != nil {
		return err
	}
	if err := unix.Unlinkat(int(image.root.Fd()), workerImageName, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return err
	}
	if err := image.verifyRootEntry(); err != nil {
		return err
	}
	if err := unix.Unlinkat(int(image.parent.Fd()), filepath.Base(image.directory), unix.AT_REMOVEDIR); err != nil {
		return err
	}
	image.closed = true
	return image.closeHandles()
}

// workerImageReader checks cancellation between bounded reads of the host file.
type workerImageReader struct {
	// context holds the cancellation context for staging reads.
	context context.Context

	// source holds the open source executable file.
	source *os.File
}

// Read checks the preparation context before each source read.
//
// Takes buffer ([]byte) which receives the next source bytes.
//
// Returns int which counts the bytes read.
// Returns error when cancelled or when the source read fails.
func (reader workerImageReader) Read(buffer []byte) (int, error) {
	if err := reader.context.Err(); err != nil {
		return 0, err
	}
	return reader.source.Read(buffer)
}

// PrepareWorkerImage copies and verifies a worker before inspecting its ELF data.
//
// Takes executable (string) which is an absolute, host-selected regular file.
// Takes expected ([sha256.Size]byte) which is the host-approved executable hash.
//
// Returns *WorkerImage which owns a fresh private root.
// Returns error when staging or verification fails.
func PrepareWorkerImage(ctx context.Context, executable string, expected [sha256.Size]byte) (*WorkerImage, error) {
	return prepareWorkerImage(ctx, executable, expected, nil)
}

// PrepareWorkerImageInStore stages only beneath an exclusively held private image store.
// The caller must retain the store until every returned image has been cleaned up.
//
// Takes executable (string) which is the absolute, host-selected regular file.
// Takes expected ([sha256.Size]byte) which is the host-approved executable hash.
// Takes store (*sandboxbroker.LinuxImageStore) which is the explicit private storage.
//
// Returns retained cleanup ownership on partial failure, never temporary-directory
// fallback.
func PrepareWorkerImageInStore(ctx context.Context, executable string, expected [sha256.Size]byte, store *sandboxbroker.LinuxImageStore) (*WorkerImage, error) {
	if store == nil {
		return nil, ErrInvalidWorker
	}
	return prepareWorkerImage(ctx, executable, expected, store)
}

// prepareWorkerImage verifies one owned copy under the selected host-private storage.
//
// Takes executable (string) which is the host-selected file.
// Takes expected ([sha256.Size]byte) which is the approved hash.
// Takes store (*sandboxbroker.LinuxImageStore) which is the
//
//	optional private store.
//
// Returns *WorkerImage and any staging error.
func prepareWorkerImage(ctx context.Context, executable string, expected [sha256.Size]byte, store *sandboxbroker.LinuxImageStore) (*WorkerImage, error) {
	if ctx == nil || !filepath.IsAbs(executable) || expected == [sha256.Size]byte{} {
		return nil, fmt.Errorf("%w: missing context, absolute path or approved digest", ErrInvalidWorker)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	descriptor, err := unix.Open(executable, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: opening approved worker: %w", ErrInvalidWorker, err)
	}
	source := os.NewFile(uintptr(descriptor), executable)
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maximumWorkerImageBytes {
		return nil, fmt.Errorf("%w: worker must be a bounded regular file", ErrInvalidWorker)
	}
	image, err := newWorkerImageInStore(store)
	if err != nil {
		return nil, err
	}
	if err := stageWorkerImage(ctx, source, image, expected); err != nil {
		if cleanup := image.Close(); cleanup != nil {
			return image, errors.Join(err, cleanup)
		}
		return nil, err
	}
	image.prepared = true
	return image, nil
}

// prepareWorkerImageCached stages an approved worker, reusing a pre-staged cache image
// when the cache holds the approved digest and otherwise falling back to a full staging.
//
// Takes executable (string) which is the absolute, host-selected regular file.
// Takes expected ([sha256.Size]byte) which is the host-approved executable hash.
// Takes store (*sandboxbroker.LinuxImageStore) which is the private image store.
// Takes cache (*WorkerImageCache) which is the optional pre-staged cache; a nil cache
// always stages fully.
//
// Returns the retained image on success, or an error mirroring prepareWorkerImage.
func prepareWorkerImageCached(ctx context.Context, executable string, expected [sha256.Size]byte,
	store *sandboxbroker.LinuxImageStore, cache *WorkerImageCache,
) (*WorkerImage, error) {
	if cache.has(expected) {
		return prepareWorkerImageFromCache(ctx, expected, store, cache)
	}
	return prepareWorkerImage(ctx, executable, expected, store)
}

// prepareWorkerImageFromCache stages a launch image by copying a pre-verified cache entry
// into a fresh private root.
//
// Takes expected ([sha256.Size]byte) which is the approved hash.
// Takes store (*sandboxbroker.LinuxImageStore) which is the private
//
//	store.
//
// Takes cache (*WorkerImageCache) which holds pre-staged entries.
//
// Returns *WorkerImage and any staging error.
func prepareWorkerImageFromCache(ctx context.Context, expected [sha256.Size]byte,
	store *sandboxbroker.LinuxImageStore, cache *WorkerImageCache,
) (*WorkerImage, error) {
	if ctx == nil || expected == ([sha256.Size]byte{}) {
		return nil, fmt.Errorf("%w: missing context or approved digest", ErrInvalidWorker)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	image, err := newWorkerImageInStore(store)
	if err != nil {
		return nil, err
	}
	if err := stageWorkerImageFromCache(image, expected, cache); err != nil {
		if cleanup := image.Close(); cleanup != nil {
			return image, errors.Join(err, cleanup)
		}
		return nil, err
	}
	image.prepared = true
	return image, nil
}

// stageWorkerImageFromCache creates the launch's staging file and fills it from the
// cache.
//
// Takes image (*WorkerImage) which owns the staging root.
// Takes expected ([sha256.Size]byte) which is the approved hash.
// Takes cache (*WorkerImageCache) which holds the pre-staged entry.
//
// Returns error when staging or the cache copy fails.
func stageWorkerImageFromCache(image *WorkerImage, expected [sha256.Size]byte, cache *WorkerImageCache) (result error) {
	target, err := image.createTarget()
	if err != nil {
		return fmt.Errorf("%w: staging worker: %w", ErrUnavailable, err)
	}
	defer func() { result = errors.Join(result, target.Close()) }()
	hit, err := cache.copyInto(target, expected)
	if err != nil {
		return err
	}
	if !hit {
		return fmt.Errorf("%w: approved cache image is unavailable", ErrInvalidWorker)
	}
	return nil
}

// stageWorkerImage verifies the bytes it writes before making them executable.
//
// Takes source (*os.File) which is a pinned regular file.
// Takes image (*WorkerImage) which owns the pinned private root.
// Takes expected ([sha256.Size]byte) which authorises the exact image bytes.
//
// Returns error if copying, approval, ELF checks or finalisation fails.
func stageWorkerImage(ctx context.Context, source *os.File, image *WorkerImage, expected [sha256.Size]byte) (result error) {
	target, err := image.createTarget()
	if err != nil {
		return fmt.Errorf("%w: staging worker: %w", ErrUnavailable, err)
	}
	defer func() { result = errors.Join(result, target.Close()) }()
	digest := sha256.New()
	reader := workerImageReader{context: ctx, source: source}
	written, err := io.Copy(io.MultiWriter(target, digest), io.LimitReader(reader, maximumWorkerImageBytes+1))
	if err != nil {
		return fmt.Errorf("copying approved worker: %w", err)
	}
	if written > maximumWorkerImageBytes || [sha256.Size]byte(digest.Sum(nil)) != expected {
		return fmt.Errorf("%w: worker image does not match approval", ErrInvalidWorker)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateWorkerELF(target); err != nil {
		return err
	}
	if err := target.Chmod(workerImageMode); err != nil {
		return fmt.Errorf("%w: sealing worker image permissions: %w", ErrUnavailable, err)
	}
	if err := target.Sync(); err != nil {
		return fmt.Errorf("%w: syncing worker image: %w", ErrUnavailable, err)
	}
	return ctx.Err()
}

// validateWorkerELF screens an already-approved executable for this worker ABI.
// Structural screening is not proof of trustworthy bootstrap code or pure Go; runtime
// capability setup must still reject unsupported cgo-linked workers.
//
// Takes source (*os.File) which contains the already-hash-verified image.
//
// Returns error for malformed, foreign or dynamically loaded executables.
func validateWorkerELF(source *os.File) error {
	executable, err := elf.NewFile(source)
	if err != nil {
		return fmt.Errorf("%w: reading ELF: %w", ErrInvalidWorker, err)
	}
	expected := elf.EM_X86_64
	if runtime.GOARCH == "arm64" {
		expected = elf.EM_AARCH64
	}
	if executable.Class != elf.ELFCLASS64 || executable.Data != elf.ELFDATA2LSB ||
		executable.Machine != expected || (executable.Type != elf.ET_EXEC && executable.Type != elf.ET_DYN) {
		return fmt.Errorf("%w: unsupported worker ABI", ErrInvalidWorker)
	}
	entryFound := false
	for _, segment := range executable.Progs {
		if segment.Type == elf.PT_INTERP || segment.Flags&(elf.PF_W|elf.PF_X) == elf.PF_W|elf.PF_X {
			return fmt.Errorf("%w: dynamic loader or writable executable segment", ErrInvalidWorker)
		}
		if segment.Type == elf.PT_LOAD && segment.Flags&elf.PF_X != 0 &&
			executable.Entry >= segment.Vaddr && executable.Entry-segment.Vaddr < segment.Filesz {
			entryFound = true
		}
	}
	if !entryFound {
		return fmt.Errorf("%w: worker entrypoint is not executable", ErrInvalidWorker)
	}
	return nil
}
