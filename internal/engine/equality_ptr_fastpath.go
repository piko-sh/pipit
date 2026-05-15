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

//go:build go1.27 && !safe && !(js && wasm)

package engine

import (
	"reflect"
	"unsafe"
)

// pointerIdentityEqual compares two same-typed values by pointer identity for kinds where
// Go's == is pointer identity (Pointer, Chan, UnsafePointer). Reads the pointer words
// through the value pun to avoid reflect.Value.Equal's heap allocation.
//
// Takes a (reflect.Value) which must be valid and of identical type to b.
// Takes b (reflect.Value) which is the second operand.
//
// Returns equal which is the comparison result.
// Returns handled which is true when the kind qualifies.
func pointerIdentityEqual(a, b reflect.Value) (equal bool, handled bool) {
	switch a.Kind() {
	case reflect.Pointer, reflect.Chan, reflect.UnsafePointer:
		aRaw := (*unsafeReflectValue)(unsafe.Pointer(&a))
		bRaw := (*unsafeReflectValue)(unsafe.Pointer(&b))
		return dereferenceIndirectPointer(aRaw) == dereferenceIndirectPointer(bRaw), true
	default:
	}
	return false, false
}
