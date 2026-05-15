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

//go:build !unix

package modloader

import (
	"io/fs"
	"os"
)

// openApprovalDirectory retains host-managed ACL protection outside Unix.
//
// Takes path (string) which is the directory path.
// Takes create (bool) which allows missing directories to be created.
//
// Returns *os.Root which is a pinned root without claiming Unix ancestor ownership
// enforcement.
// Returns error when the directory cannot be opened or created.
func openApprovalDirectory(path string, create bool) (*os.Root, error) {
	if create {
		if err := os.MkdirAll(path, cacheDirMode); err != nil {
			return nil, err
		}
	}
	return os.OpenRoot(path)
}

// syncApprovalDirectory does not promise directory-sync durability outside Unix.
//
// Returns nil on non-Unix hosts.
func syncApprovalDirectory(_ *os.Root) error {
	return nil
}

// openApprovalFile uses root confinement on hosts without Unix openat semantics.
//
// Takes root (*os.Root) which owns the directory.
// Takes name (string) which is the file basename.
// Takes flags (int) which holds the open flags.
// Takes mode (fs.FileMode) which sets the creation permissions.
//
// Returns *os.File which is the opened file without claiming Unix symlink or ACL
// enforcement.
// Returns error when the file cannot be opened.
func openApprovalFile(root *os.Root, name string, flags int, mode fs.FileMode) (*os.File, error) {
	return root.OpenFile(name, flags, mode)
}

// validateApprovalFileOwner leaves non-Unix ACL protection to the trusted host.
//
// Returns nil without claiming that native ownership or ACL checks were performed.
func validateApprovalFileOwner(_ *os.File) error {
	return nil
}
