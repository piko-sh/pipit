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
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
)

type lockLedgerHolder = struct {
	sync.Mutex
	Readers sync.RWMutex
	Entries []*lockLedgerEntry
	Any     any
}

type lockLedgerEntry = struct {
	Name string
	Lock sync.Mutex
}

var lockLedgerSink []any

func heapMutexes(count int) []*sync.Mutex {
	locks := make([]*sync.Mutex, count)
	for index := range locks {
		locks[index] = new(sync.Mutex)
		lockLedgerSink = append(lockLedgerSink, locks[index])
	}
	return locks
}

func mutexLockTarget(mu *sync.Mutex) syncWaitTarget {
	return syncWaitTarget{mutex: mu, rwMutex: nil, waitGroup: nil, cond: nil, kind: syncWaitMutexLock}
}

func newLedgerVM() *VM {
	vm := &VM{FramePointer: -1, Globals: NewGlobalStore()}
	vm.Limits.Tracker = &ResourceTracker{}
	vm.Limits.Tracker.locks.begin()
	return vm
}

func TestNoteLockAcquiredRecordsEachLockOncePerExecution(t *testing.T) {
	vm := newLedgerVM()
	tracker := vm.Limits.Tracker
	locks := heapMutexes(2)
	first, second := mutexLockTarget(locks[0]).lockPointer(), mutexLockTarget(locks[1]).lockPointer()
	vm.noteLockAcquired(first, syncWaitMutexLock)
	vm.noteLockAcquired(first, syncWaitMutexLock)
	vm.noteLockAcquired(second, syncWaitMutexLock)
	acquired, overflowed := tracker.locks.snapshot()
	require.False(t, overflowed)
	require.Len(t, acquired, 2)
	require.Contains(t, acquired, uintptr(first))

	sibling := &VM{FramePointer: -1}
	sibling.Limits.Tracker = tracker
	sibling.noteLockAcquired(first, syncWaitMutexLock)
	acquired, _ = tracker.locks.snapshot()
	require.Len(t, acquired, 2, "another VM of the execution records nothing new")

	tracker.locks.begin()
	acquired, _ = tracker.locks.snapshot()
	require.Empty(t, acquired, "a new execution starts with an empty ledger")
	vm.noteLockAcquired(first, syncWaitMutexLock)
	acquired, _ = tracker.locks.snapshot()
	require.Len(t, acquired, 1, "the new epoch empties what the VM remembered")
}

func TestFindAbandonedLocksWalksThePackageVariables(t *testing.T) {
	entry := &lockLedgerEntry{Name: "entry"}
	inner := &lockLedgerHolder{}
	holder := &lockLedgerHolder{Entries: []*lockLedgerEntry{entry}, Any: inner}
	inner.Any = holder
	locks := heapMutexes(3)
	direct, free, unrecorded := locks[0], locks[1], locks[2]
	globals := NewGlobalStore()
	globals.AllocGeneral(reflect.ValueOf(direct))
	globals.AllocGeneral(reflect.ValueOf(free))
	globals.AllocGeneral(reflect.ValueOf(unrecorded))
	globals.AllocGeneral(reflect.ValueOf(holder))
	globals.AllocGeneral(reflect.ValueOf(map[string]*lockLedgerHolder{"k": holder}))

	direct.Lock()
	unrecorded.Lock()
	holder.Lock()
	holder.Readers.RLock()
	entry.Lock.Lock()
	inner.Lock()
	defer func() {
		direct.Unlock()
		unrecorded.Unlock()
		holder.Unlock()
		holder.Readers.RUnlock()
		entry.Lock.Unlock()
		inner.Unlock()
	}()

	acquired := map[uintptr]lockAcquisition{}
	for _, lock := range []unsafe.Pointer{
		unsafe.Pointer(direct), unsafe.Pointer(free), unsafe.Pointer(&holder.Mutex),
		unsafe.Pointer(&holder.Readers), unsafe.Pointer(&entry.Lock), unsafe.Pointer(&inner.Mutex),
	} {
		acquired[uintptr(lock)] = lockAcquisition{function: nil, programCounter: 0, kind: syncWaitMutexLock}
	}
	found, _ := findAbandonedLocks(globals, acquired, false, defaultLockWalkMaxValues)
	require.Len(t, found, 5, "every held, recorded lock is found once: %v", found)
	readWrite := 0
	for _, lock := range found {
		if lock.readWrite {
			readWrite++
		}
	}
	require.Equal(t, 1, readWrite, "the read-held RWMutex is found")
	require.True(t, free.TryLock(), "the check left the free lock free")
	free.Unlock()

	everything, _ := findAbandonedLocks(globals, map[uintptr]lockAcquisition{}, true, defaultLockWalkMaxValues)
	require.Len(t, everything, 6, "an overflowed ledger counts every held lock, the unrecorded one included")
}

