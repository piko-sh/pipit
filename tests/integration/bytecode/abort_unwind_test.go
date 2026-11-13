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

//go:build integration

package bytecode_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/sdk/stdlib"
)

var censusSlowdown = func() time.Duration {
	if engine.ActiveDiagnosticBuilds().StoreCensus {
		return 4
	}
	return 1
}()

var abortUnwindHangAfter = censusSlowdown * 20 * time.Second

const abortUnwindSource = `package main

import (
	"strconv"
	"sync"
	"time"
)

var mu sync.Mutex

var spin int

func grow() int {
	var local []string
	for i := 0; i < 400000; i++ {
		local = append(local, "grow-"+strconv.Itoa(i))
	}
	return len(local)
}

func WaitGroupDone() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		grow()
	}()
	wg.Wait()
	return 1
}

func MutexHeld() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mu.Lock()
		defer mu.Unlock()
		grow()
	}()
	wg.Wait()
	return 1
}

func LockFree() int {
	mu.Lock()
	defer mu.Unlock()
	return 7
}

func DeferLoopsForever() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer func() {
			for {
				spin++
			}
		}()
		defer wg.Done()
		grow()
	}()
	wg.Wait()
	time.Sleep(200 * time.Millisecond)
	return 1
}

func DeferPanics() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			panic("raised in a defer of an aborted goroutine")
		}()
		grow()
	}()
	wg.Wait()
	return 1
}

func CostStopped() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			spin++
		}
	}()
	wg.Wait()
	return 1
}
`

type abortUnwindLane struct {
	name    string
	options []app.Option
}

var abortUnwindLanes = []abortUnwindLane{
	{name: "assembly", options: nil},
	{name: "go-dispatch", options: []app.Option{app.WithForceGoDispatch()}},
}

const mainReturnSource = `package main

import (
	"sync"
	"time"
)

var mu sync.Mutex

var spin int

func LockFree() int {
	mu.Lock()
	defer mu.Unlock()
	return 7
}

func holdAndSpin(held chan bool) {
	mu.Lock()
	defer mu.Unlock()
	held <- true
	for {
		spin++
	}
}

func SpinningHolder() int {
	held := make(chan bool)
	go holdAndSpin(held)
	<-held
	return 1
}

func ReceivingHolder() int {
	held := make(chan bool)
	go func() {
		mu.Lock()
		defer mu.Unlock()
		never := make(chan int)
		held <- true
		<-never
	}()
	<-held
	return 1
}

func SleepingHolder() int {
	held := make(chan bool)
	go func() {
		mu.Lock()
		defer mu.Unlock()
		held <- true
		time.Sleep(time.Hour)
	}()
	<-held
	return 1
}

func LoopingDeferHolder() int {
	held := make(chan bool)
	go func() {
		mu.Lock()
		defer mu.Unlock()
		defer func() {
			for {
				spin++
			}
		}()
		held <- true
		for {
			spin++
		}
	}()
	<-held
	return 1
}

func WaitingDeferHolder() int {
	held := make(chan bool)
	go func() {
		mu.Lock()
		defer mu.Unlock()
		defer func() {
			never := make(chan int)
			<-never
		}()
		held <- true
		for {
			spin++
		}
	}()
	<-held
	return 1
}
`

var mainReturnLockWait = censusSlowdown * 5 * time.Second

const stopSource = `package main

import (
	"sync"
	"time"
)

var mu sync.Mutex

var spin int

func LockFree() int {
	mu.Lock()
	defer mu.Unlock()
	return 7
}

func MainSpins() int {
	mu.Lock()
	defer mu.Unlock()
	for {
		spin++
	}
}

func MainSleeps() int {
	mu.Lock()
	defer mu.Unlock()
	time.Sleep(time.Hour)
	return 1
}

func GoroutineSpins() int {
	held := make(chan bool)
	go func() {
		mu.Lock()
		defer mu.Unlock()
		held <- true
		for {
			spin++
		}
	}()
	<-held
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Wait()
	return 1
}

func HoldOnPurpose() int {
	mu.Lock()
	return 1
}

func ReleaseOnPurpose() int {
	mu.Unlock()
	return 2
}
`

const stopAfter = 300 * time.Millisecond

func abortUnwindCall(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, function string) (any, error) {
	t.Helper()
	type outcome struct {
		result any
		err    error
	}
	ctx, cancel := context.WithTimeout(t.Context(), abortUnwindHangAfter)
	defer cancel()
	done := make(chan outcome, 1)
	go func() {
		result, err := service.CallFunction(ctx, compiled, function)
		done <- outcome{result: result, err: err}
	}()
	select {
	case got := <-done:
		return got.result, got.err
	case <-time.After(abortUnwindHangAfter + 5*time.Second):
		require.FailNowf(t, "hung", "%s did not return within %s: an aborted goroutine's defer did not run", function, abortUnwindHangAfter)
		return nil, nil
	}
}

func abortUnwindCompile(t *testing.T, lane []app.Option, extra ...app.Option) (*app.Service, *program.CompiledFileSet) {
	t.Helper()
	options := append([]app.Option{app.WithMaxArenaSizeBytes(2 << 20)}, lane...)
	options = append(options, extra...)
	service := newTestService(t, options...)
	service.UseSymbolProviders(stdlib.Providers()...)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": abortUnwindSource})
	require.NoError(t, err)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	return service, compiled
}

func TestCostStoppedGoroutineRunsItsDeferredWaitGroupDone(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled := abortUnwindCompile(t, lane.options, app.WithCostBudget(2_000_000))
			before := engine.LicenceDiagnostics().AbortUnwind
			_, err := abortUnwindCall(t, service, compiled, "CostStopped")
			require.ErrorIs(t, err, fault.ErrCostBudgetExceeded)
			require.Greater(t, engine.LicenceDiagnostics().AbortUnwind.Completed, before.Completed)
		})
	}
}

