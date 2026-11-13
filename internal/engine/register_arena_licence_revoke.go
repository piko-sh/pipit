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
	"reflect"
)

// licenceRevokes reports whether a hand-off of cause revokes in a licensed arena.
//
// Takes cause (HandOffCause) which is the hand-off's cause.
//
// Returns bool which is true when the hand-off revokes.
func licenceRevokes(cause HandOffCause) bool {
	return cause.Revokes()
}

// borrowRegistry returns the registry that classes native calls for this arena.
//
// Returns *nativeBorrowRegistry which is never nil.
func (*RegisterArena) borrowRegistry() *nativeBorrowRegistry {
	return defaultBorrowRegistry
}

// nativeBorrowClass classes a native callee for a hand-off.
//
// Takes function (reflect.Value) which is the native callee.
//
// Returns BorrowClass which is the callee's class.
func (a *RegisterArena) nativeBorrowClass(function reflect.Value) BorrowClass {
	class := a.borrowRegistry().classify(function)
	if a.licensed && a.borrowDeclarations != nil {
		if declared, ok := a.borrowDeclarations.lookup(function); ok {
			class = declared
		}
	}
	if a.licensed && class.handOffCause().Revokes() && function.IsValid() &&
		function.Kind() == reflect.Func && !function.IsNil() && a.nativeReadsOnlyByClass(function) {
		class = BorrowReadsOnly
	}
	return class
}

// revokeHandOff revokes what value references for a hand-off of cause, when the cause
// revokes (licenceRevokes).
//
// Takes cause (HandOffCause) which is the hand-off's cause.
// Takes value (reflect.Value) which is the value handed off.
// Takes boxed (bool) which is true when value is reached through, or converted to, an
// interface.
// Takes shared (bool) which is true when another goroutine may be writing what value
// reaches.
func (a *RegisterArena) revokeHandOff(cause HandOffCause, value reflect.Value, boxed bool, shared bool) {
	a.licenceState.noteSeq++
	if !licenceRevokes(cause) {
		return
	}
	if licenceCounting {
		a.stats().handOffs[cause].Add(1)
	}
	a.revokeValue(value, boxed, cause, shared)
	a.licenceRecordNote(value, boxed)
}

// revokeValue revokes every slab value references within the walk's bounds.
//
// Takes value (reflect.Value) which is the value handed off.
// Takes boxed (bool) which is true when value is reached through, or converted to, an
// interface.
// Takes cause (HandOffCause) which is the hand-off's cause.
// Takes shared (bool) which is true when another goroutine may be writing what value
// reaches; the walk then reads through no addressable value or map.
func (a *RegisterArena) revokeValue(value reflect.Value, boxed bool, cause HandOffCause, shared bool) {
	if !value.IsValid() {
		return
	}
	if !licenceExactProbes && value.Kind() != reflect.String && typeIsPointerFree(value.Type()) && !boxed {
		return
	}
	if value.Kind() == reflect.String && a.revokedEpochProbes(boxed) {
		a.probeStringInRevokedEpoch(value.String(), cause)
		return
	}
	if a.revokeWithoutWalk(cause) {
		return
	}
	if !boxed && !a.containerStoreSkipsMemo() && a.revokeTrivially(value, shared) {
		return
	}
	a.walkHandOff(value, boxed, cause, shared)
}

// containerStoreSkipsMemo reports whether the hand-off in progress is
// recordContainerStore's, whose container the memo must not settle.
//
// Returns bool which is true when the walk must not consult the memo.
func (a *RegisterArena) containerStoreSkipsMemo() bool {
	return a.licenceState.containerStore
}

// revokeWithoutWalk settles the hand-offs whose value the licence need not look at.
//
// Takes cause (HandOffCause) which is the hand-off's cause.
//
// Returns bool which is true when the hand-off is settled.
func (a *RegisterArena) revokeWithoutWalk(cause HandOffCause) bool {
	if !licenceExactProbes {
		a.revokeEpoch(epochInexactProbe, cause)
		a.revokeBlind()
		return true
	}
	if a.licenceState.epochRevoked {
		a.revokeBlind()
		return true
	}
	return false
}

// revokeEpoch revokes every slab of the current epoch.
//
// Takes reason (licenceEpochReason) which says why.
// Takes cause (HandOffCause) which is the hand-off's cause, or noHandOffCause.
func (a *RegisterArena) revokeEpoch(reason licenceEpochReason, cause HandOffCause) {
	state := &a.licenceState
	if reason == epochRegisterBank && !state.banksRevoked {
		state.banksRevoked = true
		state.revision++
	}
	if state.epochRevoked {
		return
	}
	state.revision++
	state.epochRevoked = true
	state.epochReason = reason
	state.epochCause = cause
	if licenceCounting {
		a.stats().epochRevokes[reason].Add(1)
	}
}

