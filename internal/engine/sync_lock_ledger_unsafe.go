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
	"sync"
	"unsafe"
)

// lockPointer returns the lock a decoded Lock or RLock call takes.
//
// Returns unsafe.Pointer which is the Mutex or RWMutex, or nil for another kind.
func (target syncWaitTarget) lockPointer() unsafe.Pointer {
	switch target.kind {
	case syncWaitMutexLock:
		return unsafe.Pointer(target.mutex)
	case syncWaitRWMutexLock, syncWaitRWMutexRLock:
		return unsafe.Pointer(target.rwMutex)
	default:
		return nil
	}
}

// noteLockAcquired records in the execution's ledger a lock a Lock or RLock call Pipit
// made has just taken, unless this VM recorded it last in the same execution.
//
// Takes lock (unsafe.Pointer) which is the Mutex or RWMutex.
// Takes kind (syncWaitKind) which is the method.
func (vm *VM) noteLockAcquired(lock unsafe.Pointer, kind syncWaitKind) {
	tracker := vm.Limits.Tracker
	if tracker == nil || lock == nil {
		return
	}
	address := uintptr(lock)
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
// A successful probe acquires a free lock and releases it immediately.
//
// Takes value (reflect.Value) which is the lock.
// Takes readWrite (bool) which is true for a sync.RWMutex.
func (w *lockWalk) testLock(value reflect.Value, readWrite bool) {
	if !value.CanAddr() {
		return
	}
	pointer := value.Addr().UnsafePointer()
	address := uintptr(pointer)
	acquisition, recorded := w.acquired[address]
	if !recorded && !w.anyHeld {
		return
	}
	if _, done := w.reported[address]; done {
		return
	}
	w.reported[address] = struct{}{}
	if lockHeld(pointer, readWrite) {
		w.found = append(w.found, abandonedLock{acquisition: acquisition, readWrite: readWrite})
	}
}

// lockHeld reports whether a mutex has a writer or an RWMutex has a writer or reader.
//
// It returns true when TryLock fails and immediately releases a successful probe.
//
// Takes pointer (unsafe.Pointer) which is the lock.
// Takes readWrite (bool) which is true for a sync.RWMutex.
//
// Returns bool which is true when the lock is held.
//
// Concurrency: Uses TryLock and immediately releases a lock it acquires; the result is an
// observation that another goroutine can invalidate.
func lockHeld(pointer unsafe.Pointer, readWrite bool) bool {
	if readWrite {
		lock := (*sync.RWMutex)(pointer)
		if lock.TryLock() {
			lock.Unlock()
			return false
		}
		return true
	}
	lock := (*sync.Mutex)(pointer)
	if lock.TryLock() {
		lock.Unlock()
		return false
	}
	return true
}
