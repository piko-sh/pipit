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

//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly && !windows

package modloader

import (
	"errors"
	"os"
)

// lockApprovalGuard refuses persistent approval writes without native coordination.
//
// Returns error on unsupported platforms rather than publishing unlocked approvals.
func lockApprovalGuard(_ *os.File) error {
	return errors.New("modloader: approval locking unavailable on this platform")
}

// unlockApprovalGuard has no lock to release on unsupported platforms.
//
// Returns nil.
func unlockApprovalGuard(_ *os.File) error {
	return nil
}
