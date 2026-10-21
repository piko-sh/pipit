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

// concreteMethodValue reports no method value split, so the safe and js/wasm builds call
// method values as they are.
//
// Takes function (reflect.Value) which is ignored in this build.
//
// Returns receiver (reflect.Value) which is always the zero Value.
// Returns method (reflect.Value) which is always the zero Value.
// Returns ok (bool) which is always false.
func concreteMethodValue(reflect.Value) (receiver, method reflect.Value, ok bool) {
	return reflect.Value{}, reflect.Value{}, false
}
