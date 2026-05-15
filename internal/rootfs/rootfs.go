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

package rootfs

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"
)

// seededFileMode is the permission AddFile stores content under: owner read and write,
// the same mode the production write path uses, so a seeded entry is indistinguishable
// from one the store wrote itself.
const seededFileMode = 0o600

// errEscapesRoot is returned when a name resolves outside the store's directory.
var errEscapesRoot = errors.New("rootfs: name escapes the store root")

// Store is the filesystem surface the bytecode cache needs. Every name is interpreted
// relative to a fixed directory and may not escape it.
type Store interface {
	// MkdirAll creates a directory and any missing parents beneath the root.
	//
	// Takes name (string) which is the directory path relative to the root.
	// Takes perm (fs.FileMode) which is applied to directories this call creates.
	//
	// Returns error when the name escapes the root or the directory cannot be created.
	MkdirAll(name string, perm fs.FileMode) error

	// WriteFileAtomic writes data so a reader observes either the previous content or the
	// complete new content, never a partial write.
	//
	// Takes name (string) which is the file path relative to the root.
	// Takes data ([]byte) which is the content to write.
	// Takes perm (fs.FileMode) which is applied when the file is created.
	//
	// Returns error when the name escapes the root or the write fails.
	WriteFileAtomic(name string, data []byte, perm fs.FileMode) error

	// ReadFile reads a whole file beneath the root.
	//
	// Takes name (string) which is the file path relative to the root.
	//
	// Returns []byte which holds the file content.
	// Returns error when the name escapes the root or the read fails.
	ReadFile(name string) ([]byte, error)

	// Remove deletes a file beneath the root.
	//
	// Takes name (string) which is the file path relative to the root.
	//
	// Returns error when the name escapes the root or the removal fails.
	Remove(name string) error

	// Close releases the resources this store holds.
	//
	// Returns error when the cleanup fails.
	Close() error
}

// MemoryStore is an in-memory Store for tests. It holds no operating-system resources and
// lets a test inspect exactly what was written.
type MemoryStore struct {
	// files maps a cleaned name to its content.
	files map[string][]byte

	// MkdirAllErr, WriteFileAtomicErr, ReadFileErr and RemoveErr make the corresponding
	// method fail, so a test can exercise the caller's error path without arranging a real
	// filesystem failure. Set before use; they are read without the lock.
	MkdirAllErr error

	// WriteFileAtomicErr makes WriteFileAtomic fail. See MkdirAllErr.
	WriteFileAtomicErr error

	// ReadFileErr makes ReadFile fail. See MkdirAllErr.
	ReadFileErr error

	// RemoveErr makes Remove fail. See MkdirAllErr.
	RemoveErr error

	// mu guards files, so a test may drive the store from more than one goroutine.
	mu sync.Mutex
}

// NewMemoryStore returns an empty in-memory Store.
//
// Returns *MemoryStore holding no files.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{files: make(map[string][]byte), MkdirAllErr: nil, WriteFileAtomicErr: nil, ReadFileErr: nil, RemoveErr: nil, mu: sync.Mutex{}}
}

// MkdirAll succeeds without recording anything because the memory store has no
// directories.
//
// Takes name (string) which is ignored beyond its escape check.
//
// Returns error when the name escapes the root.
func (m *MemoryStore) MkdirAll(name string, _ fs.FileMode) error {
	if m.MkdirAllErr != nil {
		return m.MkdirAllErr
	}

	_, err := cleanName(name)

	return err
}

// WriteFileAtomic stores data under name, replacing any previous content.
//
// Takes name (string) which is the file path relative to the root.
// Takes data ([]byte) which is copied, so a caller may reuse its buffer.
//
// Returns error when the name escapes the root.
//
// Concurrency: safe for concurrent use; guards m.files with m.mu.
func (m *MemoryStore) WriteFileAtomic(name string, data []byte, _ fs.FileMode) error {
	if m.WriteFileAtomicErr != nil {
		return m.WriteFileAtomicErr
	}

	cleaned, err := fileName(name)
	if err != nil {
		return err
	}

	stored := make([]byte, len(data))
	copy(stored, data)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[cleaned] = stored

	return nil
}

// ReadFile returns a copy of the stored content.
//
// Takes name (string) which is the file path relative to the root.
//
// Returns []byte which holds a copy of the content.
// Returns error which wraps fs.ErrNotExist when no such file was written.
//
// Concurrency: safe for concurrent use; guards m.files with m.mu.
func (m *MemoryStore) ReadFile(name string) ([]byte, error) {
	if m.ReadFileErr != nil {
		return nil, m.ReadFileErr
	}

	cleaned, err := fileName(name)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.files[cleaned]
	if !ok {
		return nil, fmt.Errorf("rootfs: %q: %w", name, fs.ErrNotExist)
	}

	result := make([]byte, len(stored))
	copy(result, stored)

	return result, nil
}

// Remove deletes a stored file, succeeding when it was never written.
//
// Takes name (string) which is the file path relative to the root.
//
// Returns error when the name escapes the root.
//
// Concurrency: safe for concurrent use; guards m.files with m.mu.
func (m *MemoryStore) Remove(name string) error {
	if m.RemoveErr != nil {
		return m.RemoveErr
	}

	cleaned, err := fileName(name)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, cleaned)

	return nil
}

// Close is a no-op for the in-memory store.
//
// Returns nil.
func (*MemoryStore) Close() error {
	return nil
}

// AddFile seeds content directly, so a test can set up a corrupt or pre-existing entry.
//
// Takes name (string) which is the file path relative to the root.
// Takes data ([]byte) which becomes the content.
func (m *MemoryStore) AddFile(name string, data []byte) {
	_ = m.WriteFileAtomic(name, data, seededFileMode)
}

