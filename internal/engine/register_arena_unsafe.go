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

// SAFETY: the ownership tests in this file compare a pointer's address against the bounds
// of the arena's current and retired slabs. They are sound because a slab is a single Go
// allocation whose address range is fixed for its lifetime, retired slabs stay referenced
// by the arena until MinorGC drops a whole dead generation, and the tests only compare
// addresses; the pointee is dereferenced only by callers that already hold a typed
// reference to it.

import (
	"reflect"
	"strconv"
	"unicode/utf8"
	"unsafe"

	"pipit.sh/pipit/internal/mem"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// arenaUsesUnsafeSlabs is true in the unsafe build, signalling to white-box unit tests
	// that the arena's slab-internal fields and methods (ByteSlab, SliceHeaderSlab,
	// ByteIndex, OldByteSlabs, GrowByteSlab, and so on) are real and may be exercised.
	arenaUsesUnsafeSlabs = true

	// itoaMaxDigitsBase10 is the worst-case byte length of an int64 formatted in base 10
	// (the longest is "-9223372036854775808", 20 bytes including the sign).
	itoaMaxDigitsBase10 = 20

	// formatIntMaxDigitsBase2 is the worst-case byte length of an int64 formatted in any
	// base accepted by strconv.FormatInt. Base 2 is the widest at 64 binary digits, plus 1
	// for the sign, equals 65.
	formatIntMaxDigitsBase2 = 65

	// decimalRadix is the base passed to strconv.AppendInt when formatting integers as
	// standard decimal strings.
	decimalRadix = 10
)

// OwnsString reports whether s points into the arena's current or previous byte slabs.
// Used to decide whether a string must be cloned before it can safely outlive the arena.
//
// Takes s (string) which is the string to test.
//
// Returns true when s is backed by the arena's byte slabs.
func (a *RegisterArena) OwnsString(s string) bool {
	if len(s) == 0 {
		return false
	}

	pointer := uintptr(unsafe.Pointer(unsafe.StringData(s)))
	if len(a.byteSlab) > 0 {
		base := uintptr(unsafe.Pointer(&a.byteSlab[0]))
		if pointer >= base && pointer < base+uintptr(len(a.byteSlab)) {
			return true
		}
	}
	for _, slab := range a.oldByteSlabs {
		if len(slab) == 0 {
			continue
		}

		base := uintptr(unsafe.Pointer(&slab[0]))
		if pointer >= base && pointer < base+uintptr(len(slab)) {
			return true
		}
	}
	return false
}

// OwnsSliceBacking reports whether p falls inside any arena backing slab.
//
// Covers the string byte slab, the generic byte slab and every typed element slab,
// current or retired. A slice whose backing is arena-owned dies with the arena and must
// be copied to the heap before a heap container may keep it.
//
// Takes p (unsafe.Pointer) which is the slice's data pointer to test.
//
// Returns true when p is in any arena backing slab.
func (a *RegisterArena) OwnsSliceBacking(p unsafe.Pointer) bool {
	if p == nil {
		return false
	}
	pointer := uintptr(p)
	return slabsOwnPointer(a.byteSlab, a.oldByteSlabs, pointer) ||
		slabsOwnPointer(a.genericBytesSlab, a.oldGenericByteSlabs, pointer) ||
		slabsOwnPointer(a.stringBackingSlab, a.oldStringBackings, pointer) ||
		slabsOwnPointer(a.intBackingSlab, a.oldIntBackings, pointer) ||
		slabsOwnPointer(a.floatBackingSlab, a.oldFloatBackings, pointer) ||
		slabsOwnPointer(a.boolBackingSlab, a.oldBoolBackings, pointer) ||
		slabsOwnPointer(a.uintBackingSlab, a.oldUintBackings, pointer)
}

// OwnsStringBox reports whether p is a slot in the arena's live string box slab.
//
// Reset() clears the live slab, so a boxed string escaping to the heap must be re-boxed
// even when its bytes are already heap-backed. Slabs replaced by GrowStringBoxSlab() are
// left to the garbage collector, so only the live slab is tested.
//
// Takes p (unsafe.Pointer) which is the reflect.Value storage pointer to test.
//
// Returns true when p is a slot in the live string box slab.
func (a *RegisterArena) OwnsStringBox(p unsafe.Pointer) bool {
	if p == nil || len(a.stringBoxSlab) == 0 {
		return false
	}
	pointer := uintptr(p)

	base := uintptr(unsafe.Pointer(&a.stringBoxSlab[0]))
	return pointer >= base && pointer < base+uintptr(len(a.stringBoxSlab))*unsafe.Sizeof(a.stringBoxSlab[0])
}

