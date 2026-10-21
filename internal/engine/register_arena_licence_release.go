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

	"pipit.sh/pipit/internal/engine/program"
)

// licenceReleasePhase names a point inside a licensed release at which the test-hooks
// build (tag pipit_testhooks) may pause, so a test can interleave another arena's
// hand-off with the release deterministically. The default build compiles every pause
// away.
type licenceReleasePhase uint8

const (
	// licencePhaseSnapshot follows readBlindHandOffs.
	//
	// The blind interval has been read but not yet applied or restarted, and nothing has
	// been adopted or detached.
	licencePhaseSnapshot licenceReleasePhase = iota + 1

	// licencePhaseAdopted follows adoptForeignRevocations.
	//
	// The pending foreign revocations have been moved into the licence, but no slab has been
	// detached.
	licencePhaseAdopted

	// licencePhaseDetached follows releaseLicence.
	//
	// Revoked storage has been detached, and Reset is about to clear what remains.
	licencePhaseDetached
)

// licenceReleaseTally counts one release's outcome.
type licenceReleaseTally struct {
	// causes flags each cause that contributed an abandoned slab.
	causes [numHandOffCauses + 1]bool

	// abandoned counts slabs abandoned.
	abandoned int64

	// recycled counts slabs kept for reuse.
	recycled int64
}

// regrowRecord records slab usage for sizing replacements after licensed release.
//
// releaseRevokedEpoch records usage for each abandoned slab kind before Reset clears the
// bump indices. Abandonment at the seal or the next acquisition records nothing. When
// ensureCapacity next sizes the same program, each replacement is capped at twice the
// recorded usage or the program's hint, whichever is smaller. The hint counts every value
// the program could make.
type regrowRecord struct {
	// pending is true from the abandonment that recorded the use until the next sizing.
	pending bool

	// sized holds the hints of the arena's latest sizing, and recordedFor those of the epoch
	// whose use was recorded.
	sized, recordedFor program.TypedSlabCounts

	// used holds each kind's use, in the units of program.TypedSlabCounts.
	used program.TypedSlabCounts
}

// takeLazyFloor returns the floor dropRevokedSlabs recorded for an empty slab of a
// licensed arena, and clears it; zero otherwise.
//
// Takes current (int) which is the slab's length.
// Takes floor (*int) which is the recorded floor.
//
// Returns int which is the floor to grow to.
func (a *RegisterArena) takeLazyFloor(current int, floor *int) int {
	if current != 0 || *floor == 0 || !a.licensed {
		return 0
	}
	value := *floor
	*floor = 0
	return value
}

// releaseLicence detaches and abandons revoked storage before Reset clears anything, so a
// value that escaped through a revoking hand-off keeps reading what it read.
//
// A revoked epoch abandons every data slab, as abandonEscapedSlabs does for re-entrant
// VMs. Clean slabs stay for Reset to clear and the next owner to reuse. Reset then seals
// the release (sealLicenceRelease) against revocations that arrive meanwhile.
func (a *RegisterArena) releaseLicence() {
	state := &a.licenceState
	if state.released {
		return
	}
	state.released = true
	if licenceCounting {
		a.stats().releases.Add(1)
	}
	a.refreshFamilyShared()
	reading := a.readBlindHandOffs()
	licenceReleasePause(a, licencePhaseSnapshot)
	if a.releaseFindsNothing(reading) {
		a.commitBlindReading(reading)
		if licenceCounting {
			a.stats().releasesNoData.Add(1)
		}
		state.releasedRevision = state.revision
		return
	}
	a.applyBlindReading(reading)
	a.commitBlindReading(reading)
	a.adoptForeignRevocations()
	licenceReleasePause(a, licencePhaseAdopted)
	a.closeOverRevoked()
	a.settleBytePlacements()
	a.settlePointeePlacements()
	a.settleSnapshotPlacements()
	if state.epochRevoked {
		a.releaseRevokedEpoch()
		state.releasedRevision = state.revision
		a.publishLicenceSlabs()
		return
	}
	a.releaseRevokedSlabs()
}