// GetFile returns the stored content without copying, or nil when absent.
//
// Takes name (string) which is the file path relative to the root.
//
// Returns []byte holding the content, or nil when no such file was written.
//
// Concurrency: safe for concurrent use; guards m.files with m.mu.
func (m *MemoryStore) GetFile(name string) []byte {
	cleaned, err := fileName(name)
	if err != nil {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	return m.files[cleaned]
}

// osStore is a Store confined to a directory by os.Root.
type osStore struct {
	// root confines every operation to the directory it was opened on. The confinement is
	// enforced by the runtime rather than by path inspection, so a symlink cannot walk out.
	root *os.Root
}

// MkdirAll creates a directory and any missing parents beneath the root.
//
// Takes name (string) which is the directory path relative to the root.
// Takes perm (fs.FileMode) which is applied to directories this call creates.
//
// Returns error when the directory cannot be created.
func (s *osStore) MkdirAll(name string, perm fs.FileMode) error {
	return s.root.MkdirAll(name, perm)
}

// WriteFileAtomic writes to a temporary file beside the target and renames it into place.
//
// Rename within a single directory is atomic on the platforms this runs on, so a
// concurrent reader sees either the old content or the new, never a truncated file. The
// temporary is removed on any failure before the rename.
//
// Takes name (string) which is the file path relative to the root.
// Takes data ([]byte) which is the content to write.
// Takes perm (fs.FileMode) which is applied when the file is created.
//
// Returns error when the write or the rename fails.
func (s *osStore) WriteFileAtomic(name string, data []byte, perm fs.FileMode) error {
	temporary, err := s.writeTemporary(name, data, perm)
	if err != nil {
		return err
	}
	if err := s.root.Rename(temporary, name); err != nil {
		_ = s.root.Remove(temporary)
		return fmt.Errorf("rootfs: renaming %q to %q: %w", temporary, name, err)
	}
	return nil
}

// ReadFile reads a whole file beneath the root.
//
// Takes name (string) which is the file path relative to the root.
//
// Returns []byte which holds the file content.
// Returns error when the read fails.
func (s *osStore) ReadFile(name string) ([]byte, error) {
	return s.root.ReadFile(name)
}

// Remove deletes a file beneath the root.
//
// Takes name (string) which is the file path relative to the root.
//
// Returns error when the removal fails.
func (s *osStore) Remove(name string) error {
	return s.root.Remove(name)
}

// Close releases the os.Root directory handle.
//
// Returns error when the close fails.
func (s *osStore) Close() error {
	return s.root.Close()
}

// writeTemporary creates a uniquely named sibling temporary exclusively and writes data
// into it, retrying once if a random name happens to collide.
//
// Takes name (string) which is the final file's path relative to the root.
// Takes data ([]byte) which is written in full.
// Takes perm (fs.FileMode) which is applied to the created file.
//
// Returns the temporary's path relative to the root, or an error with nothing left.
func (s *osStore) writeTemporary(name string, data []byte, perm fs.FileMode) (string, error) {
	for attempt := range 2 {
		temporary := path.Join(path.Dir(name), "."+path.Base(name)+".tmp-"+rand.Text())
		file, err := s.root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
		if errors.Is(err, os.ErrExist) && attempt == 0 {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("rootfs: writing %q: %w", temporary, err)
		}
		if _, err := file.Write(data); err != nil {
			return "", fmt.Errorf("rootfs: writing %q: %w", temporary, errors.Join(err, file.Close(), s.root.Remove(temporary)))
		}
		if err := file.Close(); err != nil {
			return "", fmt.Errorf("rootfs: closing %q: %w", temporary, errors.Join(err, s.root.Remove(temporary)))
		}
		return temporary, nil
	}
	return "", fmt.Errorf("rootfs: no unique temporary name for %q", name)
}

// Open returns a Store confined to dir.
//
// Confinement is enforced by os.Root, which resolves every name against a directory
// handle rather than by string inspection. A symlink pointing outside dir therefore fails
// rather than escaping.
//
// Takes dir (string) which must be an existing directory.
//
// Returns Store which is confined to dir.
// Returns error when dir cannot be opened.
func Open(dir string) (Store, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("rootfs: opening %q: %w", dir, err)
	}

	return &osStore{root: root}, nil
}

// cleanName normalises a name relative to the root.
//
// A name denoting the root itself, such as "." or "/", cleans to the empty string rather
// than being rejected: os.Root permits it, and MkdirAll(".") is how a caller asks for the
// root to exist. Callers that need an actual file reject the empty result themselves.
//
// Takes name (string) which is the path to normalise.
//
// Returns string which is the cleaned name, empty when it denotes the root.
// Returns error which wraps errEscapesRoot when the name walks above the root.
func cleanName(name string) (string, error) {
	if strings.Contains(name, "..") {
		for segment := range strings.SplitSeq(path.Clean(name), "/") {
			if segment == ".." {
				return "", fmt.Errorf("rootfs: %q: %w", name, errEscapesRoot)
			}
		}
	}

	cleaned := path.Clean("/" + name)

	return strings.TrimPrefix(cleaned, "/"), nil
}

// fileName normalises a name that must denote a file rather than the root.
//
// Takes name (string) which is the path to normalise.
//
// Returns string which is the cleaned name.
// Returns error when the name escapes the root or denotes the root itself.
func fileName(name string) (string, error) {
	cleaned, err := cleanName(name)
	if err != nil {
		return "", err
	}
	if cleaned == "" {
		return "", fmt.Errorf("rootfs: %q names the root, not a file: %w", name, errEscapesRoot)
	}

	return cleaned, nil
}
