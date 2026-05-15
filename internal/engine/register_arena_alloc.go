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
	"unsafe"

	"pipit.sh/pipit/internal/safeconv"
)

var (
	// emptyByteBacking is the shared zero-capacity []byte backing for AllocByteBacking(0).
	emptyByteBacking = unsafe.Slice((*byte)(zeroSizeAllocPtr), 0)

	// emptyIntBacking is the shared zero-capacity []int64 backing for AllocIntBacking(0).
	emptyIntBacking = unsafe.Slice((*int64)(zeroSizeAllocPtr), 0)

	// emptyFloatBacking is the shared zero-capacity []float64 backing for
	// AllocFloatBacking(0).
	emptyFloatBacking = unsafe.Slice((*float64)(zeroSizeAllocPtr), 0)

	// emptyStringBacking is the shared zero-capacity []string backing for
	// AllocStringBacking(0).
	emptyStringBacking = unsafe.Slice((*string)(zeroSizeAllocPtr), 0)

	// emptyBoolBacking is the shared zero-capacity []bool backing for AllocBoolBacking(0).
	emptyBoolBacking = unsafe.Slice((*bool)(zeroSizeAllocPtr), 0)

	// emptyUintBacking is the shared zero-capacity []uint64 backing for AllocUintBacking(0).
	emptyUintBacking = unsafe.Slice((*uint64)(zeroSizeAllocPtr), 0)
)

// AllocStringBytes bump-allocates n bytes from the arena's byte slab and returns a
// sub-slice the caller must fully write before use.
//
// The returned slice shares memory with the arena; strings created via unsafe.String
// point into this slab rather than the Go heap.
//
// Takes n (int) which is the number of bytes to allocate.
//
// Returns a byte slice of length n backed by the arena's byte slab.
func (a *RegisterArena) AllocStringBytes(n int) []byte {
	if a.byteIndex+n > len(a.byteSlab) {
		a.GrowByteSlab(n)
	}
	buffer := a.byteSlab[a.byteIndex : a.byteIndex+n]
	a.byteIndex += n
	a.noteAlloc(int64(n))
	return buffer
}

// AllocByteBacking bump-allocates n bytes from the arena's byte slab and returns them in
// three-index form (len == cap == n) so appends past cap go through arenaAppendByte
// rather than spilling into adjacent allocations. Returns a shared empty backing for n ==
// 0.
//
// Takes n (int) which is the number of byte elements (the slice's capacity).
//
// Returns a []byte of length n and capacity n backed by the arena.
func (a *RegisterArena) AllocByteBacking(n int) []byte {
	if n <= 0 {
		return emptyByteBacking
	}
	a.enforceSingleAllocLimit(n)
	if a.byteIndex+n > len(a.byteSlab) {
		a.GrowByteSlab(n)
	}
	index := a.byteIndex
	a.byteIndex += n
	a.noteAlloc(int64(n))
	return a.byteSlab[index : index+n : index+n]
}

// AllocIntBacking bump-allocates n int64 elements from the arena's intBacking slab in
// three-index form (len == cap == n). Returns a shared empty backing for n == 0.
//
// Takes n (int) which is the number of int64 elements (the slice's capacity).
//
// Returns an []int64 of length n and capacity n backed by the arena.
func (a *RegisterArena) AllocIntBacking(n int) []int64 {
	if n <= 0 {
		return emptyIntBacking
	}
	a.enforceSingleAllocLimit(n)
	if a.intBackingIndex+n > len(a.intBackingSlab) {
		a.GrowIntBackingSlab(n)
	}
	index := a.intBackingIndex
	a.intBackingIndex += n
	a.noteAlloc(int64(n) * 8)
	return a.intBackingSlab[index : index+n : index+n]
}

