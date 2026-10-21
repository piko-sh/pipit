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

//go:build !pipit_store_census && !safe && !(js && wasm)

package engine

import (
	"reflect"
	"unsafe"
)

// censusNoteServedStore checks a served store for the census; a no-op here.
func censusNoteServedStore(_ *RegisterArena, _ *RegisterArena, _ unsafe.Pointer, _ string) {}

// censusNoteFusedCopy checks a fused field copy for the census; a no-op here.
func censusNoteFusedCopy(_ *RegisterArena, _ unsafe.Pointer, _ reflect.Type, _ unsafe.Pointer) {
}

// censusNoteMemoSettled checks a container the walk memo settled; a no-op here.
func censusNoteMemoSettled(_ *RegisterArena, _ reflect.Value) {}
