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

package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab"
)

const (
	cancellationChildEnv = "PIPIT_CALLBACK_CANCELLATION_CHILD"
	cancellationDispatchEnv = "PIPIT_CALLBACK_CANCELLATION_DISPATCH"
	cancellationWatchdog = 20 * time.Second
)

var cancellationModes = []struct {
	name     string
	dispatch string
	procs    string
}{
	{name: "asm dispatch", dispatch: "asm", procs: ""},
	{name: "go dispatch", dispatch: "go", procs: ""},
	{name: "asm dispatch on one P", dispatch: "asm", procs: "1"},
	{name: "go dispatch on one P", dispatch: "go", procs: "1"},
}

func runCancellationChild(t *testing.T, child string) {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, mode := range cancellationModes {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), cancellationWatchdog)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^"+child+"$", "-test.count=1")
			command.Env = append(os.Environ(), cancellationChildEnv+"="+child, cancellationDispatchEnv+"="+mode.dispatch)
			if mode.procs != "" {
				command.Env = append(command.Env, "GOMAXPROCS="+mode.procs)
			}
			command.WaitDelay = time.Second
			output, err := command.CombinedOutput()
			require.NoError(t, ctx.Err(), "the child did not finish within %s: %s", cancellationWatchdog, output)
			require.NoError(t, err, "%s", output)
		})
	}
}

func cancellationChild(t *testing.T, name string) {
	t.Helper()
	if os.Getenv(cancellationChildEnv) != name {
		t.Skip("subprocess fixture")
	}
}

func newCancellationVM(t *testing.T, ctx context.Context, exports map[string]reflect.Value, limits engine.VMLimits) (*callbackHarness, *engine.VM) {
	t.Helper()
	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{"host": exports})
	service := NewService()
	service.UseSymbols(symbols)
	vm := engine.NewVM(ctx, engine.NewGlobalStore(), symbols)
	if os.Getenv(cancellationDispatchEnv) == "go" {
		limits.ForceGoDispatch = true
	}
	vm.Limits = limits
	return &callbackHarness{service: service, vm: vm}, vm
}

func newCancellationService(t *testing.T, exports map[string]reflect.Value, options ...Option) *Service {
	t.Helper()
	if os.Getenv(cancellationDispatchEnv) == "go" {
		options = append(options, WithForceGoDispatch())
	}
	service := NewService(options...)
	service.UseSymbols(symtab.NewSymbolRegistry(symtab.SymbolExports{"host": exports}))
	return service
}

func TestExecuteDeadlineStopsSpinningCallbacks(t *testing.T) {
	t.Parallel()
	runCancellationChild(t, "TestExecuteDeadlineStopsSpinningCallbacksChild")
}

func TestExecuteDeadlineStopsSpinningCallbacksChild(t *testing.T) {
	cancellationChild(t, "TestExecuteDeadlineStopsSpinningCallbacksChild")
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	var durations []time.Duration
	harness, vm := newCancellationVM(t, ctx, map[string]reflect.Value{
		"Each": reflect.ValueOf(func(n int, fn func(int) bool) int {
			kept := 0
			for index := range n {
				started := time.Now()
				if fn(index) {
					kept++
				}
				durations = append(durations, time.Since(started))
			}
			return kept
		}),
	}, engine.VMLimits{})
	started := time.Now()
	_, err := vm.Execute(harness.compile(t, `import "host"
host.Each(40, func(index int) bool {
	total := 0
	for {
		total += index
	}
})`))
	require.ErrorIs(t, err, fault.ErrExecutionCancelled, "the execution's deadline stops it")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), 10*time.Second)
	require.Len(t, durations, 40, "native code calls every callback")
	for index, duration := range durations[1:] {
		require.Less(t, duration, time.Second, "callback %d started after the deadline and must stop at once", index+1)
	}
}

func TestCallbackTimeoutStopsAStoredCallback(t *testing.T) {
	t.Parallel()
	runCancellationChild(t, "TestCallbackTimeoutStopsAStoredCallbackChild")
}

