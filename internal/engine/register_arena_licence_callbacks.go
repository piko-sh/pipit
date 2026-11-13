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
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
)

const (
	// topLevelCheckDepth bounds the frames the top-level check reads; a deeper stack fails
	// the check.
	topLevelCheckDepth = 128

	// engineFramePrefix names the engine package's functions in a stack trace.
	engineFramePrefix = "pipit.sh/pipit/internal/engine."

	// topLevelEntryFrame and wrapperEntryFramePrefix identify runTopLevel and the closure
	// wrapper that calls it, whose entry frames the top-level check skips.
	topLevelEntryFrame = engineFramePrefix + "(*callbackVMSpec).runTopLevel"

	// wrapperEntryFramePrefix identifies closure-wrapper frames in Go stack traces,
	// including the MakeFunc implementation (*closureWrapper).reflectCall and its
	// method-value wrapper, the typed adapters (*closureWrapper).typedInspect.funcN, and the
	// shared body (*closureWrapper).call.
	wrapperEntryFramePrefix = engineFramePrefix + "(*closureWrapper)."

	// reflectFramePrefix names reflect's frames, through which a wrapper may be entered.
	reflectFramePrefix = "reflect."
)

var (
	// servedStores counts served stores kept uncloned, for tests and diagnostics.
	servedStores atomic.Int64

	// servedUnshared counts served stores refused because the serving VM's walks would not
	// be shared.
	servedUnshared atomic.Int64
)

// ServedStoreStats counts served stores since the process started.
type ServedStoreStats struct {
	// Stores counts strings of the served arena stored uncloned into its storage.
	Stores int64

	// Unshared counts served stores refused because the serving VM walks unshared.
	Unshared int64
}

var (
	// parkedCallerCounters counts parked-caller walks while the licence counters are on.
	parkedCallerCounters struct {
		parkedWalks, parkedReads, fallbacks, declined atomic.Int64
	}
)

// ParkedCallerStats counts parked-caller walks since the process started.
type ParkedCallerStats struct {
	// ParkedWalks counts callback walks that read their parked caller's storage and kept the
	// result; ParkedReads the reads of that storage they made.
	ParkedWalks, ParkedReads int64

	// Fallbacks counts walks that read the caller's storage but found the count or its
	// generation changed at their end, and stopped instead.
	Fallbacks int64

	// Declined counts shared callback walks that could not use the caller's storage.
	Declined int64
}

// callbackReleaseCounters counts callback releases and declared top-level runs, for tests
// and diagnostics.
var callbackReleaseCounters struct {
	callbackRuns, boundRuns, cachedArenaReleases, topLevelRuns, topLevelFallbacks atomic.Int64

	// cachedArenaKeeps counts handed-off cached callback arenas kept instead of released
	// because their epoch was already revoked (callbackReleaseRecycles).
	cachedArenaKeeps atomic.Int64
}

// CallbackReleaseStats counts callback releases and declared top-level runs since the
// process started.
type CallbackReleaseStats struct {
	// CallbackRuns counts callback runs whose results and arguments passed the hand-off
	// boundaries, so their release needs no epoch revocation.
	CallbackRuns int64

	// BoundRuns counts bound-method runs that did the same.
	BoundRuns int64

	// CachedArenaReleases counts cached callback arenas released after their run.
	CachedArenaReleases int64

	// TopLevelRuns counts declared top-level calls run as root executions.
	TopLevelRuns int64

	// TopLevelFallbacks counts declared top-level calls whose run-time check failed, run as
	// callback VMs instead.
	TopLevelFallbacks int64
}

// servedStoreStatistics returns the served-store counters.
//
// Returns ServedStoreStats which is a snapshot.
func servedStoreStatistics() ServedStoreStats {
	return ServedStoreStats{Stores: servedStores.Load(), Unshared: servedUnshared.Load()}
}

// parkedCallerStatistics returns the parked-caller counters, which move only while the
// licence counters are on (SetLicenceCountingForTest).
//
// Returns ParkedCallerStats which is a snapshot.
func parkedCallerStatistics() ParkedCallerStats {
	return ParkedCallerStats{
		ParkedWalks: parkedCallerCounters.parkedWalks.Load(), ParkedReads: parkedCallerCounters.parkedReads.Load(),
		Fallbacks: parkedCallerCounters.fallbacks.Load(), Declined: parkedCallerCounters.declined.Load(),
	}
}

// parkedCallerBegin counts a callback that may reach arena's storage, at its
// registration.
//
// Takes arena (*RegisterArena) which is the served or making VM's arena.
func parkedCallerBegin(arena *RegisterArena) {
	state := &arena.licenceState
	state.cbStarts.Add(1)
	state.cbActive.Add(1)
}

