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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/sdk/stdlib"
)

const abortSafetyCallbackSource = `package main

import (
	"sort"
	"strconv"
)

var kept []string

func Run() int {
	recovered := 0
	func() {
		defer func() {
			if recover() != nil {
				recovered = 1
			}
		}()
		rows := []int{3, 1, 2}
		sort.Slice(rows, func(a, b int) bool {
			for i := 0; i < 200000; i++ {
				kept = append(kept, "grow-"+strconv.Itoa(i))
			}
			return rows[a] < rows[b]
		})
	}()
	return recovered
}
`

const abortSafetyGoroutinePanicSource = `package main

import "sync"

func Spawn(fail bool) int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if fail {
			var xs []int
			_ = xs[3]
		}
	}()
	wg.Wait()
	return 1
}

func Plain() int { return 7 }
`

const abortSafetyChurnSource = `package main

func Churn() int {
	sink := 0
	for j := 0; j < 400000; j++ {
		tmp := make([]int, 4)
		tmp[0] = j
		sink += tmp[0] - j + 1
	}
	return sink
}
`

const abortSafetyKeepSource = `package main

type node struct {
	next *node
	v    int
}

func KeepInts(n int) int {
	xs := make([]int, n)
	return len(xs)
}

func KeepAny(n int) int {
	xs := make([]any, n)
	for i := range xs {
		xs[i] = i
	}
	return len(xs)
}

func KeepNodes(n int) int {
	var head *node
	for i := range n {
		head = &node{next: head, v: i}
	}
	return head.v
}
`

func abortSafetyService(t *testing.T, options ...app.Option) *app.Service {
	t.Helper()
	service := newTestService(t, options...)
	service.UseSymbolProviders(stdlib.Providers()...)
	return service
}

func TestBudgetAbortInACallbackIsNotRecoverable(t *testing.T) {
	service := abortSafetyService(t, app.WithMaxArenaSizeBytes(2<<20))
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": abortSafetyCallbackSource})
	require.NoError(t, err)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	result, err := service.CallFunction(t.Context(), compiled, "Run")
	require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded, "result %v", result)
}

func TestGoroutinePanicDoesNotFailLaterExecutions(t *testing.T) {
	service := abortSafetyService(t)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": abortSafetyGoroutinePanicSource})
	require.NoError(t, err)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	_, err = service.CallFunction(t.Context(), compiled, "Spawn", true)
	require.Error(t, err, "the spawning execution reports its goroutine's panic")
	result, err := service.CallFunction(t.Context(), compiled, "Plain")
	require.NoError(t, err, "a later execution must not inherit the panic")
	require.Equal(t, 7, result)
	result, err = service.CallFunction(t.Context(), compiled, "Spawn", false)
	require.NoError(t, err)
	require.Equal(t, 1, result)
}

func TestSoftLimitServesEveryEntryPoint(t *testing.T) {
	var deferred int64
	for mib := uint64(3); mib <= 8; mib++ {
		for _, dispatch := range dispatchLoops {
			options := append([]app.Option{app.WithMaxArenaSizeBytes(mib << 20)}, dispatch.options...)
			service := abortSafetyService(t, options...)
			compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": abortSafetyChurnSource})
			require.NoError(t, err)
			before := engine.LicenceDiagnostics().Reuse
			result, err := service.CallFunction(t.Context(), compiled, "Churn")
			require.NoErrorf(t, err, "%s loop at %d MiB", dispatch.name, mib)
			require.EqualValuesf(t, 400000, result, "%s loop at %d MiB", dispatch.name, mib)
			deferred += engine.LicenceDiagnostics().Reuse.DeferredCollections - before.DeferredCollections
		}
	}
	require.Positive(t, deferred, "a grow past the budget must defer to a safepoint")
}

func TestSoftLimitServesARunOnAnArenaThatKeptMemory(t *testing.T) {
	keeper := abortSafetyService(t)
	keeping, err := keeper.CompileFileSet(t.Context(), map[string]string{"main.go": abortSafetyKeepSource})
	require.NoError(t, err)
	for _, keep := range []struct {
		function string
		count    int
	}{{function: "KeepInts", count: 3000}, {function: "KeepAny", count: 1000}, {function: "KeepNodes", count: 1000}} {
		for _, dispatch := range dispatchLoops {
			options := append([]app.Option{app.WithMaxArenaSizeBytes(3 << 20)}, dispatch.options...)
			service := abortSafetyService(t, options...)
			compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": abortSafetyChurnSource})
			require.NoError(t, err)
			_, err = keeper.CallFunction(t.Context(), keeping, keep.function, keep.count)
			require.NoError(t, err)
			result, err := service.CallFunction(t.Context(), compiled, "Churn")
			require.NoErrorf(t, err, "%s loop after %s", dispatch.name, keep.function)
			require.EqualValuesf(t, 400000, result, "%s loop after %s", dispatch.name, keep.function)
		}
	}
}