func TestCallbackTimeoutStopsAStoredCallbackChild(t *testing.T) {
	cancellationChild(t, "TestCallbackTimeoutStopsAStoredCallbackChild")
	var kept func() int
	harness, vm := newCancellationVM(t, t.Context(), map[string]reflect.Value{
		"Keep": reflect.ValueOf(func(fn func() int) { kept = fn }),
	}, engine.VMLimits{CallbackTimeout: 100 * time.Millisecond})
	_, err := vm.Execute(harness.compile(t, `import "host"
host.Keep(func() int {
	n := 0
	for {
		n++
	}
})`))
	require.NoError(t, err)
	require.NotNil(t, kept)

	started := time.Now()
	require.Zero(t, kept(), "a callback stopped by its timeout returns its zero results")
	require.Less(t, time.Since(started), 10*time.Second)
}

func TestStoredCallbackIgnoresItsEndedExecution(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	var kept func() int
	harness, vm := newCancellationVM(t, ctx, map[string]reflect.Value{
		"Keep": reflect.ValueOf(func(fn func() int) { kept = fn }),
	}, engine.VMLimits{})
	_, err := vm.Execute(harness.compile(t, `import "host"
host.Keep(func() int {
	total := 0
	for index := range 5000 {
		total += index
	}
	return total
})`))
	require.NoError(t, err)
	cancel()
	require.Equal(t, 4999*5000/2, kept(), "a callback the host calls after the execution returned runs to completion")
}

func TestSpinningCallbackDoesNotStallTheCollector(t *testing.T) {
	t.Parallel()
	runCancellationChild(t, "TestSpinningCallbackDoesNotStallTheCollectorChild")
}

func TestSpinningCallbackDoesNotStallTheCollectorChild(t *testing.T) {
	cancellationChild(t, "TestSpinningCallbackDoesNotStallTheCollectorChild")
	var kept func()
	harness, vm := newCancellationVM(t, t.Context(), map[string]reflect.Value{
		"Keep": reflect.ValueOf(func(fn func()) { kept = fn }),
	}, engine.VMLimits{CallbackTimeout: 2 * time.Second})
	_, err := vm.Execute(harness.compile(t, `import "host"
host.Keep(func() {
	n := 0
	for {
		n++
	}
})`))
	require.NoError(t, err)
	spun := make(chan struct{})
	go func() {
		defer close(spun)
		kept()
	}()
	time.Sleep(50 * time.Millisecond)
	collected := time.Now()
	for range 3 {
		runtime.GC()
	}
	require.Less(t, time.Since(collected), time.Second, "each collection stopped the world while the callback spun")
	<-spun
}

func cancellationExports() map[string]reflect.Value {
	return map[string]reflect.Value{
		"Each": reflect.ValueOf(func(n int, fn func(int)) {
			for index := range n {
				fn(index)
			}
		}),
		"Inc": reflect.ValueOf(func(n int) int { return n + 1 }),
		"Sort": reflect.ValueOf(func(values []int, less func(a, b int) bool) {
			sort.Slice(values, func(i, j int) bool { return less(values[i], values[j]) })
		}),
		"Try":   reflect.ValueOf(func(fn func() error) error { return fn() }),
		"Sleep": reflect.ValueOf(time.Sleep),
	}
}

const goExitLoopProgram = `package main

import "host"

type point struct{ x, y int }

%s

func run() int {
	n := 0
	lookup := map[string]int{"a": 1, "b": 2}
	keys := []string{"a", "b"}
	var grown []point
	var p *point
	_ = p
	host.Each(1, func(int) {
		for {
			%s
		}
	})
	return n + len(grown) + len(keys) + len(lookup)
}
`

