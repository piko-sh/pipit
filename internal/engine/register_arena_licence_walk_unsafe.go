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

//go:build !safe && !(js && wasm)

package engine

import (
	"reflect"
	"slices"
	"unsafe"
)

// licenceWalk is one bounded revocation walk over a handed-off value.
type licenceWalk struct {
	// arena is the arena whose slabs are revoked.
	arena *RegisterArena

	// memo refers to the arena's walk memo when this walk may use it.
	memo *licenceWalkMemo

	// parked is the arena of the parked caller whose storage this shared callback walk may
	// read (register_arena_licence_callbacks.go), parkedGeneration its callback-start
	// generation when the walk began, and usedParked true once the walk read that storage.
	parked *RegisterArena

	// typedLastABI and typedLastPlan are the typed walk's last plan lookup, so a run of
	// values of one type looks its plan up once.
	typedLastABI unsafe.Pointer

	// typedLastPlan is the plan of typedLastABI's type.
	typedLastPlan *licenceTypedPlan

	// fresh holds the data slabs this walk revoked, which no other goroutine can reach yet.
	fresh []uintptr

	// spans holds the slabs the walk revoked whole before cachedBase's, most recent first.
	spans [licenceWalkSpanCache]licenceWalkSpan

	// dataBase and dataEnd bound the data slab the walk last found an address in
	// (dataSlabSpan).
	dataBase, dataEnd uintptr

	// budget is the remaining leaf allowance.
	budget int

	// budgetAtStart is the allowance the epoch had left when the walk began.
	budgetAtStart int

	// cachedBase and cachedEnd bound the data slab of this arena the walk last revoked.
	cachedBase, cachedEnd uintptr

	// unmemoisedDepth records the depth excluded from memo lookups.
	unmemoisedDepth int

	// parkedGeneration is the parked caller's callback-entry generation when the walk began.
	parkedGeneration uint64

	// seenBase is where this walk's entries begin in licenceState.wrapperSeen, the closures
	// and cells it has visited.
	seenBase int

	// cause is the hand-off's cause.
	cause HandOffCause

	// exhausted is true once the depth or leaf budget ran out.
	exhausted bool

	// shared is true when another goroutine may be writing what the value reaches, so the
	// walk must not read through an addressable value or a map.
	shared bool

	// sharedStop is true once a shared walk stopped at such a value.
	sharedStop bool

	// banksPrivate is true when no earlier hand-off of the epoch referenced a register bank.
	banksPrivate bool

	// stringsSkipped is true once a shared walk skipped strings it could not read, which can
	// reference only byte slabs.
	stringsSkipped bool

	// opaque is true once the walk met a function value, whose captured variables it cannot
	// follow.
	opaque bool

	// memoReadOnly is true for a shared walk's memo.
	memoReadOnly bool

	// usedParked records whether the walk read the parked caller's storage.
	usedParked bool

	// epochStop records that revoking the epoch ended the walk.
	epochStop bool

	// typedOff keeps this walk on the reflect path, and off the span cache (spans), for the
	// reference walk of the typed-walk differential (licenceTypedWalkBoth).
	typedOff bool

	// mapOrder is true once the walk iterated a map of more than one entry, whose order Go
	// randomises.
	//
	// two walks of one value can then meet its entries, and stop, differently.
	mapOrder bool
}

// licenceWalkSpanCache is the number of revoked slabs a walk remembers besides its last.
const licenceWalkSpanCache = 6

// licenceWalkSpan bounds one slab a walk revoked whole.
type licenceWalkSpan struct {
	// base and end bound the slab by capacity; both zero for an empty entry.
	base, end uintptr
}

// licenceScratchLimit bounds the scratch values an arena keeps between walks.
const licenceScratchLimit = 64

// licenceScratchSlot is one reusable value the walks copy map keys and elements into.
type licenceScratchSlot struct {
	// typ is the slot's type.
	typ reflect.Type

	// value is the settable value.
	value reflect.Value

	// slot distinguishes nested uses of one type using twice the walk depth plus one for
	// elements.
	slot int
}

