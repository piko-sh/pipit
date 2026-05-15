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
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// arenaSizeInt64 is the byte size of a single int64 backing slot.
	arenaSizeInt64 uint64 = uint64(unsafe.Sizeof(int64(0)))

	// arenaSizeFloat64 is the byte size of a single float64 backing slot.
	arenaSizeFloat64 uint64 = uint64(unsafe.Sizeof(float64(0)))

	// arenaSizeString is the byte size of a single string header.
	arenaSizeString uint64 = uint64(unsafe.Sizeof(""))

	// arenaSizeBool is the byte size of a single bool backing slot.
	arenaSizeBool uint64 = uint64(unsafe.Sizeof(false))

	// arenaSizeUint64 is the byte size of a single uint64 backing slot.
	arenaSizeUint64 uint64 = uint64(unsafe.Sizeof(uint64(0)))

	// arenaSizeSliceHeader is the byte size of a single arenaSliceHeader.
	arenaSizeSliceHeader uint64 = uint64(unsafe.Sizeof(arenaSliceHeader{Data: nil, Len: 0, Cap: 0}))

	// arenaSizeStringBox is the byte size of a single stringBoxSlab slot (a string header).
	arenaSizeStringBox uint64 = uint64(unsafe.Sizeof(""))

	// arenaSizeScalarBox is the byte size of a single int64/uint64/float64 box slot.
	arenaSizeScalarBox uint64 = uint64(unsafe.Sizeof(int64(0)))

	// arenaSizeComplexBox is the byte size of a single complexBoxSlab slot.
	arenaSizeComplexBox uint64 = uint64(unsafe.Sizeof(complex128(0)))
)

// ChargeArenaAllocation updates the live-bytes counter with the delta between old and new
// slab capacity. When the total exceeds the budget it drives a MinorGC to reclaim dead
// slabs, and panics with errArenaBudgetExceeded only if the total still exceeds it.
//
// Takes newCapacityBytes (uint64) which is the byte capacity of the new slab.
// Takes oldCapacityBytes (uint64) which is the byte capacity of the replaced slab.
func (a *RegisterArena) ChargeArenaAllocation(newCapacityBytes, oldCapacityBytes uint64) {
	budget := a.arenaBudgetLimit()
	if newCapacityBytes <= oldCapacityBytes {
		a.releaseArenaBytes(oldCapacityBytes - newCapacityBytes)
		return
	}
	delta := newCapacityBytes - oldCapacityBytes
	if a.totalAllocatedBytes+delta < a.totalAllocatedBytes {
		panic(fmt.Errorf("arena: overflow charging %d bytes: %w", delta, fault.ErrArenaBudgetExceeded))
	}
	a.totalAllocatedBytes += delta
	a.mirrorBudgetDelta(safeconv.Uint64ToInt64(delta))
	if a.sharedWorkingSetBytes() <= budget {
		return
	}
	if a.ownerVM != nil && !a.disableMinorGC {
		a.MinorGC(a.ownerVM)
		if a.sharedWorkingSetBytes() <= budget {
			return
		}
	}
	panic(fmt.Errorf("arena: %d bytes exceeds budget %d: %w", a.sharedWorkingSetBytes(), budget, fault.ErrArenaBudgetExceeded))
}

// GrowByteSlab allocates a new byte slab, preserving the old one in oldByteSlabs so that
// existing strings pointing into it remain valid.
//
// Takes minExtra (int) which is the minimum number of bytes the new slab must hold.
//
//go:noinline
func (a *RegisterArena) GrowByteSlab(minExtra int) {
	oldCap := uint64(cap(a.byteSlab))
	a.slabRetiredSinceReset.bytes += len(a.byteSlab)
	a.oldByteSlabs = append(a.oldByteSlabs, a.byteSlab)
	trimRetainedSlabs(&a.oldByteSlabs)
	newSize := max(len(a.byteSlab)*2, minExtra)
	a.byteSlab = make([]byte, newSize)
	a.byteIndex = 0
	a.ChargeArenaAllocation(uint64(cap(a.byteSlab)), oldCap)
}

// GrowIntBackingSlab grows the int64 backing slab.
//
// The retired slab is kept in oldIntBackings so live []int64 slices the user holds (e.g.
// from prior make/append calls) remain valid.
//
// Takes minExtra (int) which is the minimum number of elements the new slab must hold.
//
//go:noinline
func (a *RegisterArena) GrowIntBackingSlab(minExtra int) {
	oldCap := uint64(cap(a.intBackingSlab)) * arenaSizeInt64
	a.oldIntBackings = append(a.oldIntBackings, a.intBackingSlab)
	trimRetainedSlabs(&a.oldIntBackings)
	newSize := max(len(a.intBackingSlab)*2, minExtra)
	a.intBackingSlab = make([]int64, newSize)
	a.intBackingIndex = 0
	a.ChargeArenaAllocation(uint64(cap(a.intBackingSlab))*arenaSizeInt64, oldCap)
}