var goExitLoops = []struct {
	name         string
	declarations string
	body         string
}{
	{name: "a native call", body: `n = host.Inc(n)`},
	{name: "a map index", body: `n += lookup[keys[n&1]]`},
	{name: "a growing append", body: `grown = append(grown, point{x: n, y: n})`},
	{name: "a tail call", declarations: "func spin(n int) int {\n\tif n < 0 {\n\t\treturn n\n\t}\n\treturn spin(n + 1)\n}", body: `n = spin(n)`},
	{name: "a nil test", body: "if p == nil {\n\t\t\t\tn = host.Inc(n)\n\t\t\t}"},
}

func TestCallbackTimeoutStopsGoExitLoops(t *testing.T) {
	t.Parallel()
	runCancellationChild(t, "TestCallbackTimeoutStopsGoExitLoopsChild")
}

func TestCallbackTimeoutStopsGoExitLoopsChild(t *testing.T) {
	cancellationChild(t, "TestCallbackTimeoutStopsGoExitLoopsChild")
	for _, loop := range goExitLoops {
		t.Run(loop.name, func(t *testing.T) {
			service := newCancellationService(t, cancellationExports(), WithCallbackTimeout(100*time.Millisecond))
			started := time.Now()

			_, err := service.EvalFile(t.Context(), fmt.Sprintf(goExitLoopProgram, loop.declarations, loop.body), "run")

			require.ErrorIs(t, err, fault.ErrCallbackTimeout, "the timeout stops the callback and fails the execution")
			require.ErrorIs(t, err, fault.ErrExecutionCancelled)
			require.Less(t, time.Since(started), 5*time.Second)
		})
	}
}

func TestExecutionDeadlineStopsNativeCallLoopInCallback(t *testing.T) {
	t.Parallel()
	runCancellationChild(t, "TestExecutionDeadlineStopsNativeCallLoopInCallbackChild")
}

func TestExecutionDeadlineStopsNativeCallLoopInCallbackChild(t *testing.T) {
	cancellationChild(t, "TestExecutionDeadlineStopsNativeCallLoopInCallbackChild")
	for _, loop := range goExitLoops {
		t.Run(loop.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()
			service := newCancellationService(t, cancellationExports())
			started := time.Now()

			_, err := service.EvalFile(ctx, fmt.Sprintf(goExitLoopProgram, loop.declarations, loop.body), "run")

			require.ErrorIs(t, err, fault.ErrExecutionCancelled)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Less(t, time.Since(started), 5*time.Second)
		})
	}
}

func TestNestedComparatorStopsWithExecution(t *testing.T) {
	t.Parallel()
	runCancellationChild(t, "TestNestedComparatorStopsWithExecutionChild")
}

func TestNestedComparatorStopsWithExecutionChild(t *testing.T) {
	cancellationChild(t, "TestNestedComparatorStopsWithExecutionChild")
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	service := newCancellationService(t, cancellationExports())
	started := time.Now()

	_, err := service.EvalFile(ctx, `package main

import "host"

func run() int {
	outer := []int{3, 1, 2}
	host.Sort(outer, func(a, b int) bool {
		inner := []int{2, 1}
		host.Sort(inner, func(c, d int) bool {
			n := 0
			for {
				n = host.Inc(n)
			}
		})
		return a < b
	})
	return outer[0]
}
`, "run")

	require.ErrorIs(t, err, fault.ErrExecutionCancelled, "cancellation reaches a callback two levels down")
	require.Less(t, time.Since(started), 5*time.Second)
}

func TestCallbackInGoroutineStopsWithExecution(t *testing.T) {
	t.Parallel()
	runCancellationChild(t, "TestCallbackInGoroutineStopsWithExecutionChild")
}

func TestCallbackInGoroutineStopsWithExecutionChild(t *testing.T) {
	cancellationChild(t, "TestCallbackInGoroutineStopsWithExecutionChild")
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	service := newCancellationService(t, cancellationExports())
	started := time.Now()

	_, err := service.EvalFile(ctx, `package main

import "host"

func run() int {
	done := make(chan int)
	go func() {
		host.Each(1, func(int) {
			n := 0
			for {
				n = host.Inc(n)
			}
		})
		done <- 1
	}()
	return <-done
}
`, "run")

	require.ErrorIs(t, err, fault.ErrExecutionCancelled)
	require.Less(t, time.Since(started), 5*time.Second)
}

