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

package sandboxbroker

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
)

// recoveryAncestorWritePermissions is the group and other write mask checked on journal
// ancestor directories.
const recoveryAncestorWritePermissions = 0022

// openRecoveryJournalDirectory pins a trusted directory chain before opening records.
//
// Takes name (string) which is an absolute host-selected path whose final directory must
// already exist.
//
// Returns an owned handle without following symlinks or unsafe writable ancestors.
func openRecoveryJournalDirectory(name string) (*os.File, error) {
	components := strings.FieldsFunc(name, func(character rune) bool { return character == '/' })
	if !filepath.IsAbs(name) || slices.Contains(components, "..") || len(components) > maximumJournalAncestorSteps {
		return nil, ErrInvalidPolicy
	}
	current, err := openLinuxDirectory("/")
	if err != nil {
		return nil, err
	}
	if err := checkRecoveryJournalAncestor(current, len(components) == 0); err != nil {
		return nil, errors.Join(err, current.Close())
	}
	for index, component := range components {
		descriptor, err := unix.Openat(int(current.Fd()), component,
			unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, errors.Join(err, current.Close())
		}
		child := os.NewFile(uintptr(descriptor), "journal-directory")
		err = errors.Join(checkRecoveryJournalAncestor(child, index == len(components)-1), current.Close())
		if err != nil {
			return nil, errors.Join(err, child.Close())
		}
		current = child
	}
	return current, nil
}

// checkRecoveryJournalAncestor validates the descriptor used for the next lookup.
//
// Takes directory (*os.File) which is a pinned directory handle.
// Takes final (bool) which indicates whether this directory directly contains journal
// records.
//
// Returns an error when another principal could replace the selected authority.
func checkRecoveryJournalAncestor(directory *os.File, final bool) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(directory.Fd()), &stat); err != nil {
		return err
	}
	return validateRecoveryJournalAncestor(stat, final, os.Geteuid())
}

// validateRecoveryJournalAncestor limits replacement authority along the host path.
//
// Takes stat (unix.Stat_t) which is the descriptor metadata.
// Takes final (bool) which indicates final-directory status.
// Takes principal (int) which is the effective host UID.
//
// Returns an error except for trusted owners and root-owned sticky ancestors.
func validateRecoveryJournalAncestor(stat unix.Stat_t, final bool, principal int) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return ErrDenied
	}
	if final {
		if int(stat.Uid) != principal || stat.Mode&stagingPermissionMask != recoveryDirectoryMode {
			return ErrDenied
		}
		return nil
	}
	if stat.Uid != 0 && int(stat.Uid) != principal {
		return ErrDenied
	}
	if stat.Mode&recoveryAncestorWritePermissions != 0 && (stat.Uid != 0 || stat.Mode&unix.S_ISVTX == 0) {
		return ErrDenied
	}
	return nil
}
