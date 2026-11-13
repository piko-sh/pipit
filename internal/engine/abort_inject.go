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

package engine

// abortSite names one place an abort can be injected.
type abortSite uint8

const (
	// abortSiteGrowCharge identifies an arena-budget panic raised while
	// ChargeArenaAllocation charges a slab growth.
	abortSiteGrowCharge abortSite = iota

	// abortSiteSafepointBudget identifies an arena-budget panic raised when a licensed arena
	// services a dispatch safepoint.
	abortSiteSafepointBudget

	// abortSiteAllocLimit identifies the recoverable allocation-limit panic raised by
	// enforceSingleAllocLimit.
	abortSiteAllocLimit

	// abortSiteCost identifies cost exhaustion when the cost budget is refilled at an
	// instruction boundary.
	abortSiteCost

	// abortSiteBlindWindow is between applying a blind reading and moving the arena's blind
	// mark past it (commitBlindReading). No limit fires there; the site stands for a panic
	// landing inside the window, for the engine tests that show the cover is not lost.
	abortSiteBlindWindow

	// abortSiteForeignWindow is between revoking a slab for a pending foreign revocation and
	// forgetting the pending entry (forgetForeignRevocationLocked), for the same tests.
	abortSiteForeignWindow

	// abortSiteLockWindow is right after a guest Lock or RLock that Pipit made took its lock
	// (waitInterruptibly).
	abortSiteLockWindow
)
