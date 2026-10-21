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

	"pipit.sh/pipit/internal/engine/program"
)

// krefChunk is one KRef chunk; this build makes none.
type krefChunk struct {
	// base and end bound the chunk.
	base, end uintptr
}

// arenaKRef is an arena's KRef chunk class. This build places nothing in KRef chunks, so
// an arena never holds one.
type arenaKRef struct {
	// sorted holds every chunk by base address; always empty in this build.
	sorted []*krefChunk

	// used is true once the epoch took a slot; never in this build.
	used bool
}

// krefFind finds no chunk in this build, which makes none.
//
// Returns *krefChunk which is nil.
// Returns bool which is false.
func (*RegisterArena) krefFind(_ uintptr) (*krefChunk, bool) {
	return nil, false
}

// krefDropAll has no chunk to leave to the collector in this build.
func (*RegisterArena) krefDropAll() {}

// krefReset has no chunk to clear in this build.
func (*RegisterArena) krefReset() {}

// krefMakeBacking never places a backing in this build.
//
// Returns reflect.Value which is the zero Value.
// Returns bool which is false.
func krefMakeBacking(_ *VM, _ reflect.Type, _, _ int) (reflect.Value, bool) {
	return reflect.Value{}, false
}

// krefPointee never places a pointee in this build.
//
// Returns reflect.Value which is the zero Value.
// Returns bool which is false.
func krefPointee(_ *VM, _ *program.CompiledFunction, _ int, _ reflect.Type, _ bool) (reflect.Value, bool) {
	return reflect.Value{}, false
}

// krefSnapshot never places a boundary snapshot in this build.
//
// Returns reflect.Value which is the zero Value.
// Returns bool which is false.
func (*VM) krefSnapshot(_ reflect.Type) (reflect.Value, bool) {
	return reflect.Value{}, false
}

// settlePointeePlacements has no placement to settle in this build.
func (*RegisterArena) settlePointeePlacements() {}

// settleSnapshotPlacements has no placement to settle in this build.
func (*RegisterArena) settleSnapshotPlacements() {}
