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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/fault"
)

func TestBoxSlabGrowthChargesBudget(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	setArenaBudget(t, a, 1<<30)
	require.Equal(t, uint64(0), arenaTotalAllocatedBytes(a), "test precondition")

	a.growIntBoxSlab()
	require.Equal(t, uint64(cap(a.intBoxSlab))*arenaSizeScalarBox, arenaTotalAllocatedBytes(a),
		"growing an int box slab must charge cap*8 against the arena budget")

	before := arenaTotalAllocatedBytes(a)
	a.growComplexBoxSlab()
	require.Equal(t, before+uint64(cap(a.complexBoxSlab))*arenaSizeComplexBox, arenaTotalAllocatedBytes(a),
		"growing a complex box slab must add its capacity to the running total")
}

func TestBoxSlabGrowthTripsBudget(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	setArenaBudget(t, a, 4096)
	require.Nil(t, a.ownerVM, "test precondition: no ownerVM so an over-budget charge panics immediately")

	defer func() {
		recovered := recover()
		require.NotNil(t, recovered, "unbounded box-slab doubling must eventually exceed the budget")
		recoveredErr, ok := recovered.(error)
		require.True(t, ok, "expected error-typed panic, got %T", recovered)
		require.True(t, errors.Is(recoveredErr, fault.ErrArenaBudgetExceeded),
			"box-slab overrun must wrap errArenaBudgetExceeded, got %v", recoveredErr)
	}()

	for range 32 {
		a.growIntBoxSlab()
	}
	t.Fatal("expected box-slab growth to trip the arena budget before 32 doublings")
}

func TestResetShrinksOvergrownBoxSlab(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	setArenaBudget(t, a, 1<<30)

	for len(a.intBoxSlab) <= initialBoxSlabCapacity*maxArenaMultiplier {
		a.growIntBoxSlab()
	}
	require.Greater(t, len(a.intBoxSlab), initialBoxSlabCapacity*maxArenaMultiplier,
		"test precondition: box slab grown past the shrink threshold")

	grownLen := len(a.intBoxSlab)
	a.Reset()
	require.Equal(t, grownLen, len(a.intBoxSlab),
		"a Reset after an Execute that grew the box slab must retain it (hot)")

	for range backingSlabIdleResetsBeforeShrink {
		a.Reset()
	}
	require.Equal(t, initialBoxSlabCapacity, len(a.intBoxSlab),
		"consecutive idle Resets must shrink an overgrown box slab back to its initial capacity")
}

func TestResetShrinksOvergrownGenericAndSliceHeaderSlabs(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	setArenaBudget(t, a, 1<<30)

	for len(a.genericBytesSlab) <= initialGenericBytesCapacity*maxArenaMultiplier {
		a.growGenericBytesSlab(len(a.genericBytesSlab) + 1)
	}
	for len(a.sliceHeaderSlab) <= initialSliceHeaderCapacity*maxArenaMultiplier {
		a.growSliceHeaderSlab()
	}

	grownGenericLen := len(a.genericBytesSlab)
	grownHeaderLen := len(a.sliceHeaderSlab)
	a.Reset()
	require.Equal(t, grownGenericLen, len(a.genericBytesSlab),
		"a Reset after an Execute that grew the generic byte slab must retain it (hot)")
	require.Equal(t, grownHeaderLen, len(a.sliceHeaderSlab),
		"a Reset after an Execute that grew the slice-header slab must retain it (hot)")

	for range backingSlabIdleResetsBeforeShrink {
		a.Reset()
	}
	require.Equal(t, initialGenericBytesCapacity, len(a.genericBytesSlab),
		"consecutive idle Resets must shrink an overgrown generic byte slab")
	require.Equal(t, initialSliceHeaderCapacity, len(a.sliceHeaderSlab),
		"consecutive idle Resets must shrink an overgrown slice-header slab")
}
