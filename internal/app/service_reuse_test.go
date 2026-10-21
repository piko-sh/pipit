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
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/policy"
)

const varInitReuseSource = "package main\n\nvar table = []int{1, 2, 3}\n\nfunc run() int { return table[1] }\n"

const varInitReuseCounterSource = "package main\n\nvar table = []int{1, 2, 3}\n\nvar calls = 0\n\nfunc run() int { return table[1] }\n\nfunc count() int { calls++; return calls }\n"

func varInitReuseStateCount(service *Service) int {
	registry := &service.varInitReuse
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return len(registry.states)
}

func TestVarInitReuseStateIsDroppedWithItsFileSet(t *testing.T) {
	service := newTestService(t)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": varInitReuseSource})
	require.NoError(t, err)
	require.True(t, service.VarInitReusePlanFor(compiled).Eligible)
	result, err := service.ExecuteEntrypoint(t.Context(), compiled, "run")
	require.NoError(t, err)
	require.EqualValues(t, 2, result)
	require.Equal(t, 1, varInitReuseStateCount(service))

	compiled = nil
	require.Eventually(t, func() bool {
		runtime.GC()
		return varInitReuseStateCount(service) == 0
	}, 5*time.Second, 10*time.Millisecond, "a collected file set takes its reuse state with it")
}

func varInitReuseWatches(service *Service) []*engine.GlobalReuseWatch {
	registry := &service.varInitReuse
	registry.mu.Lock()
	defer registry.mu.Unlock()
	var watches []*engine.GlobalReuseWatch
	for _, state := range registry.states {
		if state.watch != nil {
			watches = append(watches, state.watch)
		}
	}
	return watches
}

func TestVarInitReuseWatchIsStoppedWithItsFileSet(t *testing.T) {
	service := newTestService(t)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": varInitReuseSource})
	require.NoError(t, err)
	_, err = service.ExecuteEntrypoint(t.Context(), compiled, "run")
	require.NoError(t, err)
	watches := varInitReuseWatches(service)
	require.Len(t, watches, 1)
	require.True(t, watches[0].Installed())

	compiled = nil
	require.Eventually(t, func() bool {
		runtime.GC()
		return !watches[0].Installed()
	}, 5*time.Second, 10*time.Millisecond, "a collected file set's watch is removed")
}

func TestServiceCloseStopsTheReuseWatches(t *testing.T) {
	service := newTestService(t)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": varInitReuseSource})
	require.NoError(t, err)
	_, err = service.ExecuteEntrypoint(t.Context(), compiled, "run")
	require.NoError(t, err)
	watches := varInitReuseWatches(service)
	require.Len(t, watches, 1)
	require.True(t, watches[0].Installed())

	require.NoError(t, service.Close())
	require.False(t, watches[0].Installed(), "Close removes the watch")
	require.Zero(t, varInitReuseStateCount(service), "Close drops the reuse state")
	require.NoError(t, service.Close(), "Close is idempotent")

	_, err = service.ExecuteEntrypoint(t.Context(), compiled, "run")
	require.ErrorIs(t, err, policy.ErrClosed)
	_, err = service.CallFunction(t.Context(), compiled, "run")
	require.ErrorIs(t, err, policy.ErrClosed)
	require.ErrorIs(t, service.ExecuteInits(t.Context(), compiled), policy.ErrClosed)
	_, err = service.Eval(t.Context(), "1 + 1")
	require.ErrorIs(t, err, policy.ErrClosed)
	_, err = service.EvalFile(t.Context(), varInitReuseSource, "run")
	require.ErrorIs(t, err, policy.ErrClosed)
	_, err = service.Compile(t.Context(), "1 + 1")
	require.ErrorIs(t, err, policy.ErrClosed)
	_, err = service.CompileFileSet(t.Context(), map[string]string{"main.go": varInitReuseSource})
	require.ErrorIs(t, err, policy.ErrClosed)
	_, err = service.CompileProgram(t.Context(), "example.com/m", map[string]map[string]string{"example.com/m": {"main.go": varInitReuseSource}})
	require.ErrorIs(t, err, policy.ErrClosed)
	require.Zero(t, varInitReuseStateCount(service), "nothing records reuse state after Close")
}

