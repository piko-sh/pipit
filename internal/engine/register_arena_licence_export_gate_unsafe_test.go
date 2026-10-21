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
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func exportFixture(t *testing.T) (*VM, *RuntimeClosure, *RegisterArena) {
	t.Helper()
	maker, closure := topLevelFixture(t)
	arena := newArenaInTree(t, maker.Limits.Tracker)
	arena.setLicenceVM(maker)
	maker.Arena = arena
	require.False(t, arena.licenceShared(0), "a VM with no goroutines starts unshared")
	return maker, closure, arena
}

func TestAnExportedVMReadsUnsharedOnlyInsideAnOwnerSection(t *testing.T) {
	maker, closure, arena := exportFixture(t)
	makeClosureWrapperFunc(maker, closure, reflect.TypeFor[func() int]())
	require.True(t, arena.licenceShared(0))
	require.NotNil(t, maker.exportGate)

	shared, owner := arena.enterOwnerSection(true)
	require.Same(t, maker, owner)
	require.False(t, shared, "with its gate held no exported guest code runs, so the owner reads unshared")
	require.False(t, arena.licenceShared(0))
	shared, nested := arena.enterOwnerSection(arena.licenceShared(0))
	require.False(t, shared)
	require.Nil(t, nested, "a walk inside a section shares its hold")
	owner.leaveOwnerSection()
	require.True(t, arena.licenceShared(0), "outside the section the exported guest code may be running")
}

func TestAnOwnerSectionDoesNotHideLiveGoroutines(t *testing.T) {
	maker, closure, arena := exportFixture(t)
	makeClosureWrapperFunc(maker, closure, reflect.TypeFor[func() int]())
	maker.hasGoroutines, maker.spawnedGoroutines = true, true
	maker.Limits.Tracker.GoroutineCount.Store(1)
	shared, owner := arena.enterOwnerSection(true)
	require.True(t, shared, "a live goroutine shares the memory whatever the gate")
	require.Nil(t, owner)
	maker.Limits.Tracker.GoroutineCount.Store(0)
	shared, owner = arena.enterOwnerSection(true)
	require.False(t, shared, "once the goroutines end only the export shares it, which the section excludes")
	owner.leaveOwnerSection()
}

func TestAnOwnerSectionWaitsForAGatedRunOfItsWrapper(t *testing.T) {
	maker, closure, arena := exportFixture(t)
	makeClosureWrapperFunc(maker, closure, reflect.TypeFor[func() int]())
	spec := newCallbackVMSpec(maker)
	require.Contains(t, spec.gates, maker.exportGate, "a run of the wrapper holds its maker's gate")

	callback := newTestVM(t)
	callback.reentrantInterpreterVM = true
	callback.runGates = spec.gates
	require.True(t, callback.mayRunGated())
	require.True(t, callback.beginGatedRun())
	require.EqualValues(t, 1, maker.exportGate.readers.Load())

	shared, owner := arena.enterOwnerSection(true)
	require.True(t, shared, "a run in guest code that never yields keeps the owner out, which then runs shared")
	require.Nil(t, owner)
	require.True(t, arena.licenceShared(0))

	callback.parkGates()
	require.EqualValues(t, 0, maker.exportGate.readers.Load(), "a blocked run lets the gate go")
	shared, owner = arena.enterOwnerSection(true)
	require.False(t, shared)
	owner.leaveOwnerSection()
	callback.unparkGates()
	require.EqualValues(t, 1, maker.exportGate.readers.Load())
	callback.endGatedRun()
	require.EqualValues(t, 0, maker.exportGate.readers.Load())
}

func TestAGatedRunAboveItsOwnerOnTheGoroutineLeavesTheGateAlone(t *testing.T) {
	maker, closure, _ := exportFixture(t)
	makeClosureWrapperFunc(maker, closure, reflect.TypeFor[func() int]())
	callback := newTestVM(t)
	callback.reentrantInterpreterVM = true
	callback.runGates = maker.exportedGates()
	callback.servedChainRun.below = maker
	callback.servedChainRun.token.Store(1)
	require.True(t, callback.beginGatedRun())
	require.EqualValues(t, 0, maker.exportGate.readers.Load(), "the owner parked below cannot start a section, so the run pays nothing")
	require.False(t, callback.holdsGates)
	callback.endGatedRun()
}

