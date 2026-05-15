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
	"reflect"
	"unsafe"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// appendLimitExceededFormat is the shared fmt format string used by every alloc-limit
	// overflow path in this file. Centralising it keeps the wording consistent and satisfies
	// revive's add-constant.
	appendLimitExceededFormat = "%w: append result length %d exceeds limit %d"
)

var (
	// intSliceReflectType is the cached reflect.Type of []int, the canonical boxed type of
	// the int-slice bank (its storage is []int64, which shares the layout). Used by
	// handlePackInterface's typed-slice-to-general path to avoid the per-call
	// reflect.TypeFor lookup.
	intSliceReflectType = reflect.TypeFor[[]int]()

	// floatSliceReflectType is the cached reflect.Type of []float64.
	floatSliceReflectType = reflect.TypeFor[[]float64]()

	// stringSliceReflectType is the cached reflect.Type of []string.
	stringSliceReflectType = reflect.TypeFor[[]string]()

	// boolSliceReflectType is the cached reflect.Type of []bool.
	boolSliceReflectType = reflect.TypeFor[[]bool]()

	// uintSliceReflectType is the cached reflect.Type of []uint64.
	uintSliceReflectType = reflect.TypeFor[[]uint64]()
)

// arenaWrapTypedSlice wraps a typed []byte (or any slice whose reflect.Type matches
// sliceType) into a reflect.Value backed by an arena slice header slot. This avoids the
// 24-byte heap slice header that reflect.ValueOf allocates for slice values via
// runtime.convTslice.
//
// Takes arena (*RegisterArena) which provides the slice-header slab.
// Takes data (unsafe.Pointer) which is the slice's backing array pointer (or nil for an
// empty slice).
// Takes length (int) which is the slice's len.
// Takes capacity (int) which is the slice's cap.
// Takes sliceType (reflect.Type) which is the typed slice's reflect.Type (e.g.
// reflect.TypeFor[[]byte]()).
//
// Returns a reflect.Value of kind Slice referring to the arena slice-header slot.
func arenaWrapTypedSlice(arena *RegisterArena, data unsafe.Pointer, length, capacity int, sliceType reflect.Type) reflect.Value {
	slot := arena.allocSliceHeader()
	slot.Data = data
	slot.Len = length
	slot.Cap = capacity
	return unsafeNewAt(reflectValueABIType(sliceType), unsafe.Pointer(slot), reflect.Slice)
}

// arenaWrapByteSlice is the []byte specialisation of arenaWrapTypedSlice.
//
// Takes arena (*RegisterArena) which provides the slice-header slab.
// Takes s ([]byte) which is the source byte slice whose header is wrapped.
//
// Returns a reflect.Value of kind Slice referring to the arena slice-header slot.
func arenaWrapByteSlice(arena *RegisterArena, s []byte) reflect.Value {
	data := unsafe.Pointer(unsafe.SliceData(s))
	return arenaWrapTypedSlice(arena, data, len(s), cap(s), reflect.TypeFor[[]byte]())
}

// arenaWrapMakeBacking wraps an arena-allocated typed backing into an arena slice-header
// slot.
//
// Takes arena (*RegisterArena) which provides the slice-header slab.
// Takes backing ([]T) which is the arena-allocated backing array.
// Takes length (int) which is the make() length argument.
// Takes capacity (int) which is the make() capacity argument.
// Takes reflectType (reflect.Type) which is the slice type to attach to the resulting
// reflect.Value. Passed through verbatim so named types (e.g. type MyBytes []byte) keep
// their original identity.
//
// Returns a reflect.Value of kind Slice whose header lives in the arena slab.
func arenaWrapMakeBacking[T any](arena *RegisterArena, backing []T, length, capacity int, reflectType reflect.Type) reflect.Value {
	data := zeroSizeAllocPtr
	if capacity > 0 {
		data = unsafe.Pointer(unsafe.SliceData(backing))
	}
	return arenaWrapTypedSlice(arena, data, length, capacity, reflectType)
}

// arenaWrapTypedSliceFromSource builds a result reflect.Value for a grown typed slice by
// reusing the source value's abi type pointer directly, avoiding both reflect.ValueOf's
// per-call convTslice mallocgc AND the cached reflect.TypeFor[[]T]() lookup. Used by the
// typed-append handlers (handleAppendString/Int/Float/Bool/Uint) to box the post-grow
// slice header through the arena slab.
//
// Takes arena (*RegisterArena) which provides the slice-header slab.
// Takes sourceValue (reflect.Value) which is the input slice value whose abi type is
// reused for the result.
// Takes data (unsafe.Pointer) which is the grown slice's backing pointer.
// Takes length (int) which is the grown slice's len.
// Takes capacity (int) which is the grown slice's cap.
//
// Returns a reflect.Value of kind Slice with the same dynamic type as sourceValue, no
// heap allocation.
func arenaWrapTypedSliceFromSource(arena *RegisterArena, sourceValue reflect.Value, data unsafe.Pointer, length, capacity int) reflect.Value {
	srcType := wrapTypedSliceSrcType(sourceValue)
	slot := arena.allocSliceHeader()
	slot.Data = data
	slot.Len = length
	slot.Cap = capacity
	return unsafeNewAt(srcType, unsafe.Pointer(slot), reflect.Slice)
}

// sliceDataPtrString returns the backing-array pointer for the given []string, or nil
// when the source slice is nil.
//
// Takes s ([]string) which is the slice whose data pointer is read.
//
// Returns the unsafe.Pointer to the backing array, or nil when s is nil.
func sliceDataPtrString(s []string) unsafe.Pointer {
	return unsafe.Pointer(unsafe.SliceData(s))
}

// sliceDataPtrInt64 returns the backing-array pointer for the given []int64, or nil when
// the source slice is nil.
//
// Takes s ([]int64) which is the slice whose data pointer is read.
//
// Returns the unsafe.Pointer to the backing array, or nil when s is nil.
func sliceDataPtrInt64(s []int64) unsafe.Pointer {
	return unsafe.Pointer(unsafe.SliceData(s))
}

// sliceDataPtrFloat64 returns the backing-array pointer for the given []float64, or nil
// when the source slice is nil.
//
// Takes s ([]float64) which is the slice whose data pointer is read.
//
// Returns the unsafe.Pointer to the backing array, or nil when s is nil.
func sliceDataPtrFloat64(s []float64) unsafe.Pointer {
	return unsafe.Pointer(unsafe.SliceData(s))
}

// sliceDataPtrBool returns the backing-array pointer for the given []bool, or nil when
// the source slice is nil.
//
// Takes s ([]bool) which is the slice whose data pointer is read.
//
// Returns the unsafe.Pointer to the backing array, or nil when s is nil.
func sliceDataPtrBool(s []bool) unsafe.Pointer {
	return unsafe.Pointer(unsafe.SliceData(s))
}

// sliceDataPtrUint64 returns the backing-array pointer for the given []uint64, or nil
// when the source slice is nil.
//
// Takes s ([]uint64) which is the slice whose data pointer is read.
//
// Returns the unsafe.Pointer to the backing array, or nil when s is nil.
func sliceDataPtrUint64(s []uint64) unsafe.Pointer {
	return unsafe.Pointer(unsafe.SliceData(s))
}

// sliceDataPtrInt returns the backing-array pointer for the given []int, or nil when the
// source slice is nil.
//
// Takes s ([]int) which is the slice whose data pointer is read.
//
// Returns the unsafe.Pointer to the backing array, or nil when s is nil.
func sliceDataPtrInt(s []int) unsafe.Pointer {
	return unsafe.Pointer(unsafe.SliceData(s))
}