// AllocFloatBacking is the float64 sibling of AllocIntBacking.
//
// Takes n (int) which is the number of float64 elements.
//
// Returns an []float64 of length n and capacity n backed by the arena.
func (a *RegisterArena) AllocFloatBacking(n int) []float64 {
	if n <= 0 {
		return emptyFloatBacking
	}
	a.enforceSingleAllocLimit(n)
	if a.floatBackingIndex+n > len(a.floatBackingSlab) {
		a.growFloatBackingSlab(n)
	}
	index := a.floatBackingIndex
	a.floatBackingIndex += n
	a.noteAlloc(int64(n) * 8)
	return a.floatBackingSlab[index : index+n : index+n]
}

// AllocBytes bump-allocates aligned storage from the generic byte slab, requiring that
// the stored type contains no GC pointers (pointer-containing types need reflect.New) and
// returning a stable sentinel pointer for zero-size requests.
//
// Takes size (uintptr) which is the number of bytes to allocate.
// Takes align (uintptr) which is the required start alignment in bytes.
//
// Returns an unsafe.Pointer to the start of the allocated region.
func (a *RegisterArena) AllocBytes(size uintptr, align uintptr) unsafe.Pointer {
	if size == 0 {
		return zeroSizeAllocPtr
	}
	if align == 0 {
		align = 1
	}
	mask := int(align - 1)
	sizeAsInt := safeconv.Uint64ToInt(uint64(size))
	start := (a.genericBytesIndex + mask) &^ mask
	end := start + sizeAsInt
	if end > len(a.genericBytesSlab) {
		a.growGenericBytesSlab(sizeAsInt + mask)
		start = (a.genericBytesIndex + mask) &^ mask
		end = start + sizeAsInt
	}
	a.genericBytesIndex = end
	a.noteAlloc(safeconv.Uint64ToInt64(uint64(size)))
	return unsafe.Pointer(&a.genericBytesSlab[start])
}

// AllocStringBacking is the string sibling of AllocIntBacking. The returned slice is a
// real []string sub-slice, so writes through it go via Go's normal write barrier - the
// slab can safely hold pointers (string data pointers) without unsafe.Pointer trickery.
//
// Takes n (int) which is the number of string elements.
//
// Returns an []string of length n and capacity n backed by the arena.
func (a *RegisterArena) AllocStringBacking(n int) []string {
	if n <= 0 {
		return emptyStringBacking
	}
	a.enforceSingleAllocLimit(n)
	if a.stringBackingIndex+n > len(a.stringBackingSlab) {
		a.growStringBackingSlab(n)
	}
	index := a.stringBackingIndex
	a.stringBackingIndex += n
	a.noteAlloc(int64(n) * 16)
	return a.stringBackingSlab[index : index+n : index+n]
}

// AllocBoolBacking is the bool sibling of AllocIntBacking.
//
// Takes n (int) which is the number of bool elements.
//
// Returns an []bool of length n and capacity n backed by the arena.
func (a *RegisterArena) AllocBoolBacking(n int) []bool {
	if n <= 0 {
		return emptyBoolBacking
	}
	a.enforceSingleAllocLimit(n)
	if a.boolBackingIndex+n > len(a.boolBackingSlab) {
		a.growBoolBackingSlab(n)
	}
	index := a.boolBackingIndex
	a.boolBackingIndex += n
	a.noteAlloc(int64(n))
	return a.boolBackingSlab[index : index+n : index+n]
}

// AllocUintBacking is the uint64 sibling of AllocIntBacking.
//
// Takes n (int) which is the number of uint64 elements.
//
// Returns an []uint64 of length n and capacity n backed by the arena.
func (a *RegisterArena) AllocUintBacking(n int) []uint64 {
	if n <= 0 {
		return emptyUintBacking
	}
	a.enforceSingleAllocLimit(n)
	if a.uintBackingIndex+n > len(a.uintBackingSlab) {
		a.growUintBackingSlab(n)
	}
	index := a.uintBackingIndex
	a.uintBackingIndex += n
	a.noteAlloc(int64(n) * 8)
	return a.uintBackingSlab[index : index+n : index+n]
}

