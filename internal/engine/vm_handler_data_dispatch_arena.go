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
	"maps"
	"reflect"
	"strings"
	"unsafe"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// hostMaterialiseDepth bounds the container nesting the host-argument walk follows.
	hostMaterialiseDepth = 6

	// hostMaterialiseLeafBudget bounds the number of elements the host-argument walk visits.
	hostMaterialiseLeafBudget = 65536
)

// ResetTypeIsPointerFreeCacheForTest installs an empty cache so successive test runs
// cannot observe pointer-free decisions from a previous test.
//
// Concurrency: acquires typeIsPointerFreeMu for the duration of the swap.
func ResetTypeIsPointerFreeCacheForTest() {
	typeIsPointerFreeMu.Lock()
	defer typeIsPointerFreeMu.Unlock()
	typeIsPointerFreeCache.Store(new(make(map[unsafe.Pointer]bool, typeIsPointerFreeCacheInitialHint)))
}

// ValueCopyForBoundary enforces Go's value-copy semantics at general-bank boundaries by
// giving Struct and Array kinds independent storage while letting reference kinds pass
// through, since field and element reads must not call it because they produce lvalues
// that alias the parent's storage.
//
// Takes v (reflect.Value) which is the source value that may alias caller storage.
//
// Returns a fresh independent reflect.Value for Struct/Array, or the input unchanged.
func ValueCopyForBoundary(v reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	switch v.Kind() {
	case reflect.Struct, reflect.Array:
		return copyReflectValue(v)
	default:
		return v
	}
}

// MaterialiseArenaValue forces an arena-backed value onto the heap so it can outlive the
// bump pointer's window, heap-copying Struct/Array kinds from the byte slab and giving
// arena-owned Slices a fresh heap header and backing while letting other kinds pass
// through.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes v (reflect.Value) which is the value about to escape.
//
// Returns the heap-materialised equivalent of v when it was arena-backed, or v unchanged.
func MaterialiseArenaValue(arena *RegisterArena, v reflect.Value) reflect.Value {
	if !v.IsValid() || arena == nil {
		return v
	}
	if !arenaUsesUnsafeSlabs && !arenaResidentCompositeKind(v.Kind()) {
		return v
	}
	if v.Kind() == reflect.Slice && !arena.ownsSliceBackingOf(v) {
		return v
	}
	return materialiseArenaValueUnconditional(arena, v)
}

// MaterialiseArenaSliceUnconditional severs slice arena aliasing by heap-copying both the
// header and backing when either is arena-owned.
//
// Handles the slice branch of materialiseArenaValueUnconditional. Ownership is decided by
// the backing, not the header: a heap backing was filled through the store barrier, so
// its elements are already heap-clean and the value passes through untouched, or is
// re-wrapped on a heap header when the header itself lives in the arena's slice-header
// slab (the shape produced by packing a typed register). Only an arena-owned backing is
// copied, with its elements materialised through the fresh storage and its capacity kept
// (bounded to twice the length) so a slice that grows through repeated heap stores
// amortises instead of copying on every store.
//
// Use this when the captured slice must outlive the arena (goroutine launches that copy
// args across arenas, eval-result returns crossing the runEntrypointFunction boundary,
// panic-info capture, heap stores). For same-arena lifetime captures (defer args,
// simpleDefer args) prefer materialiseArenaSliceAliasing, which never copies the backing
// so mutations through the captured slice propagate to the caller's view.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes v (reflect.Value) which is the slice to materialise.
//
// Returns reflect.Value which is the heap-detached slice.
func MaterialiseArenaSliceUnconditional(arena *RegisterArena, v reflect.Value) reflect.Value {
	if !arena.ownsSliceBackingOf(v) {
		if !arena.OwnsSliceHeaderPointer(ReflectValuePtr(v)) {
			return v
		}
		return rewrapSliceHeaderOnHeap(v)
	}
	n := v.Len()
	heapCapacity := min(v.Cap(), 2*n)
	if asBytes, ok := reflect.TypeAssert[[]byte](v); ok {
		destination := make([]byte, n, heapCapacity)
		copy(destination, asBytes)
		return reflect.ValueOf(destination)
	}
	t := v.Type()
	if typeIsPointerFree(t.Elem()) {
		return materialisePointerFreeSlice(v, t, n, heapCapacity)
	}
	fresh := reflect.MakeSlice(t, n, heapCapacity)
	reflect.Copy(fresh, v)
	for i := range n {
		elem := fresh.Index(i)
		materialised := materialiseArenaValueUnconditional(arena, elem)
		if materialised.IsValid() && materialised != elem {
			elem.Set(materialised)
		}
	}
	return fresh
}

// materialisePointerFreeSlice heap-copies a pointer-free slice in a single allocation
// that holds both the header and the element region. The block carries no GC metadata,
// which is safe because the elements have no pointers and the header's Data points into
// the same block.
//
// Takes v (reflect.Value) which is the arena-backed slice to copy.
// Takes t (reflect.Type) which is v's slice type.
// Takes n (int) which is v's length.
// Takes heapCapacity (int) which is the capacity of the copy, at least n.
//
// Returns reflect.Value which is the heap-detached slice of type t.
func materialisePointerFreeSlice(v reflect.Value, t reflect.Type, n, heapCapacity int) reflect.Value {
	elementSize := t.Elem().Size()
	var headerLayout arenaSliceHeader
	dataOffset := (unsafe.Sizeof(headerLayout) + uintptr(t.Elem().Align()) - 1) &^ (uintptr(t.Elem().Align()) - 1)
	block := make([]byte, dataOffset+uintptr(heapCapacity)*elementSize)
	header := (*arenaSliceHeader)(unsafe.Pointer(unsafe.SliceData(block)))
	dataPointer := unsafe.Pointer(unsafe.SliceData(block))
	if heapCapacity > 0 {
		dataPointer = unsafe.Add(dataPointer, dataOffset)
	}
	header.Data = dataPointer
	header.Len = n
	header.Cap = heapCapacity
	if n > 0 {
		source := unsafe.Slice((*byte)(v.UnsafePointer()), uintptr(n)*elementSize)
		copy(block[dataOffset:], source)
	}
	return unsafeNewAt(reflectValueABIType(t), unsafe.Pointer(header), reflect.Slice)
}

