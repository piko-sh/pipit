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
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

// cacheImagePermissionMask isolates the permission bits of a cache image's mode.
const cacheImagePermissionMask = 0o7777

// WorkerImageCache holds staged, hash-verified executables keyed by their approved
// digest.
type WorkerImageCache struct {
	// entries maps approved digests to their pinned cache images.
	entries map[[sha256.Size]byte]*cachedWorkerImage

	// root holds the pinned cache directory handle.
	root *os.File

	// mutex guards concurrent access to entries and root.
	mutex sync.Mutex
}

// Close releases every pinned cache descriptor and the cache directory handle.
//
// Returns the joined error of every failed close.
//
// Safe for concurrent use by multiple goroutines.
func (cache *WorkerImageCache) Close() error {
	if cache == nil {
		return nil
	}
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	var failures error
	for digest, entry := range cache.entries {
		if entry.pinned != nil {
			failures = errors.Join(failures, entry.pinned.Close())
		}
		delete(cache.entries, digest)
	}
	if cache.root != nil {
		failures = errors.Join(failures, cache.root.Close())
		cache.root = nil
	}
	return failures
}

// has reports whether the cache holds a staged image for the approved digest.
//
// Takes digest ([sha256.Size]byte) which is the approved hash.
//
// Returns bool which is true when a pinned entry exists.
//
// Safe for concurrent use by multiple goroutines.
func (cache *WorkerImageCache) has(digest [sha256.Size]byte) bool {
	if cache == nil {
		return false
	}
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	_, ok := cache.entries[digest]
	return ok
}

// copyInto reuses a cached image for a launch by re-verifying and range-copying the
// pinned bytes into the staging file.
//
// Takes target (*os.File) which is the launch's fresh staging file.
// Takes digest ([sha256.Size]byte) which is the approved hash.
//
// Returns bool which is true when the digest was cached and copied.
// Returns error when re-verification or the copy fails.
//
// Safe for concurrent use by multiple goroutines.
func (cache *WorkerImageCache) copyInto(target *os.File, digest [sha256.Size]byte) (bool, error) {
	if cache == nil {
		return false, nil
	}
	cache.mutex.Lock()
	entry, ok := cache.entries[digest]
	cache.mutex.Unlock()
	if !ok {
		return false, nil
	}
	current, err := captureCachedImageIdentity(entry.pinned)
	if err != nil {
		return true, err
	}
	if current != entry.identity {
		return true, fmt.Errorf("%w: cache image identity changed", ErrInvalidWorker)
	}
	if err := copyFileRangeFull(entry.pinned, target, entry.identity.size); err != nil {
		return true, err
	}
	if err := target.Chmod(workerImageMode); err != nil {
		return true, fmt.Errorf("%w: sealing worker image permissions: %w", ErrUnavailable, err)
	}
	if err := target.Sync(); err != nil {
		return true, fmt.Errorf("%w: syncing worker image: %w", ErrUnavailable, err)
	}
	return true, nil
}

// WorkerImageRequest names one approved executable to stage into a cache.
type WorkerImageRequest struct {
	// Executable holds the absolute path to the approved file.
	Executable string

	// Digest holds the host-approved SHA-256 hash of the executable.
	Digest [sha256.Size]byte
}

// cachedImageIdentity is the kernel identity of one pinned cache image, captured once
// when the image is staged and re-checked on every reuse so a swapped or mutated cache
// file is refused before its bytes reach a fresh private root.
type cachedImageIdentity struct {
	// device holds the combined major and minor device number.
	device uint64

	// inode holds the file inode number.
	inode uint64

	// size holds the file size in bytes.
	size int64

	// mtime holds the last modification time in seconds.
	mtime int64

	// ctime holds the status change time in seconds.
	ctime int64

	// nlink holds the hard link count.
	nlink uint64

	// mode holds the file type and permission bits.
	mode uint16

	// uid holds the owner user identifier.
	uid uint32
}

// checkSealed refuses an identity that is not a private, sealed regular file.
//
// Returns error when the file is writable, hard-linked or foreign.
func (identity cachedImageIdentity) checkSealed() error {
	if identity.mode&unix.S_IFMT != unix.S_IFREG || identity.mode&cacheImagePermissionMask != workerImageMode ||
		identity.nlink != 1 || int64(identity.uid) != int64(os.Geteuid()) {
		return fmt.Errorf("%w: cache image is not a sealed private file", ErrInvalidWorker)
	}
	return nil
}

