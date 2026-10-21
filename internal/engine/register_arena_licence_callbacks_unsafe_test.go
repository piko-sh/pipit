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
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type trackedCallbackSpec struct {
	spec    *callbackVMSpec
	closure *RuntimeClosure
}

func newPooledArena(t *testing.T) *RegisterArena {
	t.Helper()
	arena := newTestArena(t)
	arena.attachLicencePolicy(&VMLimits{Tracker: &ResourceTracker{}})
	require.True(t, arena.licensed)
	t.Cleanup(arena.Reset)
	return arena
}

func handOffBoundaryNamed(t *testing.T, name string) isa.HandOffBoundary {
	t.Helper()
	index := slices.IndexFunc(isa.HandOffBoundaries(), func(b isa.HandOffBoundary) bool { return b.Name == name })
	require.GreaterOrEqualf(t, index, 0, "the hand-off table has no %s boundary", name)
	return isa.HandOffBoundaries()[index]
}

func stringMayOutliveRelease(a *RegisterArena, s string) bool {
	return !a.referencesArenaString(s) || a.licenceState.epochRevoked || a.licenceCoveredAt(unsafe.Pointer(unsafe.StringData(s)))
}

func storageMayOutliveRelease(a *RegisterArena, p unsafe.Pointer) bool {
	return !a.ownsAnyStorage(p) || a.licenceState.epochRevoked || a.licenceCoveredAt(p)
}

func TestCallbackBoundaryRowsAreDeclared(t *testing.T) {
	boundary := handOffBoundaryNamed(t, "callback-return")
	require.Equal(t, []string{"materialiseCallbackResults"}, boundary.Roots)
	require.Len(t, boundary.HandOffs, 2)
	require.Equal(t, isa.HandOffCopy, boundary.HandOffs[0].Kind)
	require.Equal(t, isa.RouteHostReturn, boundary.HandOffs[0].Route)
	require.Equal(t, isa.HandOffRevoke, boundary.HandOffs[1].Kind)
	require.Equal(t, isa.RouteHostReturn, boundary.HandOffs[1].Route)
	require.ElementsMatch(t, []isa.HandOffCause{isa.HandOffHeapPassthrough, isa.HandOffIdentityKept}, boundary.HandOffs[1].Causes)

	args := handOffBoundaryNamed(t, "callback-host-arguments")
	require.Equal(t, []string{"materialiseCallbackArguments"}, args.Roots)
	host := handOffBoundaryNamed(t, "host-arguments")
	require.Equal(t, host.HandOffs, args.HandOffs, "the callback host-argument boundary declares what the root host-argument walk declares")
}

func TestCallbackReturnCopiesArenaResults(t *testing.T) {
	arena := newPooledArena(t)
	text := arenaBytesToString(arena, []byte("callback-result"))
	require.True(t, arena.referencesArenaString(text), "test harness: the string must be arena-backed")
	backing := arena.AllocStringBacking(2)
	backing[0] = arenaBytesToString(arena, []byte("first"))
	backing[1] = arenaBytesToString(arena, []byte("second"))
	heap := strings.Repeat("h", 3)
	results := []any{text, backing, heap, 42, nil}

	materialiseCallbackResults(arena, results)

	got, ok := results[0].(string)
	require.True(t, ok)
	require.Equal(t, "callback-result", got)
	require.True(t, stringMayOutliveRelease(arena, got), "an arena string result must leave as a copy or covered (callback-return: copy, then revoke on host-return)")
	strs, ok := results[1].([]string)
	require.True(t, ok)
	require.Equal(t, []string{"first", "second"}, strs)
	require.True(t, storageMayOutliveRelease(arena, reflect.ValueOf(strs).UnsafePointer()), "an arena slice result must leave as a copy or covered")
	for _, s := range strs {
		require.True(t, stringMayOutliveRelease(arena, s), "the slice's strings must be copies or covered")
	}
	require.Equal(t, heap, results[2], "a heap result is left alone")
	require.Equal(t, 42, results[3])
	require.Nil(t, results[4])
}

func TestCallbackReturnRevokesWhatItKeepsByIdentity(t *testing.T) {
	arena := newPooledArena(t)
	require.False(t, arena.licenceState.epochRevoked, "test harness: a fresh licensed arena has no revocation")
	results := []any{strings.ToUpper}
	materialiseCallbackResults(arena, results)
	require.NotNil(t, results[0])
	require.True(t, arena.licenceState.epochRevoked || len(arena.licenceState.revoked) != 0,
		"a function value result is kept by identity, so the boundary revokes (callback-return: revoke identityKept)")
}