// Pointer materialisation must preserve identity, not duplicate: deep-copying an
// arena-backed pointee to the heap would break pointer aliasing because the original
// arena value and the copied heap value would diverge on subsequent writes.
// handleAllocIndirect's arena routing is therefore gated on the program staying within
// one top- level execution (the arena Reset happens between executions).

// MaterialiseHostArguments copies arena-resident values out of host containers before the
// arena is released, so the host does not retain headers into a pooled arena.
//
// Takes arena (*RegisterArena) which owns the run's storage.
// Takes arguments ([]reflect.Value) which are the host's arguments.
func MaterialiseHostArguments(arena *RegisterArena, arguments []reflect.Value) {
	if arena == nil {
		return
	}
	budget := hostMaterialiseLeafBudget
	for _, argument := range arguments {
		materialiseHostValueInPlace(arena, argument, hostMaterialiseDepth, &budget)
	}
}

// MaterialiseAnyForArena returns a heap-materialised copy of v when v is arena-backed.
// Pointer/Chan/Func kinds are preserved by identity because deep-copying would break
// aliasing semantics.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes v (any) which is the candidate value.
//
// Returns the heap-materialised equivalent when arena-backed, or v unchanged.
func MaterialiseAnyForArena(arena *RegisterArena, v any) any {
	if v == nil || arena == nil {
		return v
	}
	if s, ok := v.(string); ok {
		return materialiseStringUnconditional(arena, s)
	}
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return v
	}
	return walkArenaEscape(arena, rv).Interface()
}

// copyReflectValue returns an independent copy of v with freshly allocated backing.
//
// Used by snapshot paths (return-value placement and the deref-and-snapshot variant of
// isa.OpDeref) to break aliasing between caller and heap memory: assigning the result to
// a general register gives Go-style value-copy semantics even though reflect.Value
// normally shares its addressable backing storage with the source. For pointer and
// interface kinds this preserves the shared pointee (only the header is copied), which
// matches Go's pointer-copy semantics. For slices, maps, channels, and funcs the header
// is copied; the underlying buffer or runtime object remains shared, again matching Go.
//
// Takes v (reflect.Value) which must be Valid; the caller checks.
//
// Returns a reflect.Value of the same Type as v whose memory is owned by a fresh
// reflect.New allocation.
func copyReflectValue(v reflect.Value) reflect.Value {
	snapshot := reflect.New(v.Type()).Elem()
	snapshot.Set(v)
	return snapshot
}

// copyReflectValueArena is the arena-aware sibling of copyReflectValue.
//
// For types whose memory layout is provably pointer-free (typeIsPointerFree) it
// bump-allocates a fresh slot from arena.genericBytesSlab instead of invoking reflect.New
// Pointer-containing types fall back to copyReflectValue because pointers in a []byte
// slab are invisible to the GC. Arena is required, not nil-safe.
//
// Takes arena (*RegisterArena) which is the bump arena to allocate from.
// Takes v (reflect.Value) which is the source value to copy.
//
// Returns a reflect.Value independent of v.
func copyReflectValueArena(arena *RegisterArena, v reflect.Value) reflect.Value {
	return copyReflectValueArenaWithVM(arena, nil, v)
}

// copyReflectValueArenaWithVM is the vm-aware sibling of copyReflectValueArena. When vm
// is non-nil, pointer-containing Struct/Array kinds use the vm's boundary snapshot slabs
// instead of reflect.New.
//
// Takes arena (*RegisterArena) which is the bump arena.
// Takes vm (*VM) which provides per-type boundary snapshot slabs; may be nil.
// Takes v (reflect.Value) which is the source value to copy.
//
// Returns a reflect.Value independent of v.
func copyReflectValueArenaWithVM(arena *RegisterArena, vm *VM, v reflect.Value) reflect.Value {
	if arena == nil {
		return copyReflectValue(v)
	}
	switch v.Kind() {
	case reflect.Slice:
		return snapshotSliceHeaderArena(arena, v)
	case reflect.Pointer:
		return unsafePointerKindValue(reflectValueABIType(v.Type()), v.UnsafePointer())
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128:
		return snapshotPointerFreeValue(arena, v)
	case reflect.Struct, reflect.Array:
		return snapshotComposite(arena, vm, v)
	default:
	}
	return copyReflectValue(v)
}

// snapshotSliceHeaderArena copies the slice header into an arena slot, sharing the
// backing array. The slab's Data field is unsafe.Pointer so the backing stays
// GC-reachable.
//
// Takes arena (*RegisterArena) which provides the header slab.
// Takes v (reflect.Value) which is the source slice.
//
// Returns a reflect.Value with the arena-resident header.
func snapshotSliceHeaderArena(arena *RegisterArena, v reflect.Value) reflect.Value {
	v = ensureSliceAddressableForHeader(v)
	t := v.Type()
	ptr := ReflectValuePtr(v)
	srcHeader := (*arenaSliceHeader)(ptr)
	slot := arena.allocSliceHeader()
	slot.Data = srcHeader.Data
	slot.Len = srcHeader.Len
	slot.Cap = srcHeader.Cap
	return unsafeNewAt(reflectValueABIType(t), unsafe.Pointer(slot), reflect.Slice)
}

