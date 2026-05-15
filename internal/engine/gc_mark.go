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

// SAFETY: marking reads strings, byte slices and register banks through raw pointers to
// decide which arena slabs are still live. It runs only inside MinorGC at a dispatch
// checkpoint, when the owning VM's loop is paused and every frame and register bank is
// synchronised (the assembly holds no state of its own between loop iterations), and an
// arena is owned by exactly one VM goroutine, so nothing mutates the slabs while they are
// walked. Pointer comparisons against slab bounds classify a pointer as arena-owned or
// not; they never dereference memory outside a slab the arena allocated.

import (
	"reflect"
	"runtime"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/stablepool"
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

	// OldByteSlabLive[i] is true when oldByteSlabs[i] has at least one byte still referenced
	// by a live string or slice.
	OldByteSlabLive []bool

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

	// CurrentLiveBytes counts the bytes in the current byteSlab reachable from any root.
	// Used by the threshold tuner to gauge how much of the current generation is live.
	CurrentLiveBytes int64

	// CurrentLiveGenericBytes mirrors CurrentLiveBytes for the pointer-free generic bytes
	// slab.
	CurrentLiveGenericBytes int64
}

var (
	// gcMarkStatePool amortises gcMarkState allocations across GC cycles. Uses stablepool so
	// the per-state liveness slices keep their backing capacity across pool cycles.
	gcMarkStatePool, _ = stablepool.New[gcMarkState](
		func(s *gcMarkState) { *s = gcMarkState{} },
		nil,
		runtime.NumCPU()*2,
		stablepool.WithGrowth[gcMarkState](runtime.NumCPU()*16),
	)
)

// Reset clears state for reuse.
//
// Zeroes counters and truncates the liveness slices so the next acquireGCMarkState reuses
// backing storage.
func (state *gcMarkState) Reset() {
	state.CurrentLiveBytes = 0
	state.CurrentLiveGenericBytes = 0
	clear(state.OldByteSlabLive)
	state.OldByteSlabLive = state.OldByteSlabLive[:0]
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
	state.Reset()
	state.OldByteSlabLive = ensureBoolSlice(state.OldByteSlabLive, len(arena.oldByteSlabs))
	state.OldGenericBytesLive = ensureBoolSlice(state.OldGenericBytesLive, len(arena.oldGenericByteSlabs))
	state.OldSliceHeaderLive = ensureBoolSlice(state.OldSliceHeaderLive, len(arena.oldSliceHeaderSlabs))
	state.OldIntBackingLive = ensureBoolSlice(state.OldIntBackingLive, len(arena.oldIntBackings))
	state.OldFloatBackingLive = ensureBoolSlice(state.OldFloatBackingLive, len(arena.oldFloatBackings))
	state.OldStringBackingLive = ensureBoolSlice(state.OldStringBackingLive, len(arena.oldStringBackings))
	state.OldBoolBackingLive = ensureBoolSlice(state.OldBoolBackingLive, len(arena.oldBoolBackings))
	state.OldUintBackingLive = ensureBoolSlice(state.OldUintBackingLive, len(arena.oldUintBackings))
	return state
}

// releaseGCMarkState returns a mark state to the pool after the GC cycle has consumed it.
//
// Takes state (*gcMarkState) which has finished its cycle; nil is accepted as a no-op.
func releaseGCMarkState(state *gcMarkState) {
	if state == nil {
		return
	}
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
// marks it live. Handles both the current generation and the retention list.
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
// Takes pointer which is the address being classified.
// Takes backing which is the int64 slab to test against.
// Takes elementSize which is the byte stride per element.
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
// Takes pointer which is the address being classified.
// Takes backing which is the float64 slab to test against.
// Takes elementSize which is the byte stride per element.
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
// Takes pointer which is the address being classified.
// Takes backing which is the string slab to test against.
// Takes headerSize which is the byte stride per string header.
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
// Takes pointer which is the address being classified.
// Takes backing which is the bool slab to test against.
// Takes elementSize which is the byte stride per element.
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
// Takes pointer which is the address being classified.
// Takes backing which is the uint64 slab to test against.
// Takes elementSize which is the byte stride per element.
//
// Returns true when pointer falls inside backing; false on empty backing.
func pointerInUintSlice(pointer uintptr, backing []uint64, elementSize uintptr) bool {
	if len(backing) == 0 {
		return false
	}

	base := uintptr(unsafe.Pointer(&backing[0]))
	return pointer >= base && pointer < base+uintptr(len(backing))*elementSize
}

// markReflectValue inspects v's kind and marks reachable slab generations.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes v (reflect.Value) which is the value to inspect and mark.
func markReflectValue(arena *RegisterArena, state *gcMarkState, v reflect.Value) {
	markReflectValueAt(arena, state, v, 0)
}

// markGenericBytesPointer locates the genericBytesSlab generation containing p and marks
// it live.
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