// allocStringBox bump-allocates one slot in stringBoxSlab, avoiding the per-call mallocgc
// that reflect.ValueOf(string) would incur. The slab stays GC-reachable as long as any
// reflect.Value holds a pointer into it.
//
// Takes s (string) which is the string value to box.
//
// Returns a pointer to the bump-allocated string slot.
func (a *RegisterArena) allocStringBox(s string) *string {
	if a.stringBoxIndex >= len(a.stringBoxSlab) {
		a.growStringBoxSlab()
	}
	slot := &a.stringBoxSlab[a.stringBoxIndex]
	a.stringBoxIndex++
	*slot = s
	a.noteAlloc(stringBoxBytes)
	return slot
}

// allocIntBox bump-allocates one int64 slot. See allocStringBox.
//
// Takes v (int64) which is the integer value to box.
//
// Returns a pointer to the bump-allocated int64 slot.
func (a *RegisterArena) allocIntBox(v int64) *int64 {
	if a.intBoxIndex >= len(a.intBoxSlab) {
		a.growIntBoxSlab()
	}
	slot := &a.intBoxSlab[a.intBoxIndex]
	a.intBoxIndex++
	*slot = v
	a.noteAlloc(int64BoxBytes)
	return slot
}

// allocFloatBox bump-allocates one float64 slot. See allocStringBox.
//
// Takes v (float64) which is the floating-point value to box.
//
// Returns a pointer to the bump-allocated float64 slot.
func (a *RegisterArena) allocFloatBox(v float64) *float64 {
	if a.floatBoxIndex >= len(a.floatBoxSlab) {
		a.growFloatBoxSlab()
	}
	slot := &a.floatBoxSlab[a.floatBoxIndex]
	a.floatBoxIndex++
	*slot = v
	a.noteAlloc(float64BoxBytes)
	return slot
}

// allocUintBox bump-allocates one uint64 slot. See allocStringBox.
//
// Takes v (uint64) which is the unsigned integer value to box.
//
// Returns a pointer to the bump-allocated uint64 slot.
func (a *RegisterArena) allocUintBox(v uint64) *uint64 {
	if a.uintBoxIndex >= len(a.uintBoxSlab) {
		a.growUintBoxSlab()
	}
	slot := &a.uintBoxSlab[a.uintBoxIndex]
	a.uintBoxIndex++
	*slot = v
	a.noteAlloc(uint64BoxBytes)
	return slot
}

// allocComplexBox bump-allocates one complex128 slot. Sibling of allocIntBox /
// allocFloatBox / allocUintBox; backs reflect.Value boxing for complex128 scalars to
// replace the per-call mallocgc that reflect.ValueOf(complex128) would incur.
//
// Takes v (complex128) which is the complex value to box.
//
// Returns a pointer to the bump-allocated complex128 slot.
func (a *RegisterArena) allocComplexBox(v complex128) *complex128 {
	if a.complexBoxIndex >= len(a.complexBoxSlab) {
		a.growComplexBoxSlab()
	}
	slot := &a.complexBoxSlab[a.complexBoxIndex]
	a.complexBoxIndex++
	*slot = v
	a.noteAlloc(complex128BoxBytes)
	return slot
}

// allocSliceHeader bump-allocates one zeroed slice-header slot.
//
// Returns a pointer to the bump-allocated arenaSliceHeader slot.
//
//revive:disable-next-line:unexported-return // package-internal type stays unexported.
func (a *RegisterArena) allocSliceHeader() *arenaSliceHeader {
	if a.sliceHeaderIndex >= len(a.sliceHeaderSlab) {
		a.growSliceHeaderSlab()
	}
	slot := &a.sliceHeaderSlab[a.sliceHeaderIndex]
	a.sliceHeaderIndex++
	*slot = arenaSliceHeader{Data: nil, Len: 0, Cap: 0}
	a.noteAlloc(arenaSliceHeaderBoxBytes)
	return slot
}