// releaseFindsNothing reports whether a release has nothing to do beyond committing its
// blind reading: no escapable data, nothing revoked, nothing the reading names and no
// foreign revocation pending anywhere.
//
// Takes reading (blindReading) which is the release's blind reading.
//
// Returns bool which is true when the release may return early.
func (a *RegisterArena) releaseFindsNothing(reading blindReading) bool {
	state := &a.licenceState
	return !a.holdsEscapableData() && !state.epochRevoked && len(state.revoked) == 0 && !reading.all && !reading.bytes &&
		licenceForeignRevokedCount.Load() == 0
}

// applyBlindReading revokes what a release's blind reading names: the register banks of a
// revoked epoch, everything foreign-reachable, or the byte slabs.
//
// Takes reading (blindReading) which is the release's blind reading.
func (a *RegisterArena) applyBlindReading(reading blindReading) {
	switch {
	case a.licenceState.epochRevoked:
		if reading.all {
			a.revokeEpoch(epochRegisterBank, noHandOffCause)
		}
	case reading.all:
		if licenceCounting {
			a.stats().blindByteSlabReleases.Add(1)
		}
		a.revokeForeignReachable()
	case reading.bytes:
		if licenceCounting {
			a.stats().blindByteSlabReleases.Add(1)
		}
		a.revokeByteSlabs()
	}
}

// releaseRevokedSlabs ends a release whose epoch is not revoked: it drops the
// individually revoked slabs, keeps the rest for reuse, and counts the outcome.
func (a *RegisterArena) releaseRevokedSlabs() {
	state := &a.licenceState
	var tally licenceReleaseTally
	if len(state.revoked) != 0 {
		a.dropRevokedSlabs(&tally)
		a.publishLicenceSlabs()
	} else {
		tally.recycled = int64(a.countDataSlabs())
	}
	state.releasedRevision = state.revision
	if licenceCounting {
		a.stats().slabsAbandoned.Add(tally.abandoned)
		a.stats().slabsRecycled.Add(tally.recycled)
	}
	if !licenceCounting {
		return
	}
	if tally.abandoned == 0 {
		a.stats().releasesClean.Add(1)
		return
	}
	a.stats().releasesPartial.Add(1)
	for cause, contributed := range tally.causes {
		if contributed {
			a.stats().partialByCause[cause].Add(1)
		}
	}
}

// detachRevoked abandons every data slab of a revoked epoch, plus the register banks when
// one was referenced.
//
// Takes tally (*licenceReleaseTally) which counts the outcome.
func (a *RegisterArena) detachRevoked(tally *licenceReleaseTally) {
	state := &a.licenceState
	a.closeOverRevoked()
	if state.epochRevoked {
		tally.abandoned += int64(a.countDataSlabs())
		a.abandonEscapedSlabsLazily()
		if state.banksRevoked {
			a.abandonRegisterBanks()
			state.wholeArenaDetached = true
		}
	} else if len(state.revoked) != 0 {
		a.dropRevokedSlabs(tally)
	}
	state.releasedRevision = state.revision
}

// sealHoldsThroughReset reports whether Reset must retain the seal's lock while clearing
// the arena.
//
// The lock is unnecessary only when release left the whole arena to the collector and
// nothing has been revoked since. Reset then clears only storage no value can name.
//
// Returns bool which is true when Reset must defer unlockForeign.
func (a *RegisterArena) sealHoldsThroughReset() bool {
	state := &a.licenceState
	return !state.wholeArenaDetached || state.revision != state.releasedRevision
}

// sealLicenceRelease reapplies late hand-offs, foreign revocations and blind hand-offs
// that reached the arena after releaseLicence detached its storage.
//
// Returns bool which is true when the caller holds the lock and must release it.
func (a *RegisterArena) sealLicenceRelease() bool {
	state := &a.licenceState
	locked := !state.wholeArenaDetached || state.revision != state.releasedRevision
	if locked {
		a.lockForeign()
		a.applyBlindHandOffs()
		a.adoptForeignRevocationsLocked()
	} else {
		a.applyBlindHandOffs()
		a.adoptForeignRevocations()
	}
	if state.revision != state.releasedRevision {
		var tally licenceReleaseTally
		a.detachRevoked(&tally)
		if licenceCounting {
			a.stats().lateDetaches.Add(1)
			a.stats().lateSlabsAbandoned.Add(tally.abandoned)
		}
	}
	return locked
}