// arenaBudgetLimit returns the effective per-Execute byte budget for this arena. Zero on
// the arena field selects defaultMaxArenaBytes.
//
// Returns the budget in bytes.
func (a *RegisterArena) arenaBudgetLimit() uint64 {
	if a.MaxArenaBytes == 0 {
		return defaultMaxArenaBytes
	}
	return a.MaxArenaBytes
}

// enforceSingleAllocLimit panics with errAllocationLimit when a single backing allocation
// exceeds MaxAllocSize. This closes the ASM trampoline path, which bypasses the
// make-slice pre-check.
//
// Takes elementCount (int) which is the requested element count for a single allocation.
func (a *RegisterArena) enforceSingleAllocLimit(elementCount int) {
	if a.MaxAllocSize > 0 && elementCount > a.MaxAllocSize {
		panic(fmt.Errorf("arena: single allocation of %d elements exceeds limit %d: %w",
			elementCount, a.MaxAllocSize, fault.ErrAllocationLimit))
	}
}

// growStringBoxSlab doubles stringBoxSlab capacity. The retired slab needs no retention
// list because reflect.Values that still point into it keep it alive.
func (a *RegisterArena) growStringBoxSlab() {
	growBoxSlab(a, &a.stringBoxSlab, &a.stringBoxIndex, &a.slabRetiredSinceReset.stringBoxes, arenaSizeStringBox)
}

// growIntBoxSlab doubles intBoxSlab capacity (see growStringBoxSlab).
func (a *RegisterArena) growIntBoxSlab() {
	growBoxSlab(a, &a.intBoxSlab, &a.intBoxIndex, &a.slabRetiredSinceReset.intBoxes, arenaSizeScalarBox)
}

// growFloatBoxSlab doubles floatBoxSlab capacity (see growStringBoxSlab).
func (a *RegisterArena) growFloatBoxSlab() {
	growBoxSlab(a, &a.floatBoxSlab, &a.floatBoxIndex, &a.slabRetiredSinceReset.floatBoxes, arenaSizeScalarBox)
}

// growUintBoxSlab doubles uintBoxSlab capacity (see growStringBoxSlab).
func (a *RegisterArena) growUintBoxSlab() {
	growBoxSlab(a, &a.uintBoxSlab, &a.uintBoxIndex, &a.slabRetiredSinceReset.uintBoxes, arenaSizeScalarBox)
}

// growComplexBoxSlab doubles complexBoxSlab capacity (see growStringBoxSlab).
func (a *RegisterArena) growComplexBoxSlab() {
	growBoxSlab(a, &a.complexBoxSlab, &a.complexBoxIndex, &a.slabRetiredSinceReset.complexBoxes, arenaSizeComplexBox)
}

// growGenericBytesSlab replaces genericBytesSlab with one of at least minCap bytes. The
// retired slab is kept in oldGenericByteSlabs so OwnsBytePointer can still recognise
// pointers into it after a grow.
//
// Takes minCap (int) which is the minimum byte capacity the new slab must satisfy.
func (a *RegisterArena) growGenericBytesSlab(minCap int) {
	oldCap := uint64(cap(a.genericBytesSlab))
	a.slabRetiredSinceReset.genericBytes += len(a.genericBytesSlab)
	if len(a.genericBytesSlab) > 0 {
		a.oldGenericByteSlabs = append(a.oldGenericByteSlabs, a.genericBytesSlab)
		trimRetainedSlabs(&a.oldGenericByteSlabs)
	}
	newCap := chunkedSlabCapacity(len(a.genericBytesSlab), minCap, initialGenericBytesCapacity, genericBytesSlabChunk)
	a.genericBytesIndex = 0
	if recycled, ok := popFreeChunk(&a.freeGenericByteChunks, newCap, genericBytesSlabChunk); ok {
		a.genericBytesSlab = recycled
		return
	}
	a.genericBytesSlab = make([]byte, newCap)
	a.chargeChunkedSlab(uint64(cap(a.genericBytesSlab)), oldCap, genericBytesSlabChunk)
}