// OwnsSliceHeaderPointer reports whether p references an arenaSliceHeader slot in the
// current or any retired slice-header slab. Used by MaterialiseArenaValue when the
// reflect.Value's .ptr is the arenaSliceHeader produced by copyReflectValueArena /
// appendGenericFastPath / arenaMakeStructSliceBacking and the value is escaping the
// arena's lifetime.
//
// Takes p (unsafe.Pointer) which is the slice header pointer to test.
//
// Returns true when p falls inside any arena slice-header slab.
func (a *RegisterArena) OwnsSliceHeaderPointer(p unsafe.Pointer) bool {
	if p == nil {
		return false
	}
	pointer := uintptr(p)
	if len(a.sliceHeaderSlab) > 0 {
		base := uintptr(unsafe.Pointer(&a.sliceHeaderSlab[0]))
		stride := unsafe.Sizeof(a.sliceHeaderSlab[0])
		if pointer >= base && pointer < base+uintptr(len(a.sliceHeaderSlab))*stride {
			return true
		}
	}
	for _, slab := range a.oldSliceHeaderSlabs {
		if len(slab) == 0 {
			continue
		}

		base := uintptr(unsafe.Pointer(&slab[0]))
		stride := unsafe.Sizeof(slab[0])
		if pointer >= base && pointer < base+uintptr(len(slab))*stride {
			return true
		}
	}
	return false
}

// ownsIndirectCell reports whether p addresses a cell the arena placed for an
// isa.OpAllocIndirect site.
//
// Takes p (unsafe.Pointer) which is the cell address.
//
// Returns bool which is true for an arena-resident cell.
func (a *RegisterArena) ownsIndirectCell(p unsafe.Pointer) bool {
	return a.OwnsSliceHeaderPointer(p) || a.ownsBytePointer(p)
}

// ownsBytePointer reports whether p falls inside an arena byte slab.
//
// Covers either the current generic byte slab or any retired slab kept alive in
// OldGenericByteSlabs after a grow. Mirrors OwnsString for the string-backed ByteSlab;
// required by MaterialiseArenaValue so escape-copy guards can decide whether a
// reflect.Value's storage will outlive the local register lifetime without
// false-negativing on values that landed in a grown-away slab.
//
// Takes p (unsafe.Pointer) which is the storage pointer to test. Typically the .ptr field
// of a flagAddr|flagIndir reflect.Value, extracted via ReflectValuePtr.
//
// Returns true when p is in the arena's pointer-free byte slabs.
func (a *RegisterArena) ownsBytePointer(p unsafe.Pointer) bool {
	if p == nil {
		return false
	}
	pointer := uintptr(p)
	if len(a.genericBytesSlab) > 0 {
		base := uintptr(unsafe.Pointer(&a.genericBytesSlab[0]))
		if pointer >= base && pointer < base+uintptr(len(a.genericBytesSlab)) {
			return true
		}
	}
	for _, slab := range a.oldGenericByteSlabs {
		if len(slab) == 0 {
			continue
		}

		base := uintptr(unsafe.Pointer(&slab[0]))
		if pointer >= base && pointer < base+uintptr(len(slab)) {
			return true
		}
	}
	return false
}

// ownsStringBacking reports whether p falls inside an arena string-backing slab.
//
// Takes p (unsafe.Pointer) which is the slice's data pointer to test.
//
// Returns true when p is in the arena's string-backing slabs.
func (a *RegisterArena) ownsStringBacking(p unsafe.Pointer) bool {
	if p == nil {
		return false
	}
	return slabsOwnPointer(a.stringBackingSlab, a.oldStringBackings, uintptr(p))
}