func TestCallbackInVarInitialiserStopsWithExecution(t *testing.T) {
	t.Parallel()
	runCancellationChild(t, "TestCallbackInVarInitialiserStopsWithExecutionChild")
}

func TestCallbackInVarInitialiserStopsWithExecutionChild(t *testing.T) {
	cancellationChild(t, "TestCallbackInVarInitialiserStopsWithExecutionChild")
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	service := newCancellationService(t, cancellationExports())
	started := time.Now()

	_, err := service.EvalFile(ctx, `package main

import "host"

var spun = spin()

func spin() int {
	n := 0
	host.Each(1, func(int) {
		for {
			n = host.Inc(n)
		}
	})
	return n
}

func run() int { return spun }
`, "run")

	require.ErrorIs(t, err, fault.ErrExecutionCancelled)
	require.Less(t, time.Since(started), 5*time.Second)
}

func TestInFlightCallbackFinishesAfterMainReturns(t *testing.T) {
	t.Parallel()
	runCancellationChild(t, "TestInFlightCallbackFinishesAfterMainReturnsChild")
}

func TestInFlightCallbackFinishesAfterMainReturnsChild(t *testing.T) {
	cancellationChild(t, "TestInFlightCallbackFinishesAfterMainReturnsChild")
	started := make(chan struct{})
	mainReturned := make(chan struct{})
	finished := make(chan int, 1)
	exports := cancellationExports()
	exports["Start"] = reflect.ValueOf(func(fn func() int) {
		go func() { finished <- fn() }()
		<-started
	})
	exports["Started"] = reflect.ValueOf(func() { close(started) })
	exports["AfterMain"] = reflect.ValueOf(func() { <-mainReturned })
	service := newCancellationService(t, exports)

	_, err := service.EvalFile(t.Context(), `package main

import "host"

func run() int {
	host.Start(func() int {
		host.Started()
		host.AfterMain()
		total := 0
		for index := range 2000000 {
			total += index & 1
		}
		return total
	})
	return 0
}
`, "run")
	require.NoError(t, err)
	close(mainReturned)

	select {
	case total := <-finished:
		require.Equal(t, 1000000, total, "a callback running when main returns runs to completion")
	case <-time.After(10 * time.Second):
		require.Fail(t, "the in-flight callback did not finish")
	}
}

func TestCallbackTimeoutFailsTheRunningExecution(t *testing.T) {
	t.Parallel()
	runCancellationChild(t, "TestCallbackTimeoutFailsTheRunningExecutionChild")
}

func TestCallbackTimeoutFailsTheRunningExecutionChild(t *testing.T) {
	cancellationChild(t, "TestCallbackTimeoutFailsTheRunningExecutionChild")

	t.Run("a callback without an error result fails the execution", func(t *testing.T) {
		service := newCancellationService(t, cancellationExports(), WithCallbackTimeout(100*time.Millisecond))

		result, err := service.EvalFile(t.Context(), `package main

import "host"

func run() int {
	host.Each(1, func(int) {
		for {
		}
	})
	return 7
}
`, "run")

		require.ErrorIs(t, err, fault.ErrCallbackTimeout)
		require.ErrorIs(t, err, fault.ErrExecutionCancelled)
		require.Nil(t, result, "the execution must not carry on as if the callback had finished")
	})

	t.Run("a callback with an error result reports the timeout through it", func(t *testing.T) {
		var reported error
		exports := cancellationExports()
		exports["Try"] = reflect.ValueOf(func(fn func() error) error {
			reported = fn()
			return reported
		})
		service := newCancellationService(t, exports, WithCallbackTimeout(100*time.Millisecond))

		result, err := service.EvalFile(t.Context(), `package main

import "host"

func run() bool {
	err := host.Try(func() error {
		for {
		}
	})
	return err != nil
}
`, "run")

		require.NoError(t, err, "the program handles the error itself")
		require.Equal(t, true, result)
		require.ErrorIs(t, reported, fault.ErrCallbackTimeout)
	})
}

