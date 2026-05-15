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

//go:build unix

package modloader

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// maximumApprovalDirectoryDepth is the ceiling on path components traversed when pinning
// an approval directory.
const maximumApprovalDirectoryDepth = 256

// openApprovalDirectory pins a protected, symlink-free chain from the filesystem root.
//
// Takes path (string) which is the host directory path.
// Takes create (bool) which allows missing components to be created.
//
// Returns *os.Root which is an owned directory handle.
// Returns error which refuses untrusted ancestors.
func openApprovalDirectory(path string, create bool) (*os.Root, error) {
	if slices.Contains(strings.Split(path, string(filepath.Separator)), "..") {
		return nil, fs.ErrPermission
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	components := strings.FieldsFunc(absolute, func(character rune) bool { return character == '/' })
	if len(components) > maximumApprovalDirectoryDepth {
		return nil, fs.ErrInvalid
	}
	root, err := os.OpenRoot("/")
	if err != nil {
		return nil, err
	}
	info, err := root.Stat(".")
	if err == nil {
		err = validateApprovalAncestor(info, len(components) == 0)
	}
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	for index, component := range components {
		child, openErr := openApprovalChild(root, component, index == len(components)-1, create)
		closeErr := root.Close()
		if err := errors.Join(openErr, closeErr); err != nil {
			if child != nil {
				err = errors.Join(err, child.Close())
			}
			return nil, err
		}
		root = child
	}
	return root, nil
}

// openApprovalChild validates a component before and after opening its pinned handle.
//
// Takes parent (*os.Root) which is the validated parent directory.
// Takes name (string) which is one path component.
// Takes final (bool) which marks whether this is the last component.
// Takes create (bool) which allows the component to be created when missing.
//
// Returns *os.Root with matching inode identity and acceptable ownership.
// Returns error on ownership or identity mismatch.
func openApprovalChild(parent *os.Root, name string, final, create bool) (*os.Root, error) {
	before, err := parent.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) && create {
		if err := parent.Mkdir(name, cacheDirMode); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		before, err = parent.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if err := validateApprovalAncestor(before, final); err != nil {
		return nil, err
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := child.Stat(".")
	if err == nil && !os.SameFile(before, after) {
		err = fs.ErrPermission
	}
	if err == nil {
		err = validateApprovalAncestor(after, final)
	}
	if err != nil {
		return nil, errors.Join(err, child.Close())
	}
	return child, nil
}

// validateApprovalAncestor checks Unix directory ownership and replacement authority.
//
// Takes info (fs.FileInfo) which carries the directory metadata.
// Takes final (bool) which marks whether the directory directly holds approval files.
//
// Returns error for unsafe directories, allowing root-owned sticky ancestors only.
func validateApprovalAncestor(info fs.FileInfo, final bool) error {
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return fs.ErrPermission
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fs.ErrPermission
	}
	owner := int64(metadata.Uid)
	if final && owner != int64(os.Geteuid()) {
		return fs.ErrPermission
	}
	if owner != 0 && owner != int64(os.Geteuid()) {
		return fs.ErrPermission
	}
	if info.Mode().Perm()&approvalDirectoryWritePermissions != 0 &&
		(final || owner != 0 || info.Mode()&fs.ModeSticky == 0) {
		return fs.ErrPermission
	}
	return nil
}
