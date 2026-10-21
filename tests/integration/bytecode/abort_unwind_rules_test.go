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

//go:build integration && !safe && !(js && wasm)

package bytecode_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/sdk/stdlib"
)

const unwindRulesSource = `package main

import (
	"hostblock"
	"sort"
	"strconv"
	"sync"
	"time"
)

var mu sync.Mutex

var spin int

var recovered int

var released int

func grow() int {
	var local []string
	for i := 0; i < 400000; i++ {
		local = append(local, "grow-"+strconv.Itoa(i))
	}
	return len(local)
}

func LockFree() int {
	mu.Lock()
	defer mu.Unlock()
	return 7
}

func Spin() int { return spin }

func Recovered() int { return recovered }

func Released() int { return released }

func Reset() int {
	spin, recovered, released = 0, 0, 0
	return 0
}

func EntryHoldsMutex() int {
	mu.Lock()
	defer mu.Unlock()
	defer func() {
		if recover() != nil {
			recovered = 1
		}
	}()
	grow()
	return 1
}

func EntryCostHoldsMutex() int {
	mu.Lock()
	defer mu.Unlock()
	for {
		spin++
	}
}

func CallbackHoldsMutex() int {
	defer func() {
		if recover() != nil {
			recovered = 1
		}
	}()
	rows := []int{3, 1, 2}
	sort.Slice(rows, func(a, b int) bool {
		mu.Lock()
		defer mu.Unlock()
		grow()
		return rows[a] < rows[b]
	})
	return 1
}

type byLocked []int

func (b byLocked) Len() int { return len(b) }

func (b byLocked) Swap(i, j int) { b[i], b[j] = b[j], b[i] }

func (b byLocked) Less(i, j int) bool {
	mu.Lock()
	defer mu.Unlock()
	grow()
	return b[i] < b[j]
}

func BoundMethodHoldsMutex() int {
	defer func() {
		if recover() != nil {
			recovered = 1
		}
	}()
	sort.Sort(byLocked{3, 1, 2})
	return 1
}

func EarlierDoneLaterLoop() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { released = 1 }()
		defer func() {
			for {
				spin++
			}
		}()
		grow()
	}()
	wg.Wait()
	return 1
}

func EarlierUnlockLaterReabort() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mu.Lock()
		defer mu.Unlock()
		defer func() { grow() }()
		grow()
	}()
	wg.Wait()
	return 1
}

var work int

func EarlierWorkNestedLoops() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			for i := 0; i < 3000; i++ {
				work++
			}
			released = 1
		}()
		defer func() {
			for i := 0; i < 40; i++ {
				defer func() {
					for {
						spin++
					}
				}()
			}
			for {
				spin++
			}
		}()
		grow()
	}()
	wg.Wait()
	return 1
}

func CutDeferStillUnlocks() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			mu.Lock()
			defer func() { mu.Unlock() }()
			for {
				spin++
			}
		}()
		grow()
	}()
	wg.Wait()
	return 1
}

func DrainedRemainderStillUnlocks() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			mu.Lock()
			defer func() { mu.Unlock() }()
			for {
				spin++
			}
		}()
		defer func() {
			for i := 0; i < 5; i++ {
				defer func() {
					for {
						spin++
					}
				}()
			}
			for {
				spin++
			}
		}()
		grow()
	}()
	wg.Wait()
	return 1
}

func ManyDefersDrainedRemainderStillUnlocks() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 326; i++ {
			defer func() {}()
		}
		defer func() {
			mu.Lock()
			defer func() { mu.Unlock() }()
			for {
				spin++
			}
		}()
		defer func() {
			for i := 0; i < 5; i++ {
				defer func() {
					for {
						spin++
					}
				}()
			}
			for {
				spin++
			}
		}()
		grow()
	}()
	wg.Wait()
	return 1
}

func AbortInReturnDefer() int {
	defer func() { released = 1 }()
	defer func() { grow() }()
	return 1
}

func AbortInPanicDefer() int {
	defer func() { released = 1 }()
	defer func() { grow() }()
	panic("raised before the abort")
}

func AbortInReturnDeferGoroutine() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { released = 1 }()
		defer func() { grow() }()
	}()
	wg.Wait()
	return 1
}

func DeferWaitsOnHeldMutex() int {
	var m sync.Mutex
	m.Lock()
	defer func() { released = 1 }()
	defer func() { m.Lock() }()
	grow()
	return 1
}

func EntryLoopingDeferIsBounded() int {
	defer func() {
		for {
			spin++
		}
	}()
	grow()
	return 1
}

func ParentWaitGroup() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		grow()
		wg.Done()
	}()
	wg.Wait()
	return 1
}

func ParentMutex() int {
	var m sync.Mutex
	m.Lock()
	go func() {
		grow()
		m.Unlock()
	}()
	m.Lock()
	return 1
}

func ParentRWMutexLock() int {
	var rw sync.RWMutex
	rw.Lock()
	go func() {
		grow()
		rw.Unlock()
	}()
	rw.Lock()
	return 1
}

func ParentRWMutexRLock() int {
	var rw sync.RWMutex
	rw.Lock()
	go func() {
		grow()
		rw.Unlock()
	}()
	rw.RLock()
	return 1
}

func ParentCond() int {
	var m sync.Mutex
	c := sync.NewCond(&m)
	ready := false
	go func() {
		grow()
		m.Lock()
		ready = true
		c.Signal()
		m.Unlock()
	}()
	m.Lock()
	defer m.Unlock()
	for !ready {
		c.Wait()
	}
	return 1
}

func ParentSleep() int {
	go func() {
		grow()
	}()
	time.Sleep(time.Hour)
	return 1
}

func ParentChannel() int {
	ch := make(chan int)
	go func() {
		grow()
		ch <- 1
	}()
	return <-ch
}

func ParentDeferredWait() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		grow()
		wg.Done()
	}()
	defer wg.Wait()
	return 1
}

func WaitsForever() int {
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Wait()
	return 1
}

func EntryDeferBlocksInNative() int {
	defer func() { released = 1 }()
	defer func() { hostblock.Block() }()
	grow()
	return 1
}

func GoroutineDeferBlocksInNative() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { hostblock.Block() }()
		grow()
	}()
	wg.Wait()
	return 1
}

func EntryDeferBlocksInNativeWhenCancelled() int {
	defer func() { released = 1 }()
	defer func() { hostblock.Block() }()
	for {
		spin++
	}
}

func GoroutineDeferBlocksInNativeAtMainReturn() int {
	held := make(chan bool)
	go func() {
		defer func() { released = 1 }()
		defer func() { hostblock.Block() }()
		held <- true
		for {
			spin++
		}
	}()
	<-held
	return 1
}

func worker(wg *sync.WaitGroup) {
	defer wg.Done()
	spin++
}

func CostBeforeDeferStatement() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go worker(&wg)
	wg.Wait()
	return 1
}
`