// growSliceHeaderSlab replaces sliceHeaderSlab with a larger or chunk-sized slab.
//
// Growth doubles up to sliceHeaderSlabChunk and then installs uniform chunks, taking a
// recycled chunk from freeSliceHeaderChunks when one is available. Appends the retired
// slab to oldSliceHeaderSlabs so reflect.Values whose ptr is an arenaSliceHeader in a
// grown-away slab remain reachable from the arena root. Mirrors the retention pattern
// used by growGenericBytesSlab and GrowByteSlab.
func (a *RegisterArena) growSliceHeaderSlab() {
	oldCap := uint64(cap(a.sliceHeaderSlab)) * arenaSizeSliceHeader
	a.slabRetiredSinceReset.sliceHeaders += len(a.sliceHeaderSlab)
	if len(a.sliceHeaderSlab) > 0 {
		a.oldSliceHeaderSlabs = append(a.oldSliceHeaderSlabs, a.sliceHeaderSlab)
		trimRetainedSlabs(&a.oldSliceHeaderSlabs)
	}
	newCap := chunkedSlabCapacity(len(a.sliceHeaderSlab), 1, initialSliceHeaderCapacity, sliceHeaderSlabChunk)
	a.sliceHeaderIndex = 0
	if recycled, ok := popFreeChunk(&a.freeSliceHeaderChunks, newCap, sliceHeaderSlabChunk); ok {
		a.sliceHeaderSlab = recycled
		return
	}
	a.sliceHeaderSlab = make([]arenaSliceHeader, newCap)
	a.chargeChunkedSlab(uint64(cap(a.sliceHeaderSlab))*arenaSizeSliceHeader, oldCap, sliceHeaderSlabChunk*arenaSizeSliceHeader)
}

// growFrameStack grows the frameSlab, callInfoBasesSlab, and dispatchSavesSlab together
// to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity for the slabs.
//
// Returns the new frame, call-info base, and dispatch save slabs.
//
//go:noinline
func (a *RegisterArena) growFrameStack(minCap int) ([]CallFrame, []uintptr, []asmDispatchSave) {
	a.frameStackGrowths++
	newCap := max(len(a.frameSlab)*2, minCap)

	newFrames := make([]CallFrame, newCap)
	copy(newFrames, a.frameSlab)
	a.frameSlab = newFrames

	newCI := make([]uintptr, newCap)
	copy(newCI, a.callInfoBasesSlab)
	a.callInfoBasesSlab = newCI

	newDisp := make([]asmDispatchSave, newCap)
	copy(newDisp, a.dispatchSavesSlab)
	a.dispatchSavesSlab = newDisp

	return newFrames, newCI, newDisp
}

// growUpvalueCellSlab grows the upvalueCell slab to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity.
//
//go:noinline
func (a *RegisterArena) growUpvalueCellSlab(minCap int) {
	growSlab(a, &a.upvalueCellSlab, minCap, nil, false)
}

// growUpvalueRefSlab grows the upvalue reference slab to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity.
//
//go:noinline
func (a *RegisterArena) growUpvalueRefSlab(minCap int) {
	growSlab(a, &a.upvalueReferenceSlab, minCap, nil, false)
}

// growFloatBackingSlab is the float64 sibling of GrowIntBackingSlab.
//
// Takes minExtra (int) which is the minimum number of elements the new slab must hold.
//
//go:noinline
func (a *RegisterArena) growFloatBackingSlab(minExtra int) {
	oldCap := uint64(cap(a.floatBackingSlab)) * arenaSizeFloat64
	a.oldFloatBackings = append(a.oldFloatBackings, a.floatBackingSlab)
	trimRetainedSlabs(&a.oldFloatBackings)
	newSize := max(len(a.floatBackingSlab)*2, minExtra)
	a.floatBackingSlab = make([]float64, newSize)
	a.floatBackingIndex = 0
	a.ChargeArenaAllocation(uint64(cap(a.floatBackingSlab))*arenaSizeFloat64, oldCap)
}

// growStringBackingSlab is the string sibling of GrowIntBackingSlab.
//
// Takes minExtra (int) which is the minimum number of elements the new slab must hold.
//
//go:noinline
func (a *RegisterArena) growStringBackingSlab(minExtra int) {
	oldCap := uint64(cap(a.stringBackingSlab)) * arenaSizeString
	a.oldStringBackings = append(a.oldStringBackings, a.stringBackingSlab)
	trimRetainedSlabs(&a.oldStringBackings)
	newSize := max(len(a.stringBackingSlab)*2, minExtra)
	a.stringBackingSlab = make([]string, newSize)
	a.stringBackingIndex = 0
	a.ChargeArenaAllocation(uint64(cap(a.stringBackingSlab))*arenaSizeString, oldCap)
}