// releaseRevokedEpoch abandons every data slab of a revoked epoch, and the register banks
// when one of them was referenced.
func (a *RegisterArena) releaseRevokedEpoch() {
	state := &a.licenceState
	if licenceCounting {
		stats := a.stats()
		stats.slabsAbandoned.Add(int64(a.countDataSlabs()))
		stats.releasesEpoch.Add(1)
		stats.epochByCause[state.epochCause].Add(1)
		stats.epochByReason[state.epochReason].Add(1)
	}
	state.regrow.record(a)
	a.abandonEscapedSlabsLazily()
	if state.banksRevoked {
		a.abandonRegisterBanks()
		state.wholeArenaDetached = true
	}
}

// abandonEscapedSlabsLazily is abandonEscapedSlabs for a licensed arena's revoked epoch.
//
// It leaves every data slab and the upvalue slabs to the collector exactly as that does,
// but installs no replacement.
//
// The next epoch's sizing (ensureCapacity) or first use allocates each one once, at the
// hinted size or, from empty, at least the initial size, rather than eagerly at the
// initial size and then again at the hint.
func (a *RegisterArena) abandonEscapedSlabsLazily() {
	for _, slab := range a.oldByteSlabs {
		forgetArenaSlab(slab)
	}
	a.oldByteSlabs = nil
	a.replaceByteSlab(nil, false)
	a.byteIndex = 0
	a.dropMutByteSlabs()
	a.krefDropAll()
	a.genericBytesSlab = nil
	a.oldGenericByteSlabs = nil
	a.genericBytesIndex = 0
	a.freeGenericByteChunks = nil
	a.sliceHeaderSlab = nil
	a.oldSliceHeaderSlabs = nil
	a.sliceHeaderIndex = 0
	a.freeSliceHeaderChunks = nil
	a.intBackingSlab, a.oldIntBackings, a.intBackingIndex = nil, nil, 0
	a.floatBackingSlab, a.oldFloatBackings, a.floatBackingIndex = nil, nil, 0
	a.stringBackingSlab, a.oldStringBackings, a.stringBackingIndex = nil, nil, 0
	a.boolBackingSlab, a.oldBoolBackings, a.boolBackingIndex = nil, nil, 0
	a.uintBackingSlab, a.oldUintBackings, a.uintBackingIndex = nil, nil, 0
	a.abandonBoxSlabs()
	a.upvalueCellSlab = nil
	a.UpvalueCellIndex = 0
	a.upvalueReferenceSlab = nil
	a.UpvalueReferenceIndex = 0
	a.abandonClosureChunks()
	a.slabRetiredSinceReset = noSlabUsage
	a.slabGeneration++
}

// record reads the arena's use of each data and upvalue slab kind in the ending epoch.
//
// Takes arena (*RegisterArena) which is about to abandon its slabs.
func (r *regrowRecord) record(arena *RegisterArena) {
	if arena.byteSlab == nil && arena.upvalueCellSlab == nil && arena.intBackingSlab == nil && arena.sliceHeaderSlab == nil &&
		(arena.genericBytesSlab == nil && arena.floatBackingSlab == nil && arena.stringBackingSlab == nil &&
			arena.boolBackingSlab == nil && arena.uintBackingSlab == nil && arena.upvalueReferenceSlab == nil) {
		return
	}
	retired := arena.slabRetiredSinceReset
	r.pending = true
	r.recordedFor = r.sized
	r.used = program.TypedSlabCounts{
		Bytes:             arena.byteIndex + retired.bytes,
		IntBacking:        arena.intBackingIndex + retiredLength(arena.oldIntBackings),
		FloatBacking:      arena.floatBackingIndex + retiredLength(arena.oldFloatBackings),
		StringBacking:     arena.stringBackingIndex + retiredLength(arena.oldStringBackings),
		BoolBacking:       arena.boolBackingIndex + retiredLength(arena.oldBoolBackings),
		UintBacking:       arena.uintBackingIndex + retiredLength(arena.oldUintBackings),
		GenericBytes:      arena.genericBytesIndex + retired.genericBytes,
		SliceHeaders:      arena.sliceHeaderIndex + retired.sliceHeaders,
		IntBoxes:          arena.intBoxIndex + retired.intBoxes,
		UintBoxes:         arena.uintBoxIndex + retired.uintBoxes,
		FloatBoxes:        arena.floatBoxIndex + retired.floatBoxes,
		StringBoxes:       arena.stringBoxIndex + retired.stringBoxes,
		ComplexBoxes:      arena.complexBoxIndex + retired.complexBoxes,
		UpvalueCells:      arena.UpvalueCellIndex,
		UpvalueReferences: arena.UpvalueReferenceIndex,
		Ints:              0,
		Floats:            0,
		Strings:           0,
		Generals:          0,
		Bools:             0,
		Uints:             0,
		Complexes:         0,
		SlicesInts:        0,
		SlicesFloats:      0,
		SlicesStrings:     0,
		SlicesBools:       0,
		SlicesUints:       0,
		SlicesBytes:       0,
		FrameStack:        0,
	}
}