// sliceDataPtrUint returns the backing-array pointer for the given []uint, or nil when
// the source slice is nil.
//
// Takes s ([]uint) which is the slice whose data pointer is read.
//
// Returns the unsafe.Pointer to the backing array, or nil when s is nil.
func sliceDataPtrUint(s []uint) unsafe.Pointer {
	return unsafe.Pointer(unsafe.SliceData(s))
}

// sliceDataPtrUint32 returns the backing-array pointer for the given []uint32, or nil
// when the source slice is nil.
//
// Takes s ([]uint32) which is the slice whose data pointer is read.
//
// Returns the unsafe.Pointer to the backing array, or nil when s is nil.
func sliceDataPtrUint32(s []uint32) unsafe.Pointer {
	return unsafe.Pointer(unsafe.SliceData(s))
}

// sliceDataPtrUint16 returns the backing-array pointer for the given []uint16, or nil
// when the source slice is nil.
//
// Takes s ([]uint16) which is the slice whose data pointer is read.
//
// Returns the unsafe.Pointer to the backing array, or nil when s is nil.
func sliceDataPtrUint16(s []uint16) unsafe.Pointer {
	return unsafe.Pointer(unsafe.SliceData(s))
}

// sliceDataPtrUintptr returns the backing-array pointer for the given []uintptr, or nil
// when the source slice is nil.
//
// Takes s ([]uintptr) which is the slice whose data pointer is read.
//
// Returns the unsafe.Pointer to the backing array, or nil when s is nil.
func sliceDataPtrUintptr(s []uintptr) unsafe.Pointer {
	return unsafe.Pointer(unsafe.SliceData(s))
}

// appendFastPath attempts a type-assertion fast path for common concrete slice types to
// avoid reflect.Append overhead.
//
// When arena is non-nil, the grow path for element kinds matching an arena backing slab
// routes through the arenaAppend* helpers so the new backing lands in the arena instead
// of triggering mallocgc.
//
// Takes arena (*RegisterArena) which provides the arena-aware grow helpers (or nil to opt
// out).
// Takes sliceValue (reflect.Value) which is the slice to append to.
// Takes element (reflect.Value) which is the element to append.
//
// Returns reflect.Value and bool; true if a fast path was taken.
//
// []byte is checked first because expr_eval's `*output = append (*output, '(')`
// byte-builder pattern is the single hottest general-bank append; ordering the byte
// branch first avoids the TypeAssert misses that would otherwise precede it.
func appendFastPath(arena *RegisterArena, sliceValue, element reflect.Value) (reflect.Value, bool) {
	if s, ok := reflect.TypeAssert[[]byte](sliceValue); ok {
		return appendByteSliceFastPath(arena, s, element)
	}
	if result, matched := appendIntSliceFastPath(arena, sliceValue, element); matched {
		return result, result.IsValid()
	}
	if result, matched := appendFloatSliceFastPath(arena, sliceValue, element); matched {
		return result, result.IsValid()
	}
	if result, matched := appendStringSliceFastPath(arena, sliceValue, element); matched {
		return result, result.IsValid()
	}
	if result, matched := appendBoolSliceFastPath(arena, sliceValue, element); matched {
		return result, result.IsValid()
	}
	return reflect.Value{}, false
}

// appendIntSliceFastPath handles the []int64 and []int element-type branches of
// appendFastPath.
//
// Takes arena (*RegisterArena) which provides the arena-aware grow helpers.
// Takes sliceValue (reflect.Value) which is the slice to append to.
// Takes element (reflect.Value) which is the element to append.
//
// Returns the wrapped result and true when sliceValue had a matching element-typed slice
// (the result is invalid when the element didn't match the slice's element type).
// Returns (zero Value, false) when sliceValue did not match either int-typed slice.
func appendIntSliceFastPath(arena *RegisterArena, sliceValue, element reflect.Value) (reflect.Value, bool) {
	if s, ok := reflect.TypeAssert[[]int64](sliceValue); ok {
		if v, ok := reflect.TypeAssert[int64](element); ok {
			grown := arenaAppendInt(arena, s, v)
			return arenaWrapTypedSliceFromSource(arena, sliceValue, sliceDataPtrInt64(grown), len(grown), cap(grown)), true
		}
		return reflect.Value{}, true
	}
	if s, ok := reflect.TypeAssert[[]int](sliceValue); ok {
		if v, ok := reflect.TypeAssert[int](element); ok {
			s = append(s, v)
			return arenaWrapTypedSliceFromSource(arena, sliceValue, sliceDataPtrInt(s), len(s), cap(s)), true
		}
		return reflect.Value{}, true
	}
	return reflect.Value{}, false
}

// appendFloatSliceFastPath handles the []float64 branch of appendFastPath.
//
// Takes arena (*RegisterArena) which provides the arena-aware grow helpers.
// Takes sliceValue (reflect.Value) which is the slice to append to.
// Takes element (reflect.Value) which is the element to append.
//
// Returns the wrapped result and true when sliceValue had a matching element-typed slice;
// otherwise (zero Value, false).
func appendFloatSliceFastPath(arena *RegisterArena, sliceValue, element reflect.Value) (reflect.Value, bool) {
	if s, ok := reflect.TypeAssert[[]float64](sliceValue); ok {
		if v, ok := reflect.TypeAssert[float64](element); ok {
			grown := arenaAppendFloat(arena, s, v)
			return arenaWrapTypedSliceFromSource(arena, sliceValue, sliceDataPtrFloat64(grown), len(grown), cap(grown)), true
		}
		return reflect.Value{}, true
	}
	return reflect.Value{}, false
}

// appendStringSliceFastPath handles the []string branch of appendFastPath.
//
// Takes arena (*RegisterArena) which provides the arena-aware grow helpers.
// Takes sliceValue (reflect.Value) which is the slice to append to.
// Takes element (reflect.Value) which is the element to append.
//
// Returns the wrapped result and true when sliceValue had a matching element-typed slice;
// otherwise (zero Value, false).
func appendStringSliceFastPath(arena *RegisterArena, sliceValue, element reflect.Value) (reflect.Value, bool) {
	if s, ok := reflect.TypeAssert[[]string](sliceValue); ok {
		if v, ok := reflect.TypeAssert[string](element); ok {
			grown := arenaAppendString(arena, s, v)
			return arenaWrapTypedSliceFromSource(arena, sliceValue, sliceDataPtrString(grown), len(grown), cap(grown)), true
		}
		return reflect.Value{}, true
	}
	return reflect.Value{}, false
}

// appendBoolSliceFastPath handles the []bool branch of appendFastPath.
//
// Takes arena (*RegisterArena) which provides the arena-aware grow helpers.
// Takes sliceValue (reflect.Value) which is the slice to append to.
// Takes element (reflect.Value) which is the element to append.
//
// Returns the wrapped result and true when sliceValue had a matching element-typed slice;
// otherwise (zero Value, false).
func appendBoolSliceFastPath(arena *RegisterArena, sliceValue, element reflect.Value) (reflect.Value, bool) {
	if s, ok := reflect.TypeAssert[[]bool](sliceValue); ok {
		if v, ok := reflect.TypeAssert[bool](element); ok {
			grown := arenaAppendBool(arena, s, v)
			return arenaWrapTypedSliceFromSource(arena, sliceValue, sliceDataPtrBool(grown), len(grown), cap(grown)), true
		}
		return reflect.Value{}, true
	}
	return reflect.Value{}, false
}

