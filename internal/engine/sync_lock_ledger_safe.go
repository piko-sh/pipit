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

//go:build safe || (js && wasm)

package engine

import (
	"reflect"
	"sync"
)

// lockPointer returns the address of the lock a decoded Lock or RLock call takes.
//
// Returns uintptr which is the Mutex's or RWMutex's address, or zero for another kind.
func (target syncWaitTarget) lockPointer() uintptr {
	switch target.kind {
	case syncWaitMutexLock:
		return reflect.ValueOf(target.mutex).Pointer()
	case syncWaitRWMutexLock, syncWaitRWMutexRLock:
		return reflect.ValueOf(target.rwMutex).Pointer()
	default:
		return 0
	}
}

// noteLockAcquired records in the execution's ledger a lock a Lock or RLock call Pipit
// made has just taken, unless this VM recorded it last in the same execution.
//
// Takes address (uintptr) which is the Mutex's or RWMutex's address.
// Takes kind (syncWaitKind) which is the method.
func (vm *VM) noteLockAcquired(address uintptr, kind syncWaitKind) {
	tracker := vm.Limits.Tracker
	if tracker == nil || address == 0 {
		return
	}
	seen := &vm.lockLedgerSeen[(address>>lockLedgerSeenShift)&(lockLedgerSeenSlots-1)]
	if epoch := tracker.locks.epoch.Load(); vm.lockLedgerEpoch != epoch {
		vm.lockLedgerSeen = [lockLedgerSeenSlots]uintptr{}
		vm.lockLedgerEpoch = epoch
	} else if *seen == address {
		return
	}
	tracker.locks.record(address, vm.lockAcquisitionSite(kind))
	*seen = address
}

// testLock reports an addressable sync.Mutex or sync.RWMutex as abandoned when the ledger
// holds its address and TryLock cannot acquire it.
//
// Takes value (reflect.Value) which is the lock.
// Takes readWrite (bool) which is true for a sync.RWMutex.
func (w *lockWalk) testLock(value reflect.Value, readWrite bool) {
	if !value.CanAddr() {
		return
	}
	pointer := value.Addr()
	address := pointer.Pointer()
	acquisition, recorded := w.acquired[address]
	if !recorded && !w.anyHeld {
		return
	}
	if _, done := w.reported[address]; done {
		return
	}
	w.reported[address] = struct{}{}
	if lockHeld(reflect.NewAt(value.Type(), pointer.UnsafePointer()), readWrite) {
		w.found = append(w.found, abandonedLock{acquisition: acquisition, readWrite: readWrite})
	}
}

// lockHeld reports whether a mutex has a writer or an RWMutex has a writer or reader.
//
// Takes pointer (reflect.Value) which is a *sync.Mutex or *sync.RWMutex.
// Takes readWrite (bool) which is true for a sync.RWMutex.
//
// Returns bool which is true when the lock is held.
//
// Concurrency: Uses TryLock and immediately releases a lock it acquires; the result is an
// observation that another goroutine can invalidate.
func lockHeld(pointer reflect.Value, readWrite bool) bool {
	if readWrite {
		lock, ok := reflect.TypeAssert[*sync.RWMutex](pointer)
		if !ok {
			return true
		}
		if lock.TryLock() {
			lock.Unlock()
			return false
		}
		return true
	}
	lock, ok := reflect.TypeAssert[*sync.Mutex](pointer)
	if !ok {
		return true
	}
	if lock.TryLock() {
		lock.Unlock()
		return false
	}
	return true
}