// cachedSpan reports whether address lies in a slab the walk revoked whole earlier, other
// than its last (cachedBase), making that slab the last, exactly as revoking it again
// would.
//
// Takes address (uintptr) which is a non-zero address outside [cachedBase, cachedEnd).
//
// Returns bool which is true when address needs no revocation.
func (w *licenceWalk) cachedSpan(address uintptr) bool {
	if w.typedOff {
		return false
	}
	for index := range w.spans {
		span := w.spans[index]
		if address >= span.base && address < span.end {
			copy(w.spans[1:index+1], w.spans[:index])
			w.spans[0] = licenceWalkSpan{base: w.cachedBase, end: w.cachedEnd}
			w.cachedBase, w.cachedEnd = span.base, span.end
			return true
		}
	}
	return false
}

// cacheSpan makes [base, end), a slab the walk has just revoked whole, its last, keeping
// the previous last in spans.
//
// Takes base (uintptr) which is the slab's inclusive starting address.
// Takes end (uintptr) which is the slab's exclusive ending address.
func (w *licenceWalk) cacheSpan(base, end uintptr) {
	if w.cachedEnd != 0 && w.cachedBase != base {
		copy(w.spans[1:], w.spans[:len(w.spans)-1])
		w.spans[0] = licenceWalkSpan{base: w.cachedBase, end: w.cachedEnd}
	}
	w.cachedBase, w.cachedEnd = base, end
}

// visit revokes the storage value and everything it reaches reference.
//
// Takes value (reflect.Value) which is the value.
// Takes boxed (bool) which is true when value is reached through, or converted to, an
// interface, which shares an indirect, non-addressable value's own storage.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) visit(value reflect.Value, boxed bool, depth int) {
	if !value.IsValid() || w.exhausted {
		return
	}
	if licenceTypedWalkOn && !w.typedOff && !w.shared && w.visitTyped(value, boxed, depth) {
		return
	}
	if w.arena.licenceState.epochRevoked {
		w.epochStop, w.exhausted = true, true
		return
	}
	if depth <= 0 || w.budget <= 0 {
		w.exhausted = true
		return
	}
	if w.shared && w.sharedStops(value) {
		return
	}
	w.budget--
	if boxed {
		w.visitBoxedStorage(value)
	}
	if value.Kind() == reflect.Struct && value.Type() == reflectValueReflectType {
		w.visit(reflectValueHeld(value), true, depth-1)
		return
	}
	w.visitKind(value, depth)
}

// visitKind revokes and walks value by its kind, once visit has admitted and counted it.
//
// Takes value (reflect.Value) which is the value.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) visitKind(value reflect.Value, depth int) {
	switch value.Kind() {
	case reflect.String:
		if s := value.String(); len(s) > 0 {
			w.revoke(uintptr(unsafe.Pointer(unsafe.StringData(s))))
		}
	case reflect.Slice:
		w.visitSlice(value, depth)
	case reflect.Array:
		if !licenceTypeHoldsNoPointers(value.Type().Elem()) {
			w.visitElements(value, value.Len(), depth)
		}
	case reflect.Struct:
		if !licenceTypeHoldsNoPointers(value.Type()) {
			w.visitFields(value, depth)
		}
	case reflect.Pointer:
		if !value.IsNil() {
			w.visitPointer(value, depth)
		}
	case reflect.Interface:
		if !value.IsNil() && !isTypeDescriptor(value.Elem()) {
			w.visit(value.Elem(), true, depth-1)
		}
	case reflect.Map:
		w.visitMap(value, depth)
	case reflect.UnsafePointer:
		w.revoke(uintptr(value.UnsafePointer()))
	case reflect.Func:
		if !value.IsNil() {
			w.visitFunc(value, depth)
		}
	default:
	}
}