// appendByteSliceFastPath handles the []byte branch of appendFastPath, covering both the
// direct byte-typed element and the widened-integer boxing path that expr_eval emits for
// rune-literal appends.
//
// expr_eval's `append(*output, '(')` boxes the untyped rune constant as int/int64;
// without this widening the call would fall through to coerceValue + reflect.Append.
// Masking to a byte matches Go's `byte(int(x))` truncation semantics; the upstream
// Compiler has already type-checked that the conversion is valid.
//
// Takes arena (*RegisterArena) which provides the byte slab when a grow is needed.
// Takes destination ([]byte) which is the slice being appended to.
// Takes element (reflect.Value) which is the element to coerce and append.
//
// Returns the wrapped result reflect.Value plus true on success, or (zero Value, false)
// when the element kind is not boxable as a byte.
func appendByteSliceFastPath(arena *RegisterArena, destination []byte, element reflect.Value) (reflect.Value, bool) {
	if v, ok := reflect.TypeAssert[byte](element); ok {
		grown := arenaAppendByte(arena, destination, v)
		return arenaWrapByteSlice(arena, grown), true
	}
	switch element.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		grown := arenaAppendByte(arena, destination, safeconv.Int64ToUint8(element.Int()))
		return arenaWrapByteSlice(arena, grown), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		grown := arenaAppendByte(arena, destination, safeconv.Uint64ToUint8(element.Uint()))
		return arenaWrapByteSlice(arena, grown), true
	default:
	}
	return reflect.Value{}, false
}

// handleAppend handles the isa.OpAppend instruction by appending a general register
// element to a slice with type coercion as needed.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the slice and element values.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleAppend(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	sliceValue := registers.General[instruction.B]
	element := registers.General[instruction.C]
	if instruction.A == instruction.B && element.IsValid() {
		if header := arenaOwnedSliceHeader(vm.Arena, sliceValue); header != nil && tryReuseInPlaceHeader(frame, header, sliceValue, element) {
			return opContinue
		}
	}

	if !sliceValue.IsValid() {
		if !element.IsValid() {
			vm.evalError = newInvariantError("append to nil slice with invalid element")
			return opPanicError
		}
		sliceValue = reflect.MakeSlice(reflect.SliceOf(element.Type()), 0, 0)
	}
	if vm.Limits.MaxAllocSize > 0 && sliceValue.Len()+1 > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(appendLimitExceededFormat,
			fault.ErrAllocationLimit, sliceValue.Len()+1, vm.Limits.MaxAllocSize)
		return opPanicError
	}
	if result, ok := appendFastPath(vm.Arena, sliceValue, element); ok {
		registers.General[instruction.A] = result
		return opContinue
	}

	if result, ok := appendGenericFastPath(vm.Arena, sliceValue, element); ok {
		registers.General[instruction.A] = result
		return opContinue
	}
	elementType := sliceValue.Type().Elem()
	element = coerceValue(vm, element, elementType)
	if element.IsValid() && element.Type() != elementType && !element.Type().AssignableTo(elementType) && element.Type().ConvertibleTo(elementType) {
		element = element.Convert(elementType)
	}
	registers.General[instruction.A] = reflect.Append(sliceValue, element)
	return opContinue
}

// appendGenericFastPath handles the spare-capacity case for arbitrary slice element
// types.
//
// When sliceCap > sliceLen and the element is type-compatible with the slice's element
// kind (exact-match or assignable), it widens the slice header to len+1, writes the
// element via reflect.Value.Set (which emits the correct GC write barriers for
// pointer-containing element types), and returns the new slice. Falls back (ok=false)
// when sliceValue is invalid or not a Slice kind, when sliceLen == sliceCap (growth left
// to reflect.Append's tuned policy), or when the element type is not assignable to the
// slice's element type (the slow path runs coerceValue).
//
// Takes arena (*RegisterArena) which provides the arena-resident header slot when
// non-nil.
// Takes sliceValue (reflect.Value) which is the slice being appended to.
// Takes element (reflect.Value) which is the element to append.
//
// Returns the new slice and true on success, or zero Value and false when the slow path
// must run.
func appendGenericFastPath(arena *RegisterArena, sliceValue, element reflect.Value) (reflect.Value, bool) {
	if !sliceValue.IsValid() || sliceValue.Kind() != reflect.Slice {
		return reflect.Value{}, false
	}
	sliceLen := sliceValue.Len()
	sliceCap := sliceValue.Cap()
	if sliceLen >= sliceCap {
		return reflect.Value{}, false
	}
	if !element.IsValid() {
		return reflect.Value{}, false
	}
	elementType := sliceValue.Type().Elem()
	elementValueType := element.Type()
	if elementValueType != elementType {
		return reflect.Value{}, false
	}
	if arena != nil {
		if sourceHeaderPtr := ReflectValuePtr(sliceValue); sourceHeaderPtr != nil {
			sourceHeader := (*arenaSliceHeader)(sourceHeaderPtr)
			slot := arena.allocSliceHeader()
			slot.Data = sourceHeader.Data
			slot.Len = sliceLen + 1
			slot.Cap = sliceCap
			extended := unsafeNewAt(reflectValueABIType(sliceValue.Type()), unsafe.Pointer(slot), reflect.Slice)

			eKind := elementType.Kind()
			if (eKind == reflect.Struct || eKind == reflect.Array) &&
				typeIsPointerFree(elementType) &&
				element.CanAddr() {
				elemSize := elementType.Size()
				destinationPointer := unsafe.Add(sourceHeader.Data, elemSize*safeconv.IntToUintptr(sliceLen))
				sourcePointer := ReflectValuePtr(element)
				destination := unsafe.Slice((*byte)(destinationPointer), elemSize)
				source := unsafe.Slice((*byte)(sourcePointer), elemSize)
				copy(destination, source)
				return extended, true
			}
			extended.Index(sliceLen).Set(element)
			return extended, true
		}
	}
	extended := sliceValue.Slice(0, sliceLen+1)
	extended.Index(sliceLen).Set(element)
	return extended, true
}

// handleAppendSpread handles the isa.OpAppendSpread instruction by appending every
// element of a source slice into a destination slice via reflect.AppendSlice. This is the
// variadic-spread form `append(destination, source...)` where the spread argument is
// itself a slice.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds both slices in general.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleAppendSpread(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	destinationSlice := registers.General[instruction.B]
	sourceSlice := registers.General[instruction.C]

	if !sourceSlice.IsValid() {
		registers.General[instruction.A] = destinationSlice
		return opContinue
	}

	if sourceSlice.Kind() == reflect.String {
		return handleAppendStringSpread(vm, registers, instruction, destinationSlice, sourceSlice)
	}
	if !destinationSlice.IsValid() {
		destinationSlice = reflect.MakeSlice(sourceSlice.Type(), 0, sourceSlice.Len())
	}
	if vm.Limits.MaxAllocSize > 0 && destinationSlice.Len()+sourceSlice.Len() > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(appendLimitExceededFormat,
			fault.ErrAllocationLimit, destinationSlice.Len()+sourceSlice.Len(), vm.Limits.MaxAllocSize)
		return opPanicError
	}

	if result, ok := appendSpreadFastPath(vm.Arena, destinationSlice, sourceSlice); ok {
		registers.General[instruction.A] = result
		return opContinue
	}
	if destination, destinationOk := reflect.TypeAssert[[]byte](destinationSlice); destinationOk {
		if source, sourceOk := reflect.TypeAssert[[]byte](sourceSlice); sourceOk {
			result := arenaAppendByteSpread(vm.Arena, destination, source)
			registers.General[instruction.A] = arenaWrapByteSpreadResult(vm.Arena, destinationSlice, result)
			return opContinue
		}
	}
	registers.General[instruction.A] = reflect.AppendSlice(destinationSlice, conformSpreadSource(sourceSlice, destinationSlice.Type()))
	return opContinue
}

