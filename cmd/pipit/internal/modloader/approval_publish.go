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
	"crypto/rand"
	"errors"
	"fmt"
	"os"
)

// publishApprovalFile writes and renames within the transaction's pinned directory.
//
// Takes root (*os.Root) which is the directory protected by the approval guard.
// Takes name (string) which is the approval basename.
// Takes data ([]byte) which holds the bounded JSON content.
//
// Returns error without re-resolving the original directory pathname.
func publishApprovalFile(root *os.Root, name string, data []byte) error {
	temporaryName := ".pipit-approval-" + rand.Text()
	temporary, err := openApprovalFile(root, temporaryName, os.O_CREATE|os.O_EXCL|os.O_RDWR, approvalGuardMode)
	if err != nil {
		return fmt.Errorf("modloader: creating temporary approval file: %w", err)
	}
	defer root.Remove(temporaryName)
	if _, err := temporary.Write(data); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Sync(); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := root.Rename(temporaryName, name); err != nil {
		return fmt.Errorf("modloader: publishing approval file: %w", err)
	}
	return syncApprovalDirectory(root)
}
