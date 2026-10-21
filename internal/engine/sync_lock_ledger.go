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
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
)

const (
	// lockLedgerSeenSlots is how many recorded lock addresses a VM remembers.
	lockLedgerSeenSlots = 2

	// lockLedgerSeenShift drops a lock address's low bits, which alignment leaves zero,
	// before the address picks a remembered slot.
	lockLedgerSeenShift = 3

	// lockLedgerMaxEntries caps the distinct locks one execution's ledger records. An
	// execution that takes more stops recording, and its check then treats every held lock
	// it finds as one the execution took.
	lockLedgerMaxEntries = 1 << 16

	// defaultLockWalkMaxValues caps the values the check visits from the package variables,
	// unless VMLimits.LockWalkMaxValues sets the cap.
	defaultLockWalkMaxValues = 1 << 22

	// abandonedLocksNamed is how many abandoned locks the error names.
	abandonedLocksNamed = 3

	// lockWalkCutText says in an abandoned-lock report that the walk stopped at its budget.
	lockWalkCutText = "the check of the package variables stopped at its budget"

	// abandonedLocksConsequence ends an abandoned-lock report.
	abandonedLocksConsequence = "every later execution on this service fails"

	// interpreterPackagePrefix is the import path prefix of the interpreter's own types,
	// such as a closure's runtime representation, which the check does not walk into.
	interpreterPackagePrefix = "pipit.sh/pipit/"
)

var (
	// lockLedgerEpochs hands out ledger epochs, unique across every tracker, so a VM shell
	// recycled from another service's execution never takes the addresses it remembers for
	// this execution's.
	lockLedgerEpochs atomic.Uint64

	// mutexType is sync.Mutex.
	mutexType = reflect.TypeFor[sync.Mutex]()

	// rwMutexType is sync.RWMutex.
	rwMutexType = reflect.TypeFor[sync.RWMutex]()
)

// lockAcquisition is where the execution first took a lock in its ledger.
type lockAcquisition struct {
	// function is the function whose frame made the Lock call, or nil when unknown.
	function *program.CompiledFunction

	// programCounter is the instruction index of the call in function.
	programCounter int

	// kind identifies Mutex.Lock, RWMutex.Lock or RWMutex.RLock.
	kind syncWaitKind
}

// lockLedger holds ResourceTracker.locks, tracking locks acquired during the execution,
// whether any VM was stopped, and the report that caused the service to fail closed.
type lockLedger struct {
	// acquired maps each lock's address to its first acquisition in the execution.
	acquired map[uintptr]lockAcquisition

	// failed holds the report of the locks an execution abandoned, once one has.
	failed atomic.Pointer[abandonedLockError]

	// epoch is the current execution's epoch, from lockLedgerEpochs.
	epoch atomic.Uint64

	// mu guards acquired and overflowed.
	mu sync.Mutex

	// stopped is set once a VM of the current execution was stopped (VM.noteStopped).
	stopped atomic.Bool

	// overflowed is set when the execution took more than lockLedgerMaxEntries distinct
	// locks. Access is guarded by mu.
	overflowed bool
}

// abandonedLock is a lock a stopped execution left held.
type abandonedLock struct {
	// acquisition is where the execution took it; its function is nil when the ledger
	// overflowed before recording it.
	acquisition lockAcquisition

	// readWrite is true for a sync.RWMutex.
	readWrite bool
}

// abandonedLockError reports the locks abandoned by an execution.
type abandonedLockError struct {
	// locks are the abandoned locks, in the order the check found them.
	locks []abandonedLock

	// truncated is true when the walk of the package variables stopped at its budget, so
	// locks it did not reach were counted as held.
	truncated bool
}

// lockWalkKey identifies a pointer, slice or map the walk has visited.
type lockWalkKey struct {
	// typ is the value's type; a slice and a pointer to its first element differ in it.
	typ reflect.Type

	// address is the value's pointer.
	address uintptr
}

// lockWalk is findAbandonedLocks's state.
type lockWalk struct {
	// acquired is the ledger's record.
	acquired map[uintptr]lockAcquisition

	// seen holds the pointers, slices and maps already visited.
	seen map[lockWalkKey]struct{}

	// mayHold memoises typeMayHoldLock per type.
	mayHold map[reflect.Type]bool

	// reported holds the addresses of the locks already tested, held or free.
	reported map[uintptr]struct{}

	// found are the held locks.
	found []abandonedLock

	// stack holds the values still to visit.
	stack []reflect.Value

	// budget counts the visits left. The stack never holds more values than budget, so
	// everything queued is visited.
	budget int

	// anyHeld counts every held lock as the execution's, when the ledger overflowed.
	anyHeld bool

	// truncated is set when a value was not queued because the budget was spent.
	truncated bool
}