// cachedWorkerImage pins one staged, verified executable by an O_PATH descriptor so a
// launch can copy it without re-hashing or re-parsing its ELF header.
type cachedWorkerImage struct {
	// pinned holds the O_PATH descriptor for the staged executable.
	pinned *os.File

	// identity holds the kernel identity captured at staging time.
	identity cachedImageIdentity
}

// StageWorkerImageCache stages each approved executable once under cacheDir and pins each
// sealed result by an O_PATH descriptor.
//
// Takes cacheDir (string) which is the absolute host-private directory.
// Takes requests ([]WorkerImageRequest) which lists the approved images.
//
// Returns *WorkerImageCache owning one pinned descriptor per distinct digest.
// Returns error after releasing any partially staged entries.
func StageWorkerImageCache(ctx context.Context, cacheDir string, requests []WorkerImageRequest) (*WorkerImageCache, error) {
	if ctx == nil || !filepath.IsAbs(cacheDir) {
		return nil, fmt.Errorf("%w: cache directory must be absolute", ErrInvalidWorker)
	}
	if err := os.MkdirAll(cacheDir, workerRootMode); err != nil {
		return nil, fmt.Errorf("%w: preparing image cache: %w", ErrUnavailable, err)
	}
	root, err := openDirectory(unix.AT_FDCWD, cacheDir)
	if err != nil {
		return nil, fmt.Errorf("%w: opening image cache: %w", ErrUnavailable, err)
	}
	cache := &WorkerImageCache{mutex: sync.Mutex{}, entries: make(map[[sha256.Size]byte]*cachedWorkerImage), root: root}
	for _, request := range requests {
		if request.Digest == ([sha256.Size]byte{}) {
			continue
		}
		if _, ok := cache.entries[request.Digest]; ok {
			continue
		}
		entry, err := stageCachedWorkerImage(ctx, cache.root, request)
		if err != nil {
			return nil, errors.Join(err, cache.Close())
		}
		cache.entries[request.Digest] = entry
	}
	return cache, nil
}

// stageCachedWorkerImage copies one approved executable into the cache, verifies it, and
// pins the sealed result.
//
// Takes root (*os.File) which is the pinned cache directory.
// Takes request (WorkerImageRequest) which identifies the executable.
//
// Returns *cachedWorkerImage which owns the pinned descriptor.
// Returns error when staging or verification fails.
func stageCachedWorkerImage(ctx context.Context, root *os.File, request WorkerImageRequest) (*cachedWorkerImage, error) {
	if !filepath.IsAbs(request.Executable) {
		return nil, fmt.Errorf("%w: approved executable must be absolute", ErrInvalidWorker)
	}
	name := hex.EncodeToString(request.Digest[:])
	if err := unix.Mkdirat(int(root.Fd()), name, workerRootMode); err != nil && !errors.Is(err, unix.EEXIST) {
		return nil, fmt.Errorf("%w: preparing cache entry: %w", ErrUnavailable, err)
	}
	directory, err := openDirectory(int(root.Fd()), name)
	if err != nil {
		return nil, fmt.Errorf("%w: opening cache entry: %w", ErrUnavailable, err)
	}
	defer directory.Close()
	if err := unix.Unlinkat(int(directory.Fd()), workerImageName, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return nil, fmt.Errorf("%w: clearing stale cache image: %w", ErrUnavailable, err)
	}
	if err := stageCacheImageBytes(ctx, directory, request); err != nil {
		return nil, err
	}
	pinned, err := openBeneathNoFollow(directory, workerImageName, unix.O_RDONLY|unix.O_NONBLOCK)
	if err != nil {
		return nil, fmt.Errorf("%w: pinning cache image: %w", ErrUnavailable, err)
	}
	identity, err := captureCachedImageIdentity(pinned)
	if err != nil {
		return nil, errors.Join(err, pinned.Close())
	}
	return &cachedWorkerImage{pinned: pinned, identity: identity}, nil
}