// snapshotPointerFreeValue allocates a fresh arena byte-slab copy of a pointer-free
// value, falling back to copyReflectValue when the type's alignment requirement exceeds
// the slab guarantee.
//
// Takes arena (*RegisterArena) which provides the byte slab.
// Takes v (reflect.Value) which is the source pointer-free value.
//
// Returns a reflect.Value with arena-backed storage holding a value copy of v.
func snapshotPointerFreeValue(arena *RegisterArena, v reflect.Value) reflect.Value {
	t := v.Type()
	align := safeconv.IntToUintptr(t.Align())
	if align == 0 {
		align = 1
	}
	if align > arenaMaxAlignment {
		return copyReflectValue(v)
	}
	pointer := arena.AllocBytes(t.Size(), align)
	snapshot := unsafeNewAt(reflectValueABIType(t), pointer, t.Kind())
	snapshot.Set(v)
	return snapshot
}

// snapshotComposite handles Struct and Array kinds, routing pointer- free composites to
// the arena byte slab and pointer-containing composites to the per-VM boundary-snapshot
// chunk slab (one mallocgc per chunk of boundaryChunkSlots snapshots instead of one per
// snapshot).
//
// Takes arena (*RegisterArena) which provides the byte slab when the composite is
// pointer-free.
// Takes vm (*VM) which provides the boundary-snapshot slab when the composite contains
// pointers; may be nil, in which case the snapshot falls back to reflect.New.
// Takes v (reflect.Value) which is the source composite.
//
// Returns a reflect.Value independent of v.
func snapshotComposite(arena *RegisterArena, vm *VM, v reflect.Value) reflect.Value {
	t := v.Type()
	if typeIsPointerFree(t) {
		return snapshotPointerFreeValue(arena, v)
	}
	if vm != nil {
		snapshot := vm.acquireBoundarySnapshot(t)
		snapshot.Set(v)
		return snapshot
	}
	return copyReflectValue(v)
}

func init() {
	typeIsPointerFreeCache.Store(new(make(map[unsafe.Pointer]bool, typeIsPointerFreeCacheInitialHint)))
}

// typeIsPointerFree reports whether t's memory layout contains no GC pointers, making it
// safe to store in a []byte arena slab.
//
// Takes t (reflect.Type) which is the type to inspect.
//
// Returns true when t is pointer-free.
func typeIsPointerFree(t reflect.Type) bool {
	if t == nil {
		return false
	}
	key := reflectValueABIType(t)
	current := typeIsPointerFreeCache.Load()
	if cached, ok := (*current)[key]; ok {
		return cached
	}
	return typeIsPointerFreeSlow(t, key)
}

// typeIsPointerFreeSlow handles the cold path: compute the answer, publish a new map via
// copy-on-write under the writer lock, then return. The reader fast path never touches
// the lock.
//
// Takes t (reflect.Type) which is the type to inspect.
// Takes key (unsafe.Pointer) which is the cache key (t's *abi.Type pointer).
//
// Returns true when t is pointer-free, false otherwise.
//
// Concurrency: takes typeIsPointerFreeMu to publish the updated cache; readers consult
// the atomic snapshot without locking.
func typeIsPointerFreeSlow(t reflect.Type, key unsafe.Pointer) bool {
	result := computeTypeIsPointerFree(t)
	typeIsPointerFreeMu.Lock()
	current := typeIsPointerFreeCache.Load()
	if cached, ok := (*current)[key]; ok {
		typeIsPointerFreeMu.Unlock()
		return cached
	}
	next := make(map[unsafe.Pointer]bool, len(*current)+1)
	maps.Copy(next, *current)
	next[key] = result
	typeIsPointerFreeCache.Store(&next)
	typeIsPointerFreeMu.Unlock()
	return result
}

// computeTypeIsPointerFree walks the type tree to determine pointer- freedom. Called once
// per unique type and cached.
//
// Takes t (reflect.Type) which is the type to inspect; nil returns false.
//
// Returns true when t and all its component types are pointer-free, false otherwise.
func computeTypeIsPointerFree(t reflect.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128:
		return true
	case reflect.Array:
		return computeTypeIsPointerFree(t.Elem())
	case reflect.Struct:
		for field := range t.Fields() {
			if !computeTypeIsPointerFree(field.Type) {
				return false
			}
		}
		return true
	default:

		return false
	}
}

// valueCopyForBoundaryArena is the arena-aware sibling of ValueCopyForBoundary.
// Pointer-free Struct/Array kinds are bump-allocated from the arena byte slab.
//
// Takes arena (*RegisterArena) which is the bump arena to allocate from.
// Takes v (reflect.Value) which is the value to snapshot.
//
// Returns a fresh independent reflect.Value for Struct/Array, or v unchanged.
func valueCopyForBoundaryArena(arena *RegisterArena, v reflect.Value) reflect.Value {
	return valueCopyForBoundaryArenaWithVM(arena, nil, v)
}

// valueCopyForBoundaryArenaWithVM is the vm-aware variant of valueCopyForBoundaryArena.
//
// Passes vm through so pointer-containing struct/array kinds can bump from the per-type
// boundary slab instead of reflect.New.
//
// Takes arena (*RegisterArena) which is the bump arena to allocate from.
// Takes vm (*VM) which provides the per-type boundary snapshot slabs; may be nil.
// Takes v (reflect.Value) which is the value to snapshot.
//
// Returns either a fresh independent reflect.Value (Struct/Array) or v unchanged.
func valueCopyForBoundaryArenaWithVM(arena *RegisterArena, vm *VM, v reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	switch v.Kind() {
	case reflect.Struct, reflect.Array:
		return copyReflectValueArenaWithVM(arena, vm, v)
	default:
		return v
	}
}