func TestCallbackTimeoutBoundsBlockedChannelOps(t *testing.T) {
	t.Parallel()
	runCancellationChild(t, "TestCallbackTimeoutBoundsBlockedChannelOpsChild")
}

func TestCallbackTimeoutBoundsBlockedChannelOpsChild(t *testing.T) {
	cancellationChild(t, "TestCallbackTimeoutBoundsBlockedChannelOpsChild")
	waits := []struct {
		name string
		body string
	}{
		{name: "a receive", body: `ch := make(chan int); <-ch`},
		{name: "a send", body: `ch := make(chan int); ch <- 1`},
		{name: "a select", body: `ch := make(chan int); select { case <-ch: }`},
		{name: "a range over a channel", body: `ch := make(chan int); for range ch { }`},
		{name: "an empty select", body: `select {}`},
		{name: "a sleep", body: `host.Sleep(3600000000000)`},
	}
	for _, wait := range waits {
		t.Run(wait.name, func(t *testing.T) {
			var reported error
			exports := cancellationExports()
			exports["Try"] = reflect.ValueOf(func(fn func() error) error {
				reported = fn()
				return reported
			})
			service := newCancellationService(t, exports, WithCallbackTimeout(100*time.Millisecond))
			started := time.Now()

			_, err := service.EvalFile(t.Context(), `package main

import "host"

func run() bool {
	err := host.Try(func() error {
		`+wait.body+`
		return nil
	})
	return err != nil
}
`, "run")

			require.NoError(t, err)
			require.ErrorIs(t, reported, fault.ErrCallbackTimeout, "the timeout ends the blocked wait")
			require.Less(t, time.Since(started), 5*time.Second)
		})
	}
}

func TestGoroutineSpawnedInTimedCallbackOutlivesIt(t *testing.T) {
	t.Parallel()
	runCancellationChild(t, "TestGoroutineSpawnedInTimedCallbackOutlivesItChild")
}

func TestGoroutineSpawnedInTimedCallbackOutlivesItChild(t *testing.T) {
	cancellationChild(t, "TestGoroutineSpawnedInTimedCallbackOutlivesItChild")
	exports := cancellationExports()
	exports["Pause"] = reflect.ValueOf(func() { time.Sleep(300 * time.Millisecond) })
	service := newCancellationService(t, exports, WithCallbackTimeout(100*time.Millisecond))

	result, err := service.EvalFile(t.Context(), `package main

import "host"

func run() int {
	done := make(chan int, 1)
	host.Each(1, func(int) {
		go func() {
			host.Pause()
			done <- 42
		}()
	})
	return <-done
}
`, "run")

	require.NoError(t, err, "the callback returned in time; its goroutine is not bound by its timeout")
	require.Equal(t, 42, result)
}

func TestBoundMethodsAndGoroutinesFromHostGoroutines(t *testing.T) {
	t.Parallel()
	var group sync.WaitGroup
	var calls atomic.Int64
	exports := cancellationExports()
	exports["Async"] = reflect.ValueOf(func(fn func() int) {
		group.Go(func() {
			for range 20 {
				calls.Add(int64(fn()))
			}
		})
	})
	exports["Wait"] = reflect.ValueOf(group.Wait)
	service := newCancellationService(t, exports)

	result, err := service.EvalFile(t.Context(), `package main

import "host"

type counter struct{ step int }

func (c counter) next() int { return c.step }

func run() int {
	done := make(chan int, 64)
	host.Each(32, func(index int) {
		c := counter{step: 1}
		host.Async(c.next)
		go func() { done <- index }()
	})
	host.Wait()
	total := 0
	for range 32 {
		total += <-done
	}
	return total
}
`, "run")

	require.NoError(t, err)
	require.Equal(t, 31*32/2, result)
	require.Equal(t, int64(32*20), calls.Load())
}
