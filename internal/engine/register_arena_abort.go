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

import (
	"fmt"
	"sync/atomic"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/fault"
)

const (
	// budgetOvershootBytes bounds overshoot beyond MaxArenaBytes to one chunk of the largest
	// chunked class between safepoints.
	budgetOvershootBytes = uint64(genericBytesSlabChunk)
)

// abortedArenas counts the arenas marked aborted since start-up.
var abortedArenas atomic.Int64

// abortedArenaCount reports how many arenas a limit or a recovered handler panic marked
// aborted since start-up; each was abandoned instead of pooled at its release.
//
// Returns int64 which is the count.
func abortedArenaCount() int64 {
	return abortedArenas.Load()
}

// markAborted records that an abort exited code part-way through updating the arena, so
// its release abandons it.
func (a *RegisterArena) markAborted() {
	if a == nil || a.aborted {
		return
	}
	a.aborted = true
	abortedArenas.Add(1)
	if a.licensed {
		a.revokeEpoch(epochAborted, noHandOffCause)
		a.revokeBlind()
	}
}

// raiseAbort marks the arena aborted and panics with err. Every limit raised from inside
// arena code goes through it.
//
// Takes err (error) which is the limit's error.
func (a *RegisterArena) raiseAbort(err error) {
	a.markAborted()
	panic(err)
}

// budgetServicer returns the VM whose dispatch safepoints service the arena's pending
// collection and budget check.
//
// Returns *VM which is the VM, or nil when none runs the arena.
func (a *RegisterArena) budgetServicer() *VM {
	if a.ownerVM != nil {
		return a.ownerVM
	}
	return a.licenceVM
}

// abortOnCollectorPanic is MinorGC's deferred guard, handed what the deferred call
// recovered.
//
// Takes recovered (any) which is the recovered value, or nil when nothing panicked.
//
// Panics with an interpreter invariant failure when collection panics, after marking the
// arena aborted.
func (a *RegisterArena) abortOnCollectorPanic(recovered any) {
	if recovered == nil {
		return
	}
	a.markAborted()
	panic(newInvariantError("minor collection interrupted: %v", recovered))
}

// admitGrow is the budget's hard limit, checked before a grow changes anything.
//
// Takes newCapacityBytes (uint64) which is the capacity the grow will charge.
// Takes oldCapacityBytes (uint64) which is the capacity it will credit.
func (a *RegisterArena) admitGrow(newCapacityBytes, oldCapacityBytes uint64) {
	if newCapacityBytes <= oldCapacityBytes {
		return
	}
	delta := newCapacityBytes - oldCapacityBytes
	working := a.sharedWorkingSetBytes()
	if working+delta < working {
		a.raiseAbort(fmt.Errorf("arena: overflow charging %d bytes: %w", delta, fault.ErrArenaBudgetExceeded))
	}
	limit, bounded := a.admitLimit()
	if !bounded || working+delta <= limit {
		return
	}
	if a.licensed {
		if licenceCounting {
			a.stats().overshootPanics.Add(1)
		}
	}
	a.raiseAbort(fmt.Errorf("arena: %d bytes exceeds budget %d: %w", working+delta, a.arenaBudgetLimit(), fault.ErrArenaBudgetExceeded))
}

// admitLimit returns the working set admitGrow lets a grow reach.
//
// A licensed arena whose VM services its safepoints may pass the budget by
// budgetOvershootBytes, since the next safepoint collects or stops the run. An unlicensed
// arena that collects may grow without a limit here, since ChargeArenaAllocation collects
// and then checks the budget.
//
// Returns limit (uint64) which is the working set in bytes.
// Returns bounded (bool) which is false when admitGrow sets no limit.
func (a *RegisterArena) admitLimit() (limit uint64, bounded bool) {
	budget := a.arenaBudgetLimit()
	switch {
	case a.licensed && a.budgetServicer() != nil:
		return budget + budgetOvershootBytes, true
	case !a.licensed && a.ownerVM != nil && !a.disableMinorGC:
		return 0, false
	default:
		return budget, true
	}
}