// ownsSliceBackingOf is the element-kind-aware form of OwnsSliceBacking().
//
// Only []string can live in the string-backing slab, a scalar element type can only live
// in its own typed slab or the generic byte slab, and a slice of reference kinds can
// never be arena-backed, so most stores answer after one or two probes.
//
// Takes v (reflect.Value) which is the slice whose backing is tested.
//
// Returns true when v's backing lies in an arena slab that could hold its element type.
func (a *RegisterArena) ownsSliceBackingOf(v reflect.Value) bool {
	p := v.UnsafePointer()
	if p == nil {
		return false
	}
	pointer := uintptr(p)
	switch v.Type().Elem().Kind() {
	case reflect.String:
		return slabsOwnPointer(a.stringBackingSlab, a.oldStringBackings, pointer)
	case reflect.Uint8:
		return slabsOwnPointer(a.byteSlab, a.oldByteSlabs, pointer) || a.ownsBytePointer(p)
	case reflect.Int, reflect.Int64:
		return slabsOwnPointer(a.intBackingSlab, a.oldIntBackings, pointer) || a.ownsBytePointer(p)
	case reflect.Float64:
		return slabsOwnPointer(a.floatBackingSlab, a.oldFloatBackings, pointer) || a.ownsBytePointer(p)
	case reflect.Bool:
		return slabsOwnPointer(a.boolBackingSlab, a.oldBoolBackings, pointer) || a.ownsBytePointer(p)
	case reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return slabsOwnPointer(a.uintBackingSlab, a.oldUintBackings, pointer) || a.ownsBytePointer(p)
	case reflect.Pointer, reflect.Map, reflect.Interface, reflect.Slice, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return false
	default:
		return a.ownsBytePointer(p)
	}
}

// ownsScalarBox reports whether p is a slot in a live scalar box slab.
//
// Covers the int, float, uint and complex box slabs. Reset() clears them, so a boxed
// scalar escaping to the heap must be re-boxed. Slabs replaced by a grow are left to the
// garbage collector, so only the live slabs are tested.
//
// Takes p (unsafe.Pointer) which is the reflect.Value storage pointer to test.
//
// Returns true when p is a slot in a live scalar box slab.
func (a *RegisterArena) ownsScalarBox(p unsafe.Pointer) bool {
	if p == nil {
		return false
	}
	pointer := uintptr(p)
	return slabOwnsPointer(a.intBoxSlab, pointer) ||
		slabOwnsPointer(a.floatBoxSlab, pointer) ||
		slabOwnsPointer(a.uintBoxSlab, pointer) ||
		slabOwnsPointer(a.complexBoxSlab, pointer)
}

// isStringAtSlabTail reports whether the end of s coincides with the current byte-slab
// write position, enabling in-place extension when appending to the
// most-recently-allocated arena string.
//
// Takes s (string) which is the string to test.
//
// Returns true when s ends exactly at the current slab allocation pointer.
func (a *RegisterArena) isStringAtSlabTail(s string) bool {
	if len(s) == 0 || len(a.byteSlab) == 0 {
		return false
	}

	sEnd := uintptr(unsafe.Pointer(unsafe.StringData(s))) + uintptr(len(s))
	slabPos := uintptr(unsafe.Pointer(&a.byteSlab[0])) + safeconv.IntToUintptr(a.byteIndex)
	return sEnd == slabPos
}

// ArenaConcatString concatenates a and b, bump-allocating the result into the arena's
// byte slab. When a ends at the slab tail, the bytes of b are appended in place without
// copying a.
//
// Takes arena (*RegisterArena) which provides the byte slab.
// Takes a (string) which is the left operand.
// Takes b (string) which is the right operand.
//
// Returns the concatenated string backed by the arena.
func ArenaConcatString(arena *RegisterArena, a, b string) string {
	n := len(a) + len(b)
	if n == 0 {
		return ""
	}
	if len(a) > 0 && arena.isStringAtSlabTail(a) && arena.byteIndex+len(b) <= len(arena.byteSlab) {
		buffer := arena.AllocStringBytes(len(b))
		copy(buffer, b)

		return unsafe.String(unsafe.StringData(a), n)
	}
	buffer := arena.AllocStringBytes(n)
	copy(buffer, a)
	copy(buffer[len(a):], b)
	return mem.String(buffer)
}

// slabOwnsPointer reports whether pointer falls inside the element storage of slab.
//
// Takes slab ([]T) which is the slab to test against.
// Takes pointer (uintptr) which is the address to test.
//
// Returns true when pointer lies within slab's elements.
func slabOwnsPointer[T any](slab []T, pointer uintptr) bool {
	if len(slab) == 0 {
		return false
	}
	var zero T

	base := uintptr(unsafe.Pointer(&slab[0]))
	return pointer >= base && pointer < base+uintptr(len(slab))*unsafe.Sizeof(zero)
}

// slabsOwnPointer reports whether pointer falls inside the current slab or any retired
// slab kept alive after a grow.
//
// Takes current ([]T) which is the live slab.
// Takes retired ([][]T) which are the slabs replaced by growth since the last reset.
// Takes pointer (uintptr) which is the address to test.
//
// Returns true when any of the slabs owns pointer.
func slabsOwnPointer[T any](current []T, retired [][]T, pointer uintptr) bool {
	if slabOwnsPointer(current, pointer) {
		return true
	}
	for _, slab := range retired {
		if slabOwnsPointer(slab, pointer) {
			return true
		}
	}
	return false
}

