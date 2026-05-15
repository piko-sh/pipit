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
	"errors"
	"fmt"
	"io"
	"os"
)

// prepareCleanup verifies retained directory ownership and terminates admitted tasks.
//
// Returns an error while ownership must remain available for retry.
func (group *Group) prepareCleanup() error {
	if group.directory == nil {
		return errors.New("resource cleanup has no original directory handle")
	}
	if err := group.verifyDirectoryEntry(); err != nil {
		return err
	}
	if group.kill == nil {
		if group.started || group.parentOnly {
			return errors.New("active resource owner has no termination handle")
		}
		return nil
	}
	if written, err := group.kill.WriteAt([]byte("1"), 0); err != nil || written != 1 {
		if err == nil {
			err = io.ErrShortWrite
		}
		return fmt.Errorf("killing resource group: %w", err)
	}
	return nil
}

// closeHandles releases handles only after kernel-confirmed group removal.
//
// Returns any handle closure errors without double-closing nil partial handles.
func (group *Group) closeHandles() error {
	var result error
	for _, handle := range []*os.File{group.kill, group.directory, group.parent} {
		if handle != nil {
			result = errors.Join(result, handle.Close())
		}
	}
	return result
}

// finishGroupPreparation rolls back failed setup without discarding live ownership.
//
// Takes group (*Group) which is the non-nil newly created owner.
// Takes preparationErr (error) which is the preparation error from group setup.
//
// Returns the prepared group, nil after successful rollback, or a cleanup-only owner.
func finishGroupPreparation(group *Group, preparationErr error) (*Group, error) {
	if preparationErr == nil {
		return group, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), groupSetupCleanupTimeout)
	defer cancel()
	if cleanupErr := group.closeGroup(ctx); cleanupErr != nil {
		return group, errors.Join(preparationErr, cleanupErr)
	}
	return nil, preparationErr
}
