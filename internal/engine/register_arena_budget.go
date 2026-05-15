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

import "pipit.sh/pipit/internal/safeconv"

// AttachBudgetTracker joins the arena to an execution's shared byte accounting. The
// arena's current working set is charged immediately because pooled arenas keep their
// slab capacity across Reset and may already carry bytes when they are handed out.
//
// Takes tracker (*ResourceTracker) which owns the shared counter; nil leaves the arena on
// purely local accounting.
func (a *RegisterArena) AttachBudgetTracker(tracker *ResourceTracker) {
	a.detachBudgetTracker()
	if tracker == nil {
		return
	}
	a.budgetTracker = tracker
	tracker.ArenaBytes.Add(safeconv.Uint64ToInt64(a.totalAllocatedBytes))
}

// detachBudgetTracker surrenders the arena's working set from the shared counter and
// returns the arena to purely local accounting. Safe to call when no tracker is attached.
func (a *RegisterArena) detachBudgetTracker() {
	if a.budgetTracker == nil {
		return
	}
	a.budgetTracker.ArenaBytes.Add(-safeconv.Uint64ToInt64(a.totalAllocatedBytes))
	a.budgetTracker = nil
}

// mirrorBudgetDelta applies a change already made to totalAllocatedBytes to the shared
// counter, when one is attached.
//
// Takes delta (int64) which is the signed change in working-set bytes.
func (a *RegisterArena) mirrorBudgetDelta(delta int64) {
	if a.budgetTracker == nil || delta == 0 {
		return
	}
	a.budgetTracker.ArenaBytes.Add(delta)
}

// sharedWorkingSetBytes reports the execution-wide working set when a tracker is
// attached, or the arena's own working set otherwise. A negative shared figure (possible
// transiently while a sibling detaches) falls back to the local figure.
//
// Returns the larger of the local and shared working sets in bytes.
func (a *RegisterArena) sharedWorkingSetBytes() uint64 {
	if a.budgetTracker == nil {
		return a.totalAllocatedBytes
	}
	shared := a.budgetTracker.ArenaBytes.Load()
	if shared < 0 || uint64(shared) < a.totalAllocatedBytes {
		return a.totalAllocatedBytes
	}
	return uint64(shared)
}

// releaseArenaBytes lowers the working-set counter by released bytes, clamping at zero,
// and mirrors the actual decrement into the shared counter.
//
// Takes released (uint64) which is the byte capacity surrendered.
func (a *RegisterArena) releaseArenaBytes(released uint64) {
	if released > a.totalAllocatedBytes {
		released = a.totalAllocatedBytes
	}
	a.totalAllocatedBytes -= released
	a.mirrorBudgetDelta(-safeconv.Uint64ToInt64(released))
}
