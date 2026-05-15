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

// ensureSliceAddressableForHeader promotes a non-addressable slice into a heap-backed
// holder so the safe-build ReflectValuePtr can return a valid header pointer. The
// unsafe-build sibling is a no-op.
//
// Takes v (reflect.Value) which is the slice value to promote.
//
// Returns reflect.Value which is the addressable equivalent, or v unchanged when already
// addressable.
func ensureSliceAddressableForHeader(v reflect.Value) reflect.Value {
	if v.CanAddr() {
		return v
	}
	holder := reflect.New(v.Type()).Elem()
	holder.Set(v)
	return holder
}