// stageCacheImageBytes copies and verifies the approved bytes into the cache entry file
// before sealing it.
//
// Takes directory (*os.File) which pins the cache entry directory.
// Takes request (WorkerImageRequest) which identifies the executable.
//
// Returns error when the copy, verification or seal fails.
func stageCacheImageBytes(ctx context.Context, directory *os.File, request WorkerImageRequest) (result error) {
	descriptor, err := unix.Open(request.Executable, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("%w: opening approved worker: %w", ErrInvalidWorker, err)
	}
	source := os.NewFile(uintptr(descriptor), request.Executable)
	defer func() { result = errors.Join(result, source.Close()) }()
	targetFd, err := unix.Openat(int(directory.Fd()), workerImageName,
		unix.O_CREAT|unix.O_EXCL|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, workerStagingMode)
	if err != nil {
		return fmt.Errorf("%w: creating cache image: %w", ErrUnavailable, err)
	}
	target := os.NewFile(uintptr(targetFd), workerImageName)
	defer func() { result = errors.Join(result, target.Close()) }()
	digest := sha256.New()
	reader := workerImageReader{context: ctx, source: source}
	written, err := io.Copy(io.MultiWriter(target, digest), io.LimitReader(reader, maximumWorkerImageBytes+1))
	if err != nil {
		return fmt.Errorf("copying approved worker: %w", err)
	}
	if written > maximumWorkerImageBytes || [sha256.Size]byte(digest.Sum(nil)) != request.Digest {
		return fmt.Errorf("%w: worker image does not match approval", ErrInvalidWorker)
	}
	if err := validateWorkerELF(target); err != nil {
		return err
	}
	if err := target.Chmod(workerImageMode); err != nil {
		return fmt.Errorf("%w: sealing cache image: %w", ErrUnavailable, err)
	}
	if err := target.Sync(); err != nil {
		return fmt.Errorf("%w: syncing cache image: %w", ErrUnavailable, err)
	}
	return ctx.Err()
}

// captureCachedImageIdentity records the kernel identity of a freshly sealed cache image.
//
// Takes pinned (*os.File) which is the sealed cache descriptor.
//
// Returns cachedImageIdentity and any statx error.
func captureCachedImageIdentity(pinned *os.File) (cachedImageIdentity, error) {
	const mask = unix.STATX_INO | unix.STATX_SIZE | unix.STATX_MTIME | unix.STATX_CTIME |
		unix.STATX_NLINK | unix.STATX_MODE | unix.STATX_UID
	var status unix.Statx_t
	if err := unix.Statx(int(pinned.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, mask, &status); err != nil {
		return cachedImageIdentity{}, fmt.Errorf("%w: inspecting cache image: %w", ErrUnavailable, err)
	}
	if status.Size > uint64(maximumWorkerImageBytes) {
		return cachedImageIdentity{}, fmt.Errorf("%w: cache image exceeds the image bound", ErrInvalidWorker)
	}
	identity := cachedImageIdentity{
		device: unix.Mkdev(status.Dev_major, status.Dev_minor), inode: status.Ino, size: int64(status.Size),
		mtime: status.Mtime.Sec, ctime: status.Ctime.Sec, nlink: uint64(status.Nlink),
		mode: status.Mode, uid: status.Uid,
	}
	if err := identity.checkSealed(); err != nil {
		return cachedImageIdentity{}, err
	}
	return identity, nil
}

// copyFileRangeFull copies size bytes from source into target using copy_file_range with
// a userspace fallback.
//
// Takes source (*os.File) which is the pinned cache descriptor.
// Takes target (*os.File) which is the launch staging file.
// Takes size (int64) which is the byte count to copy.
//
// Returns error when the copy fails.
func copyFileRangeFull(source, target *os.File, size int64) error {
	var offset int64
	for offset < size {
		moved, err := unix.CopyFileRange(int(source.Fd()), &offset, int(target.Fd()), nil, int(size-offset), 0)
		if errors.Is(err, unix.EXDEV) || errors.Is(err, unix.ENOSYS) || (moved == 0 && err == nil) {
			section := io.NewSectionReader(source, offset, size-offset)
			if _, copyErr := io.Copy(target, section); copyErr != nil {
				return fmt.Errorf("copying cache image: %w", copyErr)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: range-copying cache image: %w", ErrUnavailable, err)
		}
	}
	return nil
}

// openBeneathNoFollow opens name relative to directory without following symlinks.
//
// Takes directory (*os.File) which pins the parent.
// Takes name (string) which is the relative entry name.
// Takes flags (int) which are the open flags.
//
// Returns *os.File and any open error.
func openBeneathNoFollow(directory *os.File, name string, flags int) (*os.File, error) {
	descriptor, err := unix.Openat(int(directory.Fd()), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), workerImageName), nil
}
