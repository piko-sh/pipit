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

//go:build integration && pipit_testhooks

package bytecode_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/sdk/stdlib"
)

const lockWindowSource = `package main

import "sync"

var mu sync.Mutex

var rw sync.RWMutex

var count int

func LockFree() int {
	mu.Lock()
	defer mu.Unlock()
	return 7
}

func WriterFree() int {
	rw.Lock()
	defer rw.Unlock()
	return 8
}

func Nothing() int { return 3 }

func record() {
	mu.Lock()
	defer mu.Unlock()
	count++
}

func MainInWindow() int {
	record()
	return count
}

func GoroutineInWindow() int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		record()
	}()
	wg.Wait()
	return count
}

func ReaderInWindow() int {
	rw.RLock()
	defer rw.RUnlock()
	return count
}

func LocalInWindow() int {
	var local sync.Mutex
	local.Lock()
	defer local.Unlock()
	return count
}
`

var lockWindowLines = map[string]string{
	"record":         "main.go:" + lineOf(lockWindowSource, "func record()", 1),
	"ReaderInWindow": "main.go:" + lineOf(lockWindowSource, "func ReaderInWindow()", 1),
}

func lineOf(source, marker string, offset int) string {
	for index, line := range strings.Split(source, "\n") {
		if strings.Contains(line, marker) {
			return strconv.Itoa(index + 1 + offset)
		}
	}
	return "?"
}

func lockWindowCompile(t *testing.T, lane []app.Option) (*app.Service, *program.CompiledFileSet) {
	t.Helper()
	service := newTestService(t, lane...)
	service.UseSymbolProviders(stdlib.Providers()...)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": lockWindowSource})
	require.NoError(t, err)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	return service, compiled
}

func lockWindowStop(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, function string) {
	t.Helper()
	require.NoError(t, engine.ArmAbortInjection("lock-window", 1))
	err := stopCall(t, t.Context(), service, compiled, function)
	report := engine.DisarmAbortInjection()
	require.True(t, report.Fired, "the lock-window stop fired")
	require.True(t, errors.Is(err, fault.ErrExecutionCancelled) || errors.Is(err, fault.ErrLockAbandoned),
		"the stopped run reports the cancellation or the abandoned lock: %v", err)
}

func requireFailsAtOnce(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, function string, names ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), mainReturnLockWait)
	defer cancel()
	started := time.Now()
	_, err := service.CallFunction(ctx, compiled, function)
	took := time.Since(started)
	require.ErrorIs(t, err, fault.ErrLockAbandoned, "%s fails closed (it returned after %v)", function, took)
	require.Less(t, took, mainReturnLockWait/2, "%s fails at once instead of waiting for its deadline", function)
	for _, name := range names {
		require.Contains(t, err.Error(), name, "the error names the lock")
	}
}

func TestStopInTheLockWindowFailsTheServiceClosed(t *testing.T) {
	cases := []struct {
		function string
		taker    string
		names    []string
	}{
		{function: "MainInWindow", taker: "LockFree", names: []string{"sync.Mutex", "main.record", lockWindowLines["record"]}},
		{function: "GoroutineInWindow", taker: "LockFree", names: []string{"sync.Mutex", "main.record", lockWindowLines["record"]}},
		{function: "ReaderInWindow", taker: "WriterFree", names: []string{"sync.RWMutex", "main.ReaderInWindow", lockWindowLines["ReaderInWindow"]}},
	}
	for _, testCase := range cases {
		for _, lane := range abortUnwindLanes {
			t.Run(testCase.function+"/"+lane.name, func(t *testing.T) {
				service, compiled := lockWindowCompile(t, lane.options)
				lockWindowStop(t, service, compiled, testCase.function)
				requireFailsAtOnce(t, service, compiled, testCase.taker, testCase.names...)
				requireFailsAtOnce(t, service, compiled, "Nothing", testCase.names...)
				require.EqualValues(t, 1, service.AbandonedLocks(), "the held lock is counted as abandoned")
				require.Zero(t, service.LeakedGoroutines(), "the join counted no goroutine as leaked")
			})
		}
	}
}

func TestStopInTheLockWindowOfALocalLockDoesNotFailClosed(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled := lockWindowCompile(t, lane.options)
			lockWindowStop(t, service, compiled, "LocalInWindow")
			require.Zero(t, service.AbandonedLocks(), "a local lock is not abandoned")
			requireMutexFreePromptly(t, service, compiled)
		})
	}
}