// apply notes counts as the arena's sizing and, when a record is pending for that same
// sizing, caps the data and upvalue kinds of counts at twice the recorded use; it ends
// the record either way.
//
// Takes counts (program.TypedSlabCounts) which is the program's sizing.
//
// Returns program.TypedSlabCounts which is counts with those kinds capped.
func (r *regrowRecord) apply(counts program.TypedSlabCounts) program.TypedSlabCounts {
	r.sized = counts
	if !r.pending {
		return counts
	}
	r.pending = false
	if !regrowDataKindsEqual(counts, r.recordedFor) {
		return counts
	}
	used := &r.used
	counts.Bytes = min(counts.Bytes, 2*used.Bytes)
	counts.IntBacking = min(counts.IntBacking, 2*used.IntBacking)
	counts.FloatBacking = min(counts.FloatBacking, 2*used.FloatBacking)
	counts.StringBacking = min(counts.StringBacking, 2*used.StringBacking)
	counts.BoolBacking = min(counts.BoolBacking, 2*used.BoolBacking)
	counts.UintBacking = min(counts.UintBacking, 2*used.UintBacking)
	counts.GenericBytes = min(counts.GenericBytes, 2*used.GenericBytes)
	counts.SliceHeaders = min(counts.SliceHeaders, 2*used.SliceHeaders)
	counts.IntBoxes = min(counts.IntBoxes, 2*used.IntBoxes)
	counts.UintBoxes = min(counts.UintBoxes, 2*used.UintBoxes)
	counts.FloatBoxes = min(counts.FloatBoxes, 2*used.FloatBoxes)
	counts.StringBoxes = min(counts.StringBoxes, 2*used.StringBoxes)
	counts.ComplexBoxes = min(counts.ComplexBoxes, 2*used.ComplexBoxes)
	counts.UpvalueCells = min(counts.UpvalueCells, 2*used.UpvalueCells)
	counts.UpvalueReferences = min(counts.UpvalueReferences, 2*used.UpvalueReferences)
	return counts
}

// regrowDataKindsEqual reports whether the data and upvalue sizing hints used by the
// regrowth caps match.
//
// Takes left (program.TypedSlabCounts) which is the first slab sizing.
// Takes right (program.TypedSlabCounts) which is the second slab sizing.
//
// Returns bool which is true when the data and upvalue kinds agree.
func regrowDataKindsEqual(left, right program.TypedSlabCounts) bool {
	return left.Bytes == right.Bytes && left.IntBacking == right.IntBacking && left.FloatBacking == right.FloatBacking &&
		left.StringBacking == right.StringBacking && left.BoolBacking == right.BoolBacking && left.UintBacking == right.UintBacking &&
		left.GenericBytes == right.GenericBytes && left.SliceHeaders == right.SliceHeaders && left.IntBoxes == right.IntBoxes &&
		left.UintBoxes == right.UintBoxes && left.FloatBoxes == right.FloatBoxes && left.StringBoxes == right.StringBoxes &&
		left.ComplexBoxes == right.ComplexBoxes && left.UpvalueCells == right.UpvalueCells && left.UpvalueReferences == right.UpvalueReferences
}

