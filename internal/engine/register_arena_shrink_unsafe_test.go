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
	"runtime"
	"testing"
)

func TestReuseHazardSliceBankKeptByResetIsNotAHandedOffBank(t *testing.T) {
	observations := &hazardObservations{}
	const text = "a byte slice a hand-off kept in a register bank"
	arena := newArena(t)
	overgrown := initialSlicesByteSlabs * maxArenaMultiplier * 4
	growSlab(arena, &arena.slicesByteSlab, overgrown, nil, true)
	arena.slicesByteSlab[5] = []byte(text)
	kept := &arena.slicesByteSlab[5]
	noteHandOff(arena, handOffIdentityKept, reflect.ValueOf(kept))
	banksRevoked := arena.licenceState.banksRevoked
	enterFrames(arena, 2, arenaIndices{SlicesByteIndex: overgrown / 2})
	arena.Reset()
	bankKept := len(arena.slicesByteSlab) == overgrown
	observations.note("banks revoked=%v; bank of the same length kept=%v", banksRevoked, bankKept)
	for round := range 3 {
		relicence(arena)
		for index := range arena.slicesByteSlab {
			arena.slicesByteSlab[index] = []byte("poison")
		}
		enterFrames(arena, 2, arenaIndices{SlicesByteIndex: overgrown / 2})
		arena.Reset()
		observations.note("round %d bank length %d", round, len(arena.slicesByteSlab))
	}
	observations.compareString("byte slice the host kept through the bank", text, string(*kept))
	runtime.KeepAlive(kept)
	inconclusive := ""
	switch {
	case !banksRevoked:
		inconclusive = "the hand-off did not revoke the banks"
	case !bankKept:
		inconclusive = "Reset did not keep the bank"
	}
	assertNoHazard(t, observations, inconclusive)
}
