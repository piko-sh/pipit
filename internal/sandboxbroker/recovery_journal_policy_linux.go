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

	"golang.org/x/sys/unix"
)

// maximumJournalAncestorSteps is the upper bound on ancestry traversal depth during
// overlap detection.
const maximumJournalAncestorSteps = 256

// ValidateJournal checks private storage against the original pinned grant roots. It
// rejects overlapping directory ancestry and exact inode aliases before launch.
//
// Takes journal (*LinuxRecoveryJournal) which is a fresh, independently owned journal for
// this authority's namespace.
//
// Returns error without changing records or releasing either owner's handles.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryAuthority) ValidateJournal(journal *LinuxRecoveryJournal) error {
	if owner == nil || journal == nil {
		return ErrClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed || owner.budget == nil {
		return ErrClosed
	}
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	if journal.closed || journal.failed || journal.directory == nil {
		return ErrClosed
	}
	if journal.namespace != owner.namespace || len(journal.records) != 0 {
		return ErrInvalidPolicy
	}
	if err := checkJournalDirectory(journal.directory); err != nil {
		return err
	}
	return owner.rejectJournalOverlap(journal.directory)
}

// rejectJournalOverlap compares directory identities while both owners are locked.
//
// Takes directory (*os.File) which is the original journal directory, never a reopened
// host pathname.
//
// Returns error for overlap or any incomplete ancestry inspection.
func (owner *LinuxRecoveryAuthority) rejectJournalOverlap(directory *os.File) (result error) {
	pinned, err := openLinuxBeneath(directory, ".", unix.O_PATH|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, pinned.Close()) }()
	journalIdentity, err := linuxBootstrapRootIdentity(pinned)
	if err != nil {
		return err
	}
	for _, root := range owner.roots {
		if err := rejectJournalAncestor(pinned, root.identity); err != nil {
			return err
		}
		if err := rejectJournalAncestor(root.file, journalIdentity); err != nil {
			return err
		}
	}
	return nil
}

// rejectJournalAncestor walks pinned host directory ancestry with a fixed bound. No
// script-selected path is resolved and no directory content is read.
//
// Takes directory (*os.File) which is a pinned starting directory.
// Takes forbidden (filesystemBootstrapRoot) which is an identity forbidden among its
// ancestors.
//
// Returns error on overlap, inaccessible ancestry or exhaustion of the scan bound.
func rejectJournalAncestor(directory *os.File, forbidden filesystemBootstrapRoot) (result error) {
	current, err := duplicateLinuxBootstrapFile(directory)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, current.Close()) }()
	for range maximumJournalAncestorSteps {
		identity, err := linuxBootstrapRootIdentity(current)
		if err != nil {
			return err
		}
		if identity.Device == forbidden.Device && identity.Inode == forbidden.Inode {
			return ErrDenied
		}
		descriptor, err := unix.Openat(int(current.Fd()), "..",
			unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		parent := os.NewFile(uintptr(descriptor), "journal-ancestor")
		previous := identity
		identity, err = linuxBootstrapRootIdentity(parent)
		if failure := errors.Join(err, current.Close()); failure != nil {
			current = parent
			return failure
		}
		current = parent
		if identity == previous {
			return nil
		}
	}
	return errLimit
}
