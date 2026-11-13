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
	"runtime"
	"unsafe"

	"piko.sh/stablepool"
	"pipit.sh/pipit/internal/engine/program"
)

const (
	// int64ElementSize is the byte width of an int64 slab element.
	int64ElementSize uintptr = 8

	// float64ElementSize is the byte width of a float64 slab element.
	float64ElementSize uintptr = 8

	// uint64ElementSize is the byte width of a uint64 slab element.
	uint64ElementSize uintptr = 8

	// boolElementSize is the byte width of a bool slab element.
	boolElementSize uintptr = 1

	// stringHeaderSize is the byte width of a string header in the slab.
	stringHeaderSize uintptr = 16
)

// gcMarkState carries per-cycle liveness flags and byte counts that the mark phase
// populates and the compact phase consumes to drop dead old-slab generations.
//
//exhaustruct:ignore
type gcMarkState struct {
	// link is the intrusive header for stablepool. MUST be first.
	stablepool.Link

	// program is the root function of the program whose VM is marked; the licensed mark
	// reads its function constants (programConstantFunction). Reset clears it.
	program *program.CompiledFunction

	// OldByteSlabLive[i] is true when oldByteSlabs[i] has at least one byte still referenced
	// by a live string or slice.
	OldByteSlabLive []bool

	// OldMutByteSlabLive[i] is true when oldMutByteSlabs[i] holds a byte some marked value
	// reaches (markMutBytePointer).
	OldMutByteSlabLive []bool

	// OldGenericBytesLive[i] mirrors OldByteSlabLive for the pointer-free generic bytes
	// slab.
	OldGenericBytesLive []bool

	// OldSliceHeaderLive[i] mirrors OldByteSlabLive for the slice- header slab.
	OldSliceHeaderLive []bool

	// OldIntBackingLive[i] mirrors OldByteSlabLive for retained int64 backing arrays.
	OldIntBackingLive []bool

	// OldFloatBackingLive[i] mirrors OldByteSlabLive for retained float64 backing arrays.
	OldFloatBackingLive []bool

	// OldStringBackingLive[i] mirrors OldByteSlabLive for retained string backing arrays.
	OldStringBackingLive []bool

	// OldBoolBackingLive[i] mirrors OldByteSlabLive for retained bool backing arrays.
	OldBoolBackingLive []bool

	// OldUintBackingLive[i] mirrors OldByteSlabLive for retained uint64 backing arrays.
	OldUintBackingLive []bool

	// mapHolders keeps the map walk's key and element holders between walks and cycles
	// (takeMapHolder); every holder not in use is zero.
	mapHolders []gcMapHolder

	// mapIterators keeps map walk iterators between walks and cycles (takeMapIterator).
	// Every one kept is reset.
	mapIterators []*reflect.MapIter

	// work is the mark's queued steps, taken last in first out (drainMark). Every entry past
	// its length is zero, so a pooled mark state keeps nothing reachable.
	work []gcMarkWork

	// visited holds the referents the licensed mark has traced this cycle (firstVisit).
	// Reset clears it, so a pooled mark state keeps no referent reachable.
	visited gcVisitSet

	// CurrentLiveBytes counts the bytes in the current byteSlab reachable from any root.
	// Used by the threshold tuner to gauge how much of the current generation is live.
	CurrentLiveBytes int64

	// CurrentLiveGenericBytes mirrors CurrentLiveBytes for the pointer-free generic bytes
	// slab.
	CurrentLiveGenericBytes int64

	// traceBudget is the licensed mark's remaining allowance for tracing into containers the
	// default mark leaves untraced.
	traceBudget int

	// visitBudget is the licensed mark's remaining allowance of distinct referents
	// (firstVisit).
	visitBudget int

	// stepBudget is the licensed mark's remaining allowance of steps (takeMarkStep).
	stepBudget int

	// workLimit is the most steps the work list holds at once (queueWork), or zero for
	// gcMarkWorkLimit.
	workLimit int

	// mutBytesLow is the lowest address in the arena's retired mutable byte slabs, set for
	// the cycle by prepare (markMutBytePointer).
	mutBytesLow uintptr

	// mutBytesHigh is one past the highest address in those slabs, or zero when the cycle's
	// bounds are unknown.
	mutBytesHigh uintptr

	// droppedCharge is the budget charge the slabs compactPhase dropped carry
	// (takeSlabCharge).
	droppedCharge uint64

	// mutBytesDropped is the capacity of the retired mutable byte slabs compactPhase dropped
	// (compactOldMutByteSlabs).
	mutBytesDropped int64

	// mutChargeDropped is the budget charge those slabs carry, part of droppedCharge. An
	// unshared collection hands back this charge for them rather than their capacity.
	mutChargeDropped uint64

	// failClosed is the first reason the mark could not vouch for every live reference, or
	// zero. A failed-closed cycle compacts nothing, with or without a licence.
	failClosed licenceFailClosedReason

	// licensed is true when the arena is licensed, whose mark fails closed.
	licensed bool

	// shared is true for a licensed mark made while another goroutine shares the VM's
	// memory, which reads only what the VM holds privately (sharedMarkApplies).
	shared bool
}