// begin starts a new ledger epoch with no recorded locks and no stopped VM.
//
// The new epoch invalidates every VM's remembered ledger state.
//
// Safe for concurrent use; acquires l.mu.
func (l *lockLedger) begin() {
	l.mu.Lock()
	defer l.mu.Unlock()
	clear(l.acquired)
	l.overflowed = false
	l.stopped.Store(false)
	l.epoch.Store(lockLedgerEpochs.Add(1))
}

// record adds a lock's first acquisition in the execution.
//
// Takes address (uintptr) which is the lock's address.
// Takes acquisition (lockAcquisition) which is where it was taken.
//
// Safe for concurrent use; acquires l.mu.
func (l *lockLedger) record(address uintptr, acquisition lockAcquisition) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.acquired[address]; ok {
		return
	}
	if len(l.acquired) >= lockLedgerMaxEntries {
		l.overflowed = true
		return
	}
	if l.acquired == nil {
		l.acquired = make(map[uintptr]lockAcquisition)
	}
	l.acquired[address] = acquisition
}

// snapshot copies what the execution recorded.
//
// Returns map[uintptr]lockAcquisition which is the copy.
// Returns bool which is true when the ledger overflowed.
//
// Safe for concurrent use; acquires l.mu.
func (l *lockLedger) snapshot() (map[uintptr]lockAcquisition, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return maps.Clone(l.acquired), l.overflowed
}

// lockAcquisitionSite describes the Lock call the current frame is making.
//
// Takes kind (syncWaitKind) which is the method.
//
// Returns lockAcquisition which is the site.
func (vm *VM) lockAcquisitionSite(kind syncWaitKind) lockAcquisition {
	acquisition := lockAcquisition{function: nil, programCounter: 0, kind: kind}
	if vm.FramePointer >= 0 && vm.FramePointer < len(vm.CallStack) {
		frame := &vm.CallStack[vm.FramePointer]
		if frame.Function != nil {
			acquisition.function = frame.Function
			acquisition.programCounter = frameInstructionIndex(frame)
		}
	}
	return acquisition
}

// noteStopped records on the execution's ledger that a VM of it was stopped.
func (vm *VM) noteStopped() {
	if tracker := vm.Limits.Tracker; tracker != nil {
		tracker.locks.stopped.Store(true)
	}
}

// failedClosed returns the report that failed the VM's service closed, which every run a
// VM starts at its outermost frame returns instead of running.
//
// Returns error which is the fault.ErrLockAbandoned or fault.ErrUnwindLeakLimit report,
// or nil.
func (vm *VM) failedClosed() error {
	tracker := vm.Limits.Tracker
	if tracker == nil {
		return nil
	}
	lockReport, unwinderReport := tracker.locks.failed.Load(), tracker.unwindersFailed.Load()
	switch {
	case lockReport != nil:
		return lockReport
	case unwinderReport != nil:
		return unwinderReport
	default:
		return nil
	}
}

// settleAbandonedLocks is the fail-closed check at the end of an execution whose
// goroutines have all exited.
//
// Returns error which is the fault.ErrLockAbandoned report, or nil when no lock was
// abandoned.
func (vm *VM) settleAbandonedLocks() error {
	tracker := vm.Limits.Tracker
	if tracker == nil || vm.Globals == nil || !tracker.locks.stopped.Load() {
		return nil
	}
	acquired, overflowed := tracker.locks.snapshot()
	if len(acquired) == 0 && !overflowed {
		return nil
	}
	held, truncated := findAbandonedLocks(vm.Globals, acquired, overflowed, vm.Limits.lockWalkMaxValues())
	if len(held) == 0 && !truncated {
		return nil
	}
	report := &abandonedLockError{locks: held, truncated: truncated}
	tracker.AbandonedLocks.Add(int64(len(held)))
	tracker.locks.failed.CompareAndSwap(nil, report)
	return report
}

// String identifies the lock's type and how and where the execution acquired it.
//
// Returns string which is the name.
func (l abandonedLock) String() string {
	typeName, verb := "sync.Mutex", "locked"
	if l.readWrite {
		typeName = "sync.RWMutex"
	}
	if l.acquisition.kind == syncWaitRWMutexRLock {
		verb = "read-locked"
	}
	function := l.acquisition.function
	if function == nil {
		return typeName + " (where it was locked is not recorded)"
	}
	name := function.RuntimeName
	if name == "" {
		name = function.Name
	}
	file, line := nearestSourcePosition(function, l.acquisition.programCounter)
	if line == 0 {
		return fmt.Sprintf("%s %s in %s", typeName, verb, name)
	}
	return fmt.Sprintf("%s %s in %s at %s:%d", typeName, verb, name, file, line)
}

