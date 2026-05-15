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
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
)

func TestCallbackVMCacheHandsOutOneVMAtATime(t *testing.T) {
	t.Parallel()
	cache := new(callbackVMCache)
	require.Nil(t, cache.acquire(), "an empty cache hands out nothing")

	first := newTestVM(t)
	cache.release(first)
	require.Same(t, first, cache.acquire())
	require.Nil(t, cache.acquire(), "the slot is empty while its VM is out")

	second := newTestVM(t)
	cache.release(first)
	cache.release(second)
	require.EqualValues(t, 1, cache.discarded.Load(), "a VM released into an occupied slot is discarded")
	require.Same(t, first, cache.slot.Load())

	cache.drain()
	require.Nil(t, cache.slot.Load())
	require.EqualValues(t, 2, cache.discarded.Load())
	require.Nil(t, cache.acquire(), "a drained cache hands out nothing")
	cache.release(newTestVM(t))
	require.EqualValues(t, 3, cache.discarded.Load(), "a VM released after the drain is discarded")
	require.Nil(t, cache.slot.Load())

	cache.reopen()
	third := newTestVM(t)
	cache.release(third)
	require.Same(t, third, cache.acquire(), "a reopened cache caches again")
}

func TestCallbackVMCacheDiscardsEveryVMExactlyOnce(t *testing.T) {
	t.Parallel()
	const workers = 4
	const rounds = 2000
	cache := new(callbackVMCache)
	var built atomic.Int64
	var waitGroup sync.WaitGroup

	for range workers {
		waitGroup.Go(func() {
			for round := range rounds {
				vm := cache.acquire()
				if vm == nil {
					vm = newTestVM(t)
					built.Add(1)
				}
				cache.release(vm)
				if round%97 == 0 {
					cache.drain()
					cache.reopen()
				}
			}
		})
	}
	waitGroup.Wait()
	cache.drain()

	require.Nil(t, cache.slot.Load(), "the slot is empty after the final drain")
	require.Equal(t, built.Load(), cache.discarded.Load(), "every VM built was discarded exactly once")
	require.Positive(t, built.Load())
}

func TestMinorGCOnIdleCallbackArenaRecyclesUnrootedChunk(t *testing.T) {
	t.Parallel()
	if !arenaUsesUnsafeSlabs {
		t.Skip("safe build: pointer-free bytes live on the Go heap, not in a recyclable chunk")
	}
	vm := newGCTestVM()
	vm.FramePointer = -1
	vm.recoverEligibleFrame = -1
	arena := vm.Arena

	arena.growGenericBytesSlab(genericBytesSlabChunk)
	hostHeld := unsafe.Slice((*byte)(arena.AllocBytes(64, 8)), 64)
	for i := range hostHeld {
		hostHeld[i] = 0xAB
	}
	arena.growGenericBytesSlab(genericBytesSlabChunk)
	require.NotEmpty(t, arena.oldGenericByteSlabs, "the host-held bytes now live in a retired chunk")
	require.True(t, arena.holdsEscapableData())

	vm.applyCallbackLimits(VMLimits{})
	require.True(t, arena.disableMinorGC, "an arena holding escaped data keeps the collector off")
	vm.checkpointFlags |= checkpointFlagGCPending
	vm.runPendingCheckpoints()
	require.Equal(t, byte(0xAB), hostHeld[0], "the checkpoint does not collect")

	arena.MinorGC(vm)
	require.Zero(t, hostHeld[0], "a forced collection with no frame roots zeroes the chunk the host still reads")
}

func TestApplyCallbackLimitsKeepsCollectorOnCleanArena(t *testing.T) {
	t.Parallel()
	vm := newGCTestVM()
	vm.applyCallbackLimits(VMLimits{})
	require.False(t, vm.Arena.disableMinorGC, "a clean arena collects as a fresh one does")
	vm.applyCallbackLimits(VMLimits{DisableMinorGC: true})
	require.True(t, vm.Arena.disableMinorGC, "the limits knob still wins")
}

func callbackSpecFixture(t *testing.T) (*callbackVMSpec, *RuntimeClosure) {
	t.Helper()
	root := &program.CompiledFunction{}
	root.Functions = []*program.CompiledFunction{root}
	parent := newTestVM(t)
	parent.functions = root.Functions
	parent.rootFunction = root
	closure := &RuntimeClosure{Function: root, RootFunction: root, inlineCells: [inlineUpvalueCells]*program.UpvalueCell{}, upvalues: nil, namedType: ""}
	return newCallbackVMSpec(parent), closure
}

func TestCallbackVMSpecReusesOneVMAndTurnsCollectorOffAfterEscapes(t *testing.T) {
	t.Parallel()
	spec, closure := callbackSpecFixture(t)

	first := spec.acquire(closure)
	require.False(t, first.Arena.disableMinorGC, "a fresh callback arena collects")
	spec.finish(first)
	require.EqualValues(t, 1, spec.cache.built.Load())

	second := spec.acquire(closure)
	require.Same(t, first, second, "the idle VM is handed out again")
	require.False(t, second.Arena.disableMinorGC, "a clean reused arena still collects")
	second.Arena.AllocBytes(64, 8)
	spec.finish(second)

	third := spec.acquire(closure)
	require.Same(t, first, third)
	require.True(t, third.Arena.disableMinorGC, "an arena holding escaped data stops collecting")
	spec.finish(third)
	require.EqualValues(t, 1, spec.cache.built.Load(), "no rebuild across the three callbacks")
	require.Zero(t, spec.cache.discarded.Load())
}

func TestCallbackVMSpecRetiresPastThreshold(t *testing.T) {
	t.Parallel()
	spec, closure := callbackSpecFixture(t)
	spec.retireBytes = 32

	first := spec.acquire(closure)
	first.Arena.AllocBytes(64, 8)
	spec.finish(first)
	require.EqualValues(t, 1, spec.cache.discarded.Load(), "an arena past the threshold is released")
	require.Nil(t, spec.cache.slot.Load())
	require.Nil(t, first.Arena, "the released VM gave its arena back")

	second := spec.acquire(closure)
	require.NotSame(t, first, second, "the next callback builds afresh")
	spec.finish(second)
	require.Same(t, second, spec.cache.slot.Load())
}

func TestCallbackVMSpecDiscardsDirtyVM(t *testing.T) {
	t.Parallel()
	spec, closure := callbackSpecFixture(t)
	dirty := spec.acquire(closure)
	dirty.evalError = fault.ErrCostBudgetExceeded
	spec.finish(dirty)
	require.Nil(t, spec.cache.slot.Load(), "a VM that finished with an error is not cached")
	require.EqualValues(t, 1, spec.cache.discarded.Load())
}

func TestCallbackVMSpecSafeModeNeverCaches(t *testing.T) {
	t.Parallel()
	root := &program.CompiledFunction{}
	root.Functions = []*program.CompiledFunction{root}
	parent := newTestVM(t)
	parent.Limits.SafeMode = true
	spec := newCallbackVMSpec(parent)
	require.Nil(t, spec.cache)
	closure := &RuntimeClosure{Function: root, RootFunction: root, inlineCells: [inlineUpvalueCells]*program.UpvalueCell{}, upvalues: nil, namedType: ""}
	vm := spec.acquire(closure)
	spec.finish(vm)
	require.Nil(t, vm.Arena, "safe mode releases every callback VM")
	require.Zero(t, parent.CallbackVMStats().Built)
}
