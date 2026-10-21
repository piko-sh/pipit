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
	"sync/atomic"
	"time"
	"weak"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/engine/program"
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

// callbackVMSpec captures the state needed to build or reuse a callback VM without
// keeping the originating VM alive.
type callbackVMSpec struct {
	// link identifies the execution in which the callback wrapper was created.
	link *executionLink

	// globals is the parent's shared global store.
	globals *GlobalStore

	// symbols is the parent's symbol registry, shared by all callback VMs.
	symbols *symtab.SymbolRegistry

	// rootFunction is the parent's root at wrap time, used when the closure records none.
	rootFunction *program.CompiledFunction

	// cache is the parent's callback VM cache, or nil in safe mode, where every callback
	// builds a fresh VM as before: safe mode forces Go dispatch and its pointer-provenance
	// tables are reset only by Execute.
	cache *callbackVMCache

	// parkDone is the execution context's Done channel, so a callback VM parked in the
	// debugger wakes when the execution is cancelled.
	parkDone <-chan struct{}

	// makerArena names the arena of the VM that made the wrappers, for its count of active
	// callbacks (parked callers); a nil pointer in an arena without a licence.
	makerArena weak.Pointer[RegisterArena]

	// gates are the export gates each run of the wrapped closure holds, which are those the
	// VM that made the wrappers had published when it made them (VM.exportedGates).
	gates exportGateSet

	// limits are the parent's limits at wrap time.
	limits VMLimits

	// goroutineID is the parent's goroutine identity, inherited by every callback VM.
	goroutineID uint64

	// retireBytes is the arena allocation volume past which a finished callback VM is
	// discarded rather than cached: limits.CallbackArenaRetireBytes, or
	// callbackArenaRetireBytes when that is zero.
	retireBytes int64
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
		c.retire(vm)
		return
	}
	if c.released.Load() {
		if taken := c.slot.Swap(nil); taken != nil {
			c.retire(taken)
		}
	}
}