// parkedCallerEnd uncounts a callback parkedCallerBegin counted.
//
// Takes arena (*RegisterArena) which is the arena parkedCallerBegin counted on.
func parkedCallerEnd(arena *RegisterArena) {
	arena.licenceState.cbActive.Add(-1)
}

// registerParkedCaller counts the callback vm on the arena of the VM it serves on its
// goroutine, below, and on its maker's arena, once each, at the start of its registered
// run.
//
// Takes below (*VM) which is the VM registered below it on the goroutine, or nil.
func (vm *VM) registerParkedCaller(below *VM) {
	state := &vm.servedChainRun
	if below != nil && below.Arena != nil {
		state.parkedServed = below.Arena
		parkedCallerBegin(below.Arena)
	}
	if maker := vm.parkedMaker.Value(); maker != nil && maker != state.parkedServed {
		state.parkedMade = maker
		parkedCallerBegin(maker)
	}
}

// unregisterParkedCaller uncounts what registerParkedCaller counted, at the end of the
// registered run.
func (vm *VM) unregisterParkedCaller() {
	state := &vm.servedChainRun
	if state.parkedServed != nil {
		parkedCallerEnd(state.parkedServed)
		state.parkedServed = nil
	}
	if state.parkedMade != nil {
		parkedCallerEnd(state.parkedMade)
		state.parkedMade = nil
	}
}

// callbackReleaseStatistics returns the callback release and top-level call counters.
//
// Returns CallbackReleaseStats which is a snapshot.
func callbackReleaseStatistics() CallbackReleaseStats {
	return CallbackReleaseStats{
		CallbackRuns:        callbackReleaseCounters.callbackRuns.Load(),
		BoundRuns:           callbackReleaseCounters.boundRuns.Load(),
		CachedArenaReleases: callbackReleaseCounters.cachedArenaReleases.Load(),
		TopLevelRuns:        callbackReleaseCounters.topLevelRuns.Load(),
		TopLevelFallbacks:   callbackReleaseCounters.topLevelFallbacks.Load(),
	}
}

// callbackReleaseApplies reports whether a cleanly finished callback or bound-method run
// may release its arena without revoking the epoch.
//
// The arena must be licensed and the build must provide exact probes.
//
// Takes arena (*RegisterArena) which may be nil.
//
// Returns bool which is true when a clean re-entrant run may be released without revoking
// its epoch.
func callbackReleaseApplies(arena *RegisterArena) bool {
	return arenaUsesUnsafeSlabs && licenceExactProbes && arena != nil && arena.licensed
}

// handOffReentrantRun passes a cleanly finished callback, bound-method or top-level run's
// results and host arguments through the hand-off boundaries, and marks the arena
// released by the ordinary release. A run that did not finish cleanly, or started a
// goroutine, is left unmarked, so its release revokes the epoch.
//
// Takes results ([]any) which are the run's results, materialised in place.
// Takes receiver (reflect.Value) which is a bound method's receiver, or invalid.
// Takes arguments ([]reflect.Value) which are the host's arguments.
//
// Returns bool which is true when the run was marked.
func (vm *VM) handOffReentrantRun(results []any, receiver reflect.Value, arguments []reflect.Value) bool {
	arena := vm.Arena
	rootMode := vm.topLevelRun && arena != nil && arena.licensed
	if !rootMode && !callbackReleaseApplies(arena) || vm.callbackSpawned || !vm.reusableAfterCallback() {
		return false
	}
	materialiseCallbackResults(arena, results)
	materialiseCallbackArguments(arena, receiver, arguments)
	arena.reentrantHandedOff = !arena.aborted
	return arena.reentrantHandedOff
}

// materialiseCallbackResults is the callback-return boundary of the hand-off table.
//
// Each result a callback or bound-method run hands its native caller is copied as the
// host return copies it, and what the copy keeps by identity is revoked (copy-then-revoke
// on route host-return).
//
// A pointer-free result needs no walk; it is copied out only when its own storage lies in
// the arena, since the caller reads it after the release.
//
// Takes arena (*RegisterArena) which ran the guest code.
// Takes results ([]any) which are the results, replaced in place by what may leave.
func materialiseCallbackResults(arena *RegisterArena, results []any) {
	var state *censusVMState
	was := false
	if storeCensusBuilt {
		state, was = censusEnterBoundary(arena)
	}
	for index, result := range results {
		results[index] = materialiseCallbackResult(arena, result)
	}
	if storeCensusBuilt {
		censusNoteCallbackReturn(arena, results)
		censusLeaveBoundary(state, was)
	}
}

