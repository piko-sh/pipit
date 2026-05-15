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
	"unsafe"
)

const (
	// gcTraceMaxDepth bounds the reference hops the mark phase follows. Arena slabs hold
	// only pointer-free values and slice headers, so cycles cannot form and a small bound
	// covers every real chain while guarding against malformed values.
	gcTraceMaxDepth = 8
)

// markReflectValueAt is the depth-aware body of markReflectValue().
//
// Classifies v by kind and follows references that can lead from one arena slab into
// another (pointers to arena pointees, interface roots wrapping arena values, slice
// headers whose backing lives in a slab). Every hop that reaches a slab marks its
// generation live.
//
// Soundness rests on two invariants. First, only pointer-free values and slice headers
// are carved from the slabs, so a slab's contents never reference another slab except
// through a header's Data word. Second, the materialise barrier keeps heap objects from
// retaining slab pointers, so every live slab object is reachable from a VM root.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes v (reflect.Value) which is the value to inspect and mark.
// Takes depth (int) which is the number of hops already taken from the root.
func markReflectValueAt(arena *RegisterArena, state *gcMarkState, v reflect.Value, depth int) {
	if !v.IsValid() || depth > gcTraceMaxDepth {
		return
	}
	switch v.Kind() {
	case reflect.Struct, reflect.Array:
		markGenericBytesPointer(arena, state, ReflectValuePtr(v))
		markCompositeFields(arena, state, v, depth)
	case reflect.Slice:
		markSliceHeaderAt(arena, state, v)
	case reflect.String:
		markString(arena, state, v.String())
	case reflect.Pointer:
		markPointerTarget(arena, state, v, depth)
	case reflect.Interface:
		if !v.IsNil() {
			markReflectValueAt(arena, state, v.Elem(), depth+1)
		}
	default:
	}
}

// markCompositeFields traces pointer-bearing struct fields or array elements so that
// slice backings and strings reachable only through them stay alive.
//
// Takes arena (*RegisterArena) which owns the slabs being marked.
// Takes state (*gcMarkState) which records the marks.
// Takes v (reflect.Value) which is the struct or array.
// Takes depth (int) which is the current trace depth; fields are traced one level deeper.
func markCompositeFields(arena *RegisterArena, state *gcMarkState, v reflect.Value, depth int) {
	if typeIsPointerFree(v.Type()) {
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() { //nolint:modernize // needs field values
			markReflectValueAt(arena, state, v.Field(i), depth+1)
		}
	case reflect.Array:
		for i := range v.Len() {
			markReflectValueAt(arena, state, v.Index(i), depth+1)
		}
	default:
	}
}

// markPointerTarget marks the slab holding a pointer's pointee.
//
// A pointer to a slice lands in the slice-header slab and its header is followed like any
// other slice. A pointer to a pointer-free value lands in the generic bytes slab.
// Pointers to values that can hold references (maps, channels, functions, further
// pointers) are always heap allocated and need no marking.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes v (reflect.Value) which must be Pointer-kinded.
// Takes depth (int) which is the current trace depth from the root.
func markPointerTarget(arena *RegisterArena, state *gcMarkState, v reflect.Value, depth int) {
	if v.IsNil() {
		return
	}
	pointee := v.UnsafePointer()
	switch v.Type().Elem().Kind() {
	case reflect.Slice:
		markSliceHeaderSlab(arena, state, pointee)
		markSliceHeaderAt(arena, state, v.Elem())
	case reflect.Map, reflect.Chan, reflect.Func, reflect.Pointer, reflect.Interface, reflect.UnsafePointer:
	case reflect.Struct, reflect.Array:
		markGenericBytesPointer(arena, state, pointee)
		markCompositeFields(arena, state, v.Elem(), depth)
	default:
		markGenericBytesPointer(arena, state, pointee)
	}
}

// markSliceHeaderAt follows a Slice-kinded reflect.Value: it marks the header's home slab
// generation, then the backing that the header's Data word addresses, including spare
// capacity beyond Len, and finally recurses into string elements so their bytes are
// marked too.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes v (reflect.Value) which must be Slice-kinded.
func markSliceHeaderAt(arena *RegisterArena, state *gcMarkState, v reflect.Value) {
	if headerPtr := ReflectValuePtr(v); headerPtr != nil {
		markSliceHeaderSlab(arena, state, headerPtr)
	}
	if v.Cap() == 0 {
		return
	}
	if markTypedSliceBacking(arena, state, v) {
		return
	}
	markBackingPointer(arena, state, v.UnsafePointer())
	if v.Type().Elem().Kind() == reflect.String {
		for i := range v.Len() {
			markString(arena, state, v.Index(i).String())
		}
	}
}