// visitFields walks each field of a struct that holds pointers.
//
// Takes v (reflect.Value) which is the struct.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) visitFields(v reflect.Value, depth int) {
	for index := range v.NumField() {
		w.visit(structFieldValue(v, index), false, depth-1)
	}
}

// sharedStops ends a shared walk at a value outside the walking arena's private storage.
//
// Takes value (reflect.Value) which is the value.
//
// Returns bool which is true when the walk goes no further into value.
func (w *licenceWalk) sharedStops(value reflect.Value) bool {
	if value.CanAddr() && licenceTypeHoldsOnlyStrings(value.Type()) && !w.private(value.UnsafeAddr()) {
		w.stringsSkipped = true
		return true
	}
	if value.CanAddr() && !w.private(value.UnsafeAddr()) || value.Kind() == reflect.Map && !licenceMapHoldsNoPointers(value.Type()) {
		w.sharedStop, w.exhausted = true, true
		return true
	}
	return false
}

// visitPointer revokes a non-nil pointer's target and walks what it reaches, unless it is
// a closure or cell, holds no pointers, or the memo covers it.
//
// Takes value (reflect.Value) which is the pointer.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) visitPointer(value reflect.Value, depth int) {
	if w.visitClosureOrCell(value, depth) {
		return
	}
	pointer := value.UnsafePointer()
	if licenceTypeHoldsNoPointers(value.Type().Elem()) {
		w.revokeExtent(uintptr(pointer), value.Type().Elem().Size())
		return
	}
	typ := reflectValueABIType(value.Type().Elem())
	if w.memoCovers(value, pointer, typ, 0, depth) {
		return
	}
	w.revokeExtent(uintptr(pointer), value.Type().Elem().Size())
	w.visit(value.Elem(), false, depth-1)
	w.memoNote(pointer, typ, 0)
}

// memoCovers reports whether an earlier walk of the epoch followed the Go-heap container
// at pointer, of type typ, to at least extent, so this walk need not.
//
// Takes value (reflect.Value) which is a pointer, slice or map used as the container.
// Takes pointer (unsafe.Pointer) which is the container's address.
// Takes typ (unsafe.Pointer) which is its type.
// Takes extent (int) which is the slice capacity to walk, or zero.
// Takes depth (int) which is the walk's remaining depth at the container.
//
// Returns bool which is true when the walk may stop at the container.
func (w *licenceWalk) memoCovers(value reflect.Value, pointer, typ unsafe.Pointer, extent int, depth int) bool {
	if w.memo == nil || depth == w.unmemoisedDepth {
		return false
	}
	if !w.memoReadOnly {
		covered := w.memo.covers(pointer, typ, extent) && w.memoEligible(pointer)
		if storeCensusBuilt && covered {
			censusNoteMemoSettled(w.arena, value)
		}
		return covered
	}
	if !w.memo.peek(pointer, typ, extent) || !w.memoEligible(pointer) {
		return false
	}
	if licenceCounting {
		blindScopeCounters.sharedMemoHits.Add(1)
	}
	if storeCensusBuilt {
		censusNoteMemoSettled(w.arena, value)
	}
	return true
}

// memoEligible reports whether pointer addresses Go-heap memory.
//
// Takes pointer (unsafe.Pointer) which is the container's address.
//
// Returns bool which is true when the memo may record or skip the container.
func (w *licenceWalk) memoEligible(pointer unsafe.Pointer) bool {
	address := uintptr(pointer)
	if licenceRegisteredPage(w.arena, address) && w.inRevokedSpan(address) {
		return false
	}
	return licenceOutsideEveryArena(w.arena, address)
}

// inRevokedSpan reports whether address lies in a data slab of this arena the walk
// revoked whole (cachedBase or spans), leaving the cache's order alone.
//
// Takes address (uintptr) which is the address.
//
// Returns bool which is true for such an address.
func (w *licenceWalk) inRevokedSpan(address uintptr) bool {
	if address == 0 || w.typedOff {
		return false
	}
	if address >= w.cachedBase && address < w.cachedEnd {
		return true
	}
	for index := range w.spans {
		if span := w.spans[index]; address >= span.base && address < span.end {
			return true
		}
	}
	return false
}