var (
	// gcMarkStatePool amortises gcMarkState allocations across GC cycles. Uses stablepool so
	// the per-state liveness slices keep their backing capacity across pool cycles.
	//
	// WithOverflow keeps no more than the growth ceiling after a burst of concurrent
	// collections and leaves the surplus to the garbage collector. A mark state owns only Go
	// heap memory (its liveness slices, visited map, work queue, map holders and iterators),
	// records nothing in the slab or licence registries, and is released holding no referent
	// (releaseGCMarkState), so the collector may drop one without any teardown.
	gcMarkStatePool = mustPool(stablepool.New[gcMarkState](
		func(s *gcMarkState) { *s = gcMarkState{} },
		nil,
		runtime.NumCPU()*2,
		stablepool.WithGrowth[gcMarkState](runtime.NumCPU()*16),
		stablepool.WithOverflow[gcMarkState](),
	))
)

// Reset clears state for reuse.
//
// Zeroes counters and truncates the liveness slices so the next acquireGCMarkState reuses
// backing storage.
func (state *gcMarkState) Reset() {
	state.licensed = false
	state.shared = false
	state.droppedCharge = 0
	state.mutBytesDropped = 0
	state.mutChargeDropped = 0
	state.failClosed = 0
	state.traceBudget = 0
	state.visitBudget = 0
	state.stepBudget = 0
	state.workLimit = 0
	state.mutBytesLow = 0
	state.mutBytesHigh = 0
	state.program = nil
	state.clearVisited()
	state.clearWork()
	state.CurrentLiveBytes = 0
	state.CurrentLiveGenericBytes = 0
	clear(state.OldByteSlabLive)
	state.OldByteSlabLive = state.OldByteSlabLive[:0]
	clear(state.OldMutByteSlabLive)
	state.OldMutByteSlabLive = state.OldMutByteSlabLive[:0]
	clear(state.OldGenericBytesLive)
	state.OldGenericBytesLive = state.OldGenericBytesLive[:0]
	clear(state.OldSliceHeaderLive)
	state.OldSliceHeaderLive = state.OldSliceHeaderLive[:0]
	clear(state.OldIntBackingLive)
	state.OldIntBackingLive = state.OldIntBackingLive[:0]
	clear(state.OldFloatBackingLive)
	state.OldFloatBackingLive = state.OldFloatBackingLive[:0]
	clear(state.OldStringBackingLive)
	state.OldStringBackingLive = state.OldStringBackingLive[:0]
	clear(state.OldBoolBackingLive)
	state.OldBoolBackingLive = state.OldBoolBackingLive[:0]
	clear(state.OldUintBackingLive)
	state.OldUintBackingLive = state.OldUintBackingLive[:0]
}

