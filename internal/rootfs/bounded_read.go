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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
)

var (
	// ErrReadLimit reports a file exceeding a caller's finite byte allowance.
	ErrReadLimit = errors.New("rootfs: file exceeds read limit")

	// ErrBoundedReadUnavailable reports a store without bounded read support.
	ErrBoundedReadUnavailable = errors.New("rootfs: bounded reads unavailable")
)

// boundedReader reads at most a caller-selected amount of file content. Custom
// implementations remain trusted host code, not cancellable sandbox brokers.
type boundedReader interface {
	// ReadFileBounded reads at most maximum bytes from the named file.
	//
	// Takes name (string) which is the relative filename.
	// Takes maximum (int) which is the finite byte allowance.
	//
	// Returns []byte containing the file content and any read error.
	ReadFileBounded(name string, maximum int) ([]byte, error)
}

// ReadFileBounded opens a regular file and limits reading even if it grows. File opening
// and reading use trusted storage synchronously, not hard deadlines.
//
// Takes name (string) which is the relative filename.
// Takes maximum (int) which is the finite byte allowance.
//
// Returns detached file content or no bytes on size, type, read or close failure.
func (store *osStore) ReadFileBounded(name string, maximum int) (data []byte, result error) {
	if !validReadLimit(maximum) {
		return nil, fs.ErrInvalid
	}
	file, err := store.root.OpenFile(name, boundedReadOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		result = errors.Join(result, file.Close())
		if result != nil {
			data = nil
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 {
		return nil, fs.ErrInvalid
	}
	if info.Size() > int64(maximum) {
		return nil, ErrReadLimit
	}
	data, err = io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maximum {
		return nil, ErrReadLimit
	}
	return data, nil
}

// ReadFileBounded checks stored length before copying memory-backed content.
//
// Takes name (string) which is the relative filename.
// Takes maximum (int) which is the finite byte allowance.
//
// Returns an independent copy or no content on failure.
//
// Safe for concurrent use; the store mutex serialises reads.
func (store *MemoryStore) ReadFileBounded(name string, maximum int) ([]byte, error) {
	if !validReadLimit(maximum) {
		return nil, fs.ErrInvalid
	}
	if store.ReadFileErr != nil {
		return nil, store.ReadFileErr
	}
	cleaned, err := fileName(name)
	if err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	stored, exists := store.files[cleaned]
	if !exists {
		return nil, fmt.Errorf("rootfs: %q: %w", name, fs.ErrNotExist)
	}
	if len(stored) > maximum {
		return nil, ErrReadLimit
	}
	return slices.Clone(stored), nil
}

// ReadFileBounded requires bounded storage access without a whole-file fallback.
//
// Takes store (Store) which is the host-owned storage backend.
// Takes name (string) which is the relative filename.
// Takes maximum (int) which is the finite non-negative byte allowance.
//
// Returns no partial content on failure, including from a faulty custom reader.
func ReadFileBounded(store Store, name string, maximum int) ([]byte, error) {
	if !validReadLimit(maximum) {
		return nil, fs.ErrInvalid
	}
	reader, ok := store.(boundedReader)
	if !ok {
		return nil, ErrBoundedReadUnavailable
	}
	data, err := reader.ReadFileBounded(name, maximum)
	if err != nil {
		return nil, err
	}
	if len(data) > maximum {
		return nil, ErrReadLimit
	}
	return data, nil
}

// validReadLimit leaves room for the extra byte used to detect file growth.
//
// Takes maximum (int) which is the host-selected byte ceiling.
//
// Returns false for negative values or an overflowing sentinel allowance.
func validReadLimit(maximum int) bool {
	return maximum >= 0 && maximum < int(^uint(0)>>1)
}