// Error names the abandoned locks, and says when the check stopped at its budget.
//
// Returns string which is the message.
func (e *abandonedLockError) Error() string {
	if len(e.locks) == 0 {
		return fmt.Sprintf("%v: %s, after the execution took more locks than its ledger records, so a lock it did not reach may still be held; %s",
			fault.ErrLockAbandoned, lockWalkCutText, abandonedLocksConsequence)
	}
	named := e.locks[:min(len(e.locks), abandonedLocksNamed)]
	names := make([]string, 0, len(named))
	for _, lock := range named {
		names = append(names, lock.String())
	}
	text := strings.Join(names, ", ")
	if rest := len(e.locks) - len(named); rest > 0 {
		text += fmt.Sprintf(" and %d more", rest)
	}
	verb := "is"
	if len(e.locks) > 1 {
		verb = "are"
	}
	if e.truncated {
		return fmt.Sprintf("%v: %s %s still held after its goroutines were stopped, or not reached because %s; %s",
			fault.ErrLockAbandoned, text, verb, lockWalkCutText, abandonedLocksConsequence)
	}
	return fmt.Sprintf("%v: %s %s still held after its goroutines were stopped; %s",
		fault.ErrLockAbandoned, text, verb, abandonedLocksConsequence)
}

// Unwrap exposes fault.ErrLockAbandoned.
//
// Returns error which is the sentinel.
func (*abandonedLockError) Unwrap() error { return fault.ErrLockAbandoned }

// findAbandonedLocks walks the values the package variables reach and returns the locks
// among them that the ledger holds and that are still held.
//
// Takes globals (*GlobalStore) which holds the package variables.
// Takes acquired (map[uintptr]lockAcquisition) which is the ledger's record.
// Takes overflowed (bool) which is true when the ledger stopped recording.
// Takes budget (int) which caps the values visited.
//
// Returns []abandonedLock which holds the held locks.
// Returns bool which is true when the walk was cut and the cut decides the outcome: a
// recorded lock went untested, or the ledger overflowed so an unrecorded one may be held.
//
// Concurrency: Protects the global-slot snapshot with globals.mu; values reachable from
// those slots remain subject to their own synchronisation.
func findAbandonedLocks(globals *GlobalStore, acquired map[uintptr]lockAcquisition, overflowed bool, budget int) ([]abandonedLock, bool) {
	globals.mu.RLock()
	roots := slices.Clone(globals.general)
	globals.mu.RUnlock()
	walk := &lockWalk{
		acquired:  acquired,
		seen:      make(map[lockWalkKey]struct{}),
		mayHold:   make(map[reflect.Type]bool),
		reported:  make(map[uintptr]struct{}),
		found:     nil,
		stack:     nil,
		budget:    max(budget, 0),
		anyHeld:   overflowed,
		truncated: false,
	}
	if len(roots) > walk.budget {
		roots, walk.truncated = roots[len(roots)-walk.budget:], true
	}
	walk.stack = roots
	for len(walk.stack) > 0 {
		value := walk.stack[len(walk.stack)-1]
		walk.stack = walk.stack[:len(walk.stack)-1]
		walk.budget--
		walk.visit(value)
	}
	if !walk.truncated {
		return walk.found, false
	}
	untested := walk.untestedRecordedLocks()
	return append(walk.found, untested...), len(untested) > 0 || overflowed
}

// untestedRecordedLocks returns the locks the ledger holds that the walk did not test, in
// address order.
//
// Returns []abandonedLock which holds one entry per such lock.
func (w *lockWalk) untestedRecordedLocks() []abandonedLock {
	var untested []abandonedLock
	for _, address := range slices.Sorted(maps.Keys(w.acquired)) {
		if _, tested := w.reported[address]; tested {
			continue
		}
		acquisition := w.acquired[address]
		untested = append(untested, abandonedLock{acquisition: acquisition, readWrite: acquisition.kind != syncWaitMutexLock})
	}
	return untested
}

// push queues value when the budget has room for it, and records the cut otherwise.
//
// Takes value (reflect.Value) which is the value to visit.
//
// Returns bool which is false when the budget is spent.
func (w *lockWalk) push(value reflect.Value) bool {
	if len(w.stack) >= w.budget {
		w.truncated = true
		return false
	}
	w.stack = append(w.stack, value)
	return true
}

