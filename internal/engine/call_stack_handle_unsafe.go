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
	"runtime"
	"unsafe"
)

// funcHandle presents a registry entry as the *runtime.Func a script's static types
// expect. The pointer is never dereferenced as a runtime.Func: every method call on it is
// intercepted and answered by the entry, and the entry stays reachable through the
// registry for the life of the program.
//
// Takes entry (*pipitFunc) which is the registry entry, nil for no function.
//
// Returns *runtime.Func which is the handle, nil for a nil entry.
func funcHandle(entry *pipitFunc) *runtime.Func {
	if entry == nil {
		return nil
	}
	return (*runtime.Func)(unsafe.Pointer(entry))
}

// handleKey returns the registry key a handle to entry decodes with.
//
// Takes entry (*pipitFunc) which is the registry entry.
//
// Returns uintptr which is the handle's address.
func handleKey(entry *pipitFunc) uintptr {
	return uintptr(unsafe.Pointer(entry))
}

// handleAddress returns the registry key of a *runtime.Func value a script holds.
//
// Takes value (reflect.Value) which holds the handle.
//
// Returns uintptr which is the key, 0 for a nil handle.
func handleAddress(value reflect.Value) uintptr {
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() {
		return 0
	}
	return value.Pointer()
}