// arenaResidentCompositeKind reports whether values of kind use arena-resident storage in
// every build. Struct and array literals come from AllocBytes regardless of build mode.
//
// Takes kind (reflect.Kind) which is the kind of the value about to escape.
//
// Returns true for struct and array kinds.
func arenaResidentCompositeKind(kind reflect.Kind) bool {
	return kind == reflect.Struct || kind == reflect.Array
}

// materialiseArenaValueUnconditional applies the heap-escape copy in every build. Used by
// boundary paths (return path, panic capture, closure capture) that must detach a value
// even where the safe build's store barrier is folded away.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes v (reflect.Value) which is about to escape the arena.
//
// Returns a heap-materialised equivalent when arena-backed; v unchanged otherwise.
func materialiseArenaValueUnconditional(arena *RegisterArena, v reflect.Value) reflect.Value {
	return materialiseArenaValueByMode(arena, v, false)
}

// materialiseArenaValueByMode is the shared escape-copy core for both the aliasing and
// non-aliasing barriers. The two callers differ only in the slice branch (fresh-copy the
// backing versus preserve Data aliasing) and the composite recursion mode, both selected
// by aliasing; every other branch is identical, so the logic lives here once.
//
// Strings detach both their bytes (byte slabs) and their box (the string box slab behind
// boxStringToGeneral); scalars boxed in the int, float, uint and complex box slabs are
// re-boxed on the heap. Maps are walked in place in the non-aliasing mode so entries
// written outside the store barrier are detached without breaking map identity; interface
// values re-wrap their materialised concrete value. Pointer, Chan and Func values keep
// their identity: deep-copying a pointee would fork the aliases a program relies on, so
// pointer targets are protected by the barrier at the store into the pointee instead.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes v (reflect.Value) which is about to escape the arena.
// Takes aliasing (bool) which preserves same-arena-lifetime slice aliasing when true and
// severs it for cross-arena safety when false.
//
// Returns a heap-materialised equivalent when arena-backed; v unchanged otherwise.
func materialiseArenaValueByMode(arena *RegisterArena, v reflect.Value, aliasing bool) reflect.Value {
	if !v.IsValid() {
		return v
	}
	switch v.Kind() {
	case reflect.Struct, reflect.Array:
		if typeIsPointerFree(v.Type()) {
			if !arena.ownsBytePointer(ReflectValuePtr(v)) {
				return v
			}
			return copyReflectValue(v)
		}
		return materialiseArenaCompositeFields(arena, v, aliasing)
	case reflect.Slice:
		if aliasing {
			return materialiseArenaSliceAliasing(arena, v)
		}
		return MaterialiseArenaSliceUnconditional(arena, v)
	case reflect.String:
		s := v.String()
		if arena.OwnsString(s) {
			return reflect.ValueOf(strings.Clone(s)).Convert(v.Type())
		}
		if arena.OwnsStringBox(ReflectValuePtr(v)) {
			return reflect.ValueOf(s).Convert(v.Type())
		}
		return v
	case reflect.Map:
		if !aliasing {
			materialiseArenaMapEntries(arena, v)
		}
		return v
	case reflect.Interface:
		return materialiseArenaInterfaceValue(arena, v, aliasing)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return materialiseArenaScalarBox(arena, v)
	default:
		return v
	}
}

// materialiseArenaScalarBox re-boxes a scalar whose reflect.Value storage is a slot in
// one of the arena's scalar box slabs, so the value survives the arena's reset once it
// escapes into an interface or a general-bank container on the heap. Scalars boxed on the
// heap, or held by value, are returned unchanged.
//
// Takes arena (*RegisterArena) which owns the candidate box slabs.
// Takes v (reflect.Value) which is the scalar to detach.
//
// Returns a heap-boxed copy when the storage lives in an arena box slab, else v.
func materialiseArenaScalarBox(arena *RegisterArena, v reflect.Value) reflect.Value {
	if !arena.ownsScalarBox(ReflectValuePtr(v)) {
		return v
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflect.ValueOf(v.Int()).Convert(v.Type())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return reflect.ValueOf(v.Uint()).Convert(v.Type())
	case reflect.Float32, reflect.Float64:
		return reflect.ValueOf(v.Float()).Convert(v.Type())
	default:
		return reflect.ValueOf(v.Complex()).Convert(v.Type())
	}
}

// materialiseArenaMapEntries detaches arena-backed keys and values held by a heap map,
// rewriting the affected entries in place so the map keeps its identity. Every
// interpreted map store already runs the barrier, so the walk normally finds nothing and
// costs one ownership probe per entry.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes m (reflect.Value) which is the map to scrub; nil maps are ignored.
func materialiseArenaMapEntries(arena *RegisterArena, m reflect.Value) {
	if m.IsNil() {
		return
	}
	iter := m.MapRange()
	for iter.Next() {
		key := iter.Key()
		value := iter.Value()
		materialisedKey := materialiseArenaValueUnconditional(arena, key)
		materialisedValue := materialiseArenaValueUnconditional(arena, value)
		if materialisedKey == key && materialisedValue == value {
			continue
		}
		if materialisedKey != key {
			m.SetMapIndex(key, reflect.Value{})
		}
		m.SetMapIndex(materialisedKey, materialisedValue)
	}
}

