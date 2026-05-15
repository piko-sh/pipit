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

import "pipit.sh/pipit/internal/engine/program"

// scalarBankDemand records, per scalar register bank, the largest capacity an execution
// asked for through pre-sizing or growth. It is the usage signal for the hysteretic
// shrink of overgrown scalar banks, chosen over a per-call peak because AllocRegisters is
// on the call hot path.
type scalarBankDemand struct {
	// ints is the peak int64 bank capacity requested.
	ints int

	// floats is the peak float64 bank capacity requested.
	floats int

	// strings is the peak string bank capacity requested.
	strings int

	// generals is the peak reflect.Value bank capacity requested.
	generals int

	// bools is the peak bool bank capacity requested.
	bools int

	// uints is the peak uint64 bank capacity requested.
	uints int

	// complexes is the peak complex128 bank capacity requested.
	complexes int
}

// noteRequest raises each bank's recorded demand to the capacity counts requests.
//
// Takes counts (typedSlabCounts) which is the capacity being requested.
func (d *scalarBankDemand) noteRequest(counts program.TypedSlabCounts) {
	d.ints = max(d.ints, counts.Ints)
	d.floats = max(d.floats, counts.Floats)
	d.strings = max(d.strings, counts.Strings)
	d.generals = max(d.generals, counts.Generals)
	d.bools = max(d.bools, counts.Bools)
	d.uints = max(d.uints, counts.Uints)
	d.complexes = max(d.complexes, counts.Complexes)
}

// scalarBankIdleCounters holds the consecutive-idle-Reset counters for the scalar banks.
type scalarBankIdleCounters struct {
	// ints is the consecutive-idle counter for the int64 bank.
	ints uint32

	// floats is the consecutive-idle counter for the float64 bank.
	floats uint32

	// strings is the consecutive-idle counter for the string bank.
	strings uint32

	// generals is the consecutive-idle counter for the reflect.Value bank.
	generals uint32

	// bools is the consecutive-idle counter for the bool bank.
	bools uint32

	// uints is the consecutive-idle counter for the uint64 bank.
	uints uint32

	// complexes is the consecutive-idle counter for the complex128 bank.
	complexes uint32
}

// shrinkOvergrownScalarSlabs replaces scalar register banks that grew past the DoS
// protection threshold and then sat idle across several Resets with fresh default-sized
// allocations, applying shrinkSlabWithHysteresis with the previous execution's recorded
// demand as the usage signal. A bank that was replaced bumps SlabGeneration so cached
// register views are refreshed.
func (a *RegisterArena) shrinkOvergrownScalarSlabs() {
	demand := a.scalarDemandLastReset
	a.intSlab = shrinkScalarSlab(a, a.intSlab, initialIntSlabs, demand.ints, &a.scalarIdleResets.ints)
	a.floatSlab = shrinkScalarSlab(a, a.floatSlab, initialFloatSlabs, demand.floats, &a.scalarIdleResets.floats)
	a.stringSlab = shrinkScalarSlab(a, a.stringSlab, initialStringSlabs, demand.strings, &a.scalarIdleResets.strings)
	a.generalSlab = shrinkScalarSlab(a, a.generalSlab, initialGeneralSlabs, demand.generals, &a.scalarIdleResets.generals)
	a.boolSlab = shrinkScalarSlab(a, a.boolSlab, initialBoolSlabs, demand.bools, &a.scalarIdleResets.bools)
	a.uintSlab = shrinkScalarSlab(a, a.uintSlab, initialUintSlabs, demand.uints, &a.scalarIdleResets.uints)
	a.complexSlab = shrinkScalarSlab(a, a.complexSlab, initialComplexSlabs, demand.complexes, &a.scalarIdleResets.complexes)
}

// shrinkScalarSlab applies the hysteretic shrink policy to one scalar bank and bumps the
// arena's slab generation when the bank was replaced.
//
// Takes arena (*RegisterArena) which owns the bank.
// Takes slab ([]T) which is the bank's current backing.
// Takes initial (int) which is the bank's default capacity.
// Takes demand (int) which is the previous execution's recorded demand for the bank.
// Takes idleResets (*uint32) which is the bank's consecutive-idle counter.
//
// Returns the possibly replaced backing.
func shrinkScalarSlab[T any](arena *RegisterArena, slab []T, initial int, demand int, idleResets *uint32) []T {
	shrunk := shrinkSlabWithHysteresis(slab, initial, demand, idleResets)
	if len(shrunk) != len(slab) {
		arena.slabGeneration++
	}
	return shrunk
}