// growBoolBackingSlab is the bool sibling of GrowIntBackingSlab.
//
// Takes minExtra (int) which is the minimum number of elements the new slab must hold.
//
//go:noinline
func (a *RegisterArena) growBoolBackingSlab(minExtra int) {
	oldCap := uint64(cap(a.boolBackingSlab)) * arenaSizeBool
	a.oldBoolBackings = append(a.oldBoolBackings, a.boolBackingSlab)
	trimRetainedSlabs(&a.oldBoolBackings)
	newSize := max(len(a.boolBackingSlab)*2, minExtra)
	a.boolBackingSlab = make([]bool, newSize)
	a.boolBackingIndex = 0
	a.ChargeArenaAllocation(uint64(cap(a.boolBackingSlab))*arenaSizeBool, oldCap)
}

// growUintBackingSlab is the uint64 sibling of GrowIntBackingSlab.
//
// Takes minExtra (int) which is the minimum number of elements the new slab must hold.
//
//go:noinline
func (a *RegisterArena) growUintBackingSlab(minExtra int) {
	oldCap := uint64(cap(a.uintBackingSlab)) * arenaSizeUint64
	a.oldUintBackings = append(a.oldUintBackings, a.uintBackingSlab)
	trimRetainedSlabs(&a.oldUintBackings)
	newSize := max(len(a.uintBackingSlab)*2, minExtra)
	a.uintBackingSlab = make([]uint64, newSize)
	a.uintBackingIndex = 0
	a.ChargeArenaAllocation(uint64(cap(a.uintBackingSlab))*arenaSizeUint64, oldCap)
}

// growSlabs grows each register bank whose current index plus the requested count exceeds
// its capacity.
//
// Takes counts (TypedSlabCounts) which is the per-bank request sizes.
//
//go:noinline
func (a *RegisterArena) growSlabs(counts program.TypedSlabCounts) {
	if a.IntIndex+counts.Ints > len(a.intSlab) {
		a.growIntSlab(a.IntIndex + counts.Ints)
	}
	if a.FloatIndex+counts.Floats > len(a.floatSlab) {
		a.growFloatSlab(a.FloatIndex + counts.Floats)
	}
	if a.StringIndex+counts.Strings > len(a.stringSlab) {
		a.growStringSlab(a.StringIndex + counts.Strings)
	}
	if a.GeneralIndex+counts.Generals > len(a.generalSlab) {
		a.growGeneralSlab(a.GeneralIndex + counts.Generals)
	}
	if a.BoolIndex+counts.Bools > len(a.boolSlab) {
		a.growBoolSlab(a.BoolIndex + counts.Bools)
	}
	if a.UintIndex+counts.Uints > len(a.uintSlab) {
		a.growUintSlab(a.UintIndex + counts.Uints)
	}
	if a.ComplexIndex+counts.Complexes > len(a.complexSlab) {
		a.growComplexSlab(a.ComplexIndex + counts.Complexes)
	}
	if a.SlicesIntIndex+counts.SlicesInts > len(a.slicesIntSlab) {
		a.growSlicesIntSlab(a.SlicesIntIndex + counts.SlicesInts)
	}
	if a.SlicesFloatIndex+counts.SlicesFloats > len(a.slicesFloatSlab) {
		a.growSlicesFloatSlab(a.SlicesFloatIndex + counts.SlicesFloats)
	}
	if a.SlicesStringIndex+counts.SlicesStrings > len(a.slicesStringSlab) {
		a.growSlicesStringSlab(a.SlicesStringIndex + counts.SlicesStrings)
	}
	if a.SlicesBoolIndex+counts.SlicesBools > len(a.slicesBoolSlab) {
		a.growSlicesBoolSlab(a.SlicesBoolIndex + counts.SlicesBools)
	}
	if a.SlicesUintIndex+counts.SlicesUints > len(a.slicesUintSlab) {
		a.growSlicesUintSlab(a.SlicesUintIndex + counts.SlicesUints)
	}
	if a.SlicesByteIndex+counts.SlicesBytes > len(a.slicesByteSlab) {
		a.growSlicesByteSlab(a.SlicesByteIndex + counts.SlicesBytes)
	}
}

// growSlicesIntSlab grows the slicesInt slab to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity.
func (a *RegisterArena) growSlicesIntSlab(minCap int) {
	growSlab(a, &a.slicesIntSlab, minCap, nil, false)
}

// growSlicesFloatSlab grows the slicesFloat slab to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity.
func (a *RegisterArena) growSlicesFloatSlab(minCap int) {
	growSlab(a, &a.slicesFloatSlab, minCap, nil, false)
}

// growSlicesStringSlab grows the slicesString slab to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity.
func (a *RegisterArena) growSlicesStringSlab(minCap int) {
	growSlab(a, &a.slicesStringSlab, minCap, nil, false)
}