// arenaConcatRuneString appends rune r to string s using bump allocation. When s ends at
// the slab tail, the rune bytes are written in place without copying s.
//
// Takes arena (*RegisterArena) which provides the byte slab.
// Takes s (string) which is the base string.
// Takes r (rune) which is the rune to append.
//
// Returns the resulting string backed by the arena.
func arenaConcatRuneString(arena *RegisterArena, s string, r rune) string {
	runeLen := utf8.RuneLen(r)
	if runeLen < 0 {
		runeLen = utf8.RuneLen(utf8.RuneError)
	}
	n := len(s) + runeLen
	if n == runeLen {
		return arenaRuneToString(arena, r)
	}
	if arena.isStringAtSlabTail(s) && arena.byteIndex+runeLen <= len(arena.byteSlab) {
		buffer := arena.AllocStringBytes(runeLen)
		utf8.EncodeRune(buffer, r)

		return unsafe.String(unsafe.StringData(s), n)
	}
	buffer := arena.AllocStringBytes(n)
	copy(buffer, s)
	utf8.EncodeRune(buffer[len(s):], r)
	return mem.String(buffer)
}

// arenaRuneToString converts rune r to a string using bump allocation into the arena's
// byte slab.
//
// Takes arena (*RegisterArena) which provides the byte slab.
// Takes r (rune) which is the rune to convert.
//
// Returns the single-rune string backed by the arena.
func arenaRuneToString(arena *RegisterArena, r rune) string {
	n := utf8.RuneLen(r)
	if n < 0 {
		n = utf8.RuneLen(utf8.RuneError)
	}
	buffer := arena.AllocStringBytes(n)
	utf8.EncodeRune(buffer, r)
	return mem.String(buffer)
}

// arenaBytesToString converts byte slice b to a string using bump allocation into the
// arena's byte slab.
//
// Takes arena (*RegisterArena) which provides the byte slab.
// Takes b ([]byte) which is the byte slice to convert.
//
// Returns the resulting string backed by the arena.
func arenaBytesToString(arena *RegisterArena, b []byte) string {
	n := len(b)
	if n == 0 {
		return ""
	}
	buffer := arena.AllocStringBytes(n)
	copy(buffer, b)
	return mem.String(buffer)
}

// arenaItoaString formats x as a base-10 string backed by the arena's byte slab,
// over-allocating the worst case and rewinding to the actual length.
//
// Takes arena (*RegisterArena) which provides the byte slab.
// Takes x (int64) which is the integer to format.
//
// Returns the resulting decimal string backed by the arena.
//
//go:nosplit
func arenaItoaString(arena *RegisterArena, x int64) string {
	buffer := arena.AllocStringBytes(itoaMaxDigitsBase10)
	written := strconv.AppendInt(buffer[:0], x, decimalRadix)
	arena.byteIndex -= itoaMaxDigitsBase10 - len(written)
	return mem.String(written)
}

// arenaFormatIntString formats x in the given base as a string backed by the arena's byte
// slab. Mirrors arenaItoaString but reserves the worst-case 65 bytes (base 2 of a 64-bit
// signed integer) before rewinding to the actual written length.
//
// Takes arena (*RegisterArena) which provides the byte slab.
// Takes x (int64) which is the integer to format.
// Takes base (int) which is the radix passed to strconv.AppendInt.
//
// Returns the resulting string backed by the arena.
//
//go:nosplit
func arenaFormatIntString(arena *RegisterArena, x int64, base int) string {
	buffer := arena.AllocStringBytes(formatIntMaxDigitsBase2)
	written := strconv.AppendInt(buffer[:0], x, base)
	arena.byteIndex -= formatIntMaxDigitsBase2 - len(written)
	return mem.String(written)
}

// sliceGrowsOnHeap reports whether a full slice must grow through Go's append rather than
// in the arena: backing ownership is sticky, so a slice whose backing already lives on
// the heap stays there, while an empty or arena-backed slice grows in the arena.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes s ([]T) which is the full slice about to grow.
//
// Returns true when s has a heap backing.
func sliceGrowsOnHeap[T any](arena *RegisterArena, s []T) bool {
	return cap(s) > 0 && !arena.OwnsSliceBacking(unsafe.Pointer(unsafe.SliceData(s)))
}

