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
	"sync"
	"sync/atomic"
	"time"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/engine/program"
)

// syncWaitKind names the blocking sync method a call site makes.
type syncWaitKind uint8

const (
	// syncWaitNone is any other call.
	syncWaitNone syncWaitKind = iota

	// syncWaitMutexLock is (*sync.Mutex).Lock.
	syncWaitMutexLock

	// syncWaitRWMutexLock is (*sync.RWMutex).Lock.
	syncWaitRWMutexLock

	// syncWaitRWMutexRLock is (*sync.RWMutex).RLock.
	syncWaitRWMutexRLock

	// syncWaitGroupWait is (*sync.WaitGroup).Wait.
	syncWaitGroupWait

	// syncWaitCondWait is (*sync.Cond).Wait.
	syncWaitCondWait
)

const (
	// syncWaitSpins is how many times a poll yields before it starts to sleep.
	syncWaitSpins = 32

	// syncWaitPollFirst is the first sleep of a poll.
	syncWaitPollFirst = 20 * time.Microsecond

	// syncWaitPollMax caps a poll's sleep.
	syncWaitPollMax = time.Millisecond
)

// syncWaitTarget records a decoded blocking sync method and its receiver.
type syncWaitTarget struct {
	// mutex is the receiver of syncWaitMutexLock.
	mutex *sync.Mutex

	// rwMutex is the receiver of the RWMutex kinds.
	rwMutex *sync.RWMutex

	// waitGroup is the receiver of syncWaitGroupWait.
	waitGroup *sync.WaitGroup

	// cond is the receiver of syncWaitCondWait.
	cond *sync.Cond

	// kind is the method.
	kind syncWaitKind
}

// syncWaitStats counts the interruptible waits, for tests and diagnostics.
var syncWaitStats struct {
	// waits counts calls that took the interruptible path.
	waits atomic.Int64

	// woken counts waits a wake arm ended.
	woken atomic.Int64
}

// syncWaitPoll is pollSyncWait's timed backoff between checks.
type syncWaitPoll struct {
	// timer paces the sleeps once the spins are spent, or is nil before the first.
	timer *time.Timer

	// wakeDone and panicWake end a sleep early when the wait is cancelled or a sibling
	// goroutine panics.
	wakeDone, panicWake <-chan struct{}

	// backoff is the next sleep's length, doubling up to syncWaitPollMax.
	backoff time.Duration
}

// SyncWaitGroupInterruptible reports whether guest WaitGroup.Wait calls can be
// interrupted in this build.
//
// The method-value decoder must be present and have passed its self-test.
//
// The safe and wasm builds cannot read the group's counter, so a stop does not wake a VM
// blocked in WaitGroup.Wait there (see the list of what cannot be interrupted above).
//
// Returns bool which is true when a stop wakes a WaitGroup.Wait.
func SyncWaitGroupInterruptible() bool {
	return syncMethodDecoderAvailable()
}

// syncWaitFromReceiver decodes a blocking sync call from its receiver when the build
// cannot decode method values.
//
// It accepts only receivers whose blocking method is unambiguous and requires no unsafe
// read.
//
// Takes receiver (reflect.Value) which is the call's receiver, possibly invalid.
//
// Returns syncWaitTarget which is the decoded call, kind syncWaitNone otherwise.
func syncWaitFromReceiver(receiver reflect.Value) syncWaitTarget {
	if !receiver.IsValid() {
		return syncWaitTarget{}
	}
	if receiver.Kind() != reflect.Pointer && receiver.CanAddr() {
		receiver = receiver.Addr()
	}
	if receiver.Kind() != reflect.Pointer || receiver.IsNil() || !receiver.CanInterface() {
		return syncWaitTarget{}
	}
	switch value := receiver.Interface().(type) {
	case *sync.Mutex:
		return syncWaitTarget{kind: syncWaitMutexLock, mutex: value, rwMutex: nil, waitGroup: nil, cond: nil}
	case *sync.Cond:
		return syncWaitTarget{kind: syncWaitCondWait, cond: value, mutex: nil, rwMutex: nil, waitGroup: nil}
	default:
		return syncWaitTarget{}
	}
}

// panicWakeChan returns the store's goroutine-panic signal for a blocking operation.
//
// It returns nil during unwinding, when waits follow only the current defer's wall
// deadline.
//
// Returns <-chan struct{} which is the arm, or nil.
func (vm *VM) panicWakeChan() <-chan struct{} {
	if vm.abortUnwinding {
		return nil
	}
	return vm.Globals.goroutinePanicWakeChan()
}

// waitInterruptibly performs a decoded blocking sync call so that the wake arms end it.
//
// Takes target (syncWaitTarget) which is the call; its kind is not syncWaitNone.
//
// Returns OpResult which is opContinue when the wait completed, or the wake's result.
func (vm *VM) waitInterruptibly(target syncWaitTarget) OpResult {
	syncWaitStats.waits.Add(1)
	switch target.kind {
	case syncWaitMutexLock:
		return vm.lockAcquired(target, vm.pollSyncWait(target.mutex.TryLock))
	case syncWaitRWMutexLock:
		return vm.lockAcquired(target, vm.pollSyncWait(target.rwMutex.TryLock))
	case syncWaitRWMutexRLock:
		return vm.lockAcquired(target, vm.pollSyncWait(target.rwMutex.TryRLock))
	case syncWaitGroupWait:
		return vm.pollSyncWait(func() bool { return waitGroupIdle(target.waitGroup) })
	case syncWaitCondWait:
		return vm.waitCondInterruptibly(target.cond)
	default:
		return opContinue
	}
}