type hostBlock struct {
	release  chan struct{}
	entered  chan struct{}
	returned chan struct{}
	once     sync.Once
}

func newHostBlock() *hostBlock {
	return &hostBlock{release: make(chan struct{}), entered: make(chan struct{}, 8), returned: make(chan struct{}, 8)}
}

func (h *hostBlock) symbols() map[string]reflect.Value {
	return map[string]reflect.Value{
		"Block": reflect.ValueOf(func() {
			h.entered <- struct{}{}
			<-h.release
			h.returned <- struct{}{}
		}),
	}
}

func (h *hostBlock) unblock() {
	h.once.Do(func() { close(h.release) })
}

func unwindRulesCompile(t *testing.T, lane []app.Option, extra ...app.Option) (*app.Service, *program.CompiledFileSet, *hostBlock) {
	t.Helper()
	options := append([]app.Option{app.WithMaxArenaSizeBytes(2 << 20)}, lane...)
	options = append(options, extra...)
	service := newTestService(t, options...)
	service.UseSymbolProviders(stdlib.Providers()...)
	block := newHostBlock()
	t.Cleanup(block.unblock)
	service.RegisterPackage("hostblock", block.symbols())
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": unwindRulesSource})
	require.NoError(t, err)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	return service, compiled, block
}

func unwindRulesInt(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, function string) int {
	t.Helper()
	result, err := abortUnwindCall(t, service, compiled, function)
	require.NoError(t, err, "%s", function)
	value, ok := result.(int)
	require.True(t, ok, "%s returned %T", function, result)
	return value
}

