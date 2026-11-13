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
	"cmp"
	"slices"
	"unsafe"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/engine/program"
)

// keptBackingSlabBudgetShare divides the run budget into the retained-slab allowance.
const keptBackingSlabBudgetShare = 8

const (
	// typedBackingInt names the int64 typed backing slab.
	typedBackingInt = iota

	// typedBackingFloat names the float64 typed backing slab.
	typedBackingFloat

	// typedBackingString names the string typed backing slab.
	typedBackingString

	// typedBackingBool names the bool typed backing slab.
	typedBackingBool

	// typedBackingUint names the uint64 typed backing slab.
	typedBackingUint

	// typedBackingSlabCount is the number of typed backing slabs.
	typedBackingSlabCount
)

// shrinkOvergrownSlabs replaces any slab that has grown beyond the DoS protection
// threshold with a fresh default-sized allocation, once it is idle.
//
// Takes bankPeak (*arenaIndices) which is the register banks' peak use in the run just
// ended (frameBankPeak).
func (a *RegisterArena) shrinkOvergrownSlabs(bankPeak *arenaIndices) {
	a.shrinkOvergrownScalarSlabs()
	a.shrinkOvergrownSliceSlabs(bankPeak)
	a.shrinkOvergrownFrameSlabs()
	a.shrinkOvergrownBackingSlabs()
}

// shrinkOvergrownSliceSlabs replaces typed-slice banks (int, float, string, bool, uint
// and byte) that grew past the threshold with fresh default-sized allocations when the
// run just ended used no more than a quarter of them (sliceBankIdle).
//
// Takes peak (*arenaIndices) which is the banks' peak use in the run just ended.
func (a *RegisterArena) shrinkOvergrownSliceSlabs(peak *arenaIndices) {
	if sliceBankIdle(len(a.slicesIntSlab), initialSlicesIntSlabs, peak.SlicesIntIndex) {
		a.slicesIntSlab = make([][]int64, initialSlicesIntSlabs)
	}
	if sliceBankIdle(len(a.slicesFloatSlab), initialSlicesFloatSlabs, peak.SlicesFloatIndex) {
		a.slicesFloatSlab = make([][]float64, initialSlicesFloatSlabs)
	}
	if sliceBankIdle(len(a.slicesStringSlab), initialSlicesStringSlabs, peak.SlicesStringIndex) {
		a.slicesStringSlab = make([][]string, initialSlicesStringSlabs)
	}
	if sliceBankIdle(len(a.slicesBoolSlab), initialSlicesBoolSlabs, peak.SlicesBoolIndex) {
		a.slicesBoolSlab = make([][]bool, initialSlicesBoolSlabs)
	}
	if sliceBankIdle(len(a.slicesUintSlab), initialSlicesUintSlabs, peak.SlicesUintIndex) {
		a.slicesUintSlab = make([][]uint64, initialSlicesUintSlabs)
	}
	if sliceBankIdle(len(a.slicesByteSlab), initialSlicesByteSlabs, peak.SlicesByteIndex) {
		a.slicesByteSlab = make([][]byte, initialSlicesByteSlabs)
		a.slabGeneration++
	}
}

// shrinkOvergrownFrameSlabs replaces frame-adjacent slabs (frames, call-info bases,
// dispatch saves, upvalue cells and refs) that grew past the threshold.
func (a *RegisterArena) shrinkOvergrownFrameSlabs() {
	a.shrinkFrameStackWithHysteresis()
	if len(a.upvalueCellSlab) > initialUpvalueCellSlabs*maxArenaMultiplier {
		a.upvalueCellSlab = make([]program.UpvalueCell, initialUpvalueCellSlabs)
	}
	if len(a.upvalueReferenceSlab) > initialUpvalueRefSlabs*maxArenaMultiplier {
		a.upvalueReferenceSlab = make([]upvalue, initialUpvalueRefSlabs)
	}
}

