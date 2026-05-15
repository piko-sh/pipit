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

	"pipit.sh/pipit/internal/engine/program"
)

// structLiteralEntryWords returns the runtime type word and the reflect.Value flag word
// the assembly handler writes for an arena-allocated struct of t, matching unsafeNewAt.
//
// Takes t (reflect.Type) which is the qualifying struct type.
//
// Returns the type word and the flag word.
func structLiteralEntryWords(t reflect.Type) (typeWord, flag uintptr) {
	return uintptr(reflectValueABIType(t)), uintptr(reflect.Struct) | flagAddr | flagIndir
}

// structLiteralTableAddress returns the address of the first table entry for publication
// to assembly.
//
// Takes entries ([]structLiteralEntry) which is the non-empty table.
//
// Returns the entry address.
func structLiteralTableAddress(entries []program.StructLiteralEntry) uintptr {
	return uintptr(unsafe.Pointer(&entries[0]))
}