// markPhase walks every root and records liveness against the arena's retained slab
// generations and current data slabs.
//
// Identifies which old slab generations have at least one live reference and accumulates
// a count of live bytes inside the current-generation byteSlab and genericBytesSlab.
//
// Takes state (*gcMarkState) which receives the mark output.
func (vm *VM) markPhase(state *gcMarkState) {
	arena := vm.Arena
	if arena == nil {
		return
	}
	if state.licensed {
		vm.markPhaseUnderLicence(state)
		return
	}
	visitor := rootVisitor{
		visitString:         func(s string) { markString(arena, state, s) },
		visitReflectValue:   func(v reflect.Value) { markReflectValue(arena, state, v) },
		visitSliceInt:       func(s []int64) { markIntSlice(arena, state, s) },
		visitSliceFloat:     func(s []float64) { markFloatSlice(arena, state, s) },
		visitSliceString:    func(s []string) { markStringSlice(arena, state, s) },
		visitSliceBool:      func(s []bool) { markBoolSlice(arena, state, s) },
		visitSliceUint:      func(s []uint64) { markUintSlice(arena, state, s) },
		visitSliceByte:      func(s []byte) { markByteSlice(arena, state, s) },
		visitAny:            func(v any) { markAny(arena, state, v) },
		visitUpvalueCell:    func(c *program.UpvalueCell) { markUpvalueCell(arena, state, c) },
		visitRuntimeClosure: func(c *RuntimeClosure) { markRuntimeClosure(arena, state, c) },
	}
	vm.walkRoots(visitor)
	drainMark(arena, state, 0)
}

// markPhaseUnderLicence is markPhase for a licensed arena, whose mark follows closures
// and upvalue cells by their fields' meaning (markClosureCells, markCellContents).
//
// Takes state (*gcMarkState) which receives the mark output.
func (vm *VM) markPhaseUnderLicence(state *gcMarkState) {
	arena := vm.Arena
	state.program = vm.rootFunction
	visitor := rootVisitor{
		visitString:         func(s string) { markString(arena, state, s) },
		visitReflectValue:   func(v reflect.Value) { markReflectValue(arena, state, v) },
		visitSliceInt:       func(s []int64) { markIntSlice(arena, state, s) },
		visitSliceFloat:     func(s []float64) { markFloatSlice(arena, state, s) },
		visitSliceString:    func(s []string) { markStringSlice(arena, state, s) },
		visitSliceBool:      func(s []bool) { markBoolSlice(arena, state, s) },
		visitSliceUint:      func(s []uint64) { markUintSlice(arena, state, s) },
		visitSliceByte:      func(s []byte) { markByteSlice(arena, state, s) },
		visitAny:            func(v any) { markAny(arena, state, v) },
		visitUpvalueCell:    func(c *program.UpvalueCell) { markCellContents(arena, state, c, 0) },
		visitRuntimeClosure: func(c *RuntimeClosure) { markClosureCells(arena, state, c, 0) },
	}
	if state.shared {
		vm.walkPrivateRoots(visitor)
	} else {
		vm.walkRoots(visitor)
	}
	drainMark(arena, state, 0)
}

// acquireGCMarkState rents a cleared mark state from the pool, sized to fit the arena's
// current oldXxx retention lists.
//
// Takes arena (*RegisterArena) which dictates the per-slab liveness slice lengths to
// provision.
//
// Returns the rented gcMarkState with liveness slices sized for arena.
func acquireGCMarkState(arena *RegisterArena) *gcMarkState {
	state := gcMarkStatePool.MustGet()
	state.prepare(arena)
	return state
}

// prepare clears state for a cycle over arena. It fills the budgets and sizes the
// liveness slices to the arena's retention lists.
//
// Takes arena (*RegisterArena) which is the arena the cycle collects.
func (state *gcMarkState) prepare(arena *RegisterArena) {
	state.Reset()
	state.licensed = arena.licensed
	state.traceBudget = licenceMarkTraceBudget
	state.visitBudget = licenceMarkVisitBudget
	state.stepBudget = licenceMarkStepBudget
	state.OldByteSlabLive = ensureBoolSlice(state.OldByteSlabLive, len(arena.oldByteSlabs))
	state.OldMutByteSlabLive = ensureBoolSlice(state.OldMutByteSlabLive, len(arena.oldMutByteSlabs))
	state.mutBytesLow, state.mutBytesHigh = mutByteSlabBounds(arena.oldMutByteSlabs)
	state.OldGenericBytesLive = ensureBoolSlice(state.OldGenericBytesLive, len(arena.oldGenericByteSlabs))
	state.OldSliceHeaderLive = ensureBoolSlice(state.OldSliceHeaderLive, len(arena.oldSliceHeaderSlabs))
	state.OldIntBackingLive = ensureBoolSlice(state.OldIntBackingLive, len(arena.oldIntBackings))
	state.OldFloatBackingLive = ensureBoolSlice(state.OldFloatBackingLive, len(arena.oldFloatBackings))
	state.OldStringBackingLive = ensureBoolSlice(state.OldStringBackingLive, len(arena.oldStringBackings))
	state.OldBoolBackingLive = ensureBoolSlice(state.OldBoolBackingLive, len(arena.oldBoolBackings))
	state.OldUintBackingLive = ensureBoolSlice(state.OldUintBackingLive, len(arena.oldUintBackings))
}

