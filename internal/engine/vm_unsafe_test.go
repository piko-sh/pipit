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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReuseHazardRecycleChunksSurvivesPoolReuse(t *testing.T) {
	observations := &hazardObservations{}

	direct := GetRegisterArena()
	direct.recycleChunks = false
	PutRegisterArena(direct)
	again := GetRegisterArena()
	persisted := again == direct && !again.recycleChunks
	observations.note("synthetic Put/Get: same arena=%t, recycleChunks still false=%t", again == direct, persisted)
	again.recycleChunks = ArenaChunkRecyclingEnabled
	PutRegisterArena(again)

	var factoryArena *RegisterArena
	vm := &VM{}
	vm.Limits.ArenaFactory = func() *RegisterArena {
		factoryArena = GetRegisterArena()
		return factoryArena
	}
	vm.Limits.AbandonArenasOnRelease = true
	acquired := vm.acquireArena()
	clearedOnAcquire := !acquired.recycleChunks
	vm.releaseArenaUnlessLeaked(acquired)
	reached := false
	drawnArenas := make([]*RegisterArena, 0, 64)
	defer func() {
		for _, drawn := range slices.Backward(drawnArenas) {
			PutRegisterArena(drawn)
		}
	}()
	for range 64 {
		drawn := GetRegisterArena()
		if drawn == factoryArena {
			reached = true
		}
		drawnArenas = append(drawnArenas, drawn)
	}
	observations.note("factory and abandon path: recycleChunks cleared on acquire=%t, arena seen again in the next 64 pool draws=%t", clearedOnAcquire, reached)
	require.NotNil(t, factoryArena, "test harness: the factory was not called")

	if reached && !factoryArena.recycleChunks {
		observations.fire("engine-state", "an arena with dead-chunk recycling off returned to the pool")
	} else if persisted {
		observations.note("the field persists across Reset, but no production path returns a recycling-off arena to the pool")
	}
	assertNoHazard(t, observations, "")
}