// memoNote records the Go-heap container at pointer as followed to the end, unless the
// walk ran out or stopped, in which case it revokes the epoch and no later walk runs.
//
// Takes pointer (unsafe.Pointer) which is the container's address.
// Takes typ (unsafe.Pointer) which is its type.
// Takes extent (int) which is the slice capacity walked, or zero.
func (w *licenceWalk) memoNote(pointer, typ unsafe.Pointer, extent int) {
	if w.memo == nil || w.memoReadOnly || w.exhausted || w.epochStop || w.opaque || w.stringsSkipped || !w.memoEligible(pointer) {
		return
	}
	w.memo.note(pointer, typ, extent)
}

// private reports whether address lies in storage no other goroutine can reach.
//
// Takes address (uintptr) which is the address of an addressable value.
//
// Returns bool which is true when a shared walk may read it.
func (w *licenceWalk) private(address uintptr) bool {
	if !licenceRegisteredPage(w.arena, address) {
		return false
	}
	base, class := uintptr(0), licenceSlabData
	if found, _, ok := w.dataSlabSpan(address); ok {
		base = found
	} else {
		base, class = w.arena.licenceNonDataSlabOf(address)
	}
	switch class {
	case licenceSlabData:
		if _, revoked := w.arena.licenceState.revoked[base]; !revoked {
			return true
		}
		return slices.Contains(w.fresh, base)
	case licenceSlabBank:
		return w.banksPrivate
	case licenceSlabNone:
		return w.parkedPrivate(address)
	default:
		return false
	}
}

// dataSlabSpan is licenceDataSlabSpan through the walk's one-entry cache.
//
// Takes address (uintptr) which is the address.
//
// Returns start (uintptr) which is the holding span's first address.
// Returns end (uintptr) which is one past its last byte by capacity.
// Returns found (bool) which is false when no span holds address.
func (w *licenceWalk) dataSlabSpan(address uintptr) (start, end uintptr, found bool) {
	if !w.typedOff && address >= w.dataBase && address < w.dataEnd {
		return w.dataBase, w.dataEnd, true
	}
	base, end, ok := licenceDataSlabSpan(w.arena, address)
	if ok {
		w.dataBase, w.dataEnd = base, end
	}
	return base, end, ok
}

// revoke revokes the slab holding address, remembering a data slab it newly revoked when
// the walk is shared, so the walk may still read what it is about to hand off.
//
// Takes address (uintptr) which is the address referenced.
func (w *licenceWalk) revoke(address uintptr) {
	if address == 0 || address >= w.cachedBase && address < w.cachedEnd || w.cachedSpan(address) {
		return
	}
	arena := w.arena
	if !licenceRegisteredPage(arena, address) {
		return
	}
	base, end, ok := w.dataSlabSpan(address)
	if !ok {
		arena.revokeNonDataAddress(address, w.cause)
		return
	}
	if w.shared {
		if _, revoked := arena.licenceState.revoked[base]; !revoked {
			w.fresh = append(w.fresh, base)
		}
	}
	arena.revokeSlab(base, w.cause)
	w.cacheSpan(base, end)
}

// visitBoxedStorage revokes the storage an interface would share with value.
//
// value is indirect, not addressable and not pointer-shaped.
//
// Takes value (reflect.Value) which is the boxed value.
func (w *licenceWalk) visitBoxedStorage(value reflect.Value) {
	switch value.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer, reflect.Interface:
		return
	default:
	}
	raw := (*unsafeReflectValue)(unsafe.Pointer(&value))
	if raw.flag&flagIndir != 0 && raw.flag&flagAddr == 0 {
		w.revokeExtent(uintptr(raw.ptr), value.Type().Size())
	}
}

