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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
)

func TestHeapDataArenaMarkedAtAKeepingNativeIsDropped(t *testing.T) {
	if !arenaUsesUnsafeSlabs {
		t.Skip("the marks are set only where an arena reuses its storage without a licence (reusedWithoutLicence)")
	}
	cases := []struct {
		name     string
		argument func(*RegisterArena) reflect.Value
		marked   func(*RegisterArena) bool
	}{
		{"pointer into a string box", func(a *RegisterArena) reflect.Value {
			return reflect.ValueOf(&struct{ S *string }{S: a.allocStringBox("kept by the host")})
		}, func(a *RegisterArena) bool { return a.unpooled }},
		{"closure over a cell in the arena", func(a *RegisterArena) reflect.Value {
			cells := a.allocUpvalueCells(1)
			return reflect.ValueOf(&RuntimeClosure{upvalues: []*program.UpvalueCell{&cells[0]}})
		}, func(a *RegisterArena) bool { return a.abandonAtRelease }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			drainHeapDataArenaPool()
			arena := acquireHeapDataArena(false)
			abandonIfArgumentReferencesArena(arena, testCase.argument(arena))
			require.True(t, testCase.marked(arena), "test harness: the argument reaches the arena's storage")
			releaseRegisterArena(false, arena, false)
			require.False(t, slices.Contains(drainHeapDataArenaPool(), arena), "a marked heap-data arena is dropped, not pooled")
		})
	}
}

func TestAHeapDataArenaRecordsNothingInEitherRegistry(t *testing.T) {
	vm := newTestVM(t)
	vm.EnsureVarInitCallStack()
	arena := vm.Arena
	require.True(t, arena.heapData, "test harness: a variable initialiser runs on a heap-data arena")
	arenaString(arena, "a package-level string")
	arena.AllocIntBacking(64)
	arena.allocStringBox("boxed")
	arena.allocUpvalueCells(1)
	require.False(t, arena.licensed, "a heap-data arena is never licensed, so it publishes nothing to the licence registry")
	require.Zero(t, arena.slabOwner, "nor does it record a byte slab, so it has no registry identity")
	vm.ReleaseArena()
	require.Zero(t, arena.slabOwner, "the heap-data pool's sync.Pool may drop it with nothing registered")
}