// lockAcquired ends a Lock or RLock wait.
//
// A lock it took is recorded in the execution's ledger (sync_lock_ledger.go).
//
// Takes target (syncWaitTarget) which is the call.
// Takes result (OpResult) which is opContinue when the wait acquired the lock.
//
// Returns OpResult which is result, or an armed lock-window injection's stop in the
// window after the lock was taken (pipit_testhooks build).
func (vm *VM) lockAcquired(target syncWaitTarget, result OpResult) OpResult {
	if result != opContinue {
		return result
	}
	vm.noteLockAcquired(target.lockPointer(), target.kind)
	if abortInjectBuilt && abortInjectFires(abortSiteLockWindow) {
		return vm.injectLockWindowStop()
	}
	return result
}

// pollSyncWait retries done until it succeeds or a wake arm fires.
//
// It yields a few times, then doubles its sleep from syncWaitPollFirst up to
// syncWaitPollMax.
//
// A wake arm ends the sleep at once, but the operation is retried first. A wait that can
// complete completes, as the real one would have when the group reached zero or the lock
// was released.
//
// Takes done (func() bool) which attempts the operation without blocking.
//
// Returns OpResult which is opContinue once done succeeded, or the wake's result.
func (vm *VM) pollSyncWait(done func() bool) OpResult {
	if done() {
		return opContinue
	}
	poll := syncWaitPoll{timer: nil, wakeDone: vm.blockingDone(), panicWake: vm.panicWakeChan(), backoff: syncWaitPollFirst}
	defer poll.stop()
	for spin := 0; ; spin++ {
		if spin < syncWaitSpins {
			runtime.Gosched()
		} else {
			poll.sleep()
		}
		if done() {
			return opContinue
		}
		select {
		case <-poll.wakeDone:
			return vm.wakeSyncWait(blockWakeCancelled)
		case <-poll.panicWake:
			return vm.wakeSyncWait(blockWakePanic)
		default:
		}
	}
}

// sleep waits the current backoff, or until a wake arm fires, and doubles the backoff.
func (poll *syncWaitPoll) sleep() {
	if poll.timer == nil {
		poll.timer = time.NewTimer(poll.backoff)
	} else {
		poll.timer.Reset(poll.backoff)
	}
	select {
	case <-poll.wakeDone:
	case <-poll.panicWake:
	case <-poll.timer.C:
	}
	poll.backoff = min(poll.backoff*2, syncWaitPollMax)
}

// stop releases the poll's timer.
func (poll *syncWaitPoll) stop() {
	if poll.timer != nil {
		poll.timer.Stop()
	}
}

// wakeSyncWait ends a sync wait for a wake arm.
//
// Takes wake (blockWake) which is the arm that fired.
//
// Returns OpResult which is the wake's result.
func (vm *VM) wakeSyncWait(wake blockWake) OpResult {
	syncWaitStats.woken.Add(1)
	return vm.surfaceBlockingWake(wake)
}

// waitCondInterruptibly is Cond.Wait with the wake arms.
//
// Takes cond (*sync.Cond) which is the condition.
//
// Returns OpResult which is opContinue, or the wake's result.
//
// Concurrency: Requires cond.L to be held on entry and returns with it held. A watcher
// goroutine broadcasts after cancellation or panic and exits before the wait returns.
func (vm *VM) waitCondInterruptibly(cond *sync.Cond) OpResult {
	wakeDone := vm.blockingDone()
	panicWake := vm.panicWakeChan()
	if wakeDone == nil && panicWake == nil {
		cond.Wait()
		return opContinue
	}
	var woke atomic.Uint32
	stop := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		select {
		case <-stop:
			return
		case <-wakeDone:
			woke.Store(uint32(blockWakeCancelled))
		case <-panicWake:
			woke.Store(uint32(blockWakePanic))
		}
		backoff := syncWaitPollFirst
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-stop:
				return
			case <-timer.C:
			}
			cond.Broadcast()
			timer.Reset(backoff)
			backoff = min(backoff*2, syncWaitPollMax)
		}
	}()
	cond.Wait()
	close(stop)
	<-exited
	if wake := blockWake(safeconv.Uint32ToByte(woke.Load())); wake != blockWakeValue {
		return vm.wakeSyncWait(wake)
	}
	return opContinue
}

// tryInterruptibleSyncWait performs a guest call to a blocking sync method (Mutex.Lock,
// RWMutex.Lock and RLock, WaitGroup.Wait, Cond.Wait) as an interruptible wait, after the
// capability hook, if one is installed, allows it.
//
// Takes vm (*VM) which makes the call.
// Takes registers (*Registers) which holds the method value and receiver.
// Takes site (*program.CallSite) which is a BlocksHostGoroutine site.
//
// Returns OpResult which is the wait's result.
// Returns bool which is false when the call is not such a method and must take the
// ordinary path.
func tryInterruptibleSyncWait(vm *VM, registers *Registers, site *program.CallSite) (OpResult, bool) {
	var receiver reflect.Value
	if site.IsMethod {
		receiver = registers.General[site.MethodReceiverRegister]
	}
	method := registers.General[site.NativeRegister]
	target := decodeSyncWait(method, receiver)
	if target.kind == syncWaitNone {
		return opContinue, false
	}
	if denial := consultCapabilityHookForNativeCall(vm, site, method, nil); denial != nil {
		vm.evalError = denial
		return opPanicError, true
	}
	return vm.waitInterruptibly(target), true
}

// releasesAroundBlock reports whether a native call at site releases the interpreter lock
// and export gates while it blocks.
//
// Takes site (*program.CallSite) which is the call site.
//
// Returns bool which is true when it does.
func (vm *VM) releasesAroundBlock(site *program.CallSite) bool {
	return site.BlocksHostGoroutine && (vm.holdsInterpreterLock || vm.Limits.Debug != nil || vm.holdsGates)
}
