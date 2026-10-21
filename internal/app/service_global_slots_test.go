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

package app

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

func TestFreedRegionsSplitAndCoalesce(t *testing.T) {
	t.Parallel()

	var registry loadedGlobalRegistry
	kind := int(isa.RegisterInt64)
	registry.free[kind] = sortedRegions([]globalSlotRegion{{start: 20, count: 5}, {start: 0, count: 10}, {start: 10, count: 5}})
	require.Equal(t, []globalSlotRegion{{start: 0, count: 15}, {start: 20, count: 5}}, registry.free[kind], "neighbours merge")

	region, ok := registry.takeFreeLocked(kind, 4, 3)
	require.True(t, ok)
	require.Equal(t, globalSlotRegion{start: 4, count: 3}, region, "the run starts at the lowest operand")
	require.Equal(t, []globalSlotRegion{{start: 0, count: 4}, {start: 7, count: 8}, {start: 20, count: 5}}, registry.free[kind])

	_, ok = registry.takeFreeLocked(kind, 0, 9)
	require.False(t, ok, "no freed run is long enough")
	_, ok = registry.takeFreeLocked(kind, 21, 5)
	require.False(t, ok, "a run may not start below the lowest operand")

	var fromFree [program.NumGlobalRegisterKinds]bool
	fromFree[kind] = true
	reservation := &globalReservation{}
	reservation.regions[kind] = region
	registry.returnTakenLocked(reservation, fromFree)
	require.Equal(t, []globalSlotRegion{{start: 0, count: 15}, {start: 20, count: 5}}, registry.free[kind], "a failed reservation gives its run back")
}

func TestAFailedReservationGivesBackFreedRuns(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	var first program.SlotAllocation
	first[isa.RegisterInt64] = 4
	first[isa.RegisterFloat64] = 1
	reservation, err := service.reserveLocked(program.SlotAllocation{}, first)
	require.NoError(t, err)
	reservation.handles = 0
	service.releaseIfIdleLocked(reservation)
	require.Len(t, service.loadedGlobals.free[isa.RegisterInt64], 1)

	var tooLarge program.SlotAllocation
	tooLarge[isa.RegisterInt64] = 2
	tooLarge[isa.RegisterFloat64] = maxBankSlots
	_, err = service.reserveLocked(program.SlotAllocation{}, tooLarge)
	require.ErrorIs(t, err, fault.ErrGlobalSlotsExhausted)
	require.Equal(t, []globalSlotRegion{{start: 0, count: 4}}, service.loadedGlobals.free[isa.RegisterInt64], "the int run taken first is returned")
}

func TestAnIdleReservationIsClearedAndFreedInEveryBank(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	var allocation program.SlotAllocation
	for kind := range program.NumGlobalRegisterKinds {
		allocation[kind] = 1
	}
	service.globals.ReserveSlots(allocation)
	service.globals.SetInt(0, 1)
	service.globals.SetFloat(0, 1)
	service.globals.SetString(0, "x")
	service.globals.SetGeneral(0, reflect.ValueOf(1))
	service.globals.SetBool(0, true)
	service.globals.SetUint(0, 1)
	service.globals.SetComplex(0, 1i)
	reservation := &globalReservation{}
	for kind := range program.NumGlobalRegisterKinds {
		reservation.regions[kind] = globalSlotRegion{start: 0, count: 1}
	}

	service.loadedGlobals.mu.Lock()
	service.releaseIfIdleLocked(reservation)
	service.loadedGlobals.mu.Unlock()

	for kind := range program.NumGlobalRegisterKinds {
		require.Equal(t, []globalSlotRegion{{start: 0, count: 1}}, service.loadedGlobals.free[kind],
			"the %s slot is free again", isa.RegisterKind(kind))
	}
	require.Zero(t, service.globals.GetInt(0))
	require.Zero(t, service.globals.GetFloat(0))
	require.Empty(t, service.globals.GetString(0))
	require.False(t, service.globals.GetGeneral(0).IsValid())
	require.False(t, service.globals.GetBool(0))
	require.Zero(t, service.globals.GetUint(0))
}

func TestRepeatedLoadsWithADifferentClaimDoNotShare(t *testing.T) {
	t.Parallel()

	var registry loadedGlobalRegistry
	digest := [32]byte{1}
	var count program.SlotAllocation
	count[isa.RegisterInt64] = 2
	reservation := &globalReservation{count: count}
	registry.rememberDigestLocked(reservation, &digest)
	require.Same(t, reservation, registry.reusableLocked(&digest, program.SlotAllocation{}, count))
	other := count
	other[isa.RegisterInt64] = 3
	require.Nil(t, registry.reusableLocked(&digest, program.SlotAllocation{}, other), "a mismatched claim reserves afresh")
	require.Nil(t, registry.reusableLocked(nil, program.SlotAllocation{}, count), "no digest, no sharing")
	registry.rememberDigestLocked(&globalReservation{}, nil)
	require.Len(t, registry.byDigest, 1)
}
