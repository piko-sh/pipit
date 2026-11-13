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

import "reflect"

// zeroValueBufferBytes is the size of reflect's zero-value buffer that
// findReflectZeroValues checks for.
const zeroValueBufferBytes = 1024

// reflectZeroValues is the extent of the read-only buffer reflect.Zero hands out for the
// zero value of a small type, which no code writes.
var reflectZeroValues = findReflectZeroValues()

// zeroValueSpan is a range of addresses.
type zeroValueSpan struct {
	// start is the first address.
	start uintptr

	// end is one past the last address.
	end uintptr
}

// holds reports whether address lies in the span.
//
// Takes address (uintptr) which is the address.
//
// Returns bool which is true inside the span.
func (span zeroValueSpan) holds(address uintptr) bool {
	return address >= span.start && address < span.end
}

// markOwnsPrivately reports whether a shared mark may read the memory at address. It may
// read the arena's own uncovered data storage, KRef chunks included, which only the
// arena's VM writes (ownUncoveredDataStorage), and reflect's zero-value buffer, which
// nothing writes.
//
// Takes arena (*RegisterArena) which is the collected arena.
// Takes address (uintptr) which is the memory a shared mark is about to read.
//
// Returns bool which is true when no other goroutine can be writing the memory.
func markOwnsPrivately(arena *RegisterArena, address uintptr) bool {
	return reflectZeroValues.holds(address) || ownUncoveredDataAddress(arena, address)
}

// findReflectZeroValues locates reflect's zero-value buffer from the values reflect.Zero
// returns (zeroValueSpanFrom).
//
// Returns zeroValueSpan which is the buffer, or an empty span.
func findReflectZeroValues() zeroValueSpan {
	return zeroValueSpanFrom(
		uintptr(ReflectValuePtr(reflect.Zero(reflect.TypeFor[[1]byte]()))),
		uintptr(ReflectValuePtr(reflect.Zero(reflect.TypeFor[[zeroValueBufferBytes]byte]()))),
	)
}

// zeroValueSpanFrom returns the span of reflect's zero-value buffer from the addresses of
// the zero values of a one-byte and a zeroValueBufferBytes-byte type.
//
// Takes small (uintptr) which is the one-byte type's zero value address.
// Takes large (uintptr) which is the zeroValueBufferBytes-byte type's zero value address.
//
// Returns zeroValueSpan which is the buffer, or an empty span.
func zeroValueSpanFrom(small, large uintptr) zeroValueSpan {
	if small == 0 || large != small {
		return zeroValueSpan{start: 0, end: 0}
	}
	return zeroValueSpan{start: small, end: small + zeroValueBufferBytes}
}

// reflectValueIndirect reports whether value is held in the memory its pointer names
// rather than in the reflect.Value itself.
//
// Takes value (reflect.Value) which is a valid value.
//
// Returns bool which is true for an indirect value.
func reflectValueIndirect(value reflect.Value) bool {
	return boxIndirect(value)
}
