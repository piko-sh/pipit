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

package modloader

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// approvalGuardMode is the file permission bits for approval guard sidecar files.
const approvalGuardMode = 0o600

// errApprovalChanged is returned when the approval file changed between load and save.
var errApprovalChanged = errors.New("modloader: approval file changed; reload and reapprove before saving")

// approvalGuard holds an exclusive lock on a persistent sidecar file that serialises
// approval writes.
type approvalGuard struct {
	// file holds the locked guard descriptor.
	file *os.File
}

// Close explicitly unlocks before closing, even if a concurrent fork inherited the file.
//
// Returns joined release and close errors.
func (guard *approvalGuard) Close() error {
	return errors.Join(unlockApprovalGuard(guard.file), guard.file.Close())
}

// checkApprovalRevision refuses to overwrite a revision not observed by this store.
//
// Takes root (*os.Root) which pins the approval directory.
// Takes name (string) which is the approval basename.
//
// Returns error for a changed, removed, unreadable or newly created approval file.
func (s *Store) checkApprovalRevision(root *os.Root, name string) error {
	data, err := readApprovalRoot(root, name)
	if errors.Is(err, fs.ErrNotExist) {
		if s.approvalExists {
			return errApprovalChanged
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("modloader: checking approval revision: %w", err)
	}
	if !s.approvalExists || sha256.Sum256(data) != s.approvalRevision {
		return errApprovalChanged
	}
	return nil
}

// acquireApprovalGuard locks a persistent sidecar without waiting for another writer. The
// host must protect the directory and must never unlink a live guard.
//
// Takes path (string) which is the host-selected approval file path.
//
// Returns *approvalGuard which is the lock owner whose close releases the
// operating-system lock.
// Returns error when the guard cannot be opened or locked.
func acquireApprovalGuard(path string) (*approvalGuard, error) {
	file, err := openApprovalGuard(path)
	return adoptApprovalGuard(file, err)
}

// acquireApprovalGuardRoot locks within the save transaction's pinned directory.
//
// Takes root (*os.Root) which pins the approval directory.
// Takes name (string) which is the approval basename.
//
// Returns *approvalGuard which is the lock owner.
// Returns error when locking fails.
func acquireApprovalGuardRoot(root *os.Root, name string) (*approvalGuard, error) {
	file, err := openApprovalFile(root, name+".guard", os.O_CREATE|os.O_RDWR, approvalGuardMode)
	return adoptApprovalGuard(file, err)
}

// adoptApprovalGuard validates and locks an opened guard file.
//
// Takes file (*os.File) which is the opened guard descriptor. Closed on validation
// failure.
// Takes err (error) which carries any prior open failure.
//
// Returns *approvalGuard which owns the lock.
// Returns error which joins acquisition failures.
func adoptApprovalGuard(file *os.File, err error) (*approvalGuard, error) {
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil && (!info.Mode().IsRegular() || info.Size() != 0) {
		err = fs.ErrInvalid
	}
	if err == nil {
		err = lockApprovalGuard(file)
	}
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return &approvalGuard{file: file}, nil
}

// openApprovalGuard anchors creation to the host-selected containing directory.
//
// Takes path (string) which is the approval pathname.
//
// Returns *os.File which is an independent guard descriptor. The directory handle is
// closed before return.
// Returns error which reports open or directory failure.
func openApprovalGuard(path string) (*os.File, error) {
	root, err := openApprovalDirectory(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	file, openErr := openApprovalFile(root, filepath.Base(path)+".guard", os.O_CREATE|os.O_RDWR, approvalGuardMode)
	if err := errors.Join(openErr, root.Close()); err != nil {
		if file != nil {
			err = errors.Join(err, file.Close())
		}
		return nil, err
	}
	return file, nil
}