func TestFindAbandonedLocksSkipsLocksNoLaterExecutionReaches(t *testing.T) {
	local := heapMutexes(1)[0]
	local.Lock()
	defer local.Unlock()
	globals := NewGlobalStore()
	globals.AllocGeneral(reflect.ValueOf(map[string]lockLedgerEntry{"copy": {Name: "copy"}}))
	acquired := map[uintptr]lockAcquisition{uintptr(unsafe.Pointer(local)): {function: nil, programCounter: 0, kind: syncWaitMutexLock}}
	found, truncated := findAbandonedLocks(globals, acquired, false, defaultLockWalkMaxValues)
	require.Empty(t, found)
	require.False(t, truncated)
}

func TestSettleAbandonedLocksFailsTheServiceClosed(t *testing.T) {
	vm := newLedgerVM()
	tracker := vm.Limits.Tracker
	mu := heapMutexes(1)[0]
	vm.Globals.AllocGeneral(reflect.ValueOf(mu))
	function := program.NewNamedFunction("record")
	function.RuntimeName = "main.record"
	vm.CallStack = []CallFrame{{Function: function}}
	vm.FramePointer = 0
	mu.Lock()
	defer mu.Unlock()
	vm.noteLockAcquired(mutexLockTarget(mu).lockPointer(), syncWaitMutexLock)

	require.NoError(t, vm.settleAbandonedLocks(), "no VM was stopped")
	require.NoError(t, vm.failedClosed())

	vm.noteStopped()
	report := vm.settleAbandonedLocks()
	require.ErrorIs(t, report, fault.ErrLockAbandoned)
	require.Contains(t, report.Error(), "sync.Mutex locked in main.record")
	require.EqualValues(t, 1, tracker.AbandonedLocks.Load())
	require.Same(t, report, vm.failedClosed(), "the service keeps the report")

	later := &VM{FramePointer: -1}
	later.Limits.Tracker = tracker
	_, err := later.RunDispatchedGuarded(0)
	require.Same(t, report, err, "a later run fails at once")
	_, err = later.RunGuarded(0)
	require.Same(t, report, err)
	tracker.locks.begin()
	require.Same(t, report, later.failedClosed(), "a new execution does not clear the report")
}

func TestAbandonedLockErrorNamesTheLocks(t *testing.T) {
	function := program.NewNamedFunction("worker")
	one := &abandonedLockError{locks: []abandonedLock{{acquisition: lockAcquisition{function: function, programCounter: 0, kind: syncWaitMutexLock}}}}
	require.Equal(t, "lock abandoned by a stopped execution: sync.Mutex locked in worker is still held after its goroutines were stopped; every later execution on this service fails", one.Error())
	reader := abandonedLock{acquisition: lockAcquisition{function: function, programCounter: 0, kind: syncWaitRWMutexRLock}, readWrite: true}
	require.Equal(t, "sync.RWMutex read-locked in worker", reader.String())
	unknown := abandonedLock{acquisition: lockAcquisition{function: nil, programCounter: 0, kind: 0}}
	many := &abandonedLockError{locks: []abandonedLock{reader, reader, reader, unknown, unknown}}
	require.Contains(t, many.Error(), "and 2 more are still held")

	cut := &abandonedLockError{locks: []abandonedLock{reader}, truncated: true}
	require.Equal(t, "lock abandoned by a stopped execution: sync.RWMutex read-locked in worker is still held after its goroutines were stopped, or not reached because the check of the package variables stopped at its budget; every later execution on this service fails", cut.Error())
	overflowed := &abandonedLockError{locks: nil, truncated: true}
	require.ErrorIs(t, overflowed, fault.ErrLockAbandoned)
	require.Contains(t, overflowed.Error(), "stopped at its budget, after the execution took more locks than its ledger records")
}