// conformSpreadSource narrows a spread source read from a typed bank to the destination's
// element type.
//
// The typed banks widen every integer element to eight bytes and box the slice as the
// bank's canonical type, so a []uint16 source arrives as []uint64; the destination's
// element type is the one to keep.
//
// Takes source (reflect.Value) which is the spread source.
// Takes destinationType (reflect.Type) which is the destination slice type.
//
// Returns reflect.Value which is source itself when the types agree, else a converted
// copy.
func conformSpreadSource(source reflect.Value, destinationType reflect.Type) reflect.Value {
	if source.Type() == destinationType || !source.Type().Elem().ConvertibleTo(destinationType.Elem()) {
		return source
	}
	converted := reflect.MakeSlice(destinationType, source.Len(), source.Len())
	for i := range source.Len() {
		converted.Index(i).Set(source.Index(i).Convert(destinationType.Elem()))
	}
	return converted
}

// arenaWrapByteSpreadResult constructs an arena-resident reflect.Value of slice kind that
// shares the destination's dynamic type and points at the supplied result data, length,
// and capacity.
//
// Takes arena (*RegisterArena) which provides the slice-header slab.
// Takes sourceValue (reflect.Value) whose dynamic type is reused for the result.
// Takes result ([]byte) which is the new slice's backing data and header content.
//
// Returns a reflect.Value of kind Slice referring to an arena slice- header slot.
func arenaWrapByteSpreadResult(arena *RegisterArena, sourceValue reflect.Value, result []byte) reflect.Value {
	sourceShape := (*unsafeReflectValue)(unsafe.Pointer(&sourceValue))
	slot := arena.allocSliceHeader()
	slot.Data = unsafe.Pointer(unsafe.SliceData(result))
	slot.Len = len(result)
	slot.Cap = cap(result)
	out := unsafeReflectValue{
		typ:  sourceShape.typ,
		ptr:  unsafe.Pointer(slot),
		flag: uintptr(reflect.Slice) | flagAddr | flagIndir,
	}
	return *(*reflect.Value)(unsafe.Pointer(&out))
}

// arenaAppendByteSpread appends every byte of source to destination.
//
// When destination has spare capacity for the full spread, this is an in-place memcpy +
// slice extension. When a grow is required, the new backing comes from the arena's byte
// slab (same path as arenaAppendByte's grow case) so the result avoids mallocgc.
//
// Takes arena (*RegisterArena) which supplies the byte slab when a grow is needed.
// Takes destination ([]byte) which receives the appended bytes.
// Takes source ([]byte) which is the source whose bytes are appended.
//
// Returns the extended []byte slice header (sharing destination's backing when capacity
// allowed in-place, or pointing at a fresh arena-allocated backing after a grow).
func arenaAppendByteSpread(arena *RegisterArena, destination, source []byte) []byte {
	newLen := len(destination) + len(source)
	if newLen <= cap(destination) {
		extended := destination[:newLen]
		copy(extended[len(destination):], source)
		return extended
	}
	newCap := max(2*cap(destination), newLen)
	backing := arena.AllocByteBacking(newCap)
	copy(backing, destination)
	copy(backing[len(destination):], source)
	return backing[:newLen]
}

// arenaAppendByteFromString is the string-source twin of arenaAppendByteSpread.
//
// Supports the Go spec's special case `append(byteSlice, stringValue...)`. Go's built-in
// `copy([]byte, string)` form lets us copy the string's UTF-8 bytes without materialising
// an intermediate []byte. Grow path uses the arena byte slab, matching
// arenaAppendByteSpread's policy.
//
// Takes arena (*RegisterArena) which supplies the byte slab on grow.
// Takes destination ([]byte) which receives the appended bytes.
// Takes source (string) which is the source whose bytes are appended.
//
// Returns the extended []byte slice header (sharing destination's backing when capacity
// allowed in-place, or pointing at a fresh arena-allocated backing after a grow).
func arenaAppendByteFromString(arena *RegisterArena, destination []byte, source string) []byte {
	newLen := len(destination) + len(source)
	if newLen <= cap(destination) {
		extended := destination[:newLen]
		copy(extended[len(destination):], source)
		return extended
	}
	newCap := max(2*cap(destination), newLen)
	backing := arena.AllocByteBacking(newCap)
	copy(backing, destination)
	copy(backing[len(destination):], source)
	return backing[:newLen]
}

// handleAppendStringSpread spreads a string into a byte slice destination.
//
// This is the Go-spec special case `append(byteSlice, str...)` where the spread argument
// is a string and the destination has core type []byte. Mirrors the []byte-to-[]byte fast
// path in handleAppendSpread: routes the grow through the arena byte slab and constructs
// the result reflect.Value via the heap-slot bypass to avoid runtime.convTslice on every
// call.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which is the active register file.
// Takes instruction (isa.Instruction) which is the current instruction.
// Takes destinationSlice (reflect.Value) which holds the destination byte slice. May be
// invalid; a fresh []byte is seeded in that case.
// Takes sourceValue (reflect.Value) which holds the string source (Kind() ==
// reflect.String).
//
// Returns OpResult indicating the next execution step.
func handleAppendStringSpread(vm *VM, registers *Registers, instruction isa.Instruction, destinationSlice, sourceValue reflect.Value) OpResult {
	sourceString := sourceValue.String()
	sourceLen := len(sourceString)

	if !destinationSlice.IsValid() {
		destinationSlice = reflect.MakeSlice(reflect.TypeFor[[]byte](), 0, sourceLen)
	}
	if vm.Limits.MaxAllocSize > 0 && destinationSlice.Len()+sourceLen > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(appendLimitExceededFormat,
			fault.ErrAllocationLimit, destinationSlice.Len()+sourceLen, vm.Limits.MaxAllocSize)
		return opPanicError
	}
	if destination, destinationOk := reflect.TypeAssert[[]byte](destinationSlice); destinationOk {
		result := arenaAppendByteFromString(vm.Arena, destination, sourceString)
		registers.General[instruction.A] = arenaWrapByteSpreadResult(vm.Arena, destinationSlice, result)
		return opContinue
	}

	destinationType := destinationSlice.Type()
	if destinationType.Elem().Kind() == reflect.Uint8 {
		typedSource := reflect.MakeSlice(destinationType, sourceLen, sourceLen)
		reflect.Copy(typedSource, sourceValue)
		registers.General[instruction.A] = reflect.AppendSlice(destinationSlice, typedSource)
		return opContinue
	}

	vm.evalError = newInvariantError("cannot spread string into %v", destinationType)
	return opPanicError
}