// retiredLength sums the lengths of a retention list's slabs.
//
// Takes list ([][]T) which is the list.
//
// Returns int which is the sum.
func retiredLength[T any](list [][]T) int {
	total := 0
	for _, slab := range list {
		total += len(slab)
	}
	return total
}

// regrowFloor returns the minimum growth size for an empty slab in a licensed arena.
//
// It uses the initial size that eager replacement would have allocated in
// abandonEscapedSlabsLazily. Other arenas and non-empty slabs follow the usual growth
// rule.
//
// Takes current (int) which is the slab's length.
// Takes initial (int) which is the slab kind's initial size.
//
// Returns int which is initial for an empty slab of a licensed arena, else zero.
func (a *RegisterArena) regrowFloor(current, initial int) int {
	if a.licensed && current == 0 {
		return initial
	}
	return 0
}

// countDataSlabs counts the non-empty data slabs, current and retired, a release would
// hand to the next owner.
//
// Returns int which is the count.
func (a *RegisterArena) countDataSlabs() int {
	count := 0
	add := func(capacity int) {
		if capacity != 0 {
			count++
		}
	}
	add(cap(a.byteSlab))
	add(cap(a.mutByteSlab))
	add(cap(a.genericBytesSlab))
	add(cap(a.sliceHeaderSlab))
	add(cap(a.intBackingSlab))
	add(cap(a.floatBackingSlab))
	add(cap(a.stringBackingSlab))
	add(cap(a.boolBackingSlab))
	add(cap(a.uintBackingSlab))
	add(cap(a.stringBoxSlab))
	add(cap(a.intBoxSlab))
	add(cap(a.floatBoxSlab))
	add(cap(a.uintBoxSlab))
	add(cap(a.complexBoxSlab))
	return count + a.krefChunkCount() + len(a.oldByteSlabs) + len(a.oldMutByteSlabs) + len(a.oldGenericByteSlabs) + len(a.freeGenericByteChunks) +
		len(a.oldSliceHeaderSlabs) + len(a.freeSliceHeaderChunks) + len(a.oldIntBackings) +
		len(a.oldFloatBackings) + len(a.oldStringBackings) + len(a.oldBoolBackings) + len(a.oldUintBackings) +
		a.closureChunkCount()
}

// abandonRegisterBanks replaces every register bank with a fresh one of the same length
// and clears the frame slots' references to the old ones.
func (a *RegisterArena) abandonRegisterBanks() {
	a.intSlab = make([]int64, len(a.intSlab))
	a.floatSlab = make([]float64, len(a.floatSlab))
	a.stringSlab = make([]string, len(a.stringSlab))
	a.generalSlab = make([]reflect.Value, len(a.generalSlab))
	a.boolSlab = make([]bool, len(a.boolSlab))
	a.uintSlab = make([]uint64, len(a.uintSlab))
	a.complexSlab = make([]complex128, len(a.complexSlab))
	a.slicesIntSlab = make([][]int64, len(a.slicesIntSlab))
	a.slicesFloatSlab = make([][]float64, len(a.slicesFloatSlab))
	a.slicesStringSlab = make([][]string, len(a.slicesStringSlab))
	a.slicesBoolSlab = make([][]bool, len(a.slicesBoolSlab))
	a.slicesUintSlab = make([][]uint64, len(a.slicesUintSlab))
	a.slicesByteSlab = make([][]byte, len(a.slicesByteSlab))
	a.upvalueCellSlab = make([]program.UpvalueCell, len(a.upvalueCellSlab))
	a.slabGeneration++
	for index := range a.frameSlab {
		frame := &a.frameSlab[index]
		frame.Registers = Registers{}
		frame.upvalues = nil
	}
}

// releaseAbortedRegisterArena releases an arena an abort interrupted without pooling it.
//
// Takes arena (*RegisterArena) which is the aborted arena.
func releaseAbortedRegisterArena(arena *RegisterArena) {
	if arena.licensed {
		arena.releaseLicence()
	}
	if arenaAuditEnabled {
		arena.auditRunEnded()
	}
	arena.detachBudgetTracker()
	arena.Reset()
	abandonUnpooledRegisterArena(arena)
}