// visitSlice revokes a slice's backing and walks its elements up to capacity, since a
// reslice can reach them.
//
// Takes value (reflect.Value) which is the slice.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) visitSlice(value reflect.Value, depth int) {
	if value.Cap() == 0 {
		return
	}
	extentBytes := uintptr(value.Cap()) * value.Type().Elem().Size()
	if licenceTypeHoldsNoPointers(value.Type().Elem()) {
		w.revokeExtent(uintptr(value.UnsafePointer()), extentBytes)
		return
	}
	pointer, typ, extent := value.UnsafePointer(), reflectValueABIType(value.Type().Elem()), value.Cap()
	if w.memoCovers(value, pointer, typ, extent, depth) {
		return
	}
	w.revokeExtent(uintptr(pointer), extentBytes)
	defer w.memoNote(pointer, typ, extent)
	if w.shared && licenceTypeHoldsOnlyStrings(value.Type().Elem()) && !w.private(uintptr(value.UnsafePointer())) {
		w.stringsSkipped = true
		return
	}
	w.visitBacking(value, depth)
}

// visitBacking walks a slice's elements up to its capacity, addressable as Index returns
// them, reading the backing directly rather than re-slicing it to its capacity, which
// allocates.
//
// Takes value (reflect.Value) which is a slice with capacity and pointer-bearing
// elements.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) visitBacking(value reflect.Value, depth int) {
	elementType := value.Type().Elem()
	abiType, kind, size := reflectValueABIType(elementType), elementType.Kind(), elementType.Size()
	data := value.UnsafePointer()
	for index := range value.Cap() {
		if w.exhausted {
			return
		}
		w.visit(licenceSliceElement(abiType, kind, size, data, index), false, depth-1)
	}
}

// visitElements walks the first count elements of a slice or array.
//
// Takes value (reflect.Value) which is the slice or array.
// Takes count (int) which is the number of elements.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) visitElements(value reflect.Value, count int, depth int) {
	for index := range count {
		if w.exhausted {
			return
		}
		w.visit(value.Index(index), false, depth-1)
	}
}

// visitMap walks a map's keys and values.
//
// Takes value (reflect.Value) which is the map.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) visitMap(value reflect.Value, depth int) {
	if value.IsNil() || licenceMapHoldsNoPointers(value.Type()) {
		return
	}
	mapType := value.Type()
	pointer, typ := value.UnsafePointer(), reflectValueABIType(mapType)
	if w.memoCovers(value, pointer, typ, 0, depth) {
		return
	}
	if value.Len() > 1 {
		w.mapOrder = true
	}
	defer w.memoNote(pointer, typ, 0)
	var key, element reflect.Value
	if keyType := mapType.Key(); !licenceTypeHoldsNoPointers(keyType) {
		key = w.arena.licenceScratch(keyType, 2*depth)
	}
	if elementType := mapType.Elem(); !licenceTypeHoldsNoPointers(elementType) {
		element = w.arena.licenceScratch(elementType, 2*depth+1)
	}
	var iterator reflect.MapIter
	iterator.Reset(licenceReadable(value))
	w.visitMapEntries(&iterator, key, element, depth)
	iterator.Reset(reflect.Value{})
	if key.IsValid() {
		key.SetZero()
	}
	if element.IsValid() {
		element.SetZero()
	}
}

// visitMapEntries walks a map's keys and elements through scratch values, until the walk
// is exhausted.
//
// Takes iterator (*reflect.MapIter) which iterates the map.
// Takes key (reflect.Value) which receives each key, or is invalid when keys hold no
// pointers.
// Takes element (reflect.Value) which receives each element, or is invalid when elements
// hold no pointers.
// Takes depth (int) which is the map's remaining nesting allowance.
func (w *licenceWalk) visitMapEntries(iterator *reflect.MapIter, key, element reflect.Value, depth int) {
	for iterator.Next() {
		if w.exhausted {
			return
		}
		if key.IsValid() {
			key.SetIterKey(iterator)
			w.visit(key, false, depth-1)
		}
		if element.IsValid() && !w.exhausted {
			element.SetIterValue(iterator)
			w.visit(element, false, depth-1)
		}
	}
}

