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

//go:build windows

package modloader

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockApprovalGuard takes an exclusive non-blocking byte-range lock.
//
// Takes file (*os.File) which is the guard descriptor in a host-protected directory.
//
// Returns error from the native lock call on contention or unsupported locking.
func lockApprovalGuard(file *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
}

// unlockApprovalGuard releases the guard's byte-range lock.
//
// Takes file (*os.File) which is the owned guard descriptor.
//
// Returns error from the native release.
func unlockApprovalGuard(file *os.File) error {
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
}