// growSlicesBoolSlab grows the slicesBool slab to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity.
func (a *RegisterArena) growSlicesBoolSlab(minCap int) {
	growSlab(a, &a.slicesBoolSlab, minCap, nil, false)
}

// growSlicesUintSlab grows the slicesUint slab to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity.
func (a *RegisterArena) growSlicesUintSlab(minCap int) {
	growSlab(a, &a.slicesUintSlab, minCap, nil, false)
}

// growSlicesByteSlab grows the slicesByte slab to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity.
func (a *RegisterArena) growSlicesByteSlab(minCap int) {
	growSlab(a, &a.slicesByteSlab, minCap, nil, true)
}

// growIntSlab grows the int slab to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity.
func (a *RegisterArena) growIntSlab(minCap int) {
	growSlab(a, &a.intSlab, minCap, &a.scalarDemandSinceReset.ints, true)
}

// growFloatSlab grows the float slab to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity.
func (a *RegisterArena) growFloatSlab(minCap int) {
	growSlab(a, &a.floatSlab, minCap, &a.scalarDemandSinceReset.floats, true)
}

// growStringSlab grows the string slab to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity.
func (a *RegisterArena) growStringSlab(minCap int) {
	growSlab(a, &a.stringSlab, minCap, &a.scalarDemandSinceReset.strings, true)
}

// growGeneralSlab grows the general slab to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity.
func (a *RegisterArena) growGeneralSlab(minCap int) {
	growSlab(a, &a.generalSlab, minCap, &a.scalarDemandSinceReset.generals, true)
}

// growBoolSlab grows the bool slab to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity.
func (a *RegisterArena) growBoolSlab(minCap int) {
	growSlab(a, &a.boolSlab, minCap, &a.scalarDemandSinceReset.bools, true)
}

// growUintSlab grows the uint slab to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity.
func (a *RegisterArena) growUintSlab(minCap int) {
	growSlab(a, &a.uintSlab, minCap, &a.scalarDemandSinceReset.uints, true)
}

// growComplexSlab grows the complex slab to at least minCap.
//
// Takes minCap (int) which is the minimum required capacity.
func (a *RegisterArena) growComplexSlab(minCap int) {
	growSlab(a, &a.complexSlab, minCap, &a.scalarDemandSinceReset.complexes, false)
}

// trimRetainedSlabs drops the oldest retired slab once a bank holds more than
// maxRetainedOldSlabsPerType of them.
//
// Takes retained (*[]T) which is the bank's retired-slab list, trimmed in place.
func trimRetainedSlabs[T any](retained *[]T) {
	if len(*retained) > maxRetainedOldSlabsPerType {
		*retained = (*retained)[1:]
	}
}

// growSlab replaces a register slab with one at least minCap long, preserving its
// contents. ctxMirrored must be true for banks the dispatch context mirrors, because a
// missing generation bump leaves the context pointing into a dropped slab.
//
// Takes a (*RegisterArena) which owns the slab.
// Takes slab (*[]T) which is the bank's backing slice, replaced in place.
// Takes minCap (int) which is the smallest acceptable new length.
// Takes demand (*int) which records the high-water mark for a scalar bank, or nil.
// Takes ctxMirrored (bool) which is true when the dispatch context caches this slab's
// base.
func growSlab[T any](a *RegisterArena, slab *[]T, minCap int, demand *int, ctxMirrored bool) {
	newCap := max(len(*slab)*2, minCap)
	newSlab := make([]T, newCap)
	copy(newSlab, *slab)
	*slab = newSlab
	if demand != nil {
		*demand = max(*demand, newCap)
	}
	if ctxMirrored {
		a.slabGeneration++
	}
}

// growBoxSlab retires a box slab and allocates a fresh one at double the previous length.
//
// Takes a (*RegisterArena) which owns the slab and the allocation budget.
// Takes slab (*[]T) which is the bank's backing slice, replaced in place.
// Takes index (*int) which is the bank's bump index, reset to zero.
// Takes retired (*int) which accumulates retired slots since the last reset.
// Takes elementSize (uint64) which is the per-slot budget charge for this bank.
func growBoxSlab[T any](a *RegisterArena, slab *[]T, index, retired *int, elementSize uint64) {
	oldCap := uint64(cap(*slab)) * elementSize
	*retired += len(*slab)
	newCap := max(len(*slab)*2, initialBoxSlabCapacity)
	*slab = make([]T, newCap)
	*index = 0
	a.ChargeArenaAllocation(uint64(cap(*slab))*elementSize, oldCap)
}
