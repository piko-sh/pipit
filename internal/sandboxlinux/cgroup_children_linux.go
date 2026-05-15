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
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// newChild creates a separately owned child under this group's aggregate limits.
//
// Takes limits (Limits) which are the finite per-child bounds.
//
// Returns *Group which is the child owner.
// Returns error when controller delegation or child creation fails.
//
// Safe for concurrent use; the parent mutex serialises delegation.
func (group *Group) newChild(limits Limits) (*Group, error) {
	if group == nil {
		return nil, errClosed
	}
	settings, err := resourceSettings(limits)
	if err != nil {
		return nil, err
	}
	group.mutex.Lock()
	defer group.mutex.Unlock()
	if group.closed || group.closing || group.started {
		return nil, errClosed
	}
	if !group.parentOnly {
		group.parentOnly = true
		if err := enableChildControllers(group.directory); err != nil {
			group.closing = true
			return nil, fmt.Errorf("%w: enabling private child controls: %w", ErrUnavailable, err)
		}
	}
	parent, err := openDirectory(int(group.directory.Fd()), ".")
	if err != nil {
		return nil, fmt.Errorf("%w: pinning aggregate parent: %w", ErrUnavailable, err)
	}
	return newGroupUnder(parent, settings)
}

// enableChildControllers verifies the complete private subtree controller set.
//
// Takes directory (*os.File) which is the empty, host-owned cgroup whose aggregate
// resource limits are set.
//
// Returns error if any required controller cannot be enabled and read back.
func enableChildControllers(directory *os.File) error {
	control, err := openControl(directory, "cgroup.subtree_control", unix.O_WRONLY)
	if err != nil {
		return err
	}
	const requested = "+cpu +memory +pids"
	written, writeErr := control.WriteString(requested)
	closeErr := control.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	if written != len(requested) {
		return io.ErrShortWrite
	}
	actual, err := readControl(directory, "cgroup.subtree_control")
	if err != nil {
		return err
	}
	if strings.Join(strings.Fields(actual), " ") != "cpu memory pids" {
		return errors.New("kernel subtree controllers differ from required controllers")
	}
	return nil
}