// shrinkOvergrownBoxSlabs shrinks any per-type box slab that has grown past its DoS
// threshold and then sat idle across several Resets.
func (a *RegisterArena) shrinkOvergrownBoxSlabs() {
	a.stringBoxSlab = shrinkSlabWithHysteresis(a.stringBoxSlab, initialBoxSlabCapacity, a.slabUsageLastReset.stringBoxes, &a.slabIdleResets.stringBoxes)
	a.intBoxSlab = shrinkSlabWithHysteresis(a.intBoxSlab, initialBoxSlabCapacity, a.slabUsageLastReset.intBoxes, &a.slabIdleResets.intBoxes)
	a.floatBoxSlab = shrinkSlabWithHysteresis(a.floatBoxSlab, initialBoxSlabCapacity, a.slabUsageLastReset.floatBoxes, &a.slabIdleResets.floatBoxes)
	a.uintBoxSlab = shrinkSlabWithHysteresis(a.uintBoxSlab, initialBoxSlabCapacity, a.slabUsageLastReset.uintBoxes, &a.slabIdleResets.uintBoxes)
	a.complexBoxSlab = shrinkSlabWithHysteresis(a.complexBoxSlab, initialBoxSlabCapacity, a.slabUsageLastReset.complexBoxes, &a.slabIdleResets.complexBoxes)
}

// shrinkSlabWithHysteresis returns a fresh default-capacity slab when the current one is
// overgrown and idle.
//
// Takes slab ([]T) which is the slab to consider.
// Takes initial (int) which is its default capacity.
// Takes usedLastExecute (int) which is the previous Execute's allocation volume in the
// slab's native units.
// Takes idleResets (*uint32) which is the class's consecutive idle counter.
//
// Returns the shrunk slab, or slab unchanged.
func shrinkSlabWithHysteresis[T any](slab []T, initial int, usedLastExecute int, idleResets *uint32) []T {
	if len(slab) <= initial*maxArenaMultiplier {
		*idleResets = 0
		return slab
	}
	if usedLastExecute > len(slab)/backingSlabLowUseRatioDenominator {
		*idleResets = 0
		return slab
	}
	*idleResets++
	if *idleResets < backingSlabIdleResetsBeforeShrink {
		return slab
	}
	*idleResets = 0
	return make([]T, initial)
}

// sliceBankIdle reports whether a typed-slice bank has grown past the threshold and the
// run just ended used no more than len/backingSlabLowUseRatioDenominator of it.
//
// Takes length (int) which is the bank's length.
// Takes initial (int) which is its default length.
// Takes used (int) which is its peak use in the run just ended.
//
// Returns bool which is true when the bank is to be replaced.
func sliceBankIdle(length, initial, used int) bool {
	return length > initial*maxArenaMultiplier && used <= length/backingSlabLowUseRatioDenominator
}

// recordUsageAtReset notes how much of the frame stack, the slabs and the register banks
// the arena used since the last Reset, which sizing and shrinking read, and starts
// counting afresh.
//
// Returns arenaIndices which is the register banks' peak use.
func (a *RegisterArena) recordUsageAtReset() arenaIndices {
	a.framesUsed = a.frameStackPeak()
	a.frameStackPeakLastReset = a.framesUsed
	bankPeak := a.frameBankPeak(a.framesUsed)
	a.byteSlabPeakIndexLastReset = a.byteIndex + a.slabRetiredSinceReset.bytes
	a.slabUsageLastReset = slabUsageSnapshot{genericBytes: a.genericBytesIndex + a.slabRetiredSinceReset.genericBytes,
		sliceHeaders: a.sliceHeaderIndex + a.slabRetiredSinceReset.sliceHeaders,
		stringBoxes:  a.stringBoxIndex + a.slabRetiredSinceReset.stringBoxes,
		intBoxes:     a.intBoxIndex + a.slabRetiredSinceReset.intBoxes,
		floatBoxes:   a.floatBoxIndex + a.slabRetiredSinceReset.floatBoxes,
		uintBoxes:    a.uintBoxIndex + a.slabRetiredSinceReset.uintBoxes,
		complexBoxes: a.complexBoxIndex + a.slabRetiredSinceReset.complexBoxes, bytes: 0,
		mutBytes:       a.mutByteIndex + a.slabRetiredSinceReset.mutBytes,
		intBackings:    a.intBackingIndex + a.slabRetiredSinceReset.intBackings,
		floatBackings:  a.floatBackingIndex + a.slabRetiredSinceReset.floatBackings,
		stringBackings: a.stringBackingIndex + a.slabRetiredSinceReset.stringBackings,
		boolBackings:   a.boolBackingIndex + a.slabRetiredSinceReset.boolBackings,
		uintBackings:   a.uintBackingIndex + a.slabRetiredSinceReset.uintBackings}
	a.slabRetiredSinceReset = noSlabUsage
	a.scalarDemandSinceReset.noteIndices(&bankPeak)
	a.scalarDemandLastReset = a.scalarDemandSinceReset
	a.scalarDemandSinceReset = scalarBankDemand{ints: 0, floats: 0, strings: 0, generals: 0, bools: 0, uints: 0, complexes: 0}
	return bankPeak
}