// materialiseArenaInterfaceValue detaches the concrete value held by an interface-kinded
// reflect.Value, re-wrapping it in the same interface type when it changed.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes v (reflect.Value) which is the interface value about to escape.
// Takes aliasing (bool) which selects the composite recursion mode.
//
// Returns the re-wrapped interface value when its concrete value was arena-backed; v
// unchanged otherwise.
func materialiseArenaInterfaceValue(arena *RegisterArena, v reflect.Value, aliasing bool) reflect.Value {
	if v.IsNil() {
		return v
	}
	inner := v.Elem()
	materialised := materialiseArenaValueByMode(arena, inner, aliasing)
	if !materialised.IsValid() || materialised == inner {
		return v
	}
	wrapped := reflect.New(v.Type()).Elem()
	wrapped.Set(materialised)
	return wrapped
}

// materialiseArenaCompositeFields walks the fields of a pointer-bearing struct or array
// and recursively materialises any that alias the arena. An addressable value whose
// fields are all heap-backed is returned untouched.
//
// Takes arena (*RegisterArena) which is the allocation arena.
// Takes v (reflect.Value) which is the composite value whose fields are materialised.
// Takes aliasing (bool) which preserves Data aliasing when true and severs it when false.
//
// Returns the materialised composite, or the original value when no field aliased the
// arena.
func materialiseArenaCompositeFields(arena *RegisterArena, v reflect.Value, aliasing bool) reflect.Value {
	storageOwned := arena.ownsBytePointer(ReflectValuePtr(v))

	work := v
	copied := false
	if storageOwned || !v.CanAddr() {
		work = copyReflectValue(v)
		copied = true
	}

	visit := func(field reflect.Value) (reflect.Value, bool) {
		if field.CanAddr() {
			field = unsafeNewAt(reflectValueABIType(field.Type()), ReflectValuePtr(field), field.Kind())
		}
		materialised := materialiseArenaValueByMode(arena, field, aliasing)
		return materialised, materialised.IsValid() && materialised != field
	}

	elementCount := compositeElementCount(work)
	for i := range elementCount {
		element := compositeElement(work, i)
		materialised, fieldChanged := visit(element)
		if !fieldChanged {
			continue
		}
		if !copied {
			work = copyReflectValue(v)
			copied = true
		}
		setReboundElement(compositeElement(work, i), materialised)
	}

	if copied {
		return work
	}
	return v
}

// compositeElementCount returns the field count for a struct or the length for an array;
// both are walked identically by the escape barrier.
//
// Takes v (reflect.Value) which is the struct or array to measure.
//
// Returns int which is the field count or array length.
func compositeElementCount(v reflect.Value) int {
	if v.Kind() == reflect.Struct {
		return v.NumField()
	}
	return v.Len()
}

// compositeElement returns field i of a struct or element i of an array.
//
// Takes v (reflect.Value) which is the struct or array to index.
// Takes index (int) which is the field or element position.
//
// Returns reflect.Value which is the selected field or element.
func compositeElement(v reflect.Value, index int) reflect.Value {
	if v.Kind() == reflect.Struct {
		return v.Field(index)
	}
	return v.Index(index)
}

// setReboundElement writes value into an addressable struct field or array element,
// rebinding through unsafeNewAt so an unexported field's flagRO bit does not block the
// Set.
//
// Takes destination (reflect.Value) which is the field or element to overwrite.
// Takes value (reflect.Value) which is the materialised replacement.
func setReboundElement(destination reflect.Value, value reflect.Value) {
	if destination.CanAddr() {
		destination = unsafeNewAt(reflectValueABIType(destination.Type()), ReflectValuePtr(destination), destination.Kind())
	}
	destination.Set(value)
}

// rewrapSliceHeaderOnHeap rebuilds v's slice header on the heap while keeping its Data
// pointer, so a reflect.Value that outlives the arena's slice-header slab keeps aliasing
// the original backing.
//
// Takes v (reflect.Value) which is a slice value whose header lives in the arena.
//
// Returns a reflect.Value of the same type with a heap-resident header.
func rewrapSliceHeaderOnHeap(v reflect.Value) reflect.Value {
	header := &arenaSliceHeader{
		Data: unsafe.Pointer(v.Pointer()),
		Len:  v.Len(),
		Cap:  v.Cap(),
	}
	return unsafeNewAt(reflectValueABIType(v.Type()), unsafe.Pointer(header), reflect.Slice)
}

// materialiseArenaValueAliasing detaches headers but preserves Data aliasing, so
// mutations through a captured slice propagate back to the caller. Only safe when the
// captured value's lifetime stays bounded by the arena's lifetime.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes v (reflect.Value) which is the value to capture.
//
// Returns reflect.Value whose header is heap-resident but whose Data still aliases the
// original backing.
func materialiseArenaValueAliasing(arena *RegisterArena, v reflect.Value) reflect.Value {
	return materialiseArenaValueByMode(arena, v, true)
}

// materialiseArenaSliceAliasing rewraps an arena-resident slice header on the heap while
// keeping the original Data pointer. Mutations through the resulting reflect.Value
// propagate to the caller's slice (Go's defer-and-mutate semantics).
//
// When the header is NOT arena-resident, recursively materialises element contents
// in-place and returns v unchanged (mirrors MaterialiseArenaSliceUnconditional's
// same-branch behaviour for heap-resident headers).
//
// Takes arena (*RegisterArena) which owns the slice-header slab.
// Takes v (reflect.Value) which is the slice value to materialise.
//
// Returns a reflect.Value with heap-resident header and original Data pointer.
func materialiseArenaSliceAliasing(arena *RegisterArena, v reflect.Value) reflect.Value {
	if !arena.OwnsSliceHeaderPointer(ReflectValuePtr(v)) {
		n := v.Len()
		for i := range n {
			elem := v.Index(i)
			materialised := materialiseArenaValueAliasing(arena, elem)
			if materialised.IsValid() && materialised != elem && elem.CanSet() {
				elem.Set(materialised)
			}
		}
		return v
	}
	return rewrapSliceHeaderOnHeap(v)
}