// appendSpreadFastPath bypasses reflect.AppendSlice's per-call header allocation when the
// destination has spare capacity.
//
// The element copy is done by reflect.Copy on the destination[len:len+sourceLength] view,
// which uses runtime.typedslicecopy internally for correct GC barriers. The result header
// is bump-allocated from arena.SliceHeaderSlab and shares the destination's backing
// array. Falls through (ok=false) when arena is nil (callers without arena context), when
// destination.Cap() is below destination.Len() + source.Len() (growth handled by
// reflect.AppendSlice), or when destination and source have incompatible types.
//
// Takes arena (*RegisterArena) which provides the slice-header slab; nil disables the
// fast path.
// Takes destination (reflect.Value) which is the destination slice.
// Takes source (reflect.Value) which is the source slice whose elements are spread into
// destination.
//
// Returns the new slice and true on success, or zero Value and false when the slow path
// must run.
func appendSpreadFastPath(arena *RegisterArena, destination, source reflect.Value) (reflect.Value, bool) {
	if arena == nil {
		return reflect.Value{}, false
	}
	if !destination.IsValid() || destination.Kind() != reflect.Slice {
		return reflect.Value{}, false
	}
	if source.Kind() != reflect.Slice {
		return reflect.Value{}, false
	}
	destinationLength := destination.Len()
	sourceLength := source.Len()
	newLen := destinationLength + sourceLength
	if newLen > destination.Cap() {
		return reflect.Value{}, false
	}
	if destination.Type() != source.Type() {
		return reflect.Value{}, false
	}

	destinationHeaderPtr := ReflectValuePtr(destination)
	if destinationHeaderPtr == nil {
		return reflect.Value{}, false
	}
	sourceHeader := (*arenaSliceHeader)(destinationHeaderPtr)
	slot := arena.allocSliceHeader()
	slot.Data = sourceHeader.Data
	slot.Len = newLen
	slot.Cap = destination.Cap()
	extended := unsafeNewAt(reflectValueABIType(destination.Type()), unsafe.Pointer(slot), reflect.Slice)

	if destinationBytes, destinationOk := reflect.TypeAssert[[]byte](destination); destinationOk {
		if sourceBytes, sourceOk := reflect.TypeAssert[[]byte](source); sourceOk {
			tail := unsafe.Slice((*byte)(unsafe.Add(slot.Data, destinationLength)), sourceLength)
			_ = destinationBytes
			copy(tail, sourceBytes)
			_ = arena
			return extended, true
		}
	}
	reflect.Copy(extended.Slice(destinationLength, newLen), source)
	_ = arena
	return extended, true
}

// handleSubOpAppendUint implements the isa.SubOpAppendUint tier-1 sub-op: general[B] :=
// append(general[C], byte(uints[ext.A])). Eliminates the cascading reflect.TypeAssert
// chain in appendFastPath for the statically-known uint-element-slice case, which is
// expr_eval's hot `*output = append(*output, '(')` byte-builder pattern.
//
// Encoding:
//
// op = isa.OpDrillTier1 a = isa.SubOpAppendUint b = destination register (general bank) c
// = source slice register (general bank) ext.a = element register (uint bank)
//
// Takes vm (*VM) which provides arena, evalError, and allocation limits.
// Takes frame (*CallFrame) whose programCounter is advanced past the extension word.
// Takes registers (*Registers) which holds the general slice and uint element banks.
// Takes instr (instruction) which encodes the destination, slice and element register
// indices.
//
// Returns OpResult indicating the next execution step.
func handleSubOpAppendUint(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	sliceValue := registers.General[instr.C]
	element := registers.Uints[extensionWord.A]

	if !sliceValue.IsValid() {
		registers.General[instr.B] = reflect.ValueOf([]byte{safeconv.Uint64ToUint8(element)})
		return opContinue
	}
	if rc := checkAppendLimit(vm, sliceValue); rc != opContinue {
		return rc
	}

	if s, ok := reflect.TypeAssert[[]byte](sliceValue); ok {
		grown := arenaAppendByte(vm.Arena, s, safeconv.Uint64ToUint8(element))
		registers.General[instr.B] = arenaWrapByteSlice(vm.Arena, grown)
		return opContinue
	}
	if s, ok := reflect.TypeAssert[[]uint64](sliceValue); ok {
		grown := arenaAppendUint(vm.Arena, s, element)
		registers.General[instr.B] = arenaWrapTypedSliceFromSource(vm.Arena, sliceValue, sliceDataPtrUint64(grown), len(grown), cap(grown))
		return opContinue
	}
	if s, ok := reflect.TypeAssert[[]uint](sliceValue); ok {
		s = append(s, uint(element))
		registers.General[instr.B] = arenaWrapTypedSliceFromSource(vm.Arena, sliceValue, sliceDataPtrUint(s), len(s), cap(s))
		return opContinue
	}
	if s, ok := reflect.TypeAssert[[]uint32](sliceValue); ok {
		s = append(s, safeconv.Uint64ToUint32(element))
		registers.General[instr.B] = arenaWrapTypedSliceFromSource(vm.Arena, sliceValue, sliceDataPtrUint32(s), len(s), cap(s))
		return opContinue
	}
	if s, ok := reflect.TypeAssert[[]uint16](sliceValue); ok {
		s = append(s, safeconv.Uint64ToUint16(element))
		registers.General[instr.B] = arenaWrapTypedSliceFromSource(vm.Arena, sliceValue, sliceDataPtrUint16(s), len(s), cap(s))
		return opContinue
	}
	if s, ok := reflect.TypeAssert[[]uintptr](sliceValue); ok {
		s = append(s, uintptr(element))
		registers.General[instr.B] = arenaWrapTypedSliceFromSource(vm.Arena, sliceValue, sliceDataPtrUintptr(s), len(s), cap(s))
		return opContinue
	}

	registers.General[instr.B] = reflect.Append(sliceValue, reflect.ValueOf(element).Convert(sliceValue.Type().Elem()))
	return opContinue
}

// handleAppendInt handles the isa.SubOpAppendInt instruction by appending an integer
// value from an int register to a slice in a general register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the slice and integer element.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleAppendInt(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	sliceValue := registers.General[instruction.B]
	element := registers.Ints[instruction.C]

	if instruction.A == instruction.B {
		return appendIntInPlace(vm, registers, instruction.A, sliceValue, element)
	}

	if !sliceValue.IsValid() {
		registers.General[instruction.A] = reflect.ValueOf([]int{int(element)})
		return opContinue
	}
	if rc := checkAppendLimit(vm, sliceValue); rc != opContinue {
		return rc
	}

	if s, ok := reflect.TypeAssert[[]int64](sliceValue); ok {
		grown := arenaAppendInt(vm.Arena, s, element)
		registers.General[instruction.A] = arenaWrapTypedSliceFromSource(vm.Arena, sliceValue, sliceDataPtrInt64(grown), len(grown), cap(grown))
		return opContinue
	}
	if s, ok := reflect.TypeAssert[[]int](sliceValue); ok {
		s = append(s, int(element))
		registers.General[instruction.A] = arenaWrapTypedSliceFromSource(vm.Arena, sliceValue, sliceDataPtrInt(s), len(s), cap(s))
		return opContinue
	}

	registers.General[instruction.A] = reflect.Append(sliceValue, reflect.ValueOf(int(element)))
	return opContinue
}

// appendIntInPlace appends an int element to a slice in-place using
// Grow/SetLen/Index.Set, avoiding reflect.ValueOf allocations.
//
// The slice value is promoted to addressable on first use.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the register banks.
// Takes destination (uint8) which is the register to store the result slice in.
// Takes sliceValue (reflect.Value) which is the slice to append to.
// Takes element (int64) which is the value to append.
//
// Returns OpResult indicating the next execution step.
func appendIntInPlace(vm *VM, registers *Registers, destination uint8, sliceValue reflect.Value, element int64) OpResult {
	return appendScalarInPlace(vm, registers, destination, sliceValue, reflect.TypeFor[[]int](), func(target reflect.Value) {
		target.SetInt(element)
	})
}