func mainReturnCompile(t *testing.T, lane []app.Option) (*app.Service, *program.CompiledFileSet) {
	t.Helper()
	service := newTestService(t, lane...)
	service.UseSymbolProviders(stdlib.Providers()...)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": mainReturnSource})
	require.NoError(t, err)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	return service, compiled
}

func requireMutexFreePromptly(t *testing.T, service *app.Service, compiled *program.CompiledFileSet) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), mainReturnLockWait)
	defer cancel()
	result, err := service.CallFunction(ctx, compiled, "LockFree")
	require.NoError(t, err, "a later execution must find the global mutex free")
	require.Equal(t, 7, result)
}

func TestGoroutineStoppedAtMainReturnRunsItsDefers(t *testing.T) {
	for _, function := range []string{"SpinningHolder", "ReceivingHolder", "SleepingHolder"} {
		for _, lane := range abortUnwindLanes {
			t.Run(function+"/"+lane.name, func(t *testing.T) {
				service, compiled := mainReturnCompile(t, lane.options)
				before := engine.LicenceDiagnostics().AbortUnwind
				result, err := abortUnwindCall(t, service, compiled, function)
				require.NoError(t, err)
				require.Equal(t, 1, result)
				require.Zero(t, service.LeakedGoroutines(), "the join counted no goroutine as leaked")
				requireMutexFreePromptly(t, service, compiled)
				require.Greater(t, engine.LicenceDiagnostics().AbortUnwind.Completed, before.Completed)
			})
		}
	}
}

func TestMainReturnUnwindingIsBounded(t *testing.T) {
	for _, function := range []string{"LoopingDeferHolder", "WaitingDeferHolder"} {
		for _, lane := range abortUnwindLanes {
			t.Run(function+"/"+lane.name, func(t *testing.T) {
				service, compiled := mainReturnCompile(t, lane.options)
				before := engine.LicenceDiagnostics().AbortUnwind
				started := time.Now()
				result, err := abortUnwindCall(t, service, compiled, function)
				require.NoError(t, err)
				require.Equal(t, 1, result)
				require.Less(t, time.Since(started), censusSlowdown*5*time.Second, "the unwinding kept to its allowance")
				require.Zero(t, service.LeakedGoroutines(), "the join counted no goroutine as leaked")
				requireMutexFreePromptly(t, service, compiled)
				require.Greater(t, engine.LicenceDiagnostics().AbortUnwind.Cut, before.Cut, "the looping or waiting defer was cut off")
			})
		}
	}
}

func stopCall(t *testing.T, ctx context.Context, service *app.Service, compiled *program.CompiledFileSet, function string) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := service.CallFunction(ctx, compiled, function)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(abortUnwindHangAfter):
		require.FailNowf(t, "hung", "%s did not return within %s", function, abortUnwindHangAfter)
		return nil
	}
}

func TestHostCancellationAndDeadlineRunTheStoppedDefers(t *testing.T) {
	for _, stop := range []string{"host-cancel", "deadline"} {
		for _, function := range []string{"MainSpins", "MainSleeps", "GoroutineSpins"} {
			for _, lane := range abortUnwindLanes {
				t.Run(stop+"/"+function+"/"+lane.name, func(t *testing.T) {
					if function == "GoroutineSpins" && !engine.SyncWaitGroupInterruptible() {
						t.Skip("this build cannot interrupt WaitGroup.Wait, which main is blocked in when the stop comes")
					}
					options := append([]app.Option{}, lane.options...)
					want := context.Canceled
					if stop == "deadline" {
						options = append(options, app.WithMaxExecutionTime(stopAfter))
						want = context.DeadlineExceeded
					}
					service := newTestService(t, options...)
					service.UseSymbolProviders(stdlib.Providers()...)
					compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": stopSource})
					require.NoError(t, err)
					require.NoError(t, service.ExecuteInits(t.Context(), compiled))
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					if stop == "host-cancel" {
						timer := time.AfterFunc(stopAfter, cancel)
						defer timer.Stop()
					}
					before := engine.LicenceDiagnostics().AbortUnwind
					err = stopCall(t, ctx, service, compiled, function)
					require.ErrorIs(t, err, fault.ErrExecutionCancelled)
					require.ErrorIs(t, err, want)
					require.NotErrorIs(t, err, fault.ErrUnwindBlocked)
					requireMutexFreePromptly(t, service, compiled)
					require.Greater(t, engine.LicenceDiagnostics().AbortUnwind.Completed, before.Completed, "the stopped VM ran its defers")
					require.Zero(t, service.LeakedGoroutines(), "the join counted no goroutine as leaked")
					require.Zero(t, service.AbandonedLocks(), "a lock released by its deferred Unlock is not abandoned")
				})
			}
		}
	}
}

func TestLockHeldByANormalEndDoesNotFailClosed(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service := newTestService(t, lane.options...)
			service.UseSymbolProviders(stdlib.Providers()...)
			compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": stopSource})
			require.NoError(t, err)
			require.NoError(t, service.ExecuteInits(t.Context(), compiled))
			result, err := service.CallFunction(t.Context(), compiled, "HoldOnPurpose")
			require.NoError(t, err)
			require.Equal(t, 1, result)
			require.Zero(t, service.AbandonedLocks(), "a lock held on purpose is not abandoned")
			result, err = service.CallFunction(t.Context(), compiled, "ReleaseOnPurpose")
			require.NoError(t, err)
			require.Equal(t, 2, result)
			requireMutexFreePromptly(t, service, compiled)
		})
	}
}