func newTestLockWalk(budget int) *lockWalk {
	return &lockWalk{
		acquired: map[uintptr]lockAcquisition{},
		seen:     make(map[lockWalkKey]struct{}),
		mayHold:  make(map[reflect.Type]bool),
		reported: make(map[uintptr]struct{}),
		found:    nil,
		stack:    nil,
		budget:   budget,
		anyHeld:  false,
	}
}

func TestLockWalkQueuesNoMoreThanItsBudget(t *testing.T) {
	const budget = 64
	entries := make(map[int]*lockLedgerEntry, 4*budget)
	for index := range 4 * budget {
		entries[index] = &lockLedgerEntry{Name: "entry"}
	}
	mapWalk := newTestLockWalk(budget)
	mapWalk.visit(reflect.ValueOf(entries))
	require.LessOrEqual(t, len(mapWalk.stack), budget, "a map larger than the budget queues at most the budget")

	type manyLocks struct {
		A, B, C, D, E, F sync.Mutex
	}
	fieldWalk := newTestLockWalk(4)
	fieldWalk.visit(reflect.ValueOf(&manyLocks{}).Elem())
	require.LessOrEqual(t, len(fieldWalk.stack), 4, "a struct with more fields than the budget queues at most the budget")
}

func TestFindAbandonedLocksFailsClosedWhenTheWalkIsCut(t *testing.T) {
	const budget = 64
	held := heapMutexes(1)[0]
	held.Lock()
	defer held.Unlock()
	entries := make(map[int]*lockLedgerEntry, 4*budget)
	for index := range 4 * budget {
		entries[index] = &lockLedgerEntry{Name: "entry"}
	}
	globals := NewGlobalStore()
	globals.AllocGeneral(reflect.ValueOf(held))
	globals.AllocGeneral(reflect.ValueOf(entries))
	acquired := map[uintptr]lockAcquisition{uintptr(unsafe.Pointer(held)): {function: nil, programCounter: 0, kind: syncWaitMutexLock}}

	found, truncated := findAbandonedLocks(globals, acquired, false, budget)
	require.True(t, truncated, "the walk stopped before it reached every value")
	require.Len(t, found, 1, "a recorded lock the cut walk did not reach counts as held")
	require.False(t, found[0].readWrite)

	found, truncated = findAbandonedLocks(globals, acquired, false, defaultLockWalkMaxValues)
	require.False(t, truncated)
	require.Len(t, found, 1, "the whole walk finds the held lock itself")

	free := heapMutexes(1)[0]
	reached := NewGlobalStore()
	reached.AllocGeneral(reflect.ValueOf(entries))
	reached.AllocGeneral(reflect.ValueOf(free))
	freeLedger := map[uintptr]lockAcquisition{uintptr(unsafe.Pointer(free)): {function: nil, programCounter: 0, kind: syncWaitMutexLock}}
	found, truncated = findAbandonedLocks(reached, freeLedger, false, budget)
	require.False(t, truncated, "the cut decides nothing when the walk tested every recorded lock")
	require.Empty(t, found, "a recorded lock the walk reached and found free is not counted")

	found, truncated = findAbandonedLocks(reached, map[uintptr]lockAcquisition{}, true, budget)
	require.True(t, truncated, "an overflowed ledger and a cut walk fail closed although no lock was named")
	require.Empty(t, found)
}