// materialiseHostValueInPlace rewrites the arena-resident values reachable from value
// inside the host's own containers.
//
// Takes arena (*RegisterArena) which owns the run's storage.
// Takes value (reflect.Value) which is the container to walk.
// Takes depth (int) which is the remaining nesting allowance.
// Takes budget (*int) which is the remaining element allowance.
func materialiseHostValueInPlace(arena *RegisterArena, value reflect.Value, depth int, budget *int) {
	if !value.IsValid() || depth == 0 || *budget <= 0 {
		return
	}
	switch value.Kind() {
	case reflect.Interface:
		if !value.IsNil() {
			materialiseHostValueInPlace(arena, value.Elem(), depth, budget)
		}
	case reflect.Pointer:
		if !value.IsNil() {
			materialiseHostValueInPlace(arena, value.Elem(), depth-1, budget)
		}
	case reflect.Map:
		materialiseHostMapInPlace(arena, value, budget)
	case reflect.Slice, reflect.Array:
		materialiseHostElementsInPlace(arena, value, budget)
	case reflect.Struct:
		materialiseHostFieldsInPlace(arena, value, budget)
	}
}

// materialiseHostMapInPlace re-sets every map value that lives in the arena.
//
// Takes arena (*RegisterArena) which owns the run's storage.
// Takes value (reflect.Value) which is the map; a nil map is left alone.
// Takes budget (*int) which is the remaining element allowance.
func materialiseHostMapInPlace(arena *RegisterArena, value reflect.Value, budget *int) {
	if value.IsNil() {
		return
	}
	iter := value.MapRange()
	for iter.Next() && *budget > 0 {
		*budget--
		element := iter.Value()
		if materialised := walkArenaEscape(arena, element); materialised != element {
			value.SetMapIndex(iter.Key(), materialised)
		}
	}
}

// materialiseHostElementsInPlace overwrites the settable slice or array elements that
// live in the arena.
//
// Takes arena (*RegisterArena) which owns the run's storage.
// Takes value (reflect.Value) which is the slice or array.
// Takes budget (*int) which is the remaining element allowance.
func materialiseHostElementsInPlace(arena *RegisterArena, value reflect.Value, budget *int) {
	for i := 0; i < value.Len() && *budget > 0; i++ {
		*budget--
		materialiseHostSlotInPlace(arena, value.Index(i))
	}
}

// materialiseHostFieldsInPlace overwrites the settable struct fields that live in the
// arena.
//
// Takes arena (*RegisterArena) which owns the run's storage.
// Takes value (reflect.Value) which is the struct.
// Takes budget (*int) which is the remaining element allowance.
func materialiseHostFieldsInPlace(arena *RegisterArena, value reflect.Value, budget *int) {
	for i := 0; i < value.NumField() && *budget > 0; i++ {
		*budget--
		materialiseHostSlotInPlace(arena, value.Field(i))
	}
}

// materialiseHostSlotInPlace overwrites one settable slot when its value lives in the
// arena.
//
// Takes arena (*RegisterArena) which owns the run's storage.
// Takes slot (reflect.Value) which is the element or field; a slot that cannot be set is
// left alone.
func materialiseHostSlotInPlace(arena *RegisterArena, slot reflect.Value) {
	if !slot.CanSet() {
		return
	}
	if materialised := walkArenaEscape(arena, slot); materialised != slot {
		slot.Set(materialised)
	}
}

// walkArenaEscape is the reflect-driven recursive helper used by MaterialiseAnyForArena.
// For each kind it detaches arena-backed storage by allocating fresh heap memory and
// copying the contents through (cleansing nested arena references as it goes).
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes v (reflect.Value) which is the value to walk.
//
// Returns the heap-detached equivalent of v.
func walkArenaEscape(arena *RegisterArena, v reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	switch v.Kind() {
	case reflect.String:
		s := v.String()
		if !arena.OwnsString(s) {
			return v
		}
		return reflect.ValueOf(strings.Clone(s)).Convert(v.Type())
	case reflect.Struct, reflect.Array, reflect.Slice:
		return materialiseArenaValueUnconditional(arena, v)
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		fresh := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			fresh.SetMapIndex(walkArenaEscape(arena, iter.Key()), walkArenaEscape(arena, iter.Value()))
		}
		return fresh
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		inner := walkArenaEscape(arena, v.Elem())
		if !inner.IsValid() {
			return v
		}
		wrapped := reflect.New(v.Type()).Elem()
		wrapped.Set(inner)
		return wrapped
	default:
		return v
	}
}

// handleConvert performs a type conversion on a general register value using the target
// type from the function's type table, or allocates a new pointer.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the type table index extension.
// Takes registers (*Registers) which holds the source and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleConvert(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	typeIndex := uint16(extensionWord.A) | uint16(extensionWord.B)<<isa.WideBitShift
	if int(typeIndex) >= len(frame.Function.TypeTable) {
		vMBoundsError(vm, frame, boundsTableTypeTable, int(typeIndex), len(frame.Function.TypeTable))
		return opPanicError
	}
	reflectType := frame.Function.TypeTable[typeIndex]
	if instruction.C == 1 {
		registers.General[instruction.A] = reflect.New(reflectType)
		return opContinue
	}
	source := registers.General[instruction.B]
	if converted, handled, err := convertSliceToArray(source, reflectType); handled {
		if err != nil {
			return raiseNativePanicAsInterpreted(vm, err)
		}
		registers.General[instruction.A] = converted
		return opContinue
	}
	if !source.IsValid() {
		vMPanicInvalidRegister("handleConvert", "source", instruction.B, instruction, frame, registers)
	}
	converted, result := convertGeneralOperand(vm, source, reflectType)
	if result != opContinue {
		return result
	}
	registers.General[instruction.A] = converted
	return opContinue
}