// releaseGCMarkState returns a mark state to the pool after the GC cycle has consumed it,
// forgetting the referents and program it held so the pool keeps neither reachable.
//
// Takes state (*gcMarkState) which has finished its cycle; nil is accepted as a no-op.
func releaseGCMarkState(state *gcMarkState) {
	if state == nil {
		return
	}
	state.clearVisited()
	state.clearWork()
	state.program = nil
	gcMarkStatePool.Put(state)
}

// ensureBoolSlice extends or truncates s to exactly length n, reusing backing storage
// where possible.
//
// Takes s ([]bool) which is the input slice whose capacity is reused.
// Takes n (int) which is the desired length.
//
// Returns the resulting []bool of length n, zeroed.
func ensureBoolSlice(s []bool, n int) []bool {
	if cap(s) < n {
		return make([]bool, n)
	}
	s = s[:n]
	clear(s)
	return s
}

// markString tests whether s's backing bytes live in any byteSlab generation and records
// liveness accordingly.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes s (string) whose backing bytes are checked for arena residency.
func markString(arena *RegisterArena, state *gcMarkState, s string) {
	if len(s) == 0 {
		return
	}

	dataPtr := unsafe.Pointer(unsafe.StringData(s))
	markBytePointer(arena, state, dataPtr, len(s))
}

// markBytePointer locates the byteSlab generation containing the given byte pointer and
// marks it live. Handles both the current generation and the retention list, and a
// licensed arena's mutable byte slabs (markMutBytePointer).
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes p (unsafe.Pointer) which is the first byte of the live range.
// Takes length (int) which is the count of live bytes starting at p.
func markBytePointer(arena *RegisterArena, state *gcMarkState, p unsafe.Pointer, length int) {
	if p == nil || length <= 0 {
		return
	}
	pointer := uintptr(p)
	if len(arena.byteSlab) > 0 {
		base := uintptr(unsafe.Pointer(&arena.byteSlab[0]))
		if pointer >= base && pointer < base+uintptr(len(arena.byteSlab)) {
			state.CurrentLiveBytes += int64(length)
			return
		}
	}
	for i, slab := range arena.oldByteSlabs {
		if len(slab) == 0 {
			continue
		}

		base := uintptr(unsafe.Pointer(&slab[0]))
		if pointer >= base && pointer < base+uintptr(len(slab)) {
			if i < len(state.OldByteSlabLive) {
				state.OldByteSlabLive[i] = true
			}
			return
		}
	}
	markMutBytePointer(arena, state, pointer)
}

// markMutBytePointer marks the retired mutable byte slab holding pointer live, if one
// does.
//
// Every route by which the mark meets a byte address ends here. These are []byte values,
// strings (one made with unsafe.String over a []byte included) and untyped backings
// through markBytePointer, and pointers to pointer-free values (&b[i], (*[n]byte)(b))
// through markGenericBytesPointer. compactOldMutByteSlabs drops each retired slab no
// marked value reaches. An address outside the slabs' bounds for the cycle (prepare) is
// rejected without a scan.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes pointer (uintptr) which is the address reached.
func markMutBytePointer(arena *RegisterArena, state *gcMarkState, pointer uintptr) {
	if state.mutBytesHigh != 0 && (pointer < state.mutBytesLow || pointer >= state.mutBytesHigh) {
		return
	}
	for i, slab := range arena.oldMutByteSlabs {
		if len(slab) == 0 {
			continue
		}
		base := uintptr(unsafe.Pointer(&slab[0]))
		if pointer >= base && pointer < base+uintptr(len(slab)) {
			if i < len(state.OldMutByteSlabLive) {
				state.OldMutByteSlabLive[i] = true
			}
			return
		}
	}
}

