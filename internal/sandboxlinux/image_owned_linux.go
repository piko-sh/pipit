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
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxbroker"
)

const (
	// workerImageName is the fixed filename for the staged worker executable.
	workerImageName = "worker"

	// workerImagePermissionMask isolates the permission bits of a worker image's mode.
	workerImagePermissionMask = 07777
)

// temporaryImageFallback lets a launch without an image store stage its image under the
// temporary directory. Only the package tests set it; a host always supplies a store.
var temporaryImageFallback bool

// workerImageFileID holds the kernel identity of one pinned worker image file.
type workerImageFileID struct {
	// inode holds the file inode number.
	inode uint64

	// mount holds the kernel mount identifier.
	mount uint64

	// major holds the device major number.
	major uint32

	// minor holds the device minor number.
	minor uint32
}

// createTarget retains a non-writable pin before staging any executable bytes.
//
// Returns a writable staging handle only after both handles identify the same inode.
func (image *WorkerImage) createTarget() (*os.File, error) {
	descriptor, err := unix.Openat(int(image.root.Fd()), workerImageName,
		unix.O_CREAT|unix.O_EXCL|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, workerStagingMode)
	if err != nil {
		return nil, err
	}
	target := os.NewFile(uintptr(descriptor), workerImageName)
	pinned, err := unix.Openat(int(image.root.Fd()), workerImageName, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.Join(err, target.Close())
	}
	candidate := os.NewFile(uintptr(pinned), workerImageName)
	if err := matchWorkerImageFiles(target, candidate); err != nil {
		return nil, errors.Join(err, candidate.Close(), target.Close())
	}
	image.worker = candidate
	return target, nil
}

// verifyRootEntry refuses cleanup through a missing or replaced private root.
//
// Returns failure before any unlink if the current entry is not the original inode.
func (image *WorkerImage) verifyRootEntry() (result error) {
	if image.parent == nil || image.root == nil || image.directory == "" {
		return ErrUnavailable
	}
	current, err := openDirectory(int(image.parent.Fd()), filepath.Base(image.directory))
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, current.Close()) }()
	if err := matchWorkerImageFiles(image.root, current); err != nil {
		return err
	}
	var status unix.Stat_t
	if err := unix.Fstat(int(current.Fd()), &status); err != nil {
		return err
	}
	if status.Mode&unix.S_IFMT != unix.S_IFDIR || status.Mode&workerImagePermissionMask != workerRootMode || int(status.Uid) != os.Geteuid() {
		return ErrUnavailable
	}
	return nil
}

// verifyLaunchRoot checks the launch pathname as well as retained cleanup ownership.
//
// Returns failure when a moved parent would make the launch path refer elsewhere.
func (image *WorkerImage) verifyLaunchRoot() (result error) {
	if err := image.verifyRootEntry(); err != nil {
		return err
	}
	current, err := openDirectory(unix.AT_FDCWD, image.directory)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, current.Close()) }()
	if err := matchWorkerImageFiles(image.root, current); err != nil {
		return err
	}
	return image.verifyWorkerEntry(false)
}

// verifyCleanupEntries permits only the pinned executable or an already empty root.
//
// Returns failure without deleting any entry when an unexpected name is present.
func (image *WorkerImage) verifyCleanupEntries() (result error) {
	cursor, err := openDirectory(int(image.root.Fd()), ".")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, cursor.Close()) }()
	names, err := cursor.Readdirnames(2)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(names) > 1 || len(names) == 1 && names[0] != workerImageName {
		return ErrUnavailable
	}
	return image.verifyWorkerEntry(true)
}