// convertGeneralOperand converts a valid general-bank value to reflectType: a closure to
// a func type, a pointer through the checked unsafe conversion, a float to an integer
// with saturation, or any other convertible value through reflect.
//
// Takes vm (*VM) which owns the arena and pointer checks.
// Takes source (reflect.Value) which is the valid operand.
// Takes reflectType (reflect.Type) which is the target type.
//
// Returns the converted value and opContinue, or opPanicError when a pointer conversion
// was refused (the VM's error is set).
func convertGeneralOperand(vm *VM, source reflect.Value, reflectType reflect.Type) (reflect.Value, OpResult) {
	if converted, ok := convertClosureToFuncType(vm, source, reflectType); ok {
		return converted, opContinue
	}
	if unsafePointerConvertNeeded(source.Type(), reflectType) {
		converted, ok := vm.convertUnsafePointerChecked(source, reflectType)
		if !ok {
			return reflect.Value{}, opPanicError
		}
		return converted, opContinue
	}
	if converted, ok := saturatingFloatToIntConvert(vm.Arena, source, reflectType); ok {
		return converted, opContinue
	}
	return source.Convert(reflectType), opContinue
}

// saturatingFloatToIntConvert performs a saturating float to int convert.
//
// Uses Go's native cast (which saturates to the destination width's min and max when the
// float is out-of-range) instead of reflect.Value.Convert (which returns 0 for
// out-of-range floats). Required for Go-spec parity on snippets like `int32(1e20)` which
// Go saturates to MinInt32 but reflect.Convert produces 0.
//
// Takes arena (*RegisterArena) which provides arena-backed storage for the resulting
// reflect.Value when applicable.
// Takes source (reflect.Value) which is the value being converted.
// Takes destination (reflect.Type) which is the requested target type.
//
// Returns reflect.Value which is the saturated conversion result on the float-to-int fast
// path.
// Returns bool which reports whether the fast path applied; false for non-float sources,
// non-integer destinations, or unsupported widths.
func saturatingFloatToIntConvert(arena *RegisterArena, source reflect.Value, destination reflect.Type) (reflect.Value, bool) {
	if !source.IsValid() {
		return reflect.Value{}, false
	}
	sourceKind := source.Kind()
	if sourceKind != reflect.Float32 && sourceKind != reflect.Float64 {
		return reflect.Value{}, false
	}
	f := source.Float()
	destinationKind := destination.Kind()
	if arena == nil {
		return saturatingFloatToIntConvertFallback(f, destination, destinationKind)
	}
	destinationABI := reflectValueABIType(destination)
	switch destinationKind {
	case reflect.Int:
		slot := arena.allocIntBox(int64(int(f)))
		return unsafeNewAt(destinationABI, unsafe.Pointer(slot), reflect.Int), true
	case reflect.Int8:
		slot := arena.allocIntBox(int64(int8(f)))
		return unsafeNewAt(destinationABI, unsafe.Pointer(slot), reflect.Int8), true
	case reflect.Int16:
		slot := arena.allocIntBox(int64(int16(f)))
		return unsafeNewAt(destinationABI, unsafe.Pointer(slot), reflect.Int16), true
	case reflect.Int32:
		slot := arena.allocIntBox(int64(int32(f)))
		return unsafeNewAt(destinationABI, unsafe.Pointer(slot), reflect.Int32), true
	case reflect.Int64:
		slot := arena.allocIntBox(int64(f))
		return unsafeNewAt(destinationABI, unsafe.Pointer(slot), reflect.Int64), true
	case reflect.Uint:
		slot := arena.allocUintBox(uint64(uint(f)))
		return unsafeNewAt(destinationABI, unsafe.Pointer(slot), reflect.Uint), true
	case reflect.Uint8:
		slot := arena.allocUintBox(uint64(uint8(f)))
		return unsafeNewAt(destinationABI, unsafe.Pointer(slot), reflect.Uint8), true
	case reflect.Uint16:
		slot := arena.allocUintBox(uint64(uint16(f)))
		return unsafeNewAt(destinationABI, unsafe.Pointer(slot), reflect.Uint16), true
	case reflect.Uint32:
		slot := arena.allocUintBox(uint64(uint32(f)))
		return unsafeNewAt(destinationABI, unsafe.Pointer(slot), reflect.Uint32), true
	case reflect.Uint64:
		slot := arena.allocUintBox(uint64(f))
		return unsafeNewAt(destinationABI, unsafe.Pointer(slot), reflect.Uint64), true
	case reflect.Uintptr:
		slot := arena.allocUintBox(uint64(uintptr(f)))
		return unsafeNewAt(destinationABI, unsafe.Pointer(slot), reflect.Uintptr), true
	default:
	}
	return reflect.Value{}, false
}

// saturatingFloatToIntConvertFallback handles the non-arena code path (test scaffolds
// where vm.arena is nil). Pays the reflect.ValueOf + Convert allocations that the arena
// path eliminates; preserved so test build paths still produce correct results.
//
// Takes f (float64) the source float value.
// Takes destination (reflect.Type) the target integer type.
// Takes destinationKind (reflect.Kind) cached destination kind to avoid the second
// .Kind() call.
//
// Returns the converted reflect.Value and true when destinationKind is an integer kind;
// zero value and false otherwise.
func saturatingFloatToIntConvertFallback(f float64, destination reflect.Type, destinationKind reflect.Kind) (reflect.Value, bool) {
	switch destinationKind {
	case reflect.Int:
		return reflect.ValueOf(int(f)).Convert(destination), true
	case reflect.Int8:
		return reflect.ValueOf(int8(f)).Convert(destination), true
	case reflect.Int16:
		return reflect.ValueOf(int16(f)).Convert(destination), true
	case reflect.Int32:
		return reflect.ValueOf(int32(f)).Convert(destination), true
	case reflect.Int64:
		return reflect.ValueOf(int64(f)).Convert(destination), true
	case reflect.Uint:
		return reflect.ValueOf(uint(f)).Convert(destination), true
	case reflect.Uint8:
		return reflect.ValueOf(uint8(f)).Convert(destination), true
	case reflect.Uint16:
		return reflect.ValueOf(uint16(f)).Convert(destination), true
	case reflect.Uint32:
		return reflect.ValueOf(uint32(f)).Convert(destination), true
	case reflect.Uint64:
		return reflect.ValueOf(uint64(f)).Convert(destination), true
	case reflect.Uintptr:
		return reflect.ValueOf(uintptr(f)).Convert(destination), true
	default:
	}
	return reflect.Value{}, false
}

