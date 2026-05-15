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
	"context"
	"sync/atomic"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/safeconv"
	"pipit.sh/pipit/internal/symtab"
)

// callbackArenaRetireBytes is the arena allocation volume past which a cached callback VM
// is discarded instead of being returned to its parent's cache.
const callbackArenaRetireBytes = 4 << 20

// callbackVMCache is the single-slot atomic cache of idle callback VMs a parent VM keeps
// for its reflect.MakeFunc wrappers, drained when the parent frees its arena so a wrapper
// that outlives its parent never parks a VM.
type callbackVMCache struct {
	// slot holds the one idle callback VM, or nil.
	slot atomic.Pointer[VM]

	// released is set by drain() and cleared by reopen() at the parent's next execution;
	// while set, acquire() hands out nothing and release() discards.
	released atomic.Bool

	// built counts the callback VMs constructed for this cache, for tests and diagnostics.
	built atomic.Int64

	// discarded counts the callback VMs this cache released through ReleaseArena.
	discarded atomic.Int64
}

// CallbackVMStats reports how a VM's callback cache has been used: how many callback VMs
// its host-callable wrappers built, how many were released, and whether one is idle in
// the cache now. Built minus Discarded is the number of callback VMs still alive (idle or
// running a callback).
type CallbackVMStats struct {
	// Built counts the callback VMs constructed for the VM's wrappers.
	Built int64

	// Discarded counts the callback VMs released for good: retired past
	// VMLimits.CallbackArenaRetireBytes, left dirty by a panic or error, displaced from an
	// occupied cache slot, or drained when the parent released its arena.
	Discarded int64

	// Idle is true while a callback VM is parked in the cache.
	Idle bool
}

// CallbackVMStats returns the VM's callback cache statistics.
//
// Returns the statistics; zero for a VM built without a cache.
func (vm *VM) CallbackVMStats() CallbackVMStats {
	if vm.callbackVMs == nil {
		return CallbackVMStats{Built: 0, Discarded: 0, Idle: false}
	}
	return CallbackVMStats{
		Built:     vm.callbackVMs.built.Load(),
		Discarded: vm.callbackVMs.discarded.Load(),
		Idle:      vm.callbackVMs.slot.Load() != nil,
	}
}

// acquire takes the idle callback VM out of the slot.
//
// Returns the VM, or nil when the slot is empty or the cache has been drained.
func (c *callbackVMCache) acquire() *VM {
	if c.released.Load() {
		return nil
	}
	return c.slot.Swap(nil)
}

// release parks a finished callback VM in the slot, or discards it when the slot is taken
// or the cache has been drained.
//
// The store happens before the released check so that drain and release cannot both miss
// the VM: exactly one of the two always discards it.
//
// Takes vm (*VM) which must be idle (reusableAfterCallback).
func (c *callbackVMCache) release(vm *VM) {
	if !c.slot.CompareAndSwap(nil, vm) {
		c.discard(vm)
		return
	}
	if c.released.Load() {
		if taken := c.slot.Swap(nil); taken != nil {
			c.discard(taken)
		}
	}
}

// drain marks the cache released and discards the idle VM, if any. Called when the parent
// VM releases its arena; a discard releases the callback VM's arena, which drains that
// VM's own cache in turn.
func (c *callbackVMCache) drain() {
	c.released.Store(true)
	if taken := c.slot.Swap(nil); taken != nil {
		c.discard(taken)
	}
}

// reopen clears the released mark so wrappers made in a subsequent execution cache again;
// the parent drains when it releases its arena.
func (c *callbackVMCache) reopen() {
	c.released.Store(false)
}

// discard releases a callback VM for good.
//
// Takes vm (*VM) which this goroutine owns exclusively.
func (c *callbackVMCache) discard(vm *VM) {
	c.discarded.Add(1)
	discardCallbackVM(vm)
}

// discardCallbackVM returns a callback VM's unspent cost chunk and releases its arena.
//
// Takes vm (*VM) which this goroutine owns exclusively.
func discardCallbackVM(vm *VM) {
	vm.ReturnUnspentCostChunk()
	vm.ReleaseArena()
}