// mutByteSlabBounds returns the lowest address in slabs and one past the highest, so the
// mark tests most byte addresses against every retired mutable byte slab at once.
//
// Takes slabs ([][]byte) which are the retired mutable byte slabs.
//
// Returns low (uintptr) which is the lowest address, or zero.
// Returns high (uintptr) which is one past the highest, or zero when no slab holds bytes.
func mutByteSlabBounds(slabs [][]byte) (low, high uintptr) {
	for _, slab := range slabs {
		if len(slab) == 0 {
			continue
		}
		base := uintptr(unsafe.Pointer(&slab[0]))
		end := base + uintptr(len(slab))
		if high == 0 || base < low {
			low = base
		}
		high = max(high, end)
	}
	return low, high
}

// markByteSlice marks the byteSlab range covered by s's backing array. Distinct from
// markString because a []byte may have spare capacity beyond Len() that still occupies
// arena bytes.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes s ([]byte) whose backing array is checked for arena residency.
func markByteSlice(arena *RegisterArena, state *gcMarkState, s []byte) {
	if cap(s) == 0 {
		return
	}

	dataPtr := unsafe.Pointer(unsafe.SliceData(s))
	markBytePointer(arena, state, dataPtr, cap(s))
}

// markIntSlice marks the intBackingSlab generation containing s's backing array, if any.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes s ([]int64) whose backing array is checked for arena residency.
func markIntSlice(arena *RegisterArena, state *gcMarkState, s []int64) {
	if cap(s) == 0 {
		return
	}

	pointer := uintptr(unsafe.Pointer(unsafe.SliceData(s)))
	if pointerInIntSlice(pointer, arena.intBackingSlab, int64ElementSize) {
		return
	}
	for i, backing := range arena.oldIntBackings {
		if pointerInIntSlice(pointer, backing, int64ElementSize) {
			if i < len(state.OldIntBackingLive) {
				state.OldIntBackingLive[i] = true
			}
			return
		}
	}
}

// markFloatSlice mirrors markIntSlice for floatBackingSlab.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes s ([]float64) whose backing array is checked for arena residency.
func markFloatSlice(arena *RegisterArena, state *gcMarkState, s []float64) {
	if cap(s) == 0 {
		return
	}

	pointer := uintptr(unsafe.Pointer(unsafe.SliceData(s)))
	if pointerInFloatSlice(pointer, arena.floatBackingSlab, float64ElementSize) {
		return
	}
	for i, backing := range arena.oldFloatBackings {
		if pointerInFloatSlice(pointer, backing, float64ElementSize) {
			if i < len(state.OldFloatBackingLive) {
				state.OldFloatBackingLive[i] = true
			}
			return
		}
	}
}

// markStringSlice mirrors markIntSlice for stringBackingSlab, and recurses into each
// element string so its byte range gets marked too.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes s ([]string) whose header and elements are both inspected.
func markStringSlice(arena *RegisterArena, state *gcMarkState, s []string) {
	if len(s) != 0 && !state.readsPrivately(arena, uintptr(unsafe.Pointer(unsafe.SliceData(s)))) {
		return
	}
	for _, element := range s {
		markString(arena, state, element)
	}
	if cap(s) == 0 {
		return
	}

	pointer := uintptr(unsafe.Pointer(unsafe.SliceData(s)))
	if pointerInStringSlice(pointer, arena.stringBackingSlab, stringHeaderSize) {
		return
	}
	for i, backing := range arena.oldStringBackings {
		if pointerInStringSlice(pointer, backing, stringHeaderSize) {
			if i < len(state.OldStringBackingLive) {
				state.OldStringBackingLive[i] = true
			}
			return
		}
	}
}

// markBoolSlice mirrors markIntSlice for boolBackingSlab.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes s ([]bool) whose backing array is checked for arena residency.
func markBoolSlice(arena *RegisterArena, state *gcMarkState, s []bool) {
	if cap(s) == 0 {
		return
	}

	pointer := uintptr(unsafe.Pointer(unsafe.SliceData(s)))
	if pointerInBoolSlice(pointer, arena.boolBackingSlab, boolElementSize) {
		return
	}
	for i, backing := range arena.oldBoolBackings {
		if pointerInBoolSlice(pointer, backing, boolElementSize) {
			if i < len(state.OldBoolBackingLive) {
				state.OldBoolBackingLive[i] = true
			}
			return
		}
	}
}