// unsafePointerConvertNeeded reports whether a conversion between source and destination
// types requires unsafe.Pointer intermediation.
//
// Takes source (reflect.Type) which is the source type to check.
// Takes destination (reflect.Type) which is the destination type to check.
//
// Returns bool indicating whether unsafe.Pointer intermediation is needed.
func unsafePointerConvertNeeded(source, destination reflect.Type) bool {
	sourceIsUnsafePointer := source == unsafePointerType
	destinationIsUnsafePointer := destination == unsafePointerType
	if !sourceIsUnsafePointer && !destinationIsUnsafePointer {
		return false
	}
	return sourceIsUnsafePointer != destinationIsUnsafePointer
}

// pointerWord returns the machine word a pointer-shaped or integer-shaped value carries.
//
// A uintptr that went through register arithmetic (`uintptr(p) + off`) reaches a
// conversion as an integer-kind Value, since the uint bank boxes as a plain integer, so
// the unsafe.Pointer conversion must accept integer kinds as well as pointer kinds.
//
// Takes source (reflect.Value) which must be a pointer, unsafe.Pointer or integer kind.
//
// Returns uintptr which is the address or integer the value holds.
func pointerWord(source reflect.Value) uintptr {
	switch source.Kind() {
	case reflect.Uintptr, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return uintptr(source.Uint())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return safeconv.IntToUintptr(safeconv.Int64ToInt(source.Int()))
	default:
		return source.Pointer()
	}
}

// convertUnsafePointer performs an unsafe.Pointer conversion between a pointer type and
// unsafe.Pointer, or vice versa.
//
// Takes source (reflect.Value) which is the source value to convert.
// Takes destinationType (reflect.Type) which is the target type to convert to.
//
// Returns reflect.Value holding the converted pointer value.
func convertUnsafePointer(source reflect.Value, destinationType reflect.Type) reflect.Value {
	if destinationType == unsafePointerType {
		return reflect.ValueOf(unsafe.Pointer(pointerWord(source)))
	}
	if destinationType.Kind() == reflect.Uintptr {
		converted := reflect.New(destinationType).Elem()
		converted.SetUint(uint64(source.Pointer()))
		return converted
	}
	pointer := unsafe.Pointer(source.Pointer())
	return reflect.NewAt(destinationType.Elem(), pointer)
}

// convertClosureToFuncType handles type conversions on an interpreted closure to a named
// or plain func type.
//
// The closure stays a *RuntimeClosure, and a copy records the target's named type or
// clears it for a plain func type, so a closure held in an interface dispatches the named
// type's methods and prints as it.
//
// Takes vm (*VM) which owns the type name registry.
// Takes source (reflect.Value) which is the value being converted.
// Takes reflectType (reflect.Type) which is the conversion target.
//
// Returns the converted value and true when source is a closure and the target a func
// type; false leaves the conversion to the generic paths.
func convertClosureToFuncType(vm *VM, source reflect.Value, reflectType reflect.Type) (reflect.Value, bool) {
	if reflectType.Kind() != reflect.Func || source.Type() != runtimeClosureReflectType {
		return reflect.Value{}, false
	}
	closure := closureFromValue(source)
	if closure == nil {
		return reflect.Value{}, false
	}
	name := ""
	if vm != nil && vm.rootFunction != nil && vm.rootFunction.TypeNames != nil {
		name = vm.rootFunction.TypeNames[reflectType]
	}
	if name == closure.namedType {
		return source, true
	}
	converted := *closure
	converted.namedType = name
	return reflect.ValueOf(&converted), true
}

// convertSliceToArray performs Go's slice-to-array and slice-to-array-pointer conversions
// with the runtime's length check.
//
// A nil slice converts to a nil pointer or a zero array only for a zero-length array. The
// pointer form aliases the slice's backing array, as Go's does.
//
// Takes source (reflect.Value) which is the slice operand, possibly invalid.
// Takes target (reflect.Type) which is the conversion's result type.
//
// Returns the converted value, whether the conversion was a slice-to-array one, and Go's
// runtime error when the slice is too short.
func convertSliceToArray(source reflect.Value, target reflect.Type) (reflect.Value, bool, error) {
	arrayType := target
	if target.Kind() == reflect.Pointer {
		arrayType = target.Elem()
	}
	if arrayType.Kind() != reflect.Array || (source.IsValid() && source.Kind() != reflect.Slice) {
		return reflect.Value{}, false, nil
	}
	length := 0
	if source.IsValid() {
		length = source.Len()
	}
	if length < arrayType.Len() {
		return reflect.Value{}, true, newRuntimePanicError(
			"runtime error: cannot convert slice with length %d to array or pointer to array with length %d", length, arrayType.Len())
	}
	if !source.IsValid() || source.IsNil() {
		return reflect.Zero(target), true, nil
	}
	return source.Convert(target), true, nil
}