// appendIntoArenaBacking copies s into a fresh arena backing and appends x.
//
// Takes s ([]T) which is the full source slice.
// Takes x (T) which is the element to append.
// Takes backing ([]T) which is the arena backing sized for at least len(s)+1 elements.
//
// Returns the grown slice over backing.
func appendIntoArenaBacking[T any](s []T, x T, backing []T) []T {
	copy(backing, s)
	backing = backing[:len(s)+1]
	backing[len(s)] = x
	return backing
}

// arenaAppendInt appends x to an arena-backed []int64, bump-allocating a doubled backing
// from the arena when the slice is full and falling through to Go's builtin append for
// heap-resident slices to avoid escape-barrier copies.
//
// Takes arena (*RegisterArena) which provides the backing slab.
// Takes s ([]int64) which is the slice to append to.
// Takes x (int64) which is the value to append.
//
// Returns the resulting slice.
func arenaAppendInt(arena *RegisterArena, s []int64, x int64) []int64 {
	if len(s) < cap(s) || sliceGrowsOnHeap(arena, s) {
		return append(s, x)
	}
	return appendIntoArenaBacking(s, x, arena.AllocIntBacking(max(2*cap(s), len(s)+1)))
}

// arenaAppendFloat is the float64 sibling of arenaAppendInt.
//
// Takes arena (*RegisterArena) which provides the backing slab.
// Takes s ([]float64) which is the slice to append to.
// Takes x (float64) which is the value to append.
//
// Returns the resulting slice, either reusing s or pointing at a fresh arena-backed
// backing.
func arenaAppendFloat(arena *RegisterArena, s []float64, x float64) []float64 {
	if len(s) < cap(s) || sliceGrowsOnHeap(arena, s) {
		return append(s, x)
	}
	return appendIntoArenaBacking(s, x, arena.AllocFloatBacking(max(2*cap(s), len(s)+1)))
}

// arenaAppendString is the string sibling of arenaAppendInt.
//
// Writes to the arena-backed slot go through the normal Go store barrier because the
// underlying allocation is a real []string sub-slice.
//
// Takes arena (*RegisterArena) which provides the backing slab.
// Takes s ([]string) which is the slice to append to.
// Takes x (string) which is the value to append.
//
// Returns the resulting slice, either reusing s or pointing at a fresh arena-backed
// backing.
func arenaAppendString(arena *RegisterArena, s []string, x string) []string {
	if len(s) < cap(s) || sliceGrowsOnHeap(arena, s) {
		return append(s, x)
	}
	return appendIntoArenaBacking(s, x, arena.AllocStringBacking(max(2*cap(s), len(s)+1)))
}

// arenaAppendBool is the bool sibling of arenaAppendInt.
//
// Takes arena (*RegisterArena) which provides the backing slab.
// Takes s ([]bool) which is the slice to append to.
// Takes x (bool) which is the value to append.
//
// Returns the resulting slice, either reusing s or pointing at a fresh arena-backed
// backing.
func arenaAppendBool(arena *RegisterArena, s []bool, x bool) []bool {
	if len(s) < cap(s) || sliceGrowsOnHeap(arena, s) {
		return append(s, x)
	}
	return appendIntoArenaBacking(s, x, arena.AllocBoolBacking(max(2*cap(s), len(s)+1)))
}

// arenaAppendUint is the uint64 sibling of arenaAppendInt.
//
// Takes arena (*RegisterArena) which provides the backing slab.
// Takes s ([]uint64) which is the slice to append to.
// Takes x (uint64) which is the value to append.
//
// Returns the resulting slice, either reusing s or pointing at a fresh arena-backed
// backing.
func arenaAppendUint(arena *RegisterArena, s []uint64, x uint64) []uint64 {
	if len(s) < cap(s) || sliceGrowsOnHeap(arena, s) {
		return append(s, x)
	}
	return appendIntoArenaBacking(s, x, arena.AllocUintBacking(max(2*cap(s), len(s)+1)))
}

// arenaAppendByte is the byte sibling of arenaAppendInt.
//
// Shares the arena's byte slab with AllocStringBytes / AllocByteBacking via 3-index
// slicing.
//
// Takes arena (*RegisterArena) which provides the backing slab.
// Takes s ([]byte) which is the slice to append to.
// Takes x (byte) which is the value to append.
//
// Returns the resulting slice, either reusing s or pointing at a fresh arena-backed
// backing.
func arenaAppendByte(arena *RegisterArena, s []byte, x byte) []byte {
	if len(s) < cap(s) || sliceGrowsOnHeap(arena, s) {
		return append(s, x)
	}
	return appendIntoArenaBacking(s, x, arena.AllocByteBacking(max(2*cap(s), len(s)+1)))
}