func TestANestedGatedRunParksTheHoldItLeansOn(t *testing.T) {
	maker, closure, _ := exportFixture(t)
	makeClosureWrapperFunc(maker, closure, reflect.TypeFor[func() int]())
	gate := maker.exportGate
	outer := newTestVM(t)
	outer.reentrantInterpreterVM = true
	outer.runGates = maker.exportedGates()
	require.True(t, outer.beginGatedRun())
	inner := newTestVM(t)
	inner.reentrantInterpreterVM = true
	inner.runGates = maker.exportedGates()
	inner.servedChainRun.below = outer
	inner.servedChainRun.token.Store(1)
	require.True(t, inner.beginGatedRun())
	require.EqualValues(t, 1, gate.readers.Load(), "the inner run leans on the outer run's hold")
	require.True(t, inner.gatesLeanBelow)

	inner.parkGates()
	require.EqualValues(t, 0, gate.readers.Load(), "blocking in the inner run lets the outer run's hold go too")
	inner.unparkGates()
	require.EqualValues(t, 1, gate.readers.Load())
	inner.endGatedRun()
	outer.endGatedRun()
	require.EqualValues(t, 0, gate.readers.Load())
}

func TestAGoroutineThatExportsGuestCodeSharesItsSpawnersMemory(t *testing.T) {
	maker, closure, arena := exportFixture(t)
	spawn := newGoroutineSpawn(maker)
	require.NotNil(t, maker.exportGate, "a VM that starts a goroutine has a gate for the goroutine's exports")
	require.False(t, arena.licenceShared(0), "a gate alone does not share the memory")
	child := newTestVM(t)
	child.debugKind = debugThreadGoroutine
	child.runGates, child.spawnGates = spawn.runGates, spawn.spawnGates
	child.Limits.Tracker = maker.Limits.Tracker
	makeClosureWrapperFunc(child, closure, reflect.TypeFor[func() int]())
	require.Contains(t, child.exportedGates(), maker.exportGate)
	require.True(t, maker.exportGate.exportedBySpawned.Load())
	shared, owner := arena.enterOwnerSection(arena.licenceShared(0))
	require.Same(t, maker, owner, "the spawner's next walk adopts the goroutine's export and reads in a section")
	require.False(t, shared)
	owner.leaveOwnerSection()
	require.True(t, arena.licenceShared(0),
		"the goroutine's exported closure may write what it captured from its spawner after the goroutine ends")
}

func TestAnExportedOwnersArenaThePoolRefusesLeavesItsGateWorking(t *testing.T) {
	maker, closure, arena := exportFixture(t)
	wrapper, isFunc := reflect.TypeAssert[func() int](makeClosureWrapperFunc(maker, closure, reflect.TypeFor[func() int]()))
	require.True(t, isFunc)
	gate := maker.exportGate
	require.NotNil(t, gate, "test harness: the export gave the owner a gate")
	arenaString(arena, "the owner's storage")
	owner := arena.slabOwner
	require.NotZero(t, owner)

	maker.drainCallbackVMs()
	maker.CallStack = nil
	maker.Arena = nil
	require.True(t, releaseRegisterArenaForPool(arena), "the owner's arena is released in an owner section and reset, ready for the pool")
	abandonUnpooledRegisterArena(arena)
	require.False(t, ownerHasRegistryEntries(owner), "torn down as the full pool's refusal does, it leaves both registries at once")

	require.Equal(t, 42, wrapper(), "native code keeps the wrapper, which still runs after its owner's arena was torn down")
	require.Zero(t, gate.readers.Load(), "the run let its owner's gate go")
	require.Same(t, gate, maker.exportGate, "the owner keeps its gate, which no other VM receives")

	next := newArenaInTree(t, maker.Limits.Tracker)
	next.setLicenceVM(maker)
	maker.Arena = next
	require.True(t, next.licenceShared(0), "the owner's next execution is still shared outside an owner section")
	shared, section := next.enterOwnerSection(true)
	require.False(t, shared, "and reads unshared inside one, through the same gate")
	require.Same(t, maker, section)
	section.leaveOwnerSection()
	maker.ReleaseArena()
}

func TestHostArgumentsAreMaterialisedInsideTheOwnerSection(t *testing.T) {
	maker, closure, arena := exportFixture(t)
	makeClosureWrapperFunc(maker, closure, reflect.TypeFor[func() int]())
	gates := newCallbackVMSpec(maker).gates
	require.NotEmpty(t, gates)
	values := []string{"host"}
	var writes atomic.Int64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		callback := newTestVM(t)
		callback.reentrantInterpreterVM = true
		callback.runGates = gates
		for written := 0; ; written++ {
			select {
			case <-stop:
				return
			default:
			}
			if callback.beginGatedRun() {
				values[0] = []string{"left", "right"}[written%2]
				writes.Add(1)
				callback.endGatedRun()
			}
		}
	}()
	for walks := 0; walks < 200 || writes.Load() < 200; walks++ {
		MaterialiseHostArguments(arena, []reflect.Value{reflect.ValueOf(values)})
	}
	close(stop)
	<-done
}