// materialiseCallbackResult is materialiseCallbackResults for one result.
//
// Takes arena (*RegisterArena) which ran the guest code.
// Takes result (any) which is the result.
//
// Returns any which is what may leave the arena.
func materialiseCallbackResult(arena *RegisterArena, result any) any {
	if result == nil {
		return nil
	}
	value := reflect.ValueOf(result)
	if !typeIsPointerFree(value.Type()) {
		return materialiseAnyForArena(arena, result)
	}
	if !arena.interfaceSharesArenaStorage(value) {
		return result
	}
	fresh := reflect.New(value.Type())
	fresh.Elem().Set(value)
	return unsafeReadOnlyValue(reflectValueABIType(value.Type()), fresh.UnsafePointer(), value.Kind()).Interface()
}

// materialiseCallbackArguments is the callback host-argument boundary.
//
// The host values a callback or bound-method run received (its arguments and receiver)
// are walked as the host-argument walk walks a root execution's, so no host container
// keeps a reference into the arena.
//
// Takes arena (*RegisterArena) which ran the guest code.
// Takes receiver (reflect.Value) which is a bound method's receiver, or invalid.
// Takes arguments ([]reflect.Value) which are the host's arguments.
func materialiseCallbackArguments(arena *RegisterArena, receiver reflect.Value, arguments []reflect.Value) {
	if arena == nil || arena.licenceShared(0) {
		return
	}
	budget := hostMaterialiseLeafBudget
	if receiver.IsValid() {
		materialiseHostValueInPlace(arena, receiver, hostMaterialiseDepth, &budget)
	}
	for _, argument := range arguments {
		materialiseHostValueInPlace(arena, argument, hostMaterialiseDepth, &budget)
	}
}

// callbackReleaseRecycles is the cost guard of callback release for cached callback
// arenas.
//
// It reports whether the ordinary release of a handed-off run's arena can recycle
// anything.
//
// Takes arena (*RegisterArena) which ran a handed-off callback.
//
// Returns bool which is false when the epoch is revoked.
func callbackReleaseRecycles(arena *RegisterArena) bool {
	return !arena.licenceState.epochRevoked
}

// releaseCachedCallbackArena releases a cached callback VM's arena by the ordinary
// licensed release once its run was handed off, so the next callback starts on a fresh
// arena (acquired again by prepareForCallback) and the arena never grows across
// callbacks.
func (vm *VM) releaseCachedCallbackArena() {
	arena := vm.Arena
	vm.CallStack = nil
	vm.asmCallInfoBases = nil
	vm.asmDispatchSaves = nil
	vm.Arena = nil
	if licenceCounting {
		callbackReleaseCounters.cachedArenaReleases.Add(1)
	}
	releaseRegisterArena(vm.Limits.abandonsArenas(), arena, true)
}

// reacquireCallbackArena gives a cached callback VM whose arena was released after its
// last run a fresh one.
//
// Takes limits (VMLimits) which are the wrapper's limits.
func (vm *VM) reacquireCallbackArena(limits VMLimits) {
	vm.Limits = limits
	vm.EnsureCallStack()
	vm.initialiseASMDispatch()
}

// topLevelCallApplies reports whether a call of an orphan wrapper may run as a root
// execution.
//
// Takes closure (*RuntimeClosure) which is the wrapped closure.
//
// Returns bool which is true when the call is declared top-level.
func (spec *callbackVMSpec) topLevelCallApplies(closure *RuntimeClosure) bool {
	if !arenaUsesUnsafeSlabs || !licenceExactProbes || spec.limits.SafeMode ||
		spec.limits.abandonsArenas() || spec.link.live() {
		return false
	}
	return closure.RootFunction.HostCallsTopLevel() || spec.rootFunction.HostCallsTopLevel()
}

// noVMFrameOnGoroutine is the runtime check of the first half of the host's top-level
// declaration.
//
// No VM is running on the calling goroutine, so no VM's native frame can be below this
// call.
//
// Returns bool which is true when the call may run as a root execution.
func noVMFrameOnGoroutine() bool {
	var pcs [topLevelCheckDepth]uintptr
	count := runtime.Callers(2, pcs[:])
	if count == len(pcs) {
		return false
	}
	frames := runtime.CallersFrames(pcs[:count])
	entry, below := true, 0
	for {
		frame, more := frames.Next()
		name := frame.Function
		if entry && (name == topLevelEntryFrame || strings.HasPrefix(name, wrapperEntryFramePrefix) || strings.HasPrefix(name, reflectFramePrefix)) {
			if !more {
				return false
			}
			continue
		}
		entry = false
		if strings.HasPrefix(name, engineFramePrefix) {
			return false
		}
		below++
		if !more {
			return below > 0
		}
	}
}

