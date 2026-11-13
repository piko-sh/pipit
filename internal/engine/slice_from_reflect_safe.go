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

// aliasSliceElements returns a []T over the backing array of value, which the caller has
// checked is a non-nil slice whose elements a T bank stores in place.
//
// A slice type convertible to []T, such as a named slice over []T, is converted, which
// shares the backing array. Any other value (a read-only field, or elements of another
// type with T's width and scalar class, such as int for int64) is viewed through
// reflect.SliceAt, the reflect package's public form of the same view, so this build
// imports no unsafe code.
//
// Takes value (reflect.Value) which is the slice. It may be read-only.
//
// Returns []T which shares value's elements, length and capacity.
// Returns bool which is false if reflect hands back a value of another type, which the
// caller's checks rule out.
func aliasSliceElements[T any](value reflect.Value) ([]T, bool) {
	sliceType := reflect.TypeFor[[]T]()
	if value.CanInterface() && value.Type().ConvertibleTo(sliceType) {
		return reflect.TypeAssert[[]T](value.Convert(sliceType))
	}
	view, ok := reflect.TypeAssert[[]T](reflect.SliceAt(reflect.TypeFor[T](), value.UnsafePointer(), value.Cap()))
	if !ok {
		return nil, false
	}
	return view[:value.Len():value.Cap()], true
}
