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

// runtimeMapassignFast64 is the safe-build stub for the linkname trampoline, panicking
// unconditionally with errMapLinknameSafeStubInvoked.
//
// Returns unsafe.Pointer which is never reached because the function always panics.
//
// Panics unconditionally with errMapLinknameSafeStubInvoked to signal a build-tag
// misconfiguration.
func runtimeMapassignFast64(_, _ unsafe.Pointer, _ uint64) unsafe.Pointer {
	panic(errMapLinknameSafeStubInvoked)
}

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

// mapSetStringIntProbeFirst returns false so callers materialise the key and store via
// reflect.
//
// Returns false unconditionally on the safe build.
func mapSetStringIntProbeFirst(_ *VM, _ reflect.Value, _ string, _ int64) bool {
	return false
}

// mapAddStringIntProbeFirst returns false so callers materialise the key and store via
// reflect.
//
// Returns false unconditionally on the safe build.
func mapAddStringIntProbeFirst(_ *VM, _ reflect.Value, _ string, _ int64) bool {
	return false
}

// mapSetStringStringProbeFirst returns false so callers materialise the key and store via
// reflect.
//
// Returns false unconditionally on the safe build.
func mapSetStringStringProbeFirst(_ *VM, _ reflect.Value, _, _ string) bool {
	return false
}

// mapSetStringGeneralProbeFirst returns false so callers materialise the key and store
// via reflect.
//
// Returns false unconditionally on the safe build.
func mapSetStringGeneralProbeFirst(_ *VM, _ reflect.Value, _ string, _ reflect.Value) bool {
	return false
}