func armedReuseWatch(t *testing.T) (*engine.GlobalReuseWatch, any) {
	t.Helper()
	service := newTestService(t)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": varInitReuseSource})
	require.NoError(t, err)
	_, err = service.ExecuteEntrypoint(t.Context(), compiled, "run")
	require.NoError(t, err)
	watches := varInitReuseWatches(service)
	require.Len(t, watches, 1)
	return watches[0], compiled
}

func TestVarInitReuseWatchIsStoppedWithItsService(t *testing.T) {
	watch, compiled := armedReuseWatch(t)
	require.True(t, watch.Installed())
	require.Eventually(t, func() bool {
		runtime.GC()
		return !watch.Installed()
	}, 5*time.Second, 10*time.Millisecond, "a collected service's watch is removed while its file set lives on")
	runtime.KeepAlive(compiled)
}

func TestVarInitReuseStatisticsCountSkipsAndReruns(t *testing.T) {
	service := newTestService(t)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": varInitReuseCounterSource})
	require.NoError(t, err)
	require.True(t, service.VarInitReusePlanFor(compiled).Eligible)

	for range 2 {
		_, err = service.ExecuteEntrypoint(t.Context(), compiled, "run")
		require.NoError(t, err)
	}
	stats := service.VarInitReuseStatistics()
	require.EqualValues(t, 1, stats.Reruns, "the first run has no image to reuse")
	require.EqualValues(t, 1, stats.Skips, "the second run reuses the first run's globals")
	require.Equal(t, "no finished initialiser run", stats.LastFallback)

	for range 3 {
		_, err = service.CallFunction(t.Context(), compiled, "count")
		require.NoError(t, err)
		_, err = service.ExecuteEntrypoint(t.Context(), compiled, "run")
		require.NoError(t, err)
	}
	stats = service.VarInitReuseStatistics()
	require.EqualValues(t, 4, stats.Reruns, "a changed global makes each later run repeat its initialisers")
	require.Contains(t, stats.LastFallback, "backing off", "repeated re-runs back off from capturing images")
	require.Zero(t, stats.Ineligible)
}

func TestVarInitReuseStatisticsCountIneligibleFileSets(t *testing.T) {
	service := newTestService(t)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": "package main\n\nvar table = []int{1}\n\nfunc run() []int { return table }\n"})
	require.NoError(t, err)
	plan := service.VarInitReusePlanFor(compiled)
	require.False(t, plan.Eligible)
	_, err = service.ExecuteEntrypoint(t.Context(), compiled, "run")
	require.NoError(t, err)
	require.EqualValues(t, 1, service.VarInitReuseStatistics().Ineligible)
}

func TestVarInitReusePlanForAnUnknownFileSetIsTheZeroPlan(t *testing.T) {
	other := newTestService(t)
	compiled, err := other.CompileFileSet(t.Context(), map[string]string{"main.go": varInitReuseSource})
	require.NoError(t, err)
	require.Zero(t, newTestService(t).VarInitReusePlanFor(compiled), "another service's file set has no plan here")
}

func TestDeclareNativeBorrowClassRecordsAndRefuses(t *testing.T) {
	service := newTestService(t)
	measure := func(s string) int { return len(s) }
	require.NoError(t, service.DeclareNativeBorrowClass(measure, engine.BorrowBorrows))

	err := service.DeclareNativeBorrowClass("not a function", engine.BorrowBorrows)
	require.ErrorIs(t, err, engine.ErrBorrowDeclarationRefused)
	require.Len(t, service.BorrowDeclarationRefusals(), 1)
	require.Contains(t, service.BorrowDeclarationRefusals()[0], "not a function")

	_, err = service.Eval(t.Context(), "1 + 1")
	require.NoError(t, err)
	require.ErrorIs(t, service.DeclareNativeBorrowClass(func(s string) int { return len(s) + 1 }, engine.BorrowBorrows), engine.ErrBorrowDeclarationsFrozen,
		"declarations freeze at the first evaluation")
}