// shrinkOvergrownBackingSlabs replaces backing slabs that exceed the reuse threshold.
func (a *RegisterArena) shrinkOvergrownBackingSlabs() {
	if shrunk := shrinkSlabWithHysteresis(a.byteSlab, InitialByteSlabSize, a.byteSlabPeakIndexLastReset, &a.byteSlabIdleResetCount); unsafe.SliceData(shrunk) != unsafe.SliceData(a.byteSlab) {
		a.replaceByteSlab(shrunk, false)
	}
	usage := &a.slabUsageLastReset
	if sliceBankIdle(len(a.intBackingSlab), InitialIntBackingSize, usage.intBackings) {
		a.intBackingSlab = make([]int64, InitialIntBackingSize)
	}
	if sliceBankIdle(len(a.floatBackingSlab), initialFloatBackingSize, usage.floatBackings) {
		a.floatBackingSlab = make([]float64, initialFloatBackingSize)
	}
	if sliceBankIdle(len(a.stringBackingSlab), initialStringBackingSize, usage.stringBackings) {
		a.stringBackingSlab = make([]string, initialStringBackingSize)
	}
	if sliceBankIdle(len(a.boolBackingSlab), initialBoolBackingSize, usage.boolBackings) {
		a.boolBackingSlab = make([]bool, initialBoolBackingSize)
	}
	if sliceBankIdle(len(a.uintBackingSlab), initialUintBackingSize, usage.uintBackings) {
		a.uintBackingSlab = make([]uint64, initialUintBackingSize)
	}
	a.shrinkOvergrownBoxSlabs()
	a.genericBytesSlab = shrinkSlabWithHysteresis(a.genericBytesSlab, initialGenericBytesCapacity, a.slabUsageLastReset.genericBytes, &a.slabIdleResets.genericBytes)
	a.sliceHeaderSlab = shrinkSlabWithHysteresis(a.sliceHeaderSlab, initialSliceHeaderCapacity, a.slabUsageLastReset.sliceHeaders, &a.slabIdleResets.sliceHeaders)
	a.mutByteSlab = shrinkSlabWithHysteresis(a.mutByteSlab, initialMutByteSlabSize, a.slabUsageLastReset.mutBytes, &a.slabIdleResets.mutBytes)
}

// clearUsedBackingSlabs zeroes the part of each typed backing slab used since Reset.
func (a *RegisterArena) clearUsedBackingSlabs() {
	clear(a.intBackingSlab[:min(a.intBackingIndex, len(a.intBackingSlab))])
	clear(a.floatBackingSlab[:min(a.floatBackingIndex, len(a.floatBackingSlab))])
	clear(a.stringBackingSlab[:min(a.stringBackingIndex, len(a.stringBackingSlab))])
	clear(a.boolBackingSlab[:min(a.boolBackingIndex, len(a.boolBackingSlab))])
	clear(a.uintBackingSlab[:min(a.uintBackingIndex, len(a.uintBackingSlab))])
}

// setRunBudget sets the arena's byte budget for the run about to start, bounds the typed
// backing slabs kept from the last run and charges the run for them.
//
// A slab that has handed out memory is never replaced, because the run may still read it.
// A later call, such as a cached callback arena's per-call limits, charges nothing again.
//
// Takes maxArenaBytes (uint64) which is the run's budget, zero for the default.
func (a *RegisterArena) setRunBudget(maxArenaBytes uint64) {
	a.MaxArenaBytes = maxArenaBytes
	share := a.arenaBudgetLimit() / keptBackingSlabBudgetShare
	var order [typedBackingSlabCount]typedBackingSlab
	for slab := range order {
		order[slab] = a.typedBackingSlabState(slab)
	}
	slices.SortFunc(order[:], func(left, right typedBackingSlab) int { return cmp.Compare(right.bytes, left.bytes) })
	var kept, excess uint64
	replaced := false
	for _, slab := range order {
		if slab.excess == 0 {
			continue
		}
		if slab.inUse || kept+slab.bytes <= share {
			kept += slab.bytes
			excess += slab.excess
			continue
		}
		a.replaceTypedBackingSlab(slab.which)
		replaced = true
	}
	if a.keptBackingsUncharged {
		a.keptBackingsUncharged = false
		chunkBytes, dropped := a.keepKRefChunksWithin(share - min(kept, share))
		excess += chunkBytes
		replaced = replaced || dropped
		a.totalAllocatedBytes += excess
		a.mirrorBudgetDelta(safeconv.Uint64ToInt64(excess))
	}
	if replaced && a.licensed {
		a.publishLicenceSlabs()
	}
}

