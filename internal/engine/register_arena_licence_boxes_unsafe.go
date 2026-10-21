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
	"unsafe"
)

// readOnlyBox returns v without flagAddr, so reflect passes v's own storage, the arena
// box, as an interface's data word instead of copying it to the heap (packEface copies
// only an addressable value). v must be indirect.
//
// Takes v (reflect.Value) which is an addressable, indirect box value.
//
// Returns reflect.Value which is v, read-only.
func readOnlyBox(v reflect.Value) reflect.Value {
	header := (*unsafeReflectValue)(unsafe.Pointer(&v))
	header.flag &^= flagAddr
	return v
}

// boxIndirect reports whether v's storage is a pointer to the value (flagIndir).
//
// Only such a value can share its storage with an interface.
//
// Takes v (reflect.Value) which is the value.
//
// Returns bool which is true for an indirect value.
func boxIndirect(v reflect.Value) bool {
	return (*unsafeReflectValue)(unsafe.Pointer(&v)).flag&flagIndir != 0
}
