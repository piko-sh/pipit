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

// licenceSlabSpan is licenceSlabBase that also returns the slab's end by capacity.
//
// Takes slab ([]T) which is the slab.
// Takes address (uintptr) which is the address.
//
// Returns start (uintptr) which is the holding span's first address.
// Returns end (uintptr) which is one past its last byte by capacity.
// Returns found (bool) which is false when no span holds address.
func licenceSlabSpan[T any](slab []T, address uintptr) (start, end uintptr, found bool) {
	if cap(slab) == 0 {
		return 0, 0, false
	}
	start = licenceSlabAddress(slab)
	end = start + uintptr(cap(slab))*reflect.TypeFor[T]().Size()
	return start, end, address >= start && address < end
}