func requireLockFree(t *testing.T, service *app.Service, compiled *program.CompiledFileSet) {
	t.Helper()
	require.Equal(t, 7, unwindRulesInt(t, service, compiled, "LockFree"), "a later execution must find the global mutex free")
}

func TestR1EntryVMRunsItsDefersAfterAnAbort(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled, _ := unwindRulesCompile(t, lane.options)
			_, err := abortUnwindCall(t, service, compiled, "EntryHoldsMutex")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
			requireLockFree(t, service, compiled)
			require.Zero(t, unwindRulesInt(t, service, compiled, "Recovered"), "recover() in a defer of an aborted VM returns nil")
		})
	}
}

func TestR1EntryVMRunsItsDefersAfterACostStop(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled, _ := unwindRulesCompile(t, lane.options, app.WithCostBudget(2_000_000))
			_, err := abortUnwindCall(t, service, compiled, "EntryCostHoldsMutex")
			require.ErrorIs(t, err, fault.ErrCostBudgetExceeded)
			requireLockFree(t, service, compiled)
		})
	}
}

func TestR1CallbackVMDeferReleasesAMutex(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled, _ := unwindRulesCompile(t, lane.options)
			_, err := abortUnwindCall(t, service, compiled, "CallbackHoldsMutex")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
			require.Zero(t, unwindRulesInt(t, service, compiled, "Recovered"), "the abort must not reach the caller's recover()")
			requireLockFree(t, service, compiled)
		})
	}
}

func TestR1BoundMethodVMDeferReleasesAMutex(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled, _ := unwindRulesCompile(t, lane.options)
			_, err := abortUnwindCall(t, service, compiled, "BoundMethodHoldsMutex")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
			require.Zero(t, unwindRulesInt(t, service, compiled, "Recovered"), "the abort must not reach the caller's recover()")
			requireLockFree(t, service, compiled)
		})
	}
}

func TestR2EarlierWaitGroupDoneRunsDespiteALaterLoopingDefer(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled, _ := unwindRulesCompile(t, lane.options)
			before := engine.LicenceDiagnostics().AbortUnwind
			_, err := abortUnwindCall(t, service, compiled, "EarlierDoneLaterLoop")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
			after := engine.LicenceDiagnostics().AbortUnwind
			require.Greater(t, after.Cut, before.Cut, "the looping defer is cut off")
			require.Greater(t, after.Completed, before.Completed, "the unwinding reached the first defer, wg.Done")
			require.Equal(t, 1, unwindRulesInt(t, service, compiled, "Released"), "the defer before the looping one ran")
		})
	}
}

func TestR2NestedDefersAreFundedOnlyFromTheirRegistrarsShare(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled, _ := unwindRulesCompile(t, lane.options)
			before := engine.LicenceDiagnostics().AbortUnwind
			_, err := abortUnwindCall(t, service, compiled, "EarlierWorkNestedLoops")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
			after := engine.LicenceDiagnostics().AbortUnwind
			require.GreaterOrEqual(t, after.Cut-before.Cut, int64(41), "the looping defer and its 40 nested defers are cut off")
			require.Greater(t, after.Completed, before.Completed, "the unwinding reached wg.Done")
			require.Equal(t, 1, unwindRulesInt(t, service, compiled, "Released"), "the earlier defer finished its work")
		})
	}
}

func TestR2ACutDeferStillRunsTheUnlockItDeferred(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled, _ := unwindRulesCompile(t, lane.options)
			_, err := abortUnwindCall(t, service, compiled, "CutDeferStillUnlocks")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
			requireLockFree(t, service, compiled)
		})
	}
}

func TestR2ANestedReleaseKeepsItsReserveWhenTheRemainderIsDrained(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled, _ := unwindRulesCompile(t, lane.options)
			_, err := abortUnwindCall(t, service, compiled, "DrainedRemainderStillUnlocks")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
			requireLockFree(t, service, compiled)
		})
	}
}

func TestR2ANestedReserveHasAFloorWithManyPendingDefers(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled, _ := unwindRulesCompile(t, lane.options)
			_, err := abortUnwindCall(t, service, compiled, "ManyDefersDrainedRemainderStillUnlocks")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
			requireLockFree(t, service, compiled)
		})
	}
}