// visit tests value when it is a lock, and queues what it reaches otherwise.
//
// Takes value (reflect.Value) which is the value.
func (w *lockWalk) visit(value reflect.Value) {
	if !value.IsValid() {
		return
	}
	valueType := value.Type()
	if valueType == mutexType || valueType == rwMutexType {
		w.testLock(value, valueType == rwMutexType)
		return
	}
	if w.mayHoldLock(valueType) {
		w.queueReached(value, valueType)
	}
}

// queueReached queues what a value that may hold a lock reaches: a pointer's or
// interface's target, a struct's fields, an array's or a new slice's elements, or a new
// map's entries.
//
// Takes value (reflect.Value) which is the value.
// Takes valueType (reflect.Type) which is its type.
func (w *lockWalk) queueReached(value reflect.Value, valueType reflect.Type) {
	switch value.Kind() {
	case reflect.Pointer:
		if !value.IsNil() && w.firstVisit(valueType, value.Pointer()) {
			w.push(value.Elem())
		}
	case reflect.Interface:
		if !value.IsNil() {
			w.push(value.Elem())
		}
	case reflect.Struct:
		w.queueFields(value)
	case reflect.Array:
		w.queueElements(value)
	case reflect.Slice:
		if value.Len() > 0 && w.firstVisit(valueType, value.Pointer()) {
			w.queueElements(value)
		}
	case reflect.Map:
		if value.Len() > 0 && w.firstVisit(valueType, value.Pointer()) {
			w.queueEntries(value)
		}
	default:
	}
}

// queueFields queues a struct's fields, as many as the budget allows.
//
// Takes value (reflect.Value) which is the struct.
func (w *lockWalk) queueFields(value reflect.Value) {
	for index := range value.NumField() {
		if !w.push(structFieldValue(value, index)) {
			return
		}
	}
}

// queueElements queues an array's or slice's elements, as many as the budget allows.
//
// Takes value (reflect.Value) which is the array or slice.
func (w *lockWalk) queueElements(value reflect.Value) {
	for index := range value.Len() {
		if !w.push(value.Index(index)) {
			return
		}
	}
}

// queueEntries queues a map's keys and values, as many as the budget allows.
//
// Takes value (reflect.Value) which is the map.
func (w *lockWalk) queueEntries(value reflect.Value) {
	for iterator := value.MapRange(); iterator.Next(); {
		if !w.push(iterator.Key()) || !w.push(iterator.Value()) {
			return
		}
	}
}

// firstVisit records a pointer, slice or map and reports whether the walk meets it for
// the first time.
//
// Takes valueType (reflect.Type) which is its type.
// Takes address (uintptr) which is its pointer.
//
// Returns bool which is true on the first visit.
func (w *lockWalk) firstVisit(valueType reflect.Type, address uintptr) bool {
	key := lockWalkKey{typ: valueType, address: address}
	if _, ok := w.seen[key]; ok {
		return false
	}
	w.seen[key] = struct{}{}
	return true
}

// mayHoldLock reports whether a value of valueType can hold or reach a sync.Mutex or
// sync.RWMutex, memoised per type. A type met again while its own answer is being worked
// out, which only a recursive type does, is taken to hold one.
//
// Takes valueType (reflect.Type) which is the type.
//
// Returns bool which is false when no value of the type can.
func (w *lockWalk) mayHoldLock(valueType reflect.Type) bool {
	if held, ok := w.mayHold[valueType]; ok {
		return held
	}
	w.mayHold[valueType] = true
	held := typeMayHoldLock(valueType, w.mayHoldLock)
	w.mayHold[valueType] = held
	return held
}

// typeMayHoldLock reports whether a value of valueType can hold or reach a lock the walk
// tests, given the same answer for its element and field types. Function values and
// channels are not walked, nor are the interpreter's own types.
//
// Takes valueType (reflect.Type) which is the type.
// Takes element (func(reflect.Type) bool) which answers for an element or field type.
//
// Returns bool which is false when no value of the type can.
func typeMayHoldLock(valueType reflect.Type, element func(reflect.Type) bool) bool {
	if valueType == mutexType || valueType == rwMutexType {
		return true
	}
	if strings.HasPrefix(valueType.PkgPath(), interpreterPackagePrefix) {
		return false
	}
	switch valueType.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return element(valueType.Elem())
	case reflect.Map:
		return element(valueType.Key()) || element(valueType.Elem())
	case reflect.Interface:
		return true
	case reflect.Struct:
		for index := range valueType.NumField() {
			if element(structFieldType(valueType, index).Type) {
				return true
			}
		}
		return false
	default:
		return false
	}
}
