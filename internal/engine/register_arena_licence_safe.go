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

const (
	// licenceExactProbes is false in the safe and js/wasm builds, where some ownership
	// probes are constants; every revoking hand-off there revokes the whole epoch.
	licenceExactProbes = false
)

// arenaLicenceExactState is the part of arenaLicenceState that only builds with exact
// probes use. This build has no walk and publishes no slab to the licence registry, so it
// keeps only the walk memo, which records nothing here.
type arenaLicenceExactState struct {
	// memo is the walk memo, empty in this build.
	memo licenceWalkMemo
}

// licenceReadable returns v in this build, which never walks.
//
// Takes v (reflect.Value) which is the value.
//
// Returns reflect.Value which is v.
func licenceReadable(v reflect.Value) reflect.Value { return v }

// licenceKeepReadOnly returns copied unchanged in this build.
//
// Takes copied (reflect.Value) which is the copy.
//
// Returns reflect.Value which is copied.
func licenceKeepReadOnly(copied, _ reflect.Value) reflect.Value { return copied }

// reflectValueHeld returns the reflect.Value stored in value, a value of type
// reflect.Value, so walks follow what it holds rather than its internal fields.
//
// Takes value (reflect.Value) which holds a reflect.Value.
//
// Returns reflect.Value which is the held value, or the zero Value when it cannot be
// read.
func reflectValueHeld(value reflect.Value) reflect.Value {
	if value.CanInterface() {
		held, _ := reflect.TypeAssert[reflect.Value](value)
		return held
	}
	if value.Kind() == reflect.Struct && value.CanAddr() {
		held, _ := reflect.TypeAssert[reflect.Value](reflect.NewAt(value.Type(), value.Addr().UnsafePointer()).Elem())
		return held
	}
	return reflect.Value{}
}

// licenceSlabAddress returns the address of a slab's first element, read through reflect.
//
// Takes slab ([]T) which is the slab; its capacity is not zero.
//
// Returns uintptr which is the address.
func licenceSlabAddress[T any](slab []T) uintptr {
	return reflect.ValueOf(&slab[:1][0]).Pointer()
}