// drainCallbackVMs discards the idle callback VM this VM caches, if any, and closes the
// cache until the next execution. Nil-safe for VMs built without a cache.
func (vm *VM) drainCallbackVMs() {
	if vm.callbackVMs != nil {
		vm.callbackVMs.drain()
	}
}

// reopenCallbackVMs lets this VM's wrappers cache callback VMs again at the start of a
// new execution. Nil-safe for VMs built without a cache.
func (vm *VM) reopenCallbackVMs() {
	if vm.callbackVMs != nil {
		vm.callbackVMs.reopen()
	}
}

// reusableAfterCallback reports whether a callback VM finished cleanly enough to serve
// the next callback without a rebuild: a normal return leaves no frame, defer, panic or
// error behind, while stack overflow, cost exhaustion, an uncaught panic or a debugger
// stop leave one of them set.
//
// Returns bool which is true when the VM may go back to the cache.
func (vm *VM) reusableAfterCallback() bool {
	return vm.evalError == nil && vm.FramePointer == -1 && !vm.panicking && vm.panicValue == nil &&
		len(vm.deferStack) == 0 && len(vm.panicDeferFrames) == 0 && vm.recoverEligibleFrame == -1 &&
		!vm.goroutinesLeaked && vm.Arena != nil
}

// callbackVMSpec is what a reflect.MakeFunc wrapper captures from the VM it was made on:
// enough to build a callback VM after that VM is released, and the cache to reuse one.
type callbackVMSpec struct {
	// ctx is the parent's context stripped of cancellation, so a stored wrapper still runs
	// after the parent's execution ended.
	ctx context.Context

	// globals and symbols are the parent's shared stores.
	globals *GlobalStore

	// symbols is the parent's symbol registry, shared by all callback VMs.
	symbols *symtab.SymbolRegistry

	// rootFunction is the parent's root at wrap time, used when the closure records none.
	rootFunction *program.CompiledFunction

	// cache is the parent's callback VM cache, or nil in safe mode, where every callback
	// builds a fresh VM as before: safe mode forces Go dispatch and its pointer-provenance
	// tables are reset only by Execute.
	cache *callbackVMCache

	// parkDone is the wrapping VM's cancellable context's Done channel, so a callback VM
	// parked in the debugger wakes when the parent execution is cancelled even though the
	// callback's own context never is.
	parkDone <-chan struct{}

	// limits are the parent's limits at wrap time.
	limits VMLimits

	// goroutineID is the parent's goroutine identity, inherited by every callback VM.
	goroutineID uint64

	// retireBytes is the arena allocation volume past which a finished callback VM is
	// discarded rather than cached: limits.CallbackArenaRetireBytes, or
	// callbackArenaRetireBytes when that is zero.
	retireBytes int64
}

// newCallbackVMSpec captures vm for the wrappers made on it.
//
// Takes vm (*VM) which is the VM the closure is being wrapped on.
//
// Returns the spec.
func newCallbackVMSpec(vm *VM) *callbackVMSpec {
	spec := &callbackVMSpec{
		ctx:          context.WithoutCancel(vm.ctx),
		globals:      vm.Globals,
		symbols:      vm.symbols,
		limits:       vm.Limits,
		rootFunction: vm.rootFunction,
		goroutineID:  vm.goroutineID,
		parkDone:     vm.ctx.Done(),
		cache:        vm.callbackVMs,
		retireBytes:  callbackArenaRetireBytes,
	}
	if spec.limits.CallbackArenaRetireBytes != 0 {
		spec.retireBytes = safeconv.Uint64ToInt64(spec.limits.CallbackArenaRetireBytes)
	}
	if spec.limits.SafeMode {
		spec.cache = nil
	}
	return spec
}

