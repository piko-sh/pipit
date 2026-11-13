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
	"errors"
	"reflect"
	"unsafe"
)

// errMapLinknameSafeStubInvoked is the panic value every stub in this file raises,
// indicating a build-tag misconfiguration. Callers must gate fast-path use behind
// useMapFastLinkname.
var errMapLinknameSafeStubInvoked = errors.New(
	"safe-build linkname stub invoked: caller must gate via useMapFastLinkname")

// runtimeTypedmemmove is the safe-build stub for the linkname trampoline. It panics
// unconditionally with errMapLinknameSafeStubInvoked.
func runtimeTypedmemmove(_, _, _ unsafe.Pointer) {
	panic(errMapLinknameSafeStubInvoked)
}

// mapAccessFast64ToGeneral returns (nil, false) so callers fall back to the reflect path.
//
// Returns nil and false unconditionally on the safe build.
func mapAccessFast64ToGeneral(_ reflect.Value, _ int64) (unsafe.Pointer, bool) {
	return nil, false
}

// mapAccessFastStrToGeneral returns (nil, false) so callers fall back to the reflect
// path.
//
// Returns nil and false unconditionally on the safe build.
func mapAccessFastStrToGeneral(_ reflect.Value, _ string) (unsafe.Pointer, bool) {
	return nil, false
}

// useMapFastLinkname reports whether the linkname-backed map fast paths are available.
// Safe / wasm builds return false so callers take the reflect fallback rather than
// reaching the panicking stubs.
//
// Returns false on safe / wasm builds; the unsafe build returns true.
func useMapFastLinkname() bool {
	return false
}

// setMapStringInt64ProbeFirst returns false so callers materialise the key and store via
// reflect.
//
// Returns bool which is always false on the safe build.
func setMapStringInt64ProbeFirst(_ *VM, _ reflect.Value, _ string, _ int64) bool {
	return false
}

// addToMapStringInt64ProbeFirst returns false so callers materialise the key and store
// via reflect.
//
// Returns bool which is always false on the safe build.
func addToMapStringInt64ProbeFirst(_ *VM, _ reflect.Value, _ string, _ int64) bool {
	return false
}

// setMapStringStringProbeFirst returns false so callers materialise the key and store via
// reflect.
//
// Returns bool which is always false on the safe build.
func setMapStringStringProbeFirst(_ *VM, _ reflect.Value, _, _ string) bool {
	return false
}

// setMapStringGeneralProbeFirst returns false so callers materialise the key and store
// via reflect.
//
// Returns bool which is always false on the safe build.
func setMapStringGeneralProbeFirst(_ *VM, _ reflect.Value, _ mapAccessCacheEntry, _ string, _ reflect.Value) bool {
	return false
}

// setMapInt64BoxedFast reports false so callers take the reflect path.
//
// Returns bool which is always false on the safe build.
func setMapInt64BoxedFast(_ reflect.Value, _ reflect.Type, _ int64, _ reflect.Value) bool {
	return false
}

// mapSetFast64PointerKeyed reports false so callers take the reflect path.
//
// Takes vm (*VM) which owns the arena.
// Takes m (reflect.Value) which is the destination map.
// Takes elemType (reflect.Type) which is the map's element type.
// Takes key (int64) which is the key value.
// Takes rawValue (reflect.Value) which is the value to store.
//
// Returns bool which is always false on the safe build.
func mapSetFast64PointerKeyed(*VM, reflect.Value, reflect.Type, int64, reflect.Value) bool {
	return false
}

// appendStringInLocalMapSlot returns false so callers run the lookup, the append and the
// store as separate steps.
//
// Returns bool which is always false on the safe build.
func appendStringInLocalMapSlot(_ *VM, _ *CallFrame, _ reflect.Value, _, _ string) bool {
	return false
}