// drain marks the cache released and discards the idle VM, if any. Called when the parent
// VM releases its arena; a discard releases the callback VM's arena, which drains that
// VM's own cache in turn.
func (c *callbackVMCache) drain() {
	c.released.Store(true)
	if taken := c.slot.Swap(nil); taken != nil {
		c.retire(taken)
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

// retire releases a clean callback VM for good, as discard does, and then recycles its
// shell for NewVM unless something outside its runs still holds it.
//
// Takes vm (*VM) which this goroutine owns exclusively and which finished cleanly
// (reusableAfterCallback).
func (c *callbackVMCache) retire(vm *VM) {
	c.discard(vm)
	vm.recycle()
}

// discardCallbackVM returns a callback VM's unspent cost chunk and releases its arena.
//
// Takes vm (*VM) which this goroutine owns exclusively.
func discardCallbackVM(vm *VM) {
	vm.ReturnUnspentCostChunk()
	vm.ReleaseArena()
}

// drainCallbackVMs ends the execution, discards its idle callback VM and closes the cache
// until the next execution.
func (vm *VM) drainCallbackVMs() {
	vm.endExecution()
	if vm.callbackVMs != nil {
		vm.callbackVMs.drain()
	}
	vm.drainBoundArena()
}

// beginExecution starts or reuses the VM's execution and reopens its callback cache.
//
// Takes fresh (bool) which is true to replace an execution the VM owns.
func (vm *VM) beginExecution(fresh bool) {
	if !fresh && vm.ownsLink && (vm.link == nil || (vm.link.live() && vm.link.ctx == vm.ctx)) {
		vm.reopenCallbackVMCache()
		return
	}
	vm.endExecution()
	vm.link = nil
	vm.ownsLink = true
	vm.spawnCtx = vm.ctx
	vm.reopenCallbackVMCache()
}

// endExecution ends the execution the VM owns, if any. A VM that shares another's link
// leaves it alone.
func (vm *VM) endExecution() {
	if !vm.ownsLink {
		return
	}
	if vm.link != nil {
		vm.link.end()
	}
	vm.ownsLink = false
}

// currentLink lazily creates the VM's execution link, which is live during an owned
// execution and detached otherwise.
//
// Returns *executionLink which is never nil.
func (vm *VM) currentLink() *executionLink {
	if vm.link == nil {
		if vm.ownsLink {
			vm.link = newExecutionLink(vm.ctx, vm.executionCancel)
		} else {
			vm.link = endedExecutionLink(vm.ctx)
		}
	}
	return vm.link
}

// reopenCallbackVMCache lets this VM's wrappers cache callback VMs again. Nil-safe for
// VMs built without a cache.
func (vm *VM) reopenCallbackVMCache() {
	if vm.callbackVMs != nil {
		vm.callbackVMs.reopen()
	}
}

// reusableAfterCallback reports whether a callback VM finished cleanly enough to serve
// the next callback without a rebuild.
//
// An abort the callback recovered from leaves its arena marked aborted, which must be
// released rather than reused.
//
// Returns bool which is true when the VM may go back to the cache.
func (vm *VM) reusableAfterCallback() bool {
	return vm.evalError == nil && vm.FramePointer == -1 && !vm.panicking && vm.panicValue == nil &&
		len(vm.deferStack) == 0 && len(vm.panicDeferFrames) == 0 && vm.recoverEligibleFrame == -1 &&
		!vm.goroutinesLeaked && vm.Arena != nil && !vm.Arena.aborted && !vm.unwindLeaked.Load()
}

// newCallbackVMSpec captures vm for the wrappers made on it.
//
// Takes vm (*VM) which is the VM the closure is being wrapped on.
//
// Returns the spec.
func newCallbackVMSpec(vm *VM) *callbackVMSpec {
	vm.pinIdentity()
	link := vm.currentLink()
	spec := &callbackVMSpec{
		link:         link,
		globals:      vm.Globals,
		symbols:      vm.symbols,
		limits:       vm.Limits,
		rootFunction: vm.rootFunction,
		goroutineID:  vm.goroutineID,
		parkDone:     link.ctx.Done(),
		cache:        vm.callbackVMs,
		retireBytes:  callbackArenaRetireBytes,
		makerArena:   weak.Pointer[RegisterArena]{},
		gates:        vm.exportedGates(),
	}
	if spec.limits.CallbackArenaRetireBytes != 0 {
		spec.retireBytes = safeconv.Uint64ToInt64(spec.limits.CallbackArenaRetireBytes)
	}
	if vm.Arena != nil && vm.Arena.licensed {
		spec.makerArena = weak.Make(vm.Arena)
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
			cached.bindCallbackRun(spec.link, spec.limits.CallbackTimeout)
			cached.runGates = spec.gates
			return cached
		}
	}
	built := spec.build(closure)
	built.bindCallbackRun(spec.link, spec.limits.CallbackTimeout)
	built.runGates = spec.gates
	return built
}

// bindCallbackRun connects a callback VM to its execution link and sets its deadline.
// Context cancellation is checked by polling without a watcher goroutine.
//
// Takes link (*executionLink) which is the execution the run belongs to.
// Takes timeout (time.Duration) which is VMLimits.CallbackTimeout; zero or negative for
// none.
func (vm *VM) bindCallbackRun(link *executionLink, timeout time.Duration) {
	ctx := link.callbackContext()
	vm.link = link
	vm.ownsLink = false
	vm.ctx = ctx
	vm.ctxDone = ctx.Done()
	vm.spawnCtx = link.spawnContext()
	vm.Cancelled.Store(0)
	vm.pollCause = nil
	vm.blockCtx = nil
	vm.blockCancel = nil
	vm.callbackDeadline = time.Time{}
	if vm.Arena != nil {
		vm.Arena.callbackRoot = vm.rootFunction
	}
	if timeout > 0 {
		vm.callbackDeadline = time.Now().Add(timeout)
	}
	select {
	case <-vm.ctxDone:
		vm.Cancelled.Store(1)
	default:
	}
}

// finishRun releases what a callback or bound-method run held for its blocking waits.
func (vm *VM) finishRun() {
	if vm.blockCancel != nil {
		vm.blockCancel()
	}
	vm.blockCtx = nil
	vm.blockCancel = nil
}

// build constructs a fresh callback VM for closure with its own arena and call stack.
//
// Takes closure (*RuntimeClosure) which is about to run.
//
// Returns the VM.
func (spec *callbackVMSpec) build(closure *RuntimeClosure) *VM {
	freshVM := NewVM(spec.link.detached, spec.globals, spec.symbols)
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

// finish returns a callback VM after its callback has run.
//
// Takes callbackVM (*VM) which acquire() returned.
// Takes registered (bool) which is true when the wrapper registered callbackVM on its
// goroutine through enterServedChain. The VM stays registered during arena release so
// blind hand-offs can name the served chain. It leaves the chain before being retired or
// parked, after which another goroutine may take it.
func (spec *callbackVMSpec) finish(callbackVM *VM, registered bool) {
	leave := func() {
		if registered {
			registered = false
			callbackVM.leaveServedChain()
		}
	}
	defer leave()
	callbackVM.finishRun()
	if spec.cache == nil {
		discardCallbackVM(callbackVM)
		return
	}
	if !callbackVM.reusableAfterCallback() {
		spec.cache.discard(callbackVM)
		return
	}
	handedOff := callbackVM.Arena.reentrantHandedOff
	if handedOff && !callbackReleaseRecycles(callbackVM.Arena) {
		if licenceCounting {
			callbackReleaseCounters.cachedArenaKeeps.Add(1)
		}
		handedOff = false
	}
	if spec.limits.abandonsArenas() || callbackVM.identityPinned.Load() ||
		!handedOff && callbackVM.Arena.bytesAllocated >= spec.retireBytes {
		leave()
		spec.cache.retire(callbackVM)
		return
	}
	if handedOff && callbackVM.Arena.holdsEscapableData() {
		callbackVM.releaseCachedCallbackArena()
	}
	callbackVM.ReturnUnspentCostChunk()
	leave()
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
	vm.evalSingle = false
	vm.inlineDispatchUintResult = 0
	vm.inlineDispatchExpectUintResult = false
	vm.checkpointFlags = 0
	vm.panicFrames = nil
	vm.panicUnwound = nil
	vm.callbackPanicStack = ""
	clear(vm.methodValueClasses)
	vm.baseFramePointer = 0
	vm.deferStack = vm.deferStack[:0]
	vm.debugKind = DebugThreadCallback
	if vm.Arena == nil {
		vm.reacquireCallbackArena(limits)
	}
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
	arena.setRunBudget(limits.MaxArenaBytes)
	arena.MaxAllocSize = limits.MaxAllocSize
	arena.disableMinorGC = limits.DisableMinorGC || arena.holdsEscapableData()
	if arena.budgetTracker != limits.Tracker {
		arena.AttachBudgetTracker(limits.Tracker)
	}
}