func TestR1AbortInsideADeferAReturnOrPanicIsRunning(t *testing.T) {
	for _, function := range []string{"AbortInReturnDefer", "AbortInPanicDefer", "AbortInReturnDeferGoroutine"} {
		for _, lane := range abortUnwindLanes {
			t.Run(function+"/"+lane.name, func(t *testing.T) {
				service, compiled, _ := unwindRulesCompile(t, lane.options)
				_, err := abortUnwindCall(t, service, compiled, function)
				require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
				require.NotContains(t, err.Error(), "raised before the abort", "the abort replaces the panic")
				require.Equal(t, 1, unwindRulesInt(t, service, compiled, "Released"), "the pending defer ran")
			})
		}
	}
}

func TestR4WaitInsideTheUnwindingWakesAtItsDeadline(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled, _ := unwindRulesCompile(t, lane.options)
			before := engine.LicenceDiagnostics().AbortUnwind
			started := time.Now()
			_, err := abortUnwindCall(t, service, compiled, "DeferWaitsOnHeldMutex")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
			require.NotContains(t, err.Error(), "still blocked in native code", "a Pipit wait is interrupted, not reported")
			require.Less(t, time.Since(started), 10*time.Second)
			require.Greater(t, engine.LicenceDiagnostics().AbortUnwind.Cut, before.Cut)
			require.Equal(t, 1, unwindRulesInt(t, service, compiled, "Released"))
		})
	}
}

func TestR2EarlierUnlockRunsDespiteALaterReabortingDefer(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled, _ := unwindRulesCompile(t, lane.options)
			before := engine.LicenceDiagnostics().AbortUnwind
			_, err := abortUnwindCall(t, service, compiled, "EarlierUnlockLaterReabort")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
			after := engine.LicenceDiagnostics().AbortUnwind
			require.Greater(t, after.Completed, before.Completed, "the unwinding reached the first defer, wg.Done")
			requireLockFree(t, service, compiled)
		})
	}
}

func TestR3UnwindingAllowanceIsNotRefilled(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled, _ := unwindRulesCompile(t, lane.options, app.WithCostBudget(1<<40))
			before := engine.LicenceDiagnostics().AbortUnwind
			_, err := abortUnwindCall(t, service, compiled, "EntryLoopingDeferIsBounded")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
			spin := unwindRulesInt(t, service, compiled, "Spin")
			require.Positive(t, spin, "the looping defer ran")
			require.Less(t, spin, 100_000, "the looping defer stopped within the fixed allowance")
			require.Greater(t, engine.LicenceDiagnostics().AbortUnwind.Cut, before.Cut)
		})
	}
}

func TestR4PipitWaitsWakeWhenAChildAborts(t *testing.T) {
	waits := []string{"ParentWaitGroup", "ParentMutex", "ParentRWMutexLock", "ParentRWMutexRLock", "ParentCond", "ParentSleep", "ParentChannel", "ParentDeferredWait"}
	for _, lane := range abortUnwindLanes {
		for _, function := range waits {
			t.Run(lane.name+"/"+function, func(t *testing.T) {
				service, compiled, _ := unwindRulesCompile(t, lane.options)
				_, err := abortUnwindCall(t, service, compiled, function)
				require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded, "the parent must wake with the child's abort")
			})
		}
	}
}

func TestR4WaitGroupWaitWakesOnCancellation(t *testing.T) {
	service, compiled, _ := unwindRulesCompile(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.CallFunction(ctx, compiled, "WaitsForever")
		done <- err
	}()
	select {
	case err := <-done:
		require.ErrorIs(t, err, fault.ErrExecutionCancelled)
	case <-time.After(abortUnwindHangAfter):
		require.FailNow(t, "hung", "WaitGroup.Wait did not wake at the deadline")
	}
}

func requireUnwindBlockedReport(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded, "the abort is still the execution's error")
	require.True(t, strings.Contains(err.Error(), "still blocked in native code"), "the blocked defer is reported: %v", err)
}