var (
	// typeInt64Slice is the reflect type for []int64, used by the collector.
	typeInt64Slice = reflect.TypeFor[[]int64]()

	// typeFloat64Slice is the reflect type for []float64, used by the collector.
	typeFloat64Slice = reflect.TypeFor[[]float64]()

	// typeStringSlice is the reflect type for []string, used by the collector.
	typeStringSlice = reflect.TypeFor[[]string]()

	// typeBoolSlice is the reflect type for []bool, used by the collector.
	typeBoolSlice = reflect.TypeFor[[]bool]()

	// typeUint64Slice is the reflect type for []uint64, used by the collector.
	typeUint64Slice = reflect.TypeFor[[]uint64]()

	// typeByteSlice is the reflect type for []byte, used by the collector.
	typeByteSlice = reflect.TypeFor[[]byte]()
)

// markTypedSliceBacking marks the backing of a slice whose static type is one of the
// typed-bank element types, reusing the per-bank markers.
//
// The slice header is rebuilt from the value's data pointer, length and capacity rather
// than through reflect.TypeAssert, because a value read from an unexported struct field
// carries reflect's read-only flag, which TypeAssert refuses but the header accessors do
// not. The collector reaches such values when it traces struct fields.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes v (reflect.Value) which must be Slice-kinded.
//
// Returns true when the slice matched a typed bank and has been marked.
func markTypedSliceBacking(arena *RegisterArena, state *gcMarkState, v reflect.Value) bool {
	switch v.Type() {
	case typeInt64Slice:
		markIntSlice(arena, state, sliceHeaderOf[int64](v))
	case typeFloat64Slice:
		markFloatSlice(arena, state, sliceHeaderOf[float64](v))
	case typeStringSlice:
		markStringSlice(arena, state, sliceHeaderOf[string](v))
	case typeBoolSlice:
		markBoolSlice(arena, state, sliceHeaderOf[bool](v))
	case typeUint64Slice:
		markUintSlice(arena, state, sliceHeaderOf[uint64](v))
	case typeByteSlice:
		markByteSlice(arena, state, sliceHeaderOf[byte](v))
	default:
		return false
	}
	return true
}

// sliceHeaderOf rebuilds a []T from a slice-kinded reflect.Value of that exact type using
// only the accessors reflect permits on read-only values.
//
// Takes v (reflect.Value) which holds a []T.
//
// Returns the slice sharing v's backing store.
func sliceHeaderOf[T any](v reflect.Value) []T {
	capacity := v.Cap()
	if capacity == 0 {
		return nil
	}
	return unsafe.Slice((*T)(v.UnsafePointer()), capacity)[:v.Len()]
}

// markBackingPointer marks the slab generation holding an untyped slice backing.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes data (unsafe.Pointer) which is the backing's first element address.
func markBackingPointer(arena *RegisterArena, state *gcMarkState, data unsafe.Pointer) {
	if data == nil {
		return
	}
	markGenericBytesPointer(arena, state, data)
	markBytePointer(arena, state, data, 1)
	pointer := uintptr(data)
	markOldBackingHolding(pointer, arena.oldIntBackings, int64ElementSize, state.OldIntBackingLive)
	markOldBackingHolding(pointer, arena.oldFloatBackings, float64ElementSize, state.OldFloatBackingLive)
	markOldBackingHolding(pointer, arena.oldStringBackings, stringHeaderSize, state.OldStringBackingLive)
	markOldBackingHolding(pointer, arena.oldBoolBackings, boolElementSize, state.OldBoolBackingLive)
	markOldBackingHolding(pointer, arena.oldUintBackings, uint64ElementSize, state.OldUintBackingLive)
}

// markOldBackingHolding flags the retained backing slab of one typed class that contains
// pointer, if any.
//
// Takes pointer (uintptr) which is the address being classified.
// Takes backings ([][]T) which is the class's retention list.
// Takes stride (uintptr) which is the byte size of one element.
// Takes live ([]bool) which is the class's liveness bitmap.
func markOldBackingHolding[T any](pointer uintptr, backings [][]T, stride uintptr, live []bool) {
	for i, backing := range backings {
		if i >= len(live) || len(backing) == 0 {
			continue
		}
		base := uintptr(unsafe.Pointer(&backing[0]))
		if pointer >= base && pointer < base+uintptr(len(backing))*stride {
			live[i] = true
		}
	}
}
