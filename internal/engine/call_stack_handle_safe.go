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
	"reflect"
	"runtime"
)

// funcHandle has no pointer reinterpretation to offer in the safe build, so
// runtime.FuncForPC and runtime.Frame.Func report nil there; the *runtime.Func methods
// answer as they do for a nil function in Go.
//
// Takes entry (*pipitFunc) which is the registry entry.
//
// Returns *runtime.Func which is always nil.
func funcHandle(_ *pipitFunc) *runtime.Func {
	return nil
}

// handleKey returns the registry key of an entry; the safe build hands out no handles, so
// no key ever matches.
//
// Takes entry (*pipitFunc) which is the registry entry.
//
// Returns uintptr which is always 0.
func handleKey(_ *pipitFunc) uintptr {
	return 0
}

// handleAddress returns the registry key of a *runtime.Func value; always 0 in the safe
// build.
//
// Takes value (reflect.Value) which holds the handle.
//
// Returns uintptr which is always 0.
func handleAddress(_ reflect.Value) uintptr {
	return 0
}