func TestR5NativeBlockedDeferIsReported(t *testing.T) {
	for _, function := range []string{"EntryDeferBlocksInNative", "GoroutineDeferBlocksInNative"} {
		for _, lane := range abortUnwindLanes {
			t.Run(function+"/"+lane.name, func(t *testing.T) {
				service, compiled, block := unwindRulesCompile(t, lane.options)
				started := time.Now()
				_, err := abortUnwindCall(t, service, compiled, function)
				requireUnwindBlockedReport(t, err)
				require.Less(t, time.Since(started), 10*time.Second, "the call returned at the unwinding's deadline")
				select {
				case <-block.entered:
				default:
					require.FailNow(t, "the blocking defer never ran")
				}
				require.Zero(t, unwindRulesInt(t, service, compiled, "Released"), "the defers after the blocked one did not run")
				block.unblock()
				select {
				case <-block.returned:
				case <-time.After(abortUnwindHangAfter):
					require.FailNow(t, "the host call did not return after its release")
				}
				require.Eventually(t, func() bool {
					return unwindRulesInt(t, service, compiled, "Released") == 0
				}, time.Second, 10*time.Millisecond, "the abandoned unwinding ran nothing more")
				requireLockFree(t, service, compiled)
			})
		}
	}
}

func TestR5NativeBlockedDeferAtMainReturnIsReported(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled, block := unwindRulesCompile(t, lane.options)
			started := time.Now()
			_, err := abortUnwindCall(t, service, compiled, "GoroutineDeferBlocksInNativeAtMainReturn")
			require.ErrorIs(t, err, fault.ErrUnwindBlocked, "the blocked defer is reported")
			require.False(t, errors.Is(err, fault.ErrExecutionCancelled), "the report, not the join's cancellation, is the error: %v", err)
			require.Less(t, time.Since(started), 10*time.Second, "the call returned at the unwinding's deadline")
			require.Zero(t, service.LeakedGoroutines(), "the join counted no goroutine as leaked")
			select {
			case <-block.entered:
			default:
				require.FailNow(t, "the blocking defer never ran")
			}
			require.Zero(t, unwindRulesInt(t, service, compiled, "Released"), "the defer after the blocked one did not run")
			block.unblock()
			select {
			case <-block.returned:
			case <-time.After(abortUnwindHangAfter):
				require.FailNow(t, "the host call did not return after its release")
			}
			require.Eventually(t, func() bool {
				return unwindRulesInt(t, service, compiled, "Released") == 0
			}, time.Second, 10*time.Millisecond, "the abandoned unwinding ran nothing more")
		})
	}
}

func TestR5NativeBlockedDeferAfterCancellationIsReported(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled, block := unwindRulesCompile(t, lane.options)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			timer := time.AfterFunc(stopAfter, cancel)
			defer timer.Stop()
			started := time.Now()
			err := stopCall(t, ctx, service, compiled, "EntryDeferBlocksInNativeWhenCancelled")
			require.ErrorIs(t, err, fault.ErrExecutionCancelled, "the cancellation is still the execution's error")
			require.ErrorIs(t, err, context.Canceled)
			require.ErrorIs(t, err, fault.ErrUnwindBlocked, "the blocked defer is reported")
			require.Less(t, time.Since(started), 10*time.Second, "the call returned at the unwinding's deadline")
			select {
			case <-block.entered:
			default:
				require.FailNow(t, "the blocking defer never ran")
			}
			require.Zero(t, unwindRulesInt(t, service, compiled, "Released"), "the defer after the blocked one did not run")
			block.unblock()
			select {
			case <-block.returned:
			case <-time.After(abortUnwindHangAfter):
				require.FailNow(t, "the host call did not return after its release")
			}
			require.Eventually(t, func() bool {
				return unwindRulesInt(t, service, compiled, "Released") == 0
			}, time.Second, 10*time.Millisecond, "the abandoned unwinding ran nothing more")
		})
	}
}

func TestCostStopBeforeTheDeferStatementWakesTheParentsWait(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled, _ := unwindRulesCompile(t, lane.options, app.WithCostBudget(1_000))
			_, err := abortUnwindCall(t, service, compiled, "CostBeforeDeferStatement")
			require.ErrorIs(t, err, fault.ErrCostBudgetExceeded)
			require.False(t, errors.Is(err, fault.ErrExecutionCancelled), "the wait woke on the stop, not the deadline: %v", err)
		})
	}
}
