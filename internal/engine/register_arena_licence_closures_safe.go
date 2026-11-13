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
)

// noteSpawnedClosureObject names nothing in this build, whose probes cannot tell whether
// a closure lies in an arena chunk; this build places none there.
func noteSpawnedClosureObject(_ *RegisterArena, _ *RuntimeClosure) {}

// registerClosureWrapper records nothing in this build, which has no walk to find a
// wrapper's closure for.
func registerClosureWrapper(_ *VM, _ reflect.Value, _ *RuntimeClosure) {}

// registerBoundMethodWrapper records nothing in this build, which has no walk to find a
// wrapper's bound receiver for.
func registerBoundMethodWrapper(_ *VM, _ reflect.Value, _ *wrapperBoundReceiver) {}

// closureChunkBase returns the start of a chunk list's current chunk.
//
// Takes list ([][]T) which is the list.
//
// Returns uintptr which is the start, or zero.
func closureChunkBase[T any](list [][]T) uintptr {
	if len(list) == 0 || len(list[len(list)-1]) == 0 {
		return 0
	}
	return reflect.ValueOf(&list[len(list)-1][0]).Pointer()
}