// runTopLevel runs a declared top-level call of an orphan wrapper as a root execution,
// when the run-time check holds.
//
// Takes closure (*RuntimeClosure) which is the wrapped closure.
// Takes arguments ([]reflect.Value) which are the host's arguments.
// Takes functionType (reflect.Type) which is the wrapper's signature.
// Takes sink (*callbackResultSink) which receives the results.
//
// Returns bool which is false when the call must run as a callback VM instead.
//
// Panics when a guest panic must cross an active native frame or an execution stop must
// propagate through its native caller.
func (spec *callbackVMSpec) runTopLevel(closure *RuntimeClosure, arguments []reflect.Value, functionType reflect.Type, sink *callbackResultSink) bool {
	if !spec.topLevelCallApplies(closure) {
		return false
	}
	if !noVMFrameOnGoroutine() {
		if licenceCounting {
			callbackReleaseCounters.topLevelFallbacks.Add(1)
		}
		return false
	}
	if licenceCounting {
		callbackReleaseCounters.topLevelRuns.Add(1)
	}
	vm := spec.buildTopLevel(closure)
	vm.bindCallbackRun(spec.link, spec.limits.CallbackTimeout)
	vm.runGates = spec.gates
	defer vm.finishTopLevel()
	restoreUnwindSlice := vm.adoptAbortUnwindSliceIfNested()
	defer restoreUnwindSlice()
	raw := vm.callClosureRaw(closure, arguments)
	if vm.evalError != nil {
		if vm.panicValue != nil && spec.globals.dispatchDepth.Load() > 0 {
			panic(wrapCallbackPanic(vm))
		}
		if crossesNativeFrame(vm.evalError, spec.globals) {
			panic(vm.evalError)
		}
		failOnCallbackStop(spec.link, functionType, vm.evalError)
		sink.fromValues(buildClosureErrorReturns(spec.link.detached, functionType, vm.evalError))
		return true
	}
	sink.fromRaw(raw)
	return true
}

// buildTopLevel constructs a VM for a declared top-level call using the callback
// construction path.
//
// Takes closure (*RuntimeClosure) which is about to run.
//
// Returns *VM which is the VM.
func (spec *callbackVMSpec) buildTopLevel(closure *RuntimeClosure) *VM {
	vm := NewVM(spec.link.detached, spec.globals, spec.symbols)
	vm.topLevelRun = true
	vm.goroutineID = spec.goroutineID
	vm.debugKind = DebugThreadCallback
	vm.debugParkDone = spec.parkDone
	vm.Limits = spec.limits
	vm.adoptClosureRoot(closure, spec.rootFunction)
	vm.EnsureCallStack()
	vm.initialiseASMDispatch()
	return vm
}

// finishTopLevel releases a top-level run's VM.
func (vm *VM) finishTopLevel() {
	vm.finishRun()
	vm.ReturnUnspentCostChunk()
	if arena := vm.Arena; arena != nil && arena.licensed && !arena.reentrantHandedOff && arena.holdsEscapableData() {
		arena.revokeEpoch(epochReentrant, noHandOffCause)
	}
	vm.ReleaseArena()
	vm.FinishWatcher()
	vm.recycle()
}

// keepsBoundArena reports whether a bound-method invocation that completed may leave its
// arena for the next.
//
// Takes childVM (*VM) which ran the invocation.
// Takes arena (*RegisterArena) which is its arena.
//
// Returns bool which is true when the arena may be kept.
func (b *boundMethodVM) keepsBoundArena(childVM *VM, arena *RegisterArena) bool {
	return !arenaAuditEnabled && arena != nil && arena.licensed &&
		!b.limits.abandonsArenas() && !arena.aborted && !childVM.callbackSpawned &&
		!childVM.goroutinesLeaked && !childVM.unwindLeaked.Load() &&
		!arena.licenceState.banksRevoked && arena.licenceCoverEmpty() && !arena.holdsEscapableData()
}

// parkBoundArena puts arena in the VM's one-slot bound-arena cache.
//
// Takes arena (*RegisterArena) which keepsBoundArena accepted.
//
// Returns bool which is false when the cache refused it, which the caller then releases.
func (vm *VM) parkBoundArena(arena *RegisterArena) bool {
	cache := vm.callbackVMs
	if cache == nil || cache.released.Load() {
		return false
	}
	arena.setLicenceVM(nil)
	if !vm.boundArena.CompareAndSwap(nil, arena) {
		return false
	}
	if cache.released.Load() {
		vm.drainBoundArena()
	}
	return true
}

// takeBoundArena takes the VM's kept bound-method arena, if any.
//
// Returns *RegisterArena which is the arena.
// Returns bool which is true when there was one.
func (vm *VM) takeBoundArena() (*RegisterArena, bool) {
	if cache := vm.callbackVMs; cache == nil || cache.released.Load() {
		return nil, false
	}
	arena := vm.boundArena.Swap(nil)
	return arena, arena != nil
}

// drainBoundArena releases the VM's kept bound-method arena, if any.
func (vm *VM) drainBoundArena() {
	if arena := vm.boundArena.Swap(nil); arena != nil {
		releaseRegisterArena(false, arena, true)
	}
}
