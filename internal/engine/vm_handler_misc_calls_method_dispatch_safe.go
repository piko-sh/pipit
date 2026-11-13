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

//go:build !go1.27 || safe || (js && wasm)

package engine

import "reflect"

// receiverDispatchTypeWord returns the type word the method inline cache keys on, through
// reflect alone. The type word is the address of the receiver type's runtime descriptor,
// which reflect.Value.Pointer gives for the *rtype a reflect.Type holds.
//
// A pointer receiver is keyed on its element type, so *T and T reach the same cache
// entry. The method set the call site resolved against is the same either way.
//
// Takes receiver (*reflect.Value) which is the receiver register's value.
//
// Returns the type word, or zero when the receiver carries no type.
func receiverDispatchTypeWord(receiver *reflect.Value) uintptr {
	if !receiver.IsValid() {
		return 0
	}
	receiverType := receiver.Type()
	if receiverType.Kind() == reflect.Pointer {
		receiverType = receiverType.Elem()
	}
	return reflect.ValueOf(receiverType).Pointer()
}