func TestCallbackHostArgumentsWalkOnlyAnUnsharedArena(t *testing.T) {
	t.Run("unshared (top-level run): host containers are detached", func(t *testing.T) {
		arena := newPooledArena(t)
		borrowed := arenaBytesToString(arena, []byte("stored-by-run"))
		slice := []string{borrowed}
		mapped := map[string]string{"k": borrowed}
		holder := &hostContainer{Label: borrowed, Items: []string{borrowed}}
		receiver := &hostContainer{Label: borrowed}
		materialiseCallbackArguments(arena, reflect.ValueOf(receiver), []reflect.Value{reflect.ValueOf(slice), reflect.ValueOf(mapped), reflect.ValueOf(holder)})
		for _, s := range []string{slice[0], mapped["k"], holder.Label, holder.Items[0], receiver.Label} {
			require.Equal(t, "stored-by-run", s)
			require.True(t, stringMayOutliveRelease(arena, s), "the walk must detach or cover every arena string the host values hold")
		}
	})
	t.Run("shared (callback VM): nothing is walked", func(t *testing.T) {
		arena := newPooledArena(t)
		arena.licenceAlwaysShared = true
		borrowed := arenaBytesToString(arena, []byte("stored-by-run"))
		slice := []string{borrowed}
		materialiseCallbackArguments(arena, reflect.Value{}, []reflect.Value{reflect.ValueOf(slice)})
		require.True(t, arena.referencesArenaString(slice[0]) && !arena.licenceState.epochRevoked && !arena.licenceCoveredAt(unsafe.Pointer(unsafe.StringData(slice[0]))),
			"a shared arena skips the walk (design deviation: host memory may be written by other goroutines); the store rows cover what the run stores")
	})
	t.Run("nil arena is a no-op", func(t *testing.T) {
		require.NotPanics(t, func() {
			materialiseCallbackArguments(nil, reflect.Value{}, []reflect.Value{reflect.ValueOf([]string{"x"})})
		})
	})
}

func TestTopLevelCheckFailsInsideTheEngine(t *testing.T) {
	require.False(t, noVMFrameOnGoroutine(), "a call with no reflect entry (here: straight from a test) must fail the check")
}

func trackedCallbackSpecFixture(t *testing.T) (*callbackVMSpec, *RuntimeClosure) {
	t.Helper()
	root := &program.CompiledFunction{}
	root.Functions = []*program.CompiledFunction{root}
	parent := newTestVM(t)
	parent.functions = root.Functions
	parent.rootFunction = root
	parent.Limits.Tracker = &ResourceTracker{}
	closure := &RuntimeClosure{Function: root, RootFunction: root, inlineCells: [inlineUpvalueCells]*program.UpvalueCell{}, upvalues: nil, namedType: 0}
	return newCallbackVMSpec(parent), closure
}

func TestCachedCallbackKeepsAnArenaWhoseEpochIsRevoked(t *testing.T) {
	spec, closure := trackedCallbackSpecFixture(t)

	first := spec.acquire(closure)
	if !callbackReleaseApplies(first.Arena) {
		spec.finish(first, false)
		t.Skip("the callback release does not apply to this build's arenas")
	}
	arena := first.Arena
	arenaString(arena, "revoked-epoch-run")
	require.True(t, arena.holdsEscapableData())
	arena.reentrantHandedOff = true
	arena.revokeEpoch(epochSharedContainer, handOffSharedStore)
	keeps, releases := callbackReleaseCounters.cachedArenaKeeps.Load(), callbackReleaseCounters.cachedArenaReleases.Load()
	spec.finish(first, false)
	require.Same(t, arena, first.Arena, "a revoked epoch's arena is kept by its cached VM")
	require.EqualValues(t, keeps+1, callbackReleaseCounters.cachedArenaKeeps.Load())
	require.Equal(t, releases, callbackReleaseCounters.cachedArenaReleases.Load(), "nothing was released")
	require.True(t, arena.licenceState.epochRevoked, "the kept arena's epoch stays revoked until its release")

	second := spec.acquire(closure)
	require.Same(t, first, second)
	require.Same(t, arena, second.Arena, "the next callback runs on the kept arena")
	require.True(t, second.Arena.disableMinorGC, "the kept arena does not collect while it holds escapable data")
	spec.finish(second, false)

	clean := newTrackedCallbackSpec(t)
	cleanSpec, cleanClosure := clean.spec, clean.closure
	third := cleanSpec.acquire(cleanClosure)
	cleanArena := third.Arena
	arenaString(cleanArena, "clean-epoch-run")
	cleanArena.reentrantHandedOff = true
	keeps, releases = callbackReleaseCounters.cachedArenaKeeps.Load(), callbackReleaseCounters.cachedArenaReleases.Load()
	cleanSpec.finish(third, false)
	require.Nil(t, third.Arena, "a clean handed-off run's arena is released by the ordinary release")
	require.Equal(t, keeps, callbackReleaseCounters.cachedArenaKeeps.Load())
	require.EqualValues(t, releases+1, callbackReleaseCounters.cachedArenaReleases.Load())
}

