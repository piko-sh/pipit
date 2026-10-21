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

import (
	"reflect"
	"sync/atomic"
)

// licenceNoteKeys numbers the note keys this build hands out.
var licenceNoteKeys atomic.Uintptr

// licenceNoteKeyOf returns a key no other note has. This build cannot read a
// reflect.Value's words, so a second note never matches the first and is recorded as a
// hand-off of its own.
//
// Takes boxed (bool) which is the note's boxed argument.
//
// Returns licenceNoteKey which is the key.
func licenceNoteKeyOf(_ reflect.Value, boxed bool) licenceNoteKey {
	return licenceNoteKey{typ: 0, ptr: 0, flag: licenceNoteKeys.Add(1), boxed: boxed}
}

// interfaceSharesArenaStorage reports whether packing value into an interface may leave
// the interface's data word pointing into the arena.
//
// This build cannot read where a value that is not addressable is stored, so every such
// value that is not pointer-shaped counts as sharing.
//
// Takes value (reflect.Value) which is the candidate.
//
// Returns bool which is true when the interface may share arena storage.
func (*RegisterArena) interfaceSharesArenaStorage(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Invalid, reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer, reflect.Interface:
		return false
	default:
		return !value.CanAddr()
	}
}