// keepKRefChunksWithin keeps the KRef chunks Reset kept when they fit room, and drops
// them all otherwise. It runs only straight after Reset, when no chunk holds a value.
//
// Takes room (uint64) which is the part of the kept-slab share the typed backing slabs
// left.
//
// Returns kept (uint64) which is the bytes kept, to be charged to the run.
// Returns dropped (bool) which is true when chunks were dropped.
func (a *RegisterArena) keepKRefChunksWithin(room uint64) (kept uint64, dropped bool) {
	held := a.krefHeldBytes()
	if held <= room {
		return held, false
	}
	a.krefDropAll()
	return 0, true
}

// typedBackingSlab describes one typed backing slab for setRunBudget.
type typedBackingSlab struct {
	// which names the slab, one of the typedBacking constants.
	which int

	// bytes is the slab's byte capacity.
	bytes uint64

	// excess is the byte capacity beyond the slab's initial size.
	excess uint64

	// inUse is true when the slab has handed out memory since the last Reset.
	inUse bool
}

// typedBackingSlabState describes the typed backing slab which names.
//
// Takes which (int) which names the slab, one of the typedBacking constants.
//
// Returns typedBackingSlab which describes it.
func (a *RegisterArena) typedBackingSlabState(which int) typedBackingSlab {
	switch which {
	case typedBackingInt:
		return describeTypedBackingSlab(which, len(a.intBackingSlab), InitialIntBackingSize, arenaSizeInt64, a.intBackingIndex)
	case typedBackingFloat:
		return describeTypedBackingSlab(which, len(a.floatBackingSlab), initialFloatBackingSize, arenaSizeFloat64, a.floatBackingIndex)
	case typedBackingString:
		return describeTypedBackingSlab(which, len(a.stringBackingSlab), initialStringBackingSize, arenaSizeString, a.stringBackingIndex)
	case typedBackingBool:
		return describeTypedBackingSlab(which, len(a.boolBackingSlab), initialBoolBackingSize, arenaSizeBool, a.boolBackingIndex)
	case typedBackingUint:
		return describeTypedBackingSlab(which, len(a.uintBackingSlab), initialUintBackingSize, arenaSizeUint64, a.uintBackingIndex)
	default:
		return typedBackingSlab{which: which, bytes: 0, excess: 0, inUse: false}
	}
}

// describeTypedBackingSlab builds the description of one typed backing slab.
//
// Takes which (int) which names the slab.
// Takes length (int) which is its length in elements.
// Takes initial (int) which is its initial length.
// Takes elementBytes (uint64) which is the size of one element.
// Takes index (int) which is its bump index.
//
// Returns typedBackingSlab which describes it.
func describeTypedBackingSlab(which, length, initial int, elementBytes uint64, index int) typedBackingSlab {
	excess := uint64(0)
	if length > initial {
		excess = safeconv.IntToUint64(length-initial) * elementBytes
	}
	return typedBackingSlab{which: which, bytes: safeconv.IntToUint64(length) * elementBytes, excess: excess, inUse: index != 0}
}

// replaceTypedBackingSlab replaces the typed backing slab which names with one of its
// initial size. The caller has checked that it has handed out no memory.
//
// Takes which (int) which names the slab.
func (a *RegisterArena) replaceTypedBackingSlab(which int) {
	switch which {
	case typedBackingInt:
		a.intBackingSlab = make([]int64, InitialIntBackingSize)
	case typedBackingFloat:
		a.floatBackingSlab = make([]float64, initialFloatBackingSize)
	case typedBackingString:
		a.stringBackingSlab = make([]string, initialStringBackingSize)
	case typedBackingBool:
		a.boolBackingSlab = make([]bool, initialBoolBackingSize)
	case typedBackingUint:
		a.uintBackingSlab = make([]uint64, initialUintBackingSize)
	default:
	}
}