// fitGrow lowers a grow's size so that admitGrow admits it, when a smaller grow still
// serves the request.
//
// The size is lowered only to a size that still holds the request and is larger than the
// slab it retires, so the grow still makes room and still charges for it. Otherwise it is
// left for admitGrow to refuse.
//
// Takes size (int) which is the grow's size in elements.
// Takes need (int) which is the fewest elements the new slab must hold.
// Takes elementBytes (uint64) which is the size of one element, greater than zero.
// Takes retiredLength (int) which is the length in elements of the slab the grow retires.
// Takes oldCapacityBytes (uint64) which is the charge the grow will credit.
//
// Returns int which is the size to make.
func (a *RegisterArena) fitGrow(size, need int, elementBytes uint64, retiredLength int, oldCapacityBytes uint64) int {
	limit, bounded := a.admitLimit()
	if !bounded || elementBytes == 0 {
		return size
	}
	room := limit + oldCapacityBytes
	working := a.sharedWorkingSetBytes()
	if room < limit || working >= room {
		return size
	}
	fit := (room - working) / elementBytes
	if safeconv.IntToUint64(size) <= fit {
		return size
	}
	floor := max(need, retiredLength+1)
	if fit < safeconv.IntToUint64(floor) {
		return size
	}
	return safeconv.Uint64ToInt(fit)
}

// admitChunkedGrow is admitGrow for a chunked class, which charges a fresh slab as
// chargeChunkedSlab does and nothing for a recycled chunk.
//
// Takes newBytes (uint64) which is the byte capacity of the slab the grow will make.
// Takes oldBytes (uint64) which is the byte capacity of the slab it retires.
// Takes chunkBytes (uint64) which is the byte size of one chunk.
// Takes recycles (bool) which is true when the grow will install a recycled chunk.
func (a *RegisterArena) admitChunkedGrow(newBytes, oldBytes, chunkBytes uint64, recycles bool) {
	switch {
	case recycles:
	case oldBytes >= chunkBytes:
		a.admitGrow(newBytes, 0)
	default:
		a.admitGrow(newBytes, oldBytes)
	}
}

// deferOverBudgetToSafepoint handles a grow that took a licensed arena past its budget.
//
// Takes budget (uint64) which is the arena's budget.
func (a *RegisterArena) deferOverBudgetToSafepoint(budget uint64) {
	if vm := a.budgetServicer(); vm != nil {
		vm.requestCheckpoint(checkpointFlagGCPending)
		if licenceCounting {
			a.stats().deferredCollections.Add(1)
		}
		return
	}
	if licenceCounting {
		a.stats().overshootPanics.Add(1)
	}
	a.raiseAbort(fmt.Errorf("arena: %d bytes exceeds budget %d: %w", a.sharedWorkingSetBytes(), budget, fault.ErrArenaBudgetExceeded))
}

// enforceBudgetAtSafepoint panics when the arena is still over budget after the
// safepoint's collection; a licensed arena's budget is a limit checked at safepoints.
func (a *RegisterArena) enforceBudgetAtSafepoint() {
	budget := a.arenaBudgetLimit()
	if a.sharedWorkingSetBytes() <= budget {
		return
	}
	if licenceCounting {
		a.stats().safepointBudgetPanics.Add(1)
	}
	a.raiseAbort(fmt.Errorf("arena: %d bytes exceeds budget %d at safepoint: %w", a.sharedWorkingSetBytes(), budget, fault.ErrArenaBudgetExceeded))
}

// armSafepointForThreshold brings the next safepoint forward when a grow under budget has
// crossed the arena's collection threshold.
//
// The assembly dispatcher otherwise reaches the threshold check only once per poll
// interval, so the licensed arena would grow further between collections than under the
// Go dispatch loop, which checks it every interval of instructions.
func (a *RegisterArena) armSafepointForThreshold() {
	if vm := a.budgetServicer(); vm != nil && vm.activePollBudget != nil && a.gcShouldRun() {
		*vm.activePollBudget = 0
	}
}