// markUintSlice mirrors markIntSlice for uintBackingSlab.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes s ([]uint64) whose backing array is checked for arena residency.
func markUintSlice(arena *RegisterArena, state *gcMarkState, s []uint64) {
	if cap(s) == 0 {
		return
	}

	pointer := uintptr(unsafe.Pointer(unsafe.SliceData(s)))
	if pointerInUintSlice(pointer, arena.uintBackingSlab, uint64ElementSize) {
		return
	}
	for i, backing := range arena.oldUintBackings {
		if pointerInUintSlice(pointer, backing, uint64ElementSize) {
			if i < len(state.OldUintBackingLive) {
				state.OldUintBackingLive[i] = true
			}
			return
		}
	}
}

// pointerInIntSlice reports whether pointer lies within backing, given an element stride.
//
// Takes pointer (uintptr) which is the address being classified.
// Takes backing ([]int64) which is the int64 slab to test against.
// Takes elementSize (uintptr) which is the byte stride per element.
//
// Returns true when pointer falls inside backing; false on empty backing.
func pointerInIntSlice(pointer uintptr, backing []int64, elementSize uintptr) bool {
	if len(backing) == 0 {
		return false
	}

	base := uintptr(unsafe.Pointer(&backing[0]))
	return pointer >= base && pointer < base+uintptr(len(backing))*elementSize
}

// pointerInFloatSlice reports whether pointer lies within backing, given an element
// stride.
//
// Takes pointer (uintptr) which is the address being classified.
// Takes backing ([]float64) which is the float64 slab to test against.
// Takes elementSize (uintptr) which is the byte stride per element.
//
// Returns true when pointer falls inside backing; false on empty backing.
func pointerInFloatSlice(pointer uintptr, backing []float64, elementSize uintptr) bool {
	if len(backing) == 0 {
		return false
	}

	base := uintptr(unsafe.Pointer(&backing[0]))
	return pointer >= base && pointer < base+uintptr(len(backing))*elementSize
}

// pointerInStringSlice reports whether pointer lies within backing, given the per-header
// stride.
//
// Takes pointer (uintptr) which is the address being classified.
// Takes backing ([]string) which is the string slab to test against.
// Takes headerSize (uintptr) which is the byte stride per string header.
//
// Returns true when pointer falls inside backing; false on empty backing.
func pointerInStringSlice(pointer uintptr, backing []string, headerSize uintptr) bool {
	if len(backing) == 0 {
		return false
	}

	base := uintptr(unsafe.Pointer(&backing[0]))
	return pointer >= base && pointer < base+uintptr(len(backing))*headerSize
}

// pointerInBoolSlice reports whether pointer lies within backing, given an element
// stride.
//
// Takes pointer (uintptr) which is the address being classified.
// Takes backing ([]bool) which is the bool slab to test against.
// Takes elementSize (uintptr) which is the byte stride per element.
//
// Returns true when pointer falls inside backing; false on empty backing.
func pointerInBoolSlice(pointer uintptr, backing []bool, elementSize uintptr) bool {
	if len(backing) == 0 {
		return false
	}

	base := uintptr(unsafe.Pointer(&backing[0]))
	return pointer >= base && pointer < base+uintptr(len(backing))*elementSize
}

// pointerInUintSlice reports whether pointer lies within backing, given an element
// stride.
//
// Takes pointer (uintptr) which is the address being classified.
// Takes backing ([]uint64) which is the uint64 slab to test against.
// Takes elementSize (uintptr) which is the byte stride per element.
//
// Returns true when pointer falls inside backing; false on empty backing.
func pointerInUintSlice(pointer uintptr, backing []uint64, elementSize uintptr) bool {
	if len(backing) == 0 {
		return false
	}

	base := uintptr(unsafe.Pointer(&backing[0]))
	return pointer >= base && pointer < base+uintptr(len(backing))*elementSize
}

// markReflectValue marks the slab generations v reaches, taking every step it queues
// (drainMark).
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes v (reflect.Value) which is the value to inspect and mark.
func markReflectValue(arena *RegisterArena, state *gcMarkState, v reflect.Value) {
	base := len(state.work)
	state.pushMark(v, 0)
	drainMark(arena, state, base)
}

