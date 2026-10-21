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

// byteViewFor decides a CONVERT_STRING_TO_BYTES conversion for a reads-only native.
//
// This build cannot make a []byte over a string's own bytes, so every conversion copies
// as Go does.
//
// Returns reflect.Value which is the zero Value.
// Returns bool which is false.
func byteViewFor(_ *VM, _ *CallFrame, _ *Registers, _ uint8, _ string) (reflect.Value, bool) {
	return reflect.Value{}, false
}
