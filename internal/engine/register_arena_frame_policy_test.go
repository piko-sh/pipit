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
	"pipit.sh/pipit/internal/fault"
)

const deepFrameStack = 4096

func occupyFrame(arena *RegisterArena, index int) {
	function := &program.CompiledFunction{}
	for slot := arena.framesUsed; slot <= index; slot++ {
		arena.frameSlab[slot].Function = function
	}
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
	require.Len(t, vm.CallStack, initialFrameSlabs)
	vm.growCallStack()
	require.Len(t, vm.CallStack, 5000+frameStackSafetyMargin)
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

func panicValueOf(body func()) (value any) {
	defer func() { value = recover() }()
	body()
	return nil
}

func requireInvariantPanic(t *testing.T, body func()) {
	t.Helper()
	value := panicValueOf(body)
	require.NotNil(t, value, "the check panics")
	err, isError := value.(error)
	require.True(t, isError, "the panic value %v is an error", value)
	require.ErrorIs(t, err, fault.ErrInterpreterInvariant)
}

func TestParanoidFrameStackGapIsAnInvariantFailure(t *testing.T) {
	if !arenaParanoidEnabled {
		t.Skip("the frame stack is checked in the paranoid build")
	}
	arena := &RegisterArena{frameSlab: make([]CallFrame, 4)}
	arena.frameSlab[2].Function = program.NewNamedFunction("orphan")

	requireInvariantPanic(t, func() { arena.frameStackPeak() })
}

func enterFrames(arena *RegisterArena, depth int, entry arenaIndices) {
	occupyFrame(arena, depth-1)
	arena.frameSlab[depth-1].arenaSave.arenaIndices = entry
}

func TestResetCountsFrameSavePointsAsScalarBankDemand(t *testing.T) {
	arena := NewRegisterArena()
	overgrown := initialIntSlabs*maxArenaMultiplier + 1
	arena.ensureScalarBankCapacity(program.TypedSlabCounts{Ints: overgrown})
	require.Len(t, arena.intSlab, overgrown)

	for range backingSlabIdleResetsBeforeShrink * 2 {
		enterFrames(arena, 3, arenaIndices{IntIndex: overgrown / 2})
		arena.Reset()
		require.Len(t, arena.intSlab, overgrown, "a run whose frames reached half the bank keeps it grown")
	}
	for range backingSlabIdleResetsBeforeShrink - 1 {
		enterFrames(arena, 3, arenaIndices{IntIndex: overgrown / 8})
		arena.Reset()
		require.Len(t, arena.intSlab, overgrown, "an idle streak below the threshold keeps the bank")
	}
	arena.Reset()
	require.Len(t, arena.intSlab, initialIntSlabs, "an idle overgrown bank shrinks after the threshold")
}

func TestResetShrinksASliceBankOnlyWhenTheRunLeftItIdle(t *testing.T) {
	arena := NewRegisterArena()
	overgrown := initialSlicesByteSlabs * maxArenaMultiplier * 4
	arena.slicesByteSlab = make([][]byte, overgrown)
	arena.slicesStringSlab = make([][]string, overgrown)

	for range backingSlabIdleResetsBeforeShrink * 2 {
		enterFrames(arena, 2, arenaIndices{SlicesByteIndex: overgrown / 2})
		generation := arena.slabGeneration
		arena.Reset()
		require.Len(t, arena.slicesByteSlab, overgrown, "a bank the run's frames used is kept")
		require.Equal(t, generation, arena.slabGeneration, "a kept bank leaves cached views valid")
		require.Len(t, arena.slicesStringSlab, initialSlicesStringSlabs, "a bank the run left idle is shrunk")
		arena.slicesStringSlab = make([][]string, overgrown)
	}
	generation := arena.slabGeneration
	arena.Reset()
	require.Len(t, arena.slicesByteSlab, initialSlicesByteSlabs, "an overgrown bank the run left idle is shrunk at once")
	require.Greater(t, arena.slabGeneration, generation, "shrinking the byte bank refreshes cached views")
}

func TestFrameBankPeakReadsEveryOccupiedFrame(t *testing.T) {
	arena := NewRegisterArena()
	enterFrames(arena, 2, arenaIndices{IntIndex: 7, SlicesByteIndex: 3})
	enterFrames(arena, 5, arenaIndices{IntIndex: 5, GeneralIndex: 9, ComplexIndex: 2})
	arena.GeneralIndex = 11
	peak := arena.frameBankPeak(arena.frameStackPeak())
	require.Equal(t, 7, peak.IntIndex)
	require.Equal(t, 11, peak.GeneralIndex, "the arena's own index counts")
	require.Equal(t, 3, peak.SlicesByteIndex)
	require.Equal(t, 2, peak.ComplexIndex)
	arena.GeneralIndex = 0
	arena.Reset()
}
