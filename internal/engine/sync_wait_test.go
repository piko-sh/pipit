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
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
)

func syncWaitVM(ctx context.Context) *VM {
	return NewVM(ctx, NewGlobalStore(), nil)
}

func waitInBackground(t *testing.T, vm *VM, target syncWaitTarget) <-chan OpResult {
	t.Helper()
	before := syncWaitStats.waits.Load()
	result := make(chan OpResult, 1)
	go func() { result <- vm.waitInterruptibly(target) }()
	require.Eventually(t, func() bool { return syncWaitStats.waits.Load() > before }, 5*time.Second, time.Millisecond, "the wait starts")
	return result
}

func awaitResult(t *testing.T, result <-chan OpResult) OpResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the wait did not end")
		return opContinue
	}
}

func TestSyncWaitGroupInterruptibleFollowsTheDecoder(t *testing.T) {
	require.Equal(t, syncMethodDecoderAvailable(), SyncWaitGroupInterruptible())
}

func TestSyncWaitFromReceiverAcceptsOnlyUnambiguousReceivers(t *testing.T) {
	var mutex sync.Mutex
	cond := sync.NewCond(&mutex)
	cases := []struct {
		receiver reflect.Value
		name     string
		want     syncWaitKind
	}{
		{name: "no receiver", receiver: reflect.Value{}, want: syncWaitNone},
		{name: "a mutex pointer", receiver: reflect.ValueOf(&mutex), want: syncWaitMutexLock},
		{name: "an addressable mutex", receiver: reflect.ValueOf(&mutex).Elem(), want: syncWaitMutexLock},
		{name: "a condition", receiver: reflect.ValueOf(cond), want: syncWaitCondWait},
		{name: "a read-write mutex, whose method is ambiguous", receiver: reflect.ValueOf(new(sync.RWMutex)), want: syncWaitNone},
		{name: "a nil mutex", receiver: reflect.ValueOf((*sync.Mutex)(nil)), want: syncWaitNone},
		{name: "a value that is not a pointer", receiver: reflect.ValueOf(3), want: syncWaitNone},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, syncWaitFromReceiver(testCase.receiver).kind)
		})
	}
}

func TestWaitInterruptiblyTakesAFreeLock(t *testing.T) {
	vm := syncWaitVM(context.WithoutCancel(t.Context()))
	var mutex sync.Mutex
	var rwMutex sync.RWMutex

	require.Equal(t, opContinue, vm.waitInterruptibly(syncWaitTarget{kind: syncWaitMutexLock, mutex: &mutex}))
	require.False(t, mutex.TryLock(), "the wait holds the mutex")
	mutex.Unlock()

	require.Equal(t, opContinue, vm.waitInterruptibly(syncWaitTarget{kind: syncWaitRWMutexRLock, rwMutex: &rwMutex}))
	require.False(t, rwMutex.TryLock(), "the wait holds a read lock")
	rwMutex.RUnlock()

	require.Equal(t, opContinue, vm.waitInterruptibly(syncWaitTarget{kind: syncWaitRWMutexLock, rwMutex: &rwMutex}))
	require.False(t, rwMutex.TryRLock(), "the wait holds the write lock")
	rwMutex.Unlock()

	require.Equal(t, opContinue, vm.waitInterruptibly(syncWaitTarget{kind: syncWaitNone}), "no wait to make")
}

func TestWaitInterruptiblyWaitsForTheLockToBeReleased(t *testing.T) {
	vm := syncWaitVM(t.Context())
	var mutex sync.Mutex
	mutex.Lock()
	result := waitInBackground(t, vm, syncWaitTarget{kind: syncWaitMutexLock, mutex: &mutex})
	mutex.Unlock()
	require.Equal(t, opContinue, awaitResult(t, result))
	require.False(t, mutex.TryLock(), "the wait took the released mutex")
	mutex.Unlock()
}

func TestWaitInterruptiblyEndsWhenTheContextIsCancelled(t *testing.T) {
	var held sync.Mutex
	held.Lock()
	defer held.Unlock()
	var group sync.WaitGroup
	group.Add(1)
	defer group.Done()
	targets := map[string]syncWaitTarget{
		"mutex":      {kind: syncWaitMutexLock, mutex: &held},
		"wait group": {kind: syncWaitGroupWait, waitGroup: &group},
	}
	for name, target := range targets {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			vm := syncWaitVM(ctx)
			woken := syncWaitStats.woken.Load()
			result := waitInBackground(t, vm, target)
			cancel()
			require.Equal(t, opPanicError, awaitResult(t, result))
			require.ErrorIs(t, vm.evalError, context.Canceled)
			require.Greater(t, syncWaitStats.woken.Load(), woken, "the wake is counted")
		})
	}
}