func newTrackedCallbackSpec(t *testing.T) trackedCallbackSpec {
	t.Helper()
	spec, closure := trackedCallbackSpecFixture(t)
	return trackedCallbackSpec{spec: spec, closure: closure}
}

func topLevelFixture(t *testing.T) (*VM, *RuntimeClosure) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.addIntConst(42)
	builder.intRegisters(1).returnInt()
	builder.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	function := builder.build()
	root := &program.CompiledFunction{}
	root.Functions = []*program.CompiledFunction{root, function}
	maker := newTestVM(t)
	maker.functions = root.Functions
	maker.rootFunction = root
	maker.Limits.Tracker = &ResourceTracker{}
	return maker, &RuntimeClosure{Function: function, RootFunction: root}
}

func TestTopLevelCallAppliesOnceTheHostDeclaresItAndTheExecutionEnded(t *testing.T) {
	maker, closure := topLevelFixture(t)
	spec := newCallbackVMSpec(maker)
	spec.link = newExecutionLink(t.Context(), nil)
	require.False(t, spec.topLevelCallApplies(closure), "the making execution is still running")
	spec.link.ended.Store(true)
	require.False(t, spec.topLevelCallApplies(closure), "the host has not declared its calls top-level")
	closure.RootFunction.DeclareHostTopLevelCalls()
	require.True(t, spec.topLevelCallApplies(closure))
	spec.limits.SafeMode = true
	require.False(t, spec.topLevelCallApplies(closure), "safe mode")
}

func TestDeclaredTopLevelCallRunsAsARootExecution(t *testing.T) {
	maker, closure := topLevelFixture(t)
	closure.RootFunction.DeclareHostTopLevelCalls()
	wrapper, isFunc := reflect.TypeAssert[func() int](makeClosureWrapperFunc(maker, closure, reflect.TypeFor[func() int]()))
	require.True(t, isFunc)
	maker.currentLink().ended.Store(true)

	runs, fallbacks := callbackReleaseCounters.topLevelRuns.Load(), callbackReleaseCounters.topLevelFallbacks.Load()
	fromTheHost := sync.OnceValue(wrapper)
	go fromTheHost()
	require.Eventually(t, func() bool {
		return callbackReleaseCounters.topLevelRuns.Load() != runs || callbackReleaseCounters.topLevelFallbacks.Load() != fallbacks
	}, 5*time.Second, time.Millisecond, "the host's goroutine calls the wrapper")
	require.Equal(t, 42, fromTheHost())
	require.Equal(t, runs+1, callbackReleaseCounters.topLevelRuns.Load(), "a call from host code with no VM below it runs as a root execution")
	require.Equal(t, fallbacks, callbackReleaseCounters.topLevelFallbacks.Load())

	require.Equal(t, 42, wrapper(), "a call from inside the engine takes the callback path")
	require.Equal(t, fallbacks+1, callbackReleaseCounters.topLevelFallbacks.Load())
}

func TestTopLevelVMIsBuiltAndReleasedAsARootExecution(t *testing.T) {
	maker, closure := topLevelFixture(t)
	spec := newCallbackVMSpec(maker)
	vm := spec.buildTopLevel(closure)
	require.True(t, vm.topLevelRun)
	require.Equal(t, DebugThreadCallback, vm.debugKind)
	require.NotNil(t, vm.CallStack)
	vm.Arena = newArena(t)
	arenaString(vm.Arena, "escapable")
	vm.finishTopLevel()
	require.Nil(t, vm.Arena, "the run's arena is released")
}

func TestReacquireCallbackArenaRestoresTheLimits(t *testing.T) {
	vm := new(VM)
	limits := VMLimits{CostBudget: 99}
	vm.reacquireCallbackArena(limits)
	require.Equal(t, limits, vm.Limits)
	require.NotNil(t, vm.CallStack)
}

func TestTopLevelCheckFailsOnADeepStack(t *testing.T) {
	var descend func(depth int) bool
	descend = func(depth int) bool {
		if depth == 0 {
			return noVMFrameOnGoroutine()
		}
		return descend(depth - 1)
	}
	result := make(chan bool, 1)
	go func() { result <- descend(topLevelCheckDepth + 8) }()
	require.False(t, <-result, "a stack deeper than the check reads fails it")
}
