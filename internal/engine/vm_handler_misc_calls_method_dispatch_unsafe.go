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

// receiverDispatchTypeWord returns the type word the method inline cache keys on.
//
// A pointer receiver is keyed on its element type, so *T and T reach the same cache
// entry. The method set the call site resolved against is the same either way.
//
// Takes receiver (*reflect.Value) which is the receiver register's value, taken by
// pointer so the raw header can be read without copying it.
//
// Returns the type word, or zero when the receiver carries no type.
func receiverDispatchTypeWord(receiver *reflect.Value) uintptr {
	raw := (*unsafeReflectValue)(unsafe.Pointer(receiver))
	typeWord := uintptr(raw.typ)
	if typeWord == 0 {
		return 0
	}
	if reflect.Kind(*(*uint8)(unsafe.Add(raw.typ, program.AbiTypeKindByteOffset))&uint8(flagKindMask)) == reflect.Pointer {
		typeWord = *(*uintptr)(unsafe.Add(raw.typ, program.AbiTypePointerElemOffset))
	}
	return typeWord
}