func TestWaitInterruptiblyEndsWhenASiblingGoroutinePanics(t *testing.T) {
	var held sync.Mutex
	held.Lock()
	defer held.Unlock()
	vm := syncWaitVM(context.WithoutCancel(t.Context()))
	vm.Globals.markShared()
	result := waitInBackground(t, vm, syncWaitTarget{kind: syncWaitMutexLock, mutex: &held})
	vm.Globals.recordGoroutinePanic("sibling failed", "")
	require.Equal(t, opPanicError, awaitResult(t, result))
	require.ErrorContains(t, vm.evalError, "sibling failed")
}

func TestPanicWakeChanIsOffWhileUnwinding(t *testing.T) {
	vm := syncWaitVM(context.WithoutCancel(t.Context()))
	vm.Globals.markShared()
	require.NotNil(t, vm.panicWakeChan())
	vm.abortUnwinding = true
	require.Nil(t, vm.panicWakeChan(), "an unwinding VM follows only its defer's deadline")
}

func TestWaitCondInterruptiblyReturnsHoldingTheLock(t *testing.T) {
	contexts := map[string]context.Context{
		"no wake arms":       context.WithoutCancel(t.Context()),
		"a live cancellable": t.Context(),
	}
	for name, ctx := range contexts {
		t.Run(name, func(t *testing.T) {
			vm := syncWaitVM(ctx)
			var mutex sync.Mutex
			cond := sync.NewCond(&mutex)
			ready := false
			go func() {
				mutex.Lock()
				ready = true
				mutex.Unlock()
				cond.Broadcast()
			}()
			mutex.Lock()
			for !ready {
				require.Equal(t, opContinue, vm.waitInterruptibly(syncWaitTarget{kind: syncWaitCondWait, cond: cond}))
			}
			require.False(t, mutex.TryLock(), "the wait returns holding the condition's lock")
			mutex.Unlock()
		})
	}
}

func TestWaitCondInterruptiblyEndsWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	vm := syncWaitVM(ctx)
	var mutex sync.Mutex
	cond := sync.NewCond(&mutex)
	mutex.Lock()
	result := make(chan OpResult, 1)
	go func() {
		mutex.Lock()
		result <- vm.waitInterruptibly(syncWaitTarget{kind: syncWaitCondWait, cond: cond})
		mutex.Unlock()
	}()
	before := syncWaitStats.waits.Load()
	mutex.Unlock()
	require.Eventually(t, func() bool { return syncWaitStats.waits.Load() > before }, 5*time.Second, time.Millisecond, "the wait starts")
	cancel()
	require.Equal(t, opPanicError, awaitResult(t, result))
	require.ErrorIs(t, vm.evalError, context.Canceled)
}

func TestTryInterruptibleSyncWaitDecodesTheCall(t *testing.T) {
	var mutex sync.Mutex
	site := &program.CallSite{IsMethod: true, NativeRegister: 0, MethodReceiverRegister: 1}
	registers := &Registers{General: []reflect.Value{reflect.ValueOf(&mutex).MethodByName("Lock"), reflect.ValueOf(&mutex)}}

	vm := syncWaitVM(context.WithoutCancel(t.Context()))
	result, handled := tryInterruptibleSyncWait(vm, registers, site)
	require.True(t, handled, "Mutex.Lock is an interruptible wait")
	require.Equal(t, opContinue, result)
	require.False(t, mutex.TryLock(), "the wait took the lock")
	mutex.Unlock()

	other := &Registers{General: []reflect.Value{reflect.ValueOf(func() {}), reflect.Value{}}}
	_, handled = tryInterruptibleSyncWait(vm, other, &program.CallSite{NativeRegister: 0})
	require.False(t, handled, "any other call takes the ordinary path")
}

func TestTryInterruptibleSyncWaitConsultsTheCapabilityHook(t *testing.T) {
	denied := errors.New("locks are not allowed")
	var mutex sync.Mutex
	site := &program.CallSite{IsMethod: true, NativeRegister: 0, MethodReceiverRegister: 1}
	registers := &Registers{General: []reflect.Value{reflect.ValueOf(&mutex).MethodByName("Lock"), reflect.ValueOf(&mutex)}}
	vm := syncWaitVM(context.WithoutCancel(t.Context()))
	vm.Limits.CapabilityHook = &recordingHook{denyFn: func(recordedHookCall) error { return denied }}

	result, handled := tryInterruptibleSyncWait(vm, registers, site)
	require.True(t, handled)
	require.Equal(t, opPanicError, result)
	require.ErrorIs(t, vm.evalError, denied)
	require.True(t, mutex.TryLock(), "a denied wait takes no lock")
}