// appendScalar is a generic helper for typed append handlers. It attempts a
// concrete-slice fast path before falling back to reflect.Append.
//
// Callers that own a *RegisterArena route through the arena-aware fast paths in the typed
// append handlers (handleAppendInt etc.) so the grow path keeps the new backing inside
// the arena.
//
// Takes registers (*Registers) which provides the register file.
// Takes instruction (instruction) which encodes the operand indices.
// Takes element (T) which is the element to append.
//
// Returns OpResult indicating the next execution step.
func appendScalar[T comparable](registers *Registers, instruction isa.Instruction, element T) OpResult {
	sliceValue := registers.General[instruction.B]
	if !sliceValue.IsValid() {
		registers.General[instruction.A] = reflect.ValueOf([]T{element})
		return opContinue
	}
	if s, ok := reflect.TypeAssert[[]T](sliceValue); ok {
		registers.General[instruction.A] = reflect.ValueOf(append(s, element))
		return opContinue
	}
	registers.General[instruction.A] = reflect.Append(sliceValue, reflect.ValueOf(element))
	return opContinue
}

// handleAppendString handles the isa.SubOpAppendString instruction by appending a string
// value from a string register to a slice in a general register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the slice and string element.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleAppendString(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	element := registers.Strings[instruction.C]
	if instruction.A == instruction.B {
		return appendStringInPlace(vm, registers, instruction.A, registers.General[instruction.B], element)
	}
	sliceValue := registers.General[instruction.B]
	if sliceValue.IsValid() {
		if rc := checkAppendLimit(vm, sliceValue); rc != opContinue {
			return rc
		}
		if s, ok := reflect.TypeAssert[[]string](sliceValue); ok {
			grown := arenaAppendString(vm.Arena, s, MaterialiseStringForArenaAppend(vm.Arena, s, element))
			registers.General[instruction.A] = arenaWrapTypedSliceFromSource(vm.Arena, sliceValue, sliceDataPtrString(grown), len(grown), cap(grown))
			return opContinue
		}
	}
	return appendScalarChecked(vm, registers, instruction, element)
}

// appendStringInPlace appends a string element to an addressable slice using
// Grow/SetLen/SetString, avoiding reflect.ValueOf boxing.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which provides the register file.
// Takes destination (uint8) which is the destination general register.
// Takes sliceValue (reflect.Value) which is the current slice.
// Takes element (string) which is the element to append.
//
// Returns OpResult indicating the next execution step.
func appendStringInPlace(vm *VM, registers *Registers, destination uint8, sliceValue reflect.Value, element string) OpResult {
	return appendScalarInPlace(vm, registers, destination, sliceValue, reflect.TypeFor[[]string](), func(target reflect.Value) {
		target.SetString(materialiseStringForSliceStore(vm.Arena, ReflectValuePtr(target), element))
	})
}

// handleAppendFloat handles the isa.SubOpAppendFloat instruction by appending a float
// value from a float register to a slice in a general register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the slice and float element.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
//
//nolint:dupl // hot-path twin
func handleAppendFloat(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	element := registers.Floats[instruction.C]
	if instruction.A == instruction.B {
		return appendFloatInPlace(vm, registers, instruction.A, registers.General[instruction.B], element)
	}
	sliceValue := registers.General[instruction.B]
	if sliceValue.IsValid() {
		if rc := checkAppendLimit(vm, sliceValue); rc != opContinue {
			return rc
		}
		if s, ok := reflect.TypeAssert[[]float64](sliceValue); ok {
			grown := arenaAppendFloat(vm.Arena, s, element)
			registers.General[instruction.A] = arenaWrapTypedSliceFromSource(vm.Arena, sliceValue, sliceDataPtrFloat64(grown), len(grown), cap(grown))
			return opContinue
		}
	}
	return appendScalarChecked(vm, registers, instruction, element)
}

// appendFloatInPlace appends a float element to an addressable slice using
// Grow/SetLen/SetFloat, avoiding reflect.ValueOf boxing.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which provides the register file.
// Takes destination (uint8) which is the destination general register.
// Takes sliceValue (reflect.Value) which is the current slice.
// Takes element (float64) which is the element to append.
//
// Returns OpResult indicating the next execution step.
func appendFloatInPlace(vm *VM, registers *Registers, destination uint8, sliceValue reflect.Value, element float64) OpResult {
	return appendScalarInPlace(vm, registers, destination, sliceValue, reflect.TypeFor[[]float64](), func(target reflect.Value) {
		target.SetFloat(element)
	})
}

// handleAppendBool handles the isa.SubOpAppendBool instruction by appending a bool value
// from a bool register to a slice in a general register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the slice and bool element.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
//
//nolint:dupl // hot-path twin
func handleAppendBool(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	element := registers.Bools[instruction.C]
	if instruction.A == instruction.B {
		return appendBoolInPlace(vm, registers, instruction.A, registers.General[instruction.B], element)
	}
	sliceValue := registers.General[instruction.B]
	if sliceValue.IsValid() {
		if rc := checkAppendLimit(vm, sliceValue); rc != opContinue {
			return rc
		}
		if s, ok := reflect.TypeAssert[[]bool](sliceValue); ok {
			grown := arenaAppendBool(vm.Arena, s, element)
			registers.General[instruction.A] = arenaWrapTypedSliceFromSource(vm.Arena, sliceValue, sliceDataPtrBool(grown), len(grown), cap(grown))
			return opContinue
		}
	}
	return appendScalarChecked(vm, registers, instruction, element)
}

// appendBoolInPlace appends a bool element to an addressable slice using
// Grow/SetLen/SetBool, avoiding reflect.ValueOf boxing.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which provides the register file.
// Takes destination (uint8) which is the destination general register.
// Takes sliceValue (reflect.Value) which is the current slice.
// Takes element (bool) which is the element to append.
//
// Returns OpResult indicating the next execution step.
func appendBoolInPlace(vm *VM, registers *Registers, destination uint8, sliceValue reflect.Value, element bool) OpResult {
	return appendScalarInPlace(vm, registers, destination, sliceValue, reflect.TypeFor[[]bool](), func(target reflect.Value) {
		target.SetBool(element)
	})
}

// checkAppendLimit returns opPanicError if appending one element to sliceValue would
// exceed maxAllocSize.
//
// Takes vm (*VM) which provides access to allocation limits.
// Takes sliceValue (reflect.Value) which is the slice being appended to.
//
// Returns OpResult which is opPanicError when the limit is exceeded, or opContinue
// otherwise.
func checkAppendLimit(vm *VM, sliceValue reflect.Value) OpResult {
	if vm.Limits.MaxAllocSize > 0 && sliceValue.IsValid() && sliceValue.Len()+1 > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(appendLimitExceededFormat,
			fault.ErrAllocationLimit, sliceValue.Len()+1, vm.Limits.MaxAllocSize)
		return opPanicError
	}
	return opContinue
}

