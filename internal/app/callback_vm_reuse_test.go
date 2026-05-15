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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab"
)

type callbackHarness struct {
	service *Service
	vm      *engine.VM
}

func newCallbackHarness(t *testing.T, exports map[string]reflect.Value) *callbackHarness {
	t.Helper()
	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{"host": exports})
	service := NewService()
	service.UseSymbols(symbols)
	return &callbackHarness{
		service: service,
		vm:      engine.NewVM(context.Background(), engine.NewGlobalStore(), symbols),
	}
}

func (h *callbackHarness) compile(t *testing.T, source string) *program.CompiledFunction {
	t.Helper()
	compiled, err := h.service.Compile(context.Background(), source)
	require.NoError(t, err)
	return compiled
}

func (h *callbackHarness) run(t *testing.T, source string) any {
	t.Helper()
	result, err := h.vm.Execute(h.compile(t, source))
	require.NoError(t, err)
	return result
}

func TestClosureWrapperReusesVMAcrossInvocations(t *testing.T) {
	t.Parallel()
	const walkLength = 40
	var stats []engine.CallbackVMStats
	var results []string
	var harness *callbackHarness
	harness = newCallbackHarness(t, map[string]reflect.Value{
		"Walk": reflect.ValueOf(func(fn func(int) string) int {
			for index := range walkLength {
				results = append(results, fn(index))
				stats = append(stats, harness.vm.CallbackVMStats())
			}
			return len(results)
		}),
	})
	result := harness.run(t, `import "host"
prefix := "value-"
host.Walk(func(index int) string { return prefix + string(rune('a'+index%26)) + "-tail" })`)
	require.EqualValues(t, walkLength, result)

	for index, stat := range stats {
		require.Equal(t, engine.CallbackVMStats{Built: 1, Discarded: 0, Idle: true}, stat, "callback %d ran on the one callback VM and parked it", index)
	}
	for index, got := range results {
		require.Equal(t, "value-"+string(rune('a'+index%26))+"-tail", got, "result %d intact after later callbacks reused the VM", index)
	}
	require.Equal(t, engine.CallbackVMStats{Built: 1, Discarded: 1, Idle: false}, harness.vm.CallbackVMStats(), "the parent's release drained the cache")
}

func TestClosureWrapperDifferentRootResetsClosureCache(t *testing.T) {
	t.Parallel()
	var first func() int
	harness := newCallbackHarness(t, map[string]reflect.Value{
		"Keep": reflect.ValueOf(func(fn func() int) { first = fn }),
		"Both": reflect.ValueOf(func(fn func() int) int {
			total := 0
			for range 3 {
				total += first()
				total += fn()
			}
			return total
		}),
	})
	harness.run(t, `import "host"
host.Keep(func() int { inner := func() int { return 7 }; return inner() })`)
	result := harness.run(t, `import "host"
host.Both(func() int { inner := func() int { return 100 }; return inner() })`)
	require.EqualValues(t, 3*(7+100), result, "each callback ran its own root's closure, not one cached for the other root")
	require.Equal(t, engine.CallbackVMStats{Built: 1, Discarded: 1, Idle: false}, harness.vm.CallbackVMStats(), "one callback VM served both roots")
}

func TestClosureWrapperPanicDiscardsVM(t *testing.T) {
	t.Parallel()
	var afterClean, afterPanic engine.CallbackVMStats
	var recovered any
	var harness *callbackHarness
	harness = newCallbackHarness(t, map[string]reflect.Value{
		"Try": reflect.ValueOf(func(fn func(bool) int) int {
			_ = fn(false)
			afterClean = harness.vm.CallbackVMStats()
			func() {
				defer func() { recovered = recover() }()
				_ = fn(true)
			}()
			afterPanic = harness.vm.CallbackVMStats()
			return fn(false)
		}),
	})
	result := harness.run(t, `import "host"
host.Try(func(fail bool) int { if fail { panic("boom") }; return 3 })`)
	require.EqualValues(t, 3, result)
	require.NotNil(t, recovered, "the interpreted panic crossed to the host")
	require.Equal(t, engine.CallbackVMStats{Built: 1, Discarded: 0, Idle: true}, afterClean, "a clean callback parks its VM")
	require.Equal(t, engine.CallbackVMStats{Built: 1, Discarded: 1, Idle: false}, afterPanic, "a callback that panicked releases its VM")
	require.EqualValues(t, 2, harness.vm.CallbackVMStats().Built, "the callback after the panic built afresh")
}

func TestClosureWrapperStackOverflowDiscardsVM(t *testing.T) {
	t.Parallel()
	var afterOverflow engine.CallbackVMStats
	var harness *callbackHarness
	harness = newCallbackHarness(t, map[string]reflect.Value{
		"Try": reflect.ValueOf(func(fn func(int) int) int {
			_ = fn(0)
			afterOverflow = harness.vm.CallbackVMStats()
			return fn(1)
		}),
	})
	harness.vm.Limits.MaxCallDepth = 64
	result := harness.run(t, `import "host"
func recurse(n int) int { return recurse(n+1) + 1 }
host.Try(func(n int) int { if n == 0 { return recurse(0) }; return n })`)
	require.EqualValues(t, 1, result)
	require.Equal(t, engine.CallbackVMStats{Built: 1, Discarded: 1, Idle: false}, afterOverflow, "a callback that overflowed its stack releases its VM")
	require.EqualValues(t, 2, harness.vm.CallbackVMStats().Built)
}

func TestClosureWrapperSafeModeBuildsFreshVMs(t *testing.T) {
	t.Parallel()
	var stats []engine.CallbackVMStats
	var harness *callbackHarness
	harness = newCallbackHarness(t, map[string]reflect.Value{
		"Walk": reflect.ValueOf(func(fn func(int) int) int {
			total := 0
			for index := range 3 {
				total += fn(index)
				stats = append(stats, harness.vm.CallbackVMStats())
			}
			return total
		}),
	})
	harness.vm.Limits.SafeMode = true
	harness.vm.Limits.ForceGoDispatch = true
	result := harness.run(t, `import "host"
host.Walk(func(index int) int { return index })`)
	require.EqualValues(t, 3, result)
	require.Len(t, stats, 3)
	for _, stat := range stats {
		require.Equal(t, engine.CallbackVMStats{Built: 0, Discarded: 0, Idle: false}, stat, "safe mode never caches a callback VM")
	}
}

func TestClosureWrapperReturnsUnspentCostChunk(t *testing.T) {
	t.Parallel()
	var idle []bool
	var harness *callbackHarness
	harness = newCallbackHarness(t, map[string]reflect.Value{
		"Walk": reflect.ValueOf(func(fn func(int) int) int {
			total := 0
			for index := range 3 {
				total += fn(index)
				idle = append(idle, harness.vm.CallbackVMStats().Idle)
			}
			return total
		}),
	})
	harness.vm.Limits.CostBudget = 1 << 20
	harness.vm.Limits.Tracker = &engine.ResourceTracker{}
	harness.vm.Limits.CostTable = isa.SharedDefaultCostTable()
	result := harness.run(t, `import "host"
host.Walk(func(index int) int { return index + 1 })`)
	require.EqualValues(t, 6, result)
	require.Equal(t, []bool{true, true, true}, idle, "every callback parked its VM")
	require.Less(t, harness.vm.CostUsed(), int64(3*engine.CostChunk), "the callbacks' unspent chunks went back to the shared budget")
}
