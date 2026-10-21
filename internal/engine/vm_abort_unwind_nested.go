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
	"bytes"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"pipit.sh/pipit/internal/isa"
)

// abortUnwinderStacks maps goroutine identities to their unwinding VM stacks.
//
// Each stack begins with the unwinding VM and includes callback VMs that adopted its
// slice.
var abortUnwinderStacks struct {
	byGoID map[uint64][]*VM
	mu     sync.Mutex
}

// abortUnwindersActive counts the registered entries, so the common case, no unwinding
// anywhere, costs one atomic load per callback.
var abortUnwindersActive atomic.Int64

// currentGoroutineID returns the id the runtime reports for the calling goroutine. It
// parses the first line of the goroutine's stack trace and runs only while an unwinding
// is registered.
//
// Returns uint64 which is the id, or 0 when it cannot be read.
func currentGoroutineID() uint64 {
	var buffer [64]byte
	line := buffer[:runtime.Stack(buffer[:], false)]
	line = bytes.TrimPrefix(line, []byte("goroutine "))
	if end := bytes.IndexByte(line, ' '); end > 0 {
		line = line[:end]
	}
	id, err := strconv.ParseUint(string(line), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// registerAbortUnwinder records that vm unwinds on the calling goroutine.
//
// Takes vm (*VM) which is the unwinding VM.
//
// Returns func() which removes the record.
func registerAbortUnwinder(vm *VM) func() {
	id := currentGoroutineID()
	if id == 0 {
		return func() {}
	}
	abortUnwinderStacks.mu.Lock()
	if abortUnwinderStacks.byGoID == nil {
		abortUnwinderStacks.byGoID = make(map[uint64][]*VM)
	}
	abortUnwinderStacks.byGoID[id] = append(abortUnwinderStacks.byGoID[id], vm)
	abortUnwinderStacks.mu.Unlock()
	abortUnwindersActive.Add(1)
	return func() {
		abortUnwinderStacks.mu.Lock()
		stack := abortUnwinderStacks.byGoID[id]
		if len(stack) <= 1 {
			delete(abortUnwinderStacks.byGoID, id)
		} else {
			abortUnwinderStacks.byGoID[id] = stack[:len(stack)-1]
		}
		abortUnwinderStacks.mu.Unlock()
		abortUnwindersActive.Add(-1)
	}
}

// abortUnwinderOnThisGoroutine returns the innermost VM unwinding on the calling
// goroutine, or nil.
//
// Returns *VM which is that VM, or nil.
func abortUnwinderOnThisGoroutine() *VM {
	if abortUnwindersActive.Load() == 0 {
		return nil
	}
	id := currentGoroutineID()
	abortUnwinderStacks.mu.Lock()
	defer abortUnwinderStacks.mu.Unlock()
	stack := abortUnwinderStacks.byGoID[id]
	if len(stack) == 0 {
		return nil
	}
	return stack[len(stack)-1]
}

// adoptAbortUnwindSliceIfNested makes vm, a callback or bound-method VM about to run,
// part of the defer slice of a VM unwinding on this goroutine, if there is one.
//
// Returns func() which settles the slice and restores vm; a no-op when nothing unwinds.
func (vm *VM) adoptAbortUnwindSliceIfNested() func() {
	parent := abortUnwinderOnThisGoroutine()
	if parent == nil || parent == vm {
		return func() {}
	}
	return vm.adoptAbortUnwindSlice(parent)
}

// adoptAbortUnwindSlice runs vm with parent's remaining defer cost, wall deadline and
// arena headroom.
//
// Takes parent (*VM) which is unwinding on this goroutine.
//
// Returns func() which hands the unspent cost back to parent and restores vm.
func (vm *VM) adoptAbortUnwindSlice(parent *VM) func() {
	savedLimits := vm.Limits
	savedCost := vm.CostRemaining
	savedCtx, savedDone, savedUnwindDone := vm.ctx, vm.ctxDone, vm.unwindDone
	savedDeadline := vm.callbackDeadline
	savedBlockCtx, savedBlockCancel := vm.blockCtx, vm.blockCancel
	vm.Limits.ForceGoDispatch = true
	vm.Limits.CostBudget = vm.Limits.abortUnwindAllowance().cost
	if vm.Limits.CostTable == nil {
		vm.Limits.CostTable = isa.SharedDefaultCostTable()
	}
	vm.CostRemaining = max(parent.CostRemaining, 0)
	vm.abortUnwinding = true
	vm.abortUnwindExhausted = false
	vm.ctx, vm.ctxDone, vm.unwindDone = parent.ctx, parent.ctxDone, parent.unwindDone
	vm.callbackDeadline = time.Time{}
	vm.blockCtx, vm.blockCancel = nil, nil
	vm.Cancelled.Store(0)
	arena := vm.Arena
	var savedMax uint64
	var savedNoGC bool
	if arena != nil && parent.Arena != nil {
		savedMax, savedNoGC = arena.MaxArenaBytes, arena.disableMinorGC
		arena.MaxArenaBytes = max(arena.MaxArenaBytes, parent.Arena.MaxArenaBytes)
		arena.disableMinorGC = true
	}
	unregister := registerAbortUnwinder(vm)
	return func() {
		unregister()
		if vm.abortUnwindExhausted {
			parent.abortUnwindExhausted = true
			parent.CostRemaining = 0
		} else {
			parent.CostRemaining = max(vm.CostRemaining, 0)
		}
		vm.Limits = savedLimits
		vm.CostRemaining = savedCost
		vm.ctx, vm.ctxDone, vm.unwindDone = savedCtx, savedDone, savedUnwindDone
		vm.callbackDeadline = savedDeadline
		vm.blockCtx, vm.blockCancel = savedBlockCtx, savedBlockCancel
		vm.abortUnwinding = false
		vm.abortUnwindExhausted = false
		if arena != nil && parent.Arena != nil {
			arena.MaxArenaBytes, arena.disableMinorGC = savedMax, savedNoGC
		}
	}
}