// appendScalarChecked is appendScalar with an allocation limit check.
//
// Takes vm (*VM) which provides access to allocation limits.
// Takes registers (*Registers) which holds the slice and destination.
// Takes instruction (instruction) which encodes the operand indices.
// Takes element (T) which is the value to append.
//
// Returns OpResult which is opPanicError when the limit is exceeded, or the result of
// appendScalar otherwise.
func appendScalarChecked[T comparable](vm *VM, registers *Registers, instruction isa.Instruction, element T) OpResult {
	sliceValue := registers.General[instruction.B]
	if rc := checkAppendLimit(vm, sliceValue); rc != opContinue {
		return rc
	}
	return appendScalar(registers, instruction, element)
}

// appendScalarInPlace is the shared implementation for all typed in-place append
// handlers. It uses Grow/SetLen/Index to extend the slice without allocating a new
// reflect.Value via reflect.ValueOf.
//
// Takes vm (*VM) which provides access to allocation limits.
// Takes registers (*Registers) which provides the register file.
// Takes destination (uint8) which is the destination general register.
// Takes sliceValue (reflect.Value) which is the current slice.
// Takes zeroSliceType (reflect.Type) which is the slice type to create when the current
// value is invalid (nil slice).
// Takes setter (func(reflect.Value)) which writes the element into the target
// reflect.Value at the appended index.
//
// Returns OpResult indicating the next execution step.
func appendScalarInPlace(
	vm *VM,
	registers *Registers,
	destination uint8,
	sliceValue reflect.Value,
	zeroSliceType reflect.Type,
	setter func(reflect.Value),
) OpResult {
	if !sliceValue.IsValid() {
		slicePointer := reflect.New(zeroSliceType)
		addressable := slicePointer.Elem()
		addressable.Grow(1)
		addressable.SetLen(1)
		setter(addressable.Index(0))
		registers.General[destination] = addressable
		return opContinue
	}
	if rc := checkAppendLimit(vm, sliceValue); rc != opContinue {
		return rc
	}
	if !sliceValue.CanSet() {
		slicePointer := reflect.New(sliceValue.Type())
		slicePointer.Elem().Set(sliceValue)
		sliceValue = slicePointer.Elem()
	}
	length := sliceValue.Len()
	sliceValue.Grow(1)
	sliceValue.SetLen(length + 1)
	setter(sliceValue.Index(length))
	registers.General[destination] = sliceValue
	return opContinue
}

// handleAppendByteFast is the tier-0 specialised byte-builder append. A=dest general,
// B=source general ([]byte), C=byte value uint register.
//
// Takes vm (*VM) which provides arena, evalError, and allocation limits.
// Takes registers (*Registers) which holds the general slice and uint element banks.
// Takes instr (instruction) which encodes A, B, C register indices.
//
// Returns OpResult indicating the next execution step.
func handleAppendByteFast(vm *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	sliceValue := registers.General[instr.B]
	if !sliceValue.IsValid() {
		registers.General[instr.A] = reflect.ValueOf([]byte{safeconv.Uint64ToUint8(registers.Uints[instr.C])})
		return opContinue
	}

	if vm.Limits.MaxAllocSize > 0 && sliceValue.Len()+1 > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(appendLimitExceededFormat,
			fault.ErrAllocationLimit, sliceValue.Len()+1, vm.Limits.MaxAllocSize)
		return opPanicError
	}

	s, ok := reflect.TypeAssert[[]byte](sliceValue)
	if !ok {
		element := registers.Uints[instr.C]
		boxedElement := reflect.ValueOf(safeconv.Uint64ToUint8(element))
		if result, ok := appendFastPath(vm.Arena, sliceValue, boxedElement); ok {
			registers.General[instr.A] = result
			return opContinue
		}
		registers.General[instr.A] = reflect.Append(sliceValue, boxedElement)
		return opContinue
	}
	//nolint:gocritic // fresh slice intended
	result := append(s, safeconv.Uint64ToUint8(registers.Uints[instr.C]))
	registers.General[instr.A] = arenaWrapByteSpreadResult(vm.Arena, sliceValue, result)
	return opContinue
}

// handleAppendByteFastInPlace is the unified in-place byte-builder append. Handles both
// the slice form (mutating an arena-owned header in place) and the pointer form
// (dereferencing through flagIndir and writing via runtimeTypedmemmove).
//
// Takes vm (*VM) which provides arena, evalError, and allocation limits.
// Takes frame (*CallFrame) which the x-form fallback path forwards.
// Takes registers (*Registers) which holds the slice/pointer and uint banks.
// Takes instr (instruction) which encodes the A, B, C register indices (A == B; C is the
// byte value).
//
// Returns OpResult indicating the next execution step.
func handleAppendByteFastInPlace(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	sliceValue := registers.General[instr.B]
	if !sliceValue.IsValid() {
		registers.General[instr.A] = reflect.ValueOf([]byte{safeconv.Uint64ToUint8(registers.Uints[instr.C])})
		return opContinue
	}

	switch sliceValue.Kind() {
	case reflect.Slice:
		return appendByteFastInPlaceSlice(vm, frame, registers, instr, sliceValue)
	case reflect.Pointer:
		if sliceValue.IsNil() {
			return raiseNilDereference(vm)
		}
		return appendByteFastInPlacePointer(vm, registers, instr, sliceValue)
	default:
		return handleAppendByteFast(vm, frame, registers, instr)
	}
}

// appendByteFastInPlaceSlice handles the x = append(x, b) shape.
//
// Inspects the source reflect.Value's internal ptr field; if it points at an arena-owned
// arenaSliceHeader slot, mutates Data/Len/Cap on that slot in place. Otherwise routes to
// handleAppendByteFast which allocates a fresh slot, the defensive path for cases the
// safety predicate didn't anticipate.
//
// Takes vm (*VM) which provides arena and allocation limits.
// Takes frame (*CallFrame) which is forwarded to the fallback path.
// Takes registers (*Registers) which holds the slice and uint banks.
// Takes instr (instruction) which encodes the operand indices.
// Takes sliceValue (reflect.Value) which is the source slice.
//
// Returns OpResult indicating the next execution step.
func appendByteFastInPlaceSlice(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction, sliceValue reflect.Value) OpResult {
	if vm.Limits.MaxAllocSize > 0 && sliceValue.Len()+1 > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(appendLimitExceededFormat,
			fault.ErrAllocationLimit, sliceValue.Len()+1, vm.Limits.MaxAllocSize)
		return opPanicError
	}

	sourceShape := (*unsafeReflectValue)(unsafe.Pointer(&sliceValue))
	headerPtr := (*arenaSliceHeader)(sourceShape.ptr)
	if headerPtr == nil || !vm.Arena.OwnsSliceHeaderPointer(unsafe.Pointer(headerPtr)) {
		return handleAppendByteFast(vm, frame, registers, instr)
	}

	value := safeconv.Uint64ToUint8(registers.Uints[instr.C])
	if tryReuseInPlaceByteHeader(frame, headerPtr, value) {
		return opContinue
	}
	current := unsafe.Slice((*byte)(headerPtr.Data), headerPtr.Cap)[:headerPtr.Len]
	result := arenaAppendByte(vm.Arena, current, value)

	headerPtr.Data = unsafe.Pointer(unsafe.SliceData(result))
	headerPtr.Len = len(result)
	headerPtr.Cap = cap(result)
	return opContinue
}