// verifyWorkerEntry matches a no-follow handle before launch or deletion.
//
// Takes allowMissing (bool) which permits a previously unlinked private root.
//
// Returns error when the entry is absent, replaced or has unexpected permissions.
func (image *WorkerImage) verifyWorkerEntry(allowMissing bool) (result error) {
	descriptor, err := unix.Openat(int(image.root.Fd()), workerImageName, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if allowMissing && errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	current := os.NewFile(uintptr(descriptor), workerImageName)
	defer func() { result = errors.Join(result, current.Close()) }()
	if err := matchWorkerImageFiles(image.worker, current); err != nil {
		return err
	}
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil {
		return err
	}
	mode := status.Mode & workerImagePermissionMask
	if status.Mode&unix.S_IFMT != unix.S_IFREG || status.Nlink != 1 || int(status.Uid) != os.Geteuid() ||
		mode != workerImageMode && (image.prepared || mode != workerStagingMode) {
		return ErrUnavailable
	}
	return nil
}

// closeHandles releases pins only after successful resource removal.
//
// Returns every descriptor closure failure without retrying already closed handles.
func (image *WorkerImage) closeHandles() error {
	var result error
	for _, file := range []*os.File{image.worker, image.root} {
		if file != nil {
			result = errors.Join(result, file.Close())
		}
	}
	return errors.Join(result, releaseImageParent(image.store, image.parent))
}

// newWorkerImage creates and pins a private root relative to one retained parent.
//
// Returns an owner without opening a worker-selected path or adopting an existing root.
func newWorkerImage() (*WorkerImage, error) {
	return newWorkerImageInStore(nil)
}

// newWorkerImageInStore keeps explicit storage leased through image cleanup.
//
// Takes store (*sandboxbroker.LinuxImageStore) which is the optional private store.
//
// Returns no fallback when an explicitly selected image store refuses admission.
func newWorkerImageInStore(store *sandboxbroker.LinuxImageStore) (*WorkerImage, error) {
	parent, path, err := acquireImageParent(store)
	if err != nil {
		return nil, err
	}
	name := "pipit-worker-" + rand.Text()
	if err := unix.Mkdirat(int(parent.Fd()), name, workerRootMode); err != nil {
		return nil, errors.Join(err, releaseImageParent(store, parent))
	}
	root, err := openDirectory(int(parent.Fd()), name)
	if err != nil {
		return nil, errors.Join(err, releaseImageParent(store, parent))
	}
	return &WorkerImage{
		store: store, parent: parent, root: root, worker: nil, directory: filepath.Join(path, name),
		mutex: sync.Mutex{}, closed: false, prepared: false,
	}, nil
}

// acquireImageParent selects explicit private storage or, for the package tests only, the
// host's temporary directory.
//
// Takes store (*sandboxbroker.LinuxImageStore) which is the optional private store.
//
// Returns a descriptor whose ownership must be returned to the selected store.
func acquireImageParent(store *sandboxbroker.LinuxImageStore) (*os.File, string, error) {
	if store != nil {
		return store.AcquireDirectory()
	}
	if !temporaryImageFallback {
		return nil, "", ErrInvalidLimits
	}
	path, err := filepath.Abs(os.TempDir())
	if err != nil {
		return nil, "", err
	}
	parent, err := openDirectory(unix.AT_FDCWD, path)
	if err != nil {
		return nil, "", err
	}
	return parent, path, nil
}

// matchWorkerImageFiles compares pinned inode, device and mount identity.
//
// Takes original (*os.File) which is the retained pin.
// Takes current (*os.File) which is the freshly opened handle.
//
// Returns failure without interpreting a path string as ownership.
func matchWorkerImageFiles(original, current *os.File) error {
	expected, err := workerImageIdentity(original)
	if err != nil {
		return err
	}
	observed, err := workerImageIdentity(current)
	if err != nil {
		return err
	}
	if observed != expected {
		return ErrUnavailable
	}
	return nil
}

// workerImageIdentity obtains mandatory kernel identity from one retained descriptor.
//
// Takes file (*os.File) which is the retained descriptor.
//
// Returns no identity when statx cannot supply inode and mount information.
func workerImageIdentity(file *os.File) (workerImageFileID, error) {
	var empty workerImageFileID
	if file == nil {
		return empty, ErrUnavailable
	}
	var status unix.Statx_t
	const required = unix.STATX_INO | unix.STATX_MNT_ID | unix.STATX_TYPE
	if err := unix.Statx(int(file.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, required, &status); err != nil {
		return empty, err
	}
	if status.Mask&required != required || status.Ino == 0 || status.Mnt_id == 0 {
		return empty, ErrUnavailable
	}
	return workerImageFileID{inode: status.Ino, mount: status.Mnt_id, major: status.Dev_major, minor: status.Dev_minor}, nil
}

// releaseImageParent returns private storage admission or closes a non-store parent pin.
//
// Takes store (*sandboxbroker.LinuxImageStore) which is the optional private store.
// Takes parent (*os.File) which is the pinned parent directory.
//
// Returns any closure error without deleting resources through the parent.
func releaseImageParent(store *sandboxbroker.LinuxImageStore, parent *os.File) error {
	if store != nil {
		return store.ReleaseDirectory(parent)
	}
	if parent == nil {
		return nil
	}
	return parent.Close()
}
