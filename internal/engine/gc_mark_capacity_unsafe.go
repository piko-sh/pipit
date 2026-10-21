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

// capacityElements returns what the licensed mark's walk of a slice up to its capacity
// reads its elements from (capacityElement), which in this build is the slice itself.
//
// Takes value (reflect.Value) which is a slice with non-zero capacity.
//
// Returns reflect.Value which is value.
func capacityElements(value reflect.Value) reflect.Value {
	return value
}

// capacityElement returns element index of a slice's backing, which may lie past its
// length but not past its capacity.
//
// Takes elements (reflect.Value) which is capacityElements' result.
// Takes index (int) which is below the slice's capacity.
//
// Returns reflect.Value which is the element, addressable.
func capacityElement(elements reflect.Value, index int) reflect.Value {
	elementType := elements.Type().Elem()
	return reflect.NewAt(elementType, unsafe.Add(elements.UnsafePointer(), uintptr(index)*elementType.Size())).Elem()
}
