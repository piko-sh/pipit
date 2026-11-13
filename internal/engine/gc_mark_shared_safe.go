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

//go:build safe || (js && wasm)

package engine

import "reflect"

// markOwnsPrivately reports false. This build cannot tell the arena's own storage by
// address, so a shared mark reads no referent and classifies by address alone.
//
// Returns bool which is false.
func markOwnsPrivately(_ *RegisterArena, _ uintptr) bool {
	return false
}

// reflectValueIndirect reports whether value may be held in the memory its pointer names
// rather than in the reflect.Value itself. Without the value's flags this build counts
// every value of a kind reflect holds indirectly, and an addressable value of any kind.
//
// Takes value (reflect.Value) which is a valid value.
//
// Returns bool which is true unless value is a non-addressable pointer-shaped value.
func reflectValueIndirect(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return value.CanAddr()
	default:
		return true
	}
}
