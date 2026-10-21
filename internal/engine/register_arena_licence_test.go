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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type licenceAllocNode struct {
	label    string
	children []*licenceAllocNode
	tags     map[string]int
}

func TestAcquisitionTakesDisableMinorGCFromTheLimits(t *testing.T) {
	arena := NewRegisterArena()
	arena.disableMinorGC = true
	arena.recycleChunks = !ArenaChunkRecyclingEnabled
	arena.attachLicencePolicy(&VMLimits{})
	require.False(t, arena.disableMinorGC, "a pooled arena must not keep a previous owner's collector setting")
	arena.disableMinorGC = true
	arena.Reset()
	require.False(t, arena.disableMinorGC)
	require.Equal(t, ArenaChunkRecyclingEnabled, arena.recycleChunks, "Reset restores chunk recycling for the next owner")
}

func requireNoAllocations(t *testing.T, run func()) {
	t.Helper()
	if raceDetectorOn {
		t.Skip("allocation counts are not meaningful under the race detector")
	}
	if storeCensusBuilt {
		t.Skip("the store census probes every container the walk memo settles, and its records allocate")
	}
	run()
	quiesceCleanups(t)
	fewest := testing.AllocsPerRun(50, run)
	for attempt := 1; attempt < 3 && fewest != 0; attempt++ {
		quiesceCleanups(t)
		fewest = min(fewest, testing.AllocsPerRun(50, run))
	}
	require.Zero(t, fewest, "allocations per run, the fewest of up to three measurements")
}

type cleanupSentinel struct {
	next    *cleanupSentinel
	padding [32]byte
}

func quiesceCleanups(t *testing.T) {
	t.Helper()
	for range 2 {
		ran := make(chan struct{})
		func() {
			sentinel := &cleanupSentinel{next: nil, padding: [32]byte{}}
			runtime.AddCleanup(sentinel, func(done chan struct{}) { close(done) }, ran)
		}()
		require.Eventually(t, func() bool {
			runtime.GC()
			select {
			case <-ran:
				return true
			default:
				return false
			}
		}, 10*time.Second, time.Millisecond, "the collector runs the queued cleanups")
	}
}

func TestLicenceMachineryAllocatesNothing(t *testing.T) {
	t.Run("walk of arena strings", func(t *testing.T) {
		arena := newArena(t)
		backing := arena.AllocStringBacking(8)
		for index := range backing {
			backing[index] = arenaString(arena, "arena string")
		}
		value := reflect.ValueOf(backing)
		requireNoAllocations(t, func() { arena.revokeHandOff(handOffUntracedContainer, value, false, false) })
		arena.Reset()
	})
	t.Run("walk of a Go-heap map with arena keys", func(t *testing.T) {
		arena := newArena(t)
		tags := map[string]int{}
		for index := range 8 {
			tags[arenaString(arena, "key")+strings.Repeat("k", index)] = index
		}
		value := reflect.ValueOf(tags)
		requireNoAllocations(t, func() { arena.revokeHandOff(handOffIdentityKept, value, false, false) })
		arena.Reset()
	})
	t.Run("walk of a Go-heap graph", func(t *testing.T) {
		arena := newArena(t)
		root := &licenceAllocNode{label: arenaString(arena, "root"), tags: map[string]int{"a": 1}}
		for range 16 {
			root.children = append(root.children, &licenceAllocNode{label: arenaString(arena, "child")})
		}
		value := reflect.ValueOf(root)
		requireNoAllocations(t, func() { arena.revokeHandOff(handOffIdentityKept, value, true, false) })
		arena.Reset()
	})
	t.Run("walk of a shared value", func(t *testing.T) {
		arena := newArena(t)
		backing := arena.AllocStringBacking(4)
		for index := range backing {
			backing[index] = arenaString(arena, "shared")
		}
		value := reflect.ValueOf(backing)
		requireNoAllocations(t, func() { arena.revokeValue(value, false, handOffSpawn, true) })
		arena.Reset()
	})
	t.Run("blind hand-off at a revoked epoch", func(t *testing.T) {
		arena := newArena(t)
		noteHandOff(arena, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
		require.True(t, arena.licenceState.epochRevoked)
		value := reflect.ValueOf(arenaString(arena, "blind"))
		requireNoAllocations(t, func() { arena.revokeHandOff(handOffUntracedContainer, value, false, false) })
		arena.Reset()
	})
	t.Run("publication of an unchanged slab set", func(t *testing.T) {
		a := newArena(t)
		requireNoAllocations(t, a.publishLicenceSlabs)
		a.Reset()
	})
	t.Run("release and acquisition of a clean arena", func(t *testing.T) {
		arena := newArena(t)
		limits := &VMLimits{}
		requireNoAllocations(t, func() {
			_ = arenaString(arena, "kept in the arena")
			arena.Reset()
			arena.attachLicencePolicy(limits)
		})
		arena.Reset()
	})
	t.Run("registry update", func(t *testing.T) {
		arena := newArena(t)
		slabs := [2][]int64{make([]int64, 64), make([]int64, 64)}
		turn := 0
		requireNoAllocations(t, func() {
			turn ^= 1
			arena.intBackingSlab = slabs[turn]
			arena.publishLicenceSlabs()
		})
		arena.Reset()
	})
}
