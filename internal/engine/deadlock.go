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
	"time"
	"weak"

	"pipit.sh/pipit/internal/fault"
)

// blockToken records whether a parked goroutine was counted towards the deadlock check,
// so leaveBlocking undoes exactly what enterBlocking did.
type blockToken struct {
	// counted is true when the goroutine was included in the blocked tally.
	counted bool
}

// channelCleanup carries the data a runtime cleanup needs to forget a collected channel.
// It holds the store weakly so a global channel does not root the store for the life of
// the process.
type channelCleanup struct {
	// store is the store that noted the channel, or nil once the store itself is gone.
	store weak.Pointer[GlobalStore]

	// key is the channel address the store recorded.
	key uintptr
}

// enterBlocking records that this goroutine is about to park.
//
// Takes countable (bool) which is true when only another VM goroutine can complete the
// operation.
//
// Returns blockToken which must be handed to leaveBlocking when the goroutine resumes.
func (vm *VM) enterBlocking(countable bool) blockToken {
	tracker := vm.Limits.Tracker
	if !countable || tracker == nil || vm.Limits.DeadlockGrace <= 0 {
		return blockToken{counted: false}
	}
	blocked := tracker.blocked.Add(1)
	if tracker.allParked(blocked) {
		epoch := tracker.blockEpoch.Load()
		time.AfterFunc(vm.Limits.DeadlockGrace, func() { tracker.reportDeadlock(epoch) })
	}
	return blockToken{counted: true}
}

// leaveBlocking records that this goroutine resumed.
//
// Takes token (blockToken) which is the value enterBlocking returned.
func (vm *VM) leaveBlocking(token blockToken) {
	if !token.counted {
		return
	}
	tracker := vm.Limits.Tracker
	tracker.blocked.Add(-1)
	tracker.blockEpoch.Add(1)
}

// interruptibleSleep implements time.Sleep for interpreted code so a deadline cancels the
// sleep instead of waiting it out, and a sibling goroutine's recorded panic or abort ends
// it; during unwinding only the defer's wall deadline does.
//
// A sleeping goroutine is never counted as deadlocked. Its timer is a waker.
//
// The interpreter lock is not touched here. time.Sleep is a BlocksHostGoroutine call
// site, so handleCallNative has already released the lock around the call and re-acquires
// it on return.
//
// Takes duration (time.Duration) which is the requested sleep.
//
// Returns opContinue when the sleep completed, or the cancellation result.
func (vm *VM) interruptibleSleep(duration time.Duration) OpResult {
	if duration <= 0 {
		return opContinue
	}
	vm.parkGates()
	defer vm.unparkGates()
	done := vm.blockingDone()
	panicWake := vm.panicWakeChan()
	if done == nil && panicWake == nil {
		time.Sleep(duration)
		return opContinue
	}
	timer := time.NewTimer(duration)
	select {
	case <-timer.C:
		return opContinue
	case <-done:
		timer.Stop()
		return vm.surfaceContextCancellation()
	case <-panicWake:
		timer.Stop()
		return vm.surfaceGoroutinePanicAbort()
	}
}

// allParked reports whether every live VM goroutine, the main one included, is parked on
// a countable operation and no spawned goroutine is still waiting for the scheduler.
//
// Takes blocked (int32) which is the current blocked count.
//
// Returns bool which is true when nothing inside the interpreter can make progress.
func (t *ResourceTracker) allParked(blocked int32) bool {
	return t.GoroutinesUnscheduled.Load() == 0 && blocked >= t.GoroutineCount.Load()+1
}

// reportDeadlock runs when the grace timer fires: if no goroutine has unblocked since the
// timer was armed and every goroutine is still parked, the execution is cancelled with
// fault.ErrDeadlock.
//
// Takes epoch (uint64) which is the blockEpoch value when the timer was armed.
func (t *ResourceTracker) reportDeadlock(epoch uint64) {
	if t.blockEpoch.Load() != epoch || !t.allParked(t.blocked.Load()) {
		return
	}
	if cancel := t.executionCancel.Load(); cancel != nil {
		(*cancel)(fault.ErrDeadlock)
	}
}

// noteInterpretedChannel records a channel the program made, so a park on it is known to
// be completable only by another VM goroutine. The entry is dropped when the channel is
// collected, so a later runtime channel at the same address is not misattributed.
//
// Takes channel (reflect.Value) which holds a freshly made channel.
func (g *GlobalStore) noteInterpretedChannel(channel reflect.Value) {
	pointer := channel.UnsafePointer()
	if pointer == nil {
		return
	}
	key := uintptr(pointer)
	g.channelsMu.Lock()
	if g.interpretedChannels == nil {
		g.interpretedChannels = make(map[uintptr]struct{})
	}
	g.interpretedChannels[key] = struct{}{}
	g.channelsMu.Unlock()
	runtime.AddCleanup((*byte)(pointer), forgetInterpretedChannel, channelCleanup{store: weak.Make(g), key: key})
}

// forgetInterpretedChannel runs when a noted channel is collected and removes it from the
// store that recorded it, if that store is still alive.
//
// Takes cleanup (channelCleanup) which names the store and the channel address.
func forgetInterpretedChannel(cleanup channelCleanup) {
	if g := cleanup.store.Value(); g != nil {
		g.forgetInterpretedChannel(cleanup.key)
	}
}

// forgetInterpretedChannel drops a collected channel's entry.
//
// Takes key (uintptr) which is the channel's former address.
//
// Safe for concurrent use; acquires channelsMu.
func (g *GlobalStore) forgetInterpretedChannel(key uintptr) {
	g.channelsMu.Lock()
	delete(g.interpretedChannels, key)
	g.channelsMu.Unlock()
}

// isInterpretedChannel reports whether the program made channel (as opposed to a runtime
// or host channel such as one from time.After), so a park on it counts towards deadlock
// detection.
//
// Takes channel (reflect.Value) which is the channel about to be parked on.
//
// Returns bool which is true for channels made by interpreted code.
//
// Safe for concurrent use; acquires channelsMu.
func (g *GlobalStore) isInterpretedChannel(channel reflect.Value) bool {
	if !channel.IsValid() || channel.Kind() != reflect.Chan || channel.IsNil() {
		return false
	}
	key := uintptr(channel.UnsafePointer())
	g.channelsMu.Lock()
	_, ok := g.interpretedChannels[key]
	g.channelsMu.Unlock()
	return ok
}

// selectIsCountable reports whether every case of a select parks on a program channel, so
// the select as a whole can only be completed by another VM goroutine.
//
// Takes cases ([]reflect.SelectCase) which are the program's cases, without wake arms.
//
// Returns bool which is false when the select has a default arm or a foreign channel.
func (g *GlobalStore) selectIsCountable(cases []reflect.SelectCase) bool {
	for i := range cases {
		if cases[i].Dir == reflect.SelectDefault || !g.isInterpretedChannel(cases[i].Chan) {
			return false
		}
	}
	return true
}
