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
	"runtime"
	"sync/atomic"
)

// gilHandoff carries the family interpreter lock across the reflect-wrapped native-call
// boundary in safe mode.
type gilHandoff struct {
	// globals is the per-family global store that owns the interpreter mutex.
	globals *GlobalStore

	// active indicates whether the handoff is armed for the current native call.
	active atomic.Bool

	// hasClosure is true when the native call carries an interpreted closure argument.
	hasClosure bool
}

// acquireForCallback locks the family interpreter lock when this handoff is armed.
//
// Returns bool reporting whether this call acquired the lock, so the caller pairs it with
// releaseAfterCallback.
//
// Concurrency: acquires the per-family interpreter mutex (the GIL) when the handoff is
// active.
func (h *gilHandoff) acquireForCallback() bool {
	if h == nil || !h.active.Load() {
		return false
	}
	h.globals.interpreterLock.Lock()
	return true
}

// releaseAfterCallback releases the family interpreter lock previously taken by
// acquireForCallback, but only when acquired reports it was taken.
//
// Takes acquired (bool) which is the value returned by the paired acquireForCallback.
//
// Concurrency: unlocks the per-family interpreter mutex (the GIL) when acquireForCallback
// reported it locked.
func (h *gilHandoff) releaseAfterCallback(acquired bool) {
	if !acquired {
		return
	}
	h.globals.interpreterLock.Unlock()
}

// lockInterpreter acquires the family interpreter lock when this VM is the host-goroutine
// boundary that should own it under safe mode.
//
// Returns bool reporting whether this call acquired the lock.
func (vm *VM) lockInterpreter() bool {
	if !vm.Limits.SafeMode || vm.holdsInterpreterLock || vm.reentrantInterpreterVM {
		return false
	}
	vm.Globals.interpreterLock.Lock()
	vm.holdsInterpreterLock = true
	return true
}

// unlockInterpreter releases the family interpreter lock if this VM holds it.
//
// Concurrency: unlocks the per-family interpreter mutex (the GIL) shared across every VM
// in the family.
func (vm *VM) unlockInterpreter() {
	if !vm.holdsInterpreterLock {
		return
	}
	vm.holdsInterpreterLock = false
	vm.Globals.interpreterLock.Unlock()
}

// yieldInterpreterLock briefly releases and re-acquires the interpreter lock so a sibling
// interpreted goroutine can run.
//
// Called at the periodic instruction checkpoint so a CPU-bound safe-mode goroutine cannot
// starve its siblings. A no-op unless this VM currently holds the lock.
//
// Concurrency: unlocks then re-locks the per-family interpreter mutex (the GIL), with a
// runtime.Gosched between, so a waiting sibling can acquire it.
func (vm *VM) yieldInterpreterLock() {
	if !vm.holdsInterpreterLock {
		return
	}
	vm.Globals.interpreterLock.Unlock()
	runtime.Gosched()
	vm.Globals.interpreterLock.Lock()
}

// releaseAroundBlock releases the interpreter lock before this VM parks on a blocking
// operation, so a sibling can run the matching operation that unblocks it.
//
// Returns bool reporting whether the lock was released and must be re-acquired.
func (vm *VM) releaseAroundBlock() bool {
	vm.debugBlockEnter()
	return vm.releaseInterpreterLockForBlock()
}

// releaseInterpreterLockForBlock releases the interpreter lock when this VM holds it,
// without telling the debugger; the debugger's own park uses it.
//
// Returns bool reporting whether the lock was released and must be re-acquired.
//
// Concurrency: unlocks the per-family interpreter mutex (the GIL) when held.
func (vm *VM) releaseInterpreterLockForBlock() bool {
	if !vm.holdsInterpreterLock {
		return false
	}
	vm.Globals.interpreterLock.Unlock()
	return true
}

// reacquireAfterBlock re-acquires the interpreter lock after a blocking operation
// returns.
//
// Takes released (bool) which is the value returned by the paired releaseAroundBlock.
func (vm *VM) reacquireAfterBlock(released bool) {
	vm.debugBlockLeave()
	vm.reacquireInterpreterLockAfterBlock(released)
}

// reacquireInterpreterLockAfterBlock re-locks the interpreter lock when
// releaseInterpreterLockForBlock released it, without telling the debugger.
//
// Takes released (bool) which is the value the paired release returned.
//
// Concurrency: re-locks the per-family interpreter mutex (the GIL) when released is true.
func (vm *VM) reacquireInterpreterLockAfterBlock(released bool) {
	if !released {
		return
	}
	vm.Globals.interpreterLock.Lock()
}
