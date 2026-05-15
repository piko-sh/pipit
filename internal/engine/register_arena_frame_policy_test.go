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

package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
)

const deepFrameStack = 4096

func occupyFrame(arena *RegisterArena, index int) {
	arena.frameSlab[index].Function = &program.CompiledFunction{}
}

func requireFrameArraysMatch(t *testing.T, arena *RegisterArena, want int) {
	t.Helper()
	require.Len(t, arena.frameSlab, want)
	require.Len(t, arena.callInfoBasesSlab, want)
	require.Len(t, arena.dispatchSavesSlab, want)
}

func TestFrameStackPeakSeesFramesPushedWithoutTheCounter(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	arena.growFrameStack(deepFrameStack)
	occupyFrame(arena, 100)
	require.Zero(t, arena.framesUsed)

	require.Equal(t, 101, arena.frameStackPeak())
	arena.Reset()

	require.Equal(t, 101, arena.frameStackPeakLastReset)
	require.Nil(t, arena.frameSlab[100].Function, "a frame the counter missed is still cleared")
	require.Zero(t, arena.framesUsed)
}

func TestResetKeepsFrameStackWhileHot(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	arena.growFrameStack(deepFrameStack)
	requireFrameArraysMatch(t, arena, deepFrameStack)

	occupyFrame(arena, deepFrameStack-100)
	arena.Reset()
	requireFrameArraysMatch(t, arena, deepFrameStack)

	for range backingSlabIdleResetsBeforeShrink - 1 {
		arena.Reset()
		requireFrameArraysMatch(t, arena, deepFrameStack)
	}
	arena.Reset()
	requireFrameArraysMatch(t, arena, initialFrameSlabs)
}

func TestObservedCallDepthRaisesFrameStackPreSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		observed     int
		maxCallDepth int
		want         int
	}{
		{name: "below the static cap", observed: 3000, maxCallDepth: 0, want: 3000 + frameStackSafetyMargin},
		{name: "above the static cap is honoured in full", observed: 50000, maxCallDepth: 200000, want: 50000 + frameStackSafetyMargin},
		{name: "above the call-depth limit is capped at the limit", observed: 50000, maxCallDepth: 20000, want: 20000 + frameStackSafetyMargin},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := &program.CompiledFunction{Name: "root"}
			vm := newTestVM(t)
			vm.Limits.MaxCallDepth = tt.maxCallDepth
			vm.Arena = NewRegisterArena()
			vm.sizeArenaForEntry(root, root)
			require.Less(t, vm.Arena.frameStackCapacity(), tt.want)
			root.RecordObservedCallDepth(tt.observed)
			vm.sizeArenaForEntry(root, root)
			require.Equal(t, tt.want, vm.Arena.frameStackCapacity())
		})
	}
}

func TestGrowCallStackJumpsToObservedDepth(t *testing.T) {
	t.Parallel()
	root := &program.CompiledFunction{Name: "root"}
	root.RecordObservedCallDepth(5000)
	vm := newTestVM(t)
	vm.rootFunction = root
	vm.Arena = NewRegisterArena()
	vm.CallStack = vm.Arena.frameStack()
	require.Equal(t, initialFrameSlabs, len(vm.CallStack))
	vm.growCallStack()
	require.Equal(t, 5000+frameStackSafetyMargin, len(vm.CallStack))
	require.Equal(t, 1, vm.Arena.frameStackGrowths)
}

func TestReleaseRecordsObservedCallDepth(t *testing.T) {
	t.Parallel()
	root := &program.CompiledFunction{Name: "root"}
	vm := newTestVM(t)
	vm.rootFunction = root
	arena := NewRegisterArena()
	arena.growFrameStack(deepFrameStack)
	occupyFrame(arena, 2500)

	vm.releaseArenaUnlessLeaked(arena)

	require.Equal(t, 2501, root.ObservedCallDepth())
}
