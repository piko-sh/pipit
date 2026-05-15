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
	"pipit.sh/pipit/internal/isa"
)

func newSizingFixture() (*program.CompiledFunction, *program.CompiledFunction) {
	heavy := &program.CompiledFunction{}
	heavy.NumRegisters[isa.RegisterInt] = 200
	helper := &program.CompiledFunction{}
	helper.NumRegisters[isa.RegisterInt] = 3
	entry := &program.CompiledFunction{}
	entry.NumRegisters[isa.RegisterInt] = 4
	entry.CallSites = []program.CallSite{{FunctionIndex: 2}}
	root := &program.CompiledFunction{}
	root.NumRegisters[isa.RegisterInt] = 2
	root.SetFunctions([]*program.CompiledFunction{heavy, entry, helper})
	return root, entry
}

func TestSizeArenaUsesMaxPerBankTimesDepth(t *testing.T) {
	t.Parallel()
	root, entry := newSizingFixture()
	depth := clampedArenaDepth(root)
	require.Equal(t, 2+frameStackSafetyMargin, depth)

	vm := newTestVM(t)
	vm.Arena = NewRegisterArena()
	vm.sizeArenaFromFunctions(root)
	require.Len(t, vm.Arena.intSlab, 200*depth, "root sizing is the per-bank peak times the clamped depth")

	child := newTestVM(t)
	child.Arena = NewRegisterArena()
	child.sizeArenaForEntry(root, entry)
	require.Len(t, child.Arena.intSlab, max(initialIntSlabs, 4*depth), "entry sizing ignores functions unreachable from the entry")
}

func TestSizeArenaFallsBackToWholeTableOnClosureCalls(t *testing.T) {
	t.Parallel()
	root, entry := newSizingFixture()
	entry.CallSites = append(entry.CallSites, program.CallSite{IsClosure: true})
	require.Len(t, functionsReachableFrom(root, entry, append([]*program.CompiledFunction{root}, program.ExportFunctions(root)...)), 4)
}

func TestChildGoroutineArenaStableAcrossSpawns(t *testing.T) {
	t.Parallel()
	root, entry := newSizingFixture()
	child := newTestVM(t)
	child.Arena = NewRegisterArena()
	child.sizeArenaForEntry(root, entry)
	intLen, generation := len(child.Arena.intSlab), child.Arena.slabGeneration
	for range 8 {
		child.Arena.Reset()
		child.sizeArenaForEntry(root, entry)
		require.Len(t, child.Arena.intSlab, intLen)
		require.Equal(t, generation, child.Arena.slabGeneration)
	}
}

func TestShrinkOvergrownScalarSlabsKeepsHotBanks(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	overgrown := initialIntSlabs*maxArenaMultiplier + 1
	arena.ensureScalarBankCapacity(program.TypedSlabCounts{Ints: overgrown})
	require.Len(t, arena.intSlab, overgrown)

	for range backingSlabIdleResetsBeforeShrink * 2 {
		arena.ensureScalarBankCapacity(program.TypedSlabCounts{Ints: overgrown})
		arena.Reset()
		require.Len(t, arena.intSlab, overgrown, "a bank the previous execution asked for stays grown")
	}
	for range backingSlabIdleResetsBeforeShrink - 1 {
		arena.Reset()
		require.Len(t, arena.intSlab, overgrown, "idle streak below the threshold keeps the bank")
	}
	arena.Reset()
	require.Len(t, arena.intSlab, initialIntSlabs, "an idle overgrown bank shrinks after the threshold")
}
