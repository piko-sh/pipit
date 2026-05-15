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

//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package modloader

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockApprovalGuard verifies a private, singly linked guard and locks it.
//
// Takes file (*os.File) which is the guard descriptor owned by the caller.
//
// Returns error immediately on unsafe metadata or lock contention.
func lockApprovalGuard(file *os.File) error {
	if err := validateApprovalFileOwner(file); err != nil {
		return err
	}
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

// unlockApprovalGuard releases this open-file description's advisory lock.
//
// Takes file (*os.File) which is the owned guard descriptor.
//
// Returns error from the native release.
func unlockApprovalGuard(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
