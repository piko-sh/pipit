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

//go:build linux

package modloader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestApprovalGuardCloseReleasesDuplicatedDescription(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), LockfileName)
	guard, err := acquireApprovalGuard(path)
	require.NoError(t, err)
	duplicate, err := unix.Dup(int(guard.file.Fd()))
	if err != nil {
		_ = guard.Close()
	}
	require.NoError(t, err)
	defer unix.Close(duplicate)
	require.NoError(t, guard.Close())
	next, err := acquireApprovalGuard(path)
	require.NoError(t, err, "a duplicate descriptor does not keep a released lock")
	require.NoError(t, next.Close())
}

func TestApprovalGuardRejectsUnsafeNodes(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"symlink", "relative-symlink", "hardlink", "public", "fifo", "directory", "content"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			path := filepath.Join(directory, LockfileName)
			guardPath := path + ".guard"
			target := filepath.Join(directory, "target")
			require.NoError(t, os.WriteFile(target, nil, 0o600))
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(target, guardPath)
			case "relative-symlink":
				err = os.Symlink("target", guardPath)
			case "hardlink":
				err = os.Link(target, guardPath)
			case "public":
				err = os.WriteFile(guardPath, nil, 0o600)
				if err == nil {
					err = os.Chmod(guardPath, 0o644)
				}
			case "fifo":
				err = unix.Mkfifo(guardPath, 0o600)
			case "directory":
				err = os.Mkdir(guardPath, 0o700)
			case "content":
				err = os.WriteFile(guardPath, []byte("occupied"), 0o600)
			}
			require.NoError(t, err)
			guard, err := acquireApprovalGuard(path)
			if guard != nil {
				_ = guard.Close()
			}
			require.Error(t, err, "an unsafe guard is refused")
		})
	}
}