// acquire returns a VM ready to run closure: the parent's idle callback VM when the cache
// holds one, prepared for this closure, or a freshly built one.
//
// Takes closure (*RuntimeClosure) which is about to run.
//
// Returns the VM; the caller must pass it to finish() when the callback returns.
func (spec *callbackVMSpec) acquire(closure *RuntimeClosure) *VM {
	if spec.cache != nil {
		if cached := spec.cache.acquire(); cached != nil {
			cached.debugParkDone = spec.parkDone
			cached.prepareForCallback(closure, spec.rootFunction, spec.limits)
			return cached
		}
	}
	return spec.build(closure)
}

// build constructs a fresh callback VM for closure with its own arena and call stack.
//
// Takes closure (*RuntimeClosure) which is about to run.
//
// Returns the VM.
func (spec *callbackVMSpec) build(closure *RuntimeClosure) *VM {
	freshVM := NewVM(spec.ctx, spec.globals, spec.symbols)
	freshVM.reentrantInterpreterVM = true
	freshVM.goroutineID = spec.goroutineID
	freshVM.hasGoroutines = true
	freshVM.debugKind = DebugThreadCallback
	freshVM.debugParkDone = spec.parkDone
	freshVM.Limits = spec.limits
	freshVM.adoptClosureRoot(closure, spec.rootFunction)
	freshVM.EnsureCallStack()
	freshVM.initialiseASMDispatch()
	if spec.cache != nil {
		spec.cache.built.Add(1)
	}
	return freshVM
}

// finish returns a callback VM once its callback has run: to the cache when it finished
// cleanly and its arena is still below the retire threshold, otherwise through the
// release path that abandons its escaped slabs.
//
// Takes callbackVM (*VM) which acquire() returned.
func (spec *callbackVMSpec) finish(callbackVM *VM) {
	if spec.cache == nil {
		discardCallbackVM(callbackVM)
		return
	}
	if !callbackVM.reusableAfterCallback() || callbackVM.Arena.bytesAllocated >= spec.retireBytes {
		spec.cache.discard(callbackVM)
		return
	}
	callbackVM.ReturnUnspentCostChunk()
	spec.cache.release(callbackVM)
}

// prepareForCallback resets per-run state on a cached callback VM and adopts closure's
// root function.
//
// Takes closure (*RuntimeClosure) which is about to run on vm.
// Takes wrapperRoot (*program.CompiledFunction) which is the wrapping VM's root, used
// when the closure records none.
// Takes limits (VMLimits) which are the wrapper's limits.
func (vm *VM) prepareForCallback(closure *RuntimeClosure, wrapperRoot *program.CompiledFunction, limits VMLimits) {
	vm.liveCtx = nil
	vm.evalResult = nil
	vm.evalError = nil
	vm.EvalAllResults = nil
	vm.inlineDispatchUintResult = 0
	vm.inlineDispatchExpectUintResult = false
	vm.checkpointFlags = 0
	vm.panicFrames = nil
	vm.panicUnwound = nil
	vm.callbackPanicStack = ""
	vm.baseFramePointer = 0
	vm.deferStack = vm.deferStack[:0]
	vm.debugKind = DebugThreadCallback
	vm.applyCallbackLimits(limits)

	root := closure.RootFunction
	if root == nil {
		root = wrapperRoot
	}
	if root == nil || root == vm.rootFunction {
		return
	}
	vm.closureCache = nil
	vm.dropRuntimeCallInfoFills()
	vm.adoptClosureRoot(closure, wrapperRoot)
}

// applyCallbackLimits installs limits on a cached callback VM and its arena.
//
// The minor collector is disabled when the arena holds escapable data, because a value an
// earlier callback handed to the host may still point into arena slabs without being
// rooted by any frame.
//
// Takes limits (VMLimits) which are the wrapper's limits.
func (vm *VM) applyCallbackLimits(limits VMLimits) {
	vm.Limits = limits
	arena := vm.Arena
	arena.MaxArenaBytes = limits.MaxArenaBytes
	arena.MaxAllocSize = limits.MaxAllocSize
	arena.disableMinorGC = limits.DisableMinorGC || arena.holdsEscapableData()
	if arena.budgetTracker != limits.Tracker {
		arena.AttachBudgetTracker(limits.Tracker)
	}
}
