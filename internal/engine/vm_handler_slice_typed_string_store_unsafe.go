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

import "reflect"

// setElementStringWithoutReflect is OpSetElementString's reflection-free route, with a
// string element written as Index(index).SetString of the barrier's string.
//
// Takes arena (*RegisterArena) which is the storing VM's arena.
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
// Takes value (string) which is the string to store.
//
// Returns bool which is false when the caller must take the reflect route.
func setElementStringWithoutReflect(arena *RegisterArena, collection reflect.Value, index int64, value string) bool {
	element, ok := typedElementAt(collection, index, true)
	if !ok || element.kind != reflect.String {
		return false
	}
	if element.backing != nil {
		*(*string)(element.address) = materialiseStringForSliceStore(arena, element.backing, value)
		return true
	}
	*(*string)(element.address) = materialiseString(arena, value)
	return true
}