// revokeSlab marks the slab starting at base revoked.
//
// Takes base (uintptr) which is the slab's start address.
// Takes cause (HandOffCause) which is the hand-off's cause.
func (a *RegisterArena) revokeSlab(base uintptr, cause HandOffCause) {
	state := &a.licenceState
	if state.revoked == nil {
		state.revoked = make(map[uintptr]HandOffCause)
	}
	if _, already := state.revoked[base]; already {
		if a.forgetRevokedPages(base) {
			state.revision++
		}
		return
	}
	state.revoked[base] = cause
	state.revision++
	if licenceCounting {
		a.stats().revokedSlabs[cause].Add(1)
	}
}

// adoptForeignRevocations applies revocations recorded by other arenas against this
// arena's storage to its own licence.
//
// An entry pending at one of those starts for an arena that held the address before is
// not this arena's; that arena drops it.
func (a *RegisterArena) adoptForeignRevocations() {
	if licenceForeignRevokedCount.Load() == 0 {
		return
	}
	a.lockForeign()
	defer a.unlockForeign()
	a.adoptForeignRevocationsLocked()
}

// forgetForeignRevocationLocked removes a pending foreign revocation the arena has just
// applied to its own licence.
//
// Takes key (licenceForeignKey) which is the revoked slab's start and the arena's
// identity.
//
// Panics with the injected abort error when the foreign-revocation window site fires in a
// pipit_testhooks build.
func forgetForeignRevocationLocked(key licenceForeignKey) {
	if abortInjectBuilt && abortInjectFires(abortSiteForeignWindow) {
		panic(abortInjectedError(abortSiteForeignWindow))
	}
	if licenceForeignRevoked.forget(key) {
		licenceForeignRevokedCount.Add(-1)
	}
}

// dropOwnerForeignRevocations removes every foreign revocation pending against owner's
// slabs, wherever they start.
//
// Takes owner (uint64) which identifies the collected arena.
//
// Concurrency: Protects pending foreign revocations with licenceForeignRevokedMu; the
// caller must retire the owner's registry entries first.
func dropOwnerForeignRevocations(owner uint64) {
	if licenceForeignRevokedCount.Load() == 0 {
		return
	}
	licenceForeignRevokedMu.Lock()
	defer licenceForeignRevokedMu.Unlock()
	removed := licenceForeignRevoked.forgetOwner(owner)
	if removed == 0 {
		return
	}
	licenceForeignRevokedCount.Add(int64(-removed))
	if licenceCounting {
		licenceStats().foreignDropped.Add(int64(removed))
	}
}

// licenceRevokedBase reports whether the slab starting at base may not be reused.
//
// Takes base (uintptr) which is the slab's start address.
//
// Returns bool which is true for a revoked slab or a revoked epoch.
func (a *RegisterArena) licenceRevokedBase(base uintptr) bool {
	if !a.licensed {
		return false
	}
	if a.licenceState.epochRevoked {
		return true
	}
	_, revoked := a.licenceState.revoked[base]
	return revoked
}

// licenceCoveredSlab is licenceCoveredAt for the arena's data slab starting at base, once
// the cover is known not to be empty.
//
// Takes base (uintptr) which is the start of one of the arena's data slabs.
//
// Returns bool which is true when the slab is covered.
func (a *RegisterArena) licenceCoveredSlab(base uintptr) bool {
	state := &a.licenceState
	if state.coverCoveredKnown && base == state.coverCoveredBase {
		return true
	}
	if base != state.coverCleanBase || state.revision != state.coverCleanRevision || state.epochRevoked {
		if a.licenceRevokedBase(base) {
			state.coverCoveredBase, state.coverCoveredKnown = base, true
			return true
		}
	}
	revision := state.revision
	if a.blindSinceReading() {
		a.applyBlindHandOffs()
	}
	if licenceForeignRevokedCount.Load() != 0 {
		a.adoptForeignRevocation(base)
	}
	if state.revision == revision {
		state.coverCleanBase, state.coverCleanRevision = base, revision
		return false
	}
	return a.licenceRevokedBase(base)
}

// licenceCoverEmpty reports whether the licence covers none of the arena's storage.
//
// Returns bool which is true when nothing is covered.
func (a *RegisterArena) licenceCoverEmpty() bool {
	state := &a.licenceState
	return !state.epochRevoked && len(state.revoked) == 0 && licenceForeignRevokedCount.Load() == 0 && !a.blindSinceReading()
}

// blindSinceReading reports whether a walk of the arena's tree was skipped since the
// arena last read its blind counters.
//
// Returns bool which is true when either counter moved.
func (a *RegisterArena) blindSinceReading() bool {
	state := &a.licenceState
	if !state.blindMarked {
		return true
	}
	all, bytes := a.blindTotals()
	return all != state.blindMark || bytes != state.blindByteMark
}

// adoptForeignRevocation moves a foreign revocation pending against the slab starting at
// base into the arena's own licence.
//
// Takes base (uintptr) which is the start of one of the arena's data slabs.
func (a *RegisterArena) adoptForeignRevocation(base uintptr) {
	if !a.licenceState.foreignLocked {
		a.lockForeign()
		defer a.unlockForeign()
	}
	key := licenceForeignKey{start: base, owner: a.slabOwner}
	cause, found := licenceForeignRevoked.lookup(key)
	if !found {
		return
	}
	a.revokeSlab(base, cause)
	forgetForeignRevocationLocked(key)
}