// handleAppendInPlace is the generic in-place append.
//
// Handles arbitrary element types via the same kind switch as
// handleAppendByteFastInPlace; the only difference is the element register lives on the
// general bank (passed by reflect.Value) so the per-type fast paths route through
// appendGenericFastPath / appendFastPath helpers. At runtime A == B by construction.
// Falls back to handleAppend on non-arena slices or non-slice/pointer source kinds.
//
// Encoding (mirrors isa.OpAppend): A=dest general, B=source general, C=element general,
// followed by isa.OpExt.
//
// Takes vm (*VM) which provides arena and allocation limits.
// Takes frame (*CallFrame) which is forwarded to fallback paths.
// Takes registers (*Registers) which holds the general bank.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleAppendInPlace(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	sliceValue := registers.General[instr.B]
	if !sliceValue.IsValid() {
		return handleAppend(vm, frame, registers, instr)
	}
	switch sliceValue.Kind() {
	case reflect.Slice:
		return appendInPlaceSlice(vm, frame, registers, instr, sliceValue)
	case reflect.Pointer:
		if sliceValue.IsNil() {
			return raiseNilDereference(vm)
		}
		return appendInPlacePointer(vm, frame, registers, instr, sliceValue)
	default:
		return handleAppend(vm, frame, registers, instr)
	}
}

// appendInPlaceSlice mutates the slice header on the arena-owned slot.
//
// Routes through appendGenericFastPath when the element fits in spare capacity (no grow
// needed). On grow or non-arena slot, falls back to handleAppend.
//
// Takes vm (*VM) which provides arena and allocation limits.
// Takes frame (*CallFrame) which is forwarded to fallback paths.
// Takes registers (*Registers) which holds the general bank.
// Takes instr (instruction) which encodes the operand indices.
// Takes sliceValue (reflect.Value) which is the source slice.
//
// Returns OpResult indicating the next execution step.
func appendInPlaceSlice(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction, sliceValue reflect.Value) OpResult {
	sourceShape := (*unsafeReflectValue)(unsafe.Pointer(&sliceValue))
	headerPtr := (*arenaSliceHeader)(sourceShape.ptr)
	if headerPtr == nil || !vm.Arena.OwnsSliceHeaderPointer(unsafe.Pointer(headerPtr)) {
		return handleAppend(vm, frame, registers, instr)
	}
	if vm.Limits.MaxAllocSize > 0 && sliceValue.Len()+1 > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(appendLimitExceededFormat,
			fault.ErrAllocationLimit, sliceValue.Len()+1, vm.Limits.MaxAllocSize)
		return opPanicError
	}
	element := registers.General[instr.C]
	if !element.IsValid() {
		return handleAppend(vm, frame, registers, instr)
	}
	if tryReuseInPlaceHeader(frame, headerPtr, sliceValue, element) {
		return opContinue
	}

	if result, ok := appendFastPath(vm.Arena, sliceValue, element); ok {
		resultShape := (*unsafeReflectValue)(unsafe.Pointer(&result))
		resultHeader := (*arenaSliceHeader)(resultShape.ptr)
		if resultHeader != nil && vm.Arena.OwnsSliceHeaderPointer(unsafe.Pointer(resultHeader)) {
			headerPtr.Data = resultHeader.Data
			headerPtr.Len = resultHeader.Len
			headerPtr.Cap = resultHeader.Cap
			return opContinue
		}
		registers.General[instr.A] = result
		return opContinue
	}
	if result, ok := appendGenericFastPath(vm.Arena, sliceValue, element); ok {
		resultShape := (*unsafeReflectValue)(unsafe.Pointer(&result))
		resultHeader := (*arenaSliceHeader)(resultShape.ptr)
		if resultHeader != nil && vm.Arena.OwnsSliceHeaderPointer(unsafe.Pointer(resultHeader)) {
			headerPtr.Data = resultHeader.Data
			headerPtr.Len = resultHeader.Len
			headerPtr.Cap = resultHeader.Cap
			return opContinue
		}
		registers.General[instr.A] = result
		return opContinue
	}
	return handleAppend(vm, frame, registers, instr)
}

// appendInPlacePointer handles *p = append(*p, e) for arbitrary types.
//
// Uses the same runtimeTypedmemmove machinery as the byte variant. Falls back to
// handleAppend for slot-extraction failures.
//
// Takes vm (*VM) which provides arena and allocation limits.
// Takes frame (*CallFrame) which is forwarded to fallback paths.
// Takes registers (*Registers) which holds the general bank.
// Takes instr (instruction) which encodes the operand indices.
// Takes pointerValue (reflect.Value) which is the pointer to the destination slice.
//
// Returns OpResult indicating the next execution step.
func appendInPlacePointer(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction, pointerValue reflect.Value) OpResult {
	recvShape := (*unsafeReflectValue)(unsafe.Pointer(&pointerValue))
	var headerPtr unsafe.Pointer
	if recvShape.flag&flagIndir != 0 {
		headerPtr = *(*unsafe.Pointer)(recvShape.ptr)
	} else {
		headerPtr = recvShape.ptr
	}
	if headerPtr == nil {
		return handleAppend(vm, frame, registers, instr)
	}

	elemType := pointerValue.Type().Elem()
	current := reflect.NewAt(elemType, headerPtr).Elem()
	if vm.Limits.MaxAllocSize > 0 && current.Len()+1 > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(appendLimitExceededFormat,
			fault.ErrAllocationLimit, current.Len()+1, vm.Limits.MaxAllocSize)
		return opPanicError
	}
	element := registers.General[instr.C]
	if !element.IsValid() {
		return handleAppend(vm, frame, registers, instr)
	}
	coerced := coerceValue(vm, element, elemType.Elem())
	extended := reflect.Append(current, coerced)

	current.Set(extended)
	return opContinue
}

// handleAppendSpreadInPlace is the spread sibling of handleAppendInPlace. Routes to
// handleAppendSpread for the generic path.
//
// Takes vm (*VM) which provides arena and allocation limits.
// Takes frame (*CallFrame) which is forwarded to handleAppendSpread.
// Takes registers (*Registers) which holds the general bank.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleAppendSpreadInPlace(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	return handleAppendSpread(vm, frame, registers, instr)
}

// conformSliceWidth converts a slice read from a typed bank to a slot's slice type when
// the two differ only in element width.
//
// The typed banks widen every integer element to eight bytes and box the slice as the
// bank's canonical type, so a []uint16 value arrives as []uint64 wherever no static type
// converted it earlier.
//
// Takes value (reflect.Value) which is the value being stored.
// Takes targetType (reflect.Type) which is the slot's type.
//
// Returns reflect.Value which is the converted slice.
// Returns bool which is true when a conversion applied; false leaves the caller to its
// ordinary coercion.
func conformSliceWidth(value reflect.Value, targetType reflect.Type) (reflect.Value, bool) {
	if !value.IsValid() || value.Kind() != reflect.Slice || targetType.Kind() != reflect.Slice || value.Type() == targetType {
		return reflect.Value{}, false
	}
	sourceElem, targetElem := value.Type().Elem(), targetType.Elem()
	if !isBankScalarKind(sourceElem.Kind()) || !isBankScalarKind(targetElem.Kind()) || !sourceElem.ConvertibleTo(targetElem) {
		return reflect.Value{}, false
	}
	if value.Type().ConvertibleTo(targetType) {
		return value.Convert(targetType), true
	}
	converted := reflect.MakeSlice(targetType, value.Len(), value.Len())
	for i := range value.Len() {
		converted.Index(i).Set(value.Index(i).Convert(targetElem))
	}
	return converted, true
}

// isBankScalarKind reports whether a kind is one the typed banks widen: the integer,
// unsigned and floating-point kinds.
//
// Takes kind (reflect.Kind) which is the element kind.
//
// Returns bool which is true for a widened scalar kind.
func isBankScalarKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}