// licenceScratch returns a settable value of type valueType that no enclosing visit of
// the walk is using, made once per type and slot and kept for the arena's later walks, so
// iterating a map copies its keys and elements without allocating.
//
// Takes valueType (reflect.Type) which is the type.
// Takes slot (int) which distinguishes nested uses.
//
// Returns reflect.Value which is settable and zero or holding a stale copy.
func (a *RegisterArena) licenceScratch(valueType reflect.Type, slot int) reflect.Value {
	scratch := a.licenceState.scratch
	for index := range scratch {
		if scratch[index].typ == valueType && scratch[index].slot == slot {
			return scratch[index].value
		}
	}
	value := reflect.New(valueType).Elem()
	if len(scratch) < licenceScratchLimit {
		a.licenceState.scratch = append(a.licenceState.scratch, licenceScratchSlot{typ: valueType, slot: slot, value: value})
	}
	return value
}

// licenceTypeHoldsOnlyStrings reports whether every pointer in values of valueType is a
// string's data pointer, so what they reference lies in byte slabs.
//
// Takes valueType (reflect.Type) which is the type.
//
// Returns bool which is true for a string, or an array or struct of strings and
// pointer-free values.
func licenceTypeHoldsOnlyStrings(valueType reflect.Type) bool {
	switch valueType.Kind() {
	case reflect.String:
		return true
	case reflect.Array:
		return licenceTypeHoldsNoPointers(valueType.Elem()) || licenceTypeHoldsOnlyStrings(valueType.Elem())
	case reflect.Struct:
		for index := range valueType.NumField() {
			field := structFieldType(valueType, index).Type
			if !licenceTypeHoldsNoPointers(field) && !licenceTypeHoldsOnlyStrings(field) {
				return false
			}
		}
		return true
	default:
		return licenceTypeHoldsNoPointers(valueType)
	}
}

// licenceMapHoldsNoPointers reports whether a map type's keys and values hold no
// pointers, so nothing in the map can reference arena storage.
//
// Takes t (reflect.Type) which is a map type.
//
// Returns bool which is true when both are pointer-free.
func licenceMapHoldsNoPointers(t reflect.Type) bool {
	return licenceTypeHoldsNoPointers(t.Key()) && licenceTypeHoldsNoPointers(t.Elem())
}

// licenceSlabSpan is licenceSlabBase that also returns the slab's end by capacity.
//
// Takes slab ([]T) which is the slab.
// Takes address (uintptr) which is the address.
//
// Returns start (uintptr) which is the holding span's first address.
// Returns end (uintptr) which is one past its last byte by capacity.
// Returns found (bool) which is false when no span holds address.
func licenceSlabSpan[T any](slab []T, address uintptr) (start, end uintptr, found bool) {
	if cap(slab) == 0 {
		return 0, 0, false
	}
	var zero T
	start = uintptr(unsafe.Pointer(unsafe.SliceData(slab)))
	end = start + uintptr(cap(slab))*unsafe.Sizeof(zero)
	return start, end, address >= start && address < end
}

// licenceOutsideEveryArena reports whether address lies outside this arena's storage and
// every registered slab.
//
// Takes arena (*RegisterArena) which is the walking arena.
// Takes address (uintptr) which is the address.
//
// Returns bool which is true for Go-heap memory no arena holds.
func licenceOutsideEveryArena(arena *RegisterArena, address uintptr) bool {
	if !licenceRegisteredPage(arena, address) {
		return true
	}
	if _, class := arena.licenceSlabOf(address); class != licenceSlabNone {
		return false
	}
	if _, registered := licenceSlabContaining(address); registered {
		return false
	}
	_, _, found := arenaSlabContaining(address)
	return !found
}
