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

	"pipit.sh/pipit/internal/safeconv"
)

// arenaOwnedSliceHeader returns the arena-owned header a slice value views, or nil when
// the value is not a slice or its header lives outside the arena.
//
// Takes arena (*RegisterArena) which owns the header slabs; nil yields nil.
// Takes sliceValue (reflect.Value) which is the candidate slice.
//
// Returns the header pointer when the arena owns it, nil otherwise.
func arenaOwnedSliceHeader(arena *RegisterArena, sliceValue reflect.Value) *arenaSliceHeader {
	if arena == nil || !sliceValue.IsValid() || sliceValue.Kind() != reflect.Slice {
		return nil
	}
	pointer := ReflectValuePtr(sliceValue)
	if pointer == nil || !arena.OwnsSliceHeaderPointer(pointer) {
		return nil
	}
	return (*arenaSliceHeader)(pointer)
}

// inPlaceHeaderReuseAuthorised reports whether the in-place append executing at the
// frame's current instruction may bump its arena-owned header directly.
//
// Takes frame (*CallFrame) which identifies the executing instruction; the program
// counter has already advanced past the instruction word.
// Takes header (*arenaSliceHeader) which is the arena-owned header being appended to.
//
// Returns true when the escape pass annotated the site alias-free, the header has spare
// capacity, and the build uses raw arena slabs.
func inPlaceHeaderReuseAuthorised(frame *CallFrame, header *arenaSliceHeader) bool {
	if !arenaUsesUnsafeSlabs || header.Len >= header.Cap {
		return false
	}
	return frame.Function.InPlaceHeaderReusePCs[frame.ProgramCounter-1]
}

// tryReuseInPlaceHeader appends element by writing it into the header's spare capacity
// and bumping the length, without allocating a replacement header.
//
// Only pointer-free element types take this path: pointer-free structs and arrays are
// copied byte for byte, scalars are stored through reflect on the extended slice, and
// everything else (strings, pointers, interfaces) keeps the allocating path so the escape
// barrier and write barriers apply.
//
// Takes frame (*CallFrame) which identifies the executing instruction.
// Takes header (*arenaSliceHeader) which is the arena-owned header to extend.
// Takes sliceValue (reflect.Value) which views the header.
// Takes element (reflect.Value) which is the value to append.
//
// Returns true when the append completed in place.
func tryReuseInPlaceHeader(frame *CallFrame, header *arenaSliceHeader, sliceValue, element reflect.Value) bool {
	if !inPlaceHeaderReuseAuthorised(frame, header) {
		return false
	}
	elementType := sliceValue.Type().Elem()
	if element.Type() != elementType || !typeIsPointerFree(elementType) {
		return false
	}
	kind := elementType.Kind()
	if (kind == reflect.Struct || kind == reflect.Array) && element.CanAddr() {
		size := elementType.Size()
		destination := unsafe.Slice((*byte)(unsafe.Add(header.Data, size*safeconv.IntToUintptr(header.Len))), size)
		source := unsafe.Slice((*byte)(ReflectValuePtr(element)), size)
		copy(destination, source)
		header.Len++
		return true
	}
	header.Len++
	sliceValue.Index(header.Len - 1).Set(element)
	return true
}

// tryReuseInPlaceByteHeader is the []byte twin of tryReuseInPlaceHeader.
//
// Takes frame (*CallFrame) which identifies the executing instruction.
// Takes header (*arenaSliceHeader) which is the arena-owned header to extend.
// Takes value (byte) which is the byte to append.
//
// Returns true when the append completed in place.
func tryReuseInPlaceByteHeader(frame *CallFrame, header *arenaSliceHeader, value byte) bool {
	if !inPlaceHeaderReuseAuthorised(frame, header) {
		return false
	}
	unsafe.Slice((*byte)(header.Data), header.Cap)[header.Len] = value
	header.Len++
	return true
}