// markGenericBytesPointer locates the genericBytesSlab generation containing p and marks
// it live. A pointer to a pointer-free value may instead address a []byte's backing, in a
// licensed arena's mutable byte slabs (markMutBytePointer).
//
// Takes arena (*RegisterArena) which owns the generic bytes slabs.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes p (unsafe.Pointer) which is the byte address to classify.
func markGenericBytesPointer(arena *RegisterArena, state *gcMarkState, p unsafe.Pointer) {
	if p == nil {
		return
	}
	pointer := uintptr(p)
	if len(arena.genericBytesSlab) > 0 {
		base := uintptr(unsafe.Pointer(&arena.genericBytesSlab[0]))
		if pointer >= base && pointer < base+uintptr(len(arena.genericBytesSlab)) {
			state.CurrentLiveGenericBytes++
			return
		}
	}
	for i, slab := range arena.oldGenericByteSlabs {
		if len(slab) == 0 {
			continue
		}

		base := uintptr(unsafe.Pointer(&slab[0]))
		if pointer >= base && pointer < base+uintptr(len(slab)) {
			if i < len(state.OldGenericBytesLive) {
				state.OldGenericBytesLive[i] = true
			}
			return
		}
	}
	markMutBytePointer(arena, state, pointer)
}

// markSliceHeaderSlab flags an old slice-header slab as live when headerPtr falls within
// it.
//
// When the pointer lives in an old slab, the slab is recorded as still-live in the GC
// state. The current slab is always-live and needs no flagging.
//
// Takes arena (*RegisterArena) which owns the slabs to test against.
// Takes state (*gcMarkState) which records old-slab liveness.
// Takes headerPtr (unsafe.Pointer) which is the slice-header pointer from a Slice-kind
// reflect.Value.
func markSliceHeaderSlab(arena *RegisterArena, state *gcMarkState, headerPtr unsafe.Pointer) {
	pointer := uintptr(headerPtr)
	if len(arena.sliceHeaderSlab) > 0 {
		base := uintptr(unsafe.Pointer(&arena.sliceHeaderSlab[0]))
		stride := unsafe.Sizeof(arena.sliceHeaderSlab[0])
		if pointer >= base && pointer < base+uintptr(len(arena.sliceHeaderSlab))*stride {
			return
		}
	}
	for i, slab := range arena.oldSliceHeaderSlabs {
		if len(slab) == 0 {
			continue
		}

		base := uintptr(unsafe.Pointer(&slab[0]))
		stride := unsafe.Sizeof(slab[0])
		if pointer >= base && pointer < base+uintptr(len(slab))*stride {
			if i < len(state.OldSliceHeaderLive) {
				state.OldSliceHeaderLive[i] = true
			}
			return
		}
	}
}

// markAny extracts the underlying reflect.Value from an any-typed root (panicValue,
// evalResult) and dispatches to markReflectValue.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes v (any) which is the boxed root to inspect.
func markAny(arena *RegisterArena, state *gcMarkState, v any) {
	if v == nil {
		return
	}
	if s, ok := v.(string); ok {
		markString(arena, state, s)
		return
	}
	rv := reflect.ValueOf(v)
	if rv.IsValid() {
		markReflectValue(arena, state, rv)
	}
}

// markUpvalueCell follows the cell's payload fields and marks any arena-backed values
// they reference.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes cell (*UpvalueCell) which is the upvalue to inspect; nil is accepted as a no-op.
func markUpvalueCell(arena *RegisterArena, state *gcMarkState, cell *program.UpvalueCell) {
	if cell == nil {
		return
	}
	if cell.StringValue != "" {
		markString(arena, state, cell.StringValue)
	}
	if cell.GeneralValue.IsValid() {
		markReflectValue(arena, state, cell.GeneralValue)
	}
}

// markRuntimeClosure follows the closure's upvalue references.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes closure (*RuntimeClosure) which is the closure to inspect; nil is accepted as a
// no-op.
func markRuntimeClosure(arena *RegisterArena, state *gcMarkState, closure *RuntimeClosure) {
	if closure == nil {
		return
	}
	for _, cell := range closure.upvalues {
		markUpvalueCell(arena, state, cell)
	}
}
