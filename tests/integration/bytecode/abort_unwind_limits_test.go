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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/sdk/stdlib"
)

const unwindLimitsSource = `package main

import (
	"hostblock"
	"strconv"
	"sync"
	"time"
)

var mu sync.Mutex

var spin int

var released int

func grow() int {
	var local []string
	for i := 0; i < 400000; i++ {
		local = append(local, "grow-"+strconv.Itoa(i))
	}
	return len(local)
}

func Spin() int { return spin }

func Released() int { return released }

func LockFree() int {
	mu.Lock()
	defer mu.Unlock()
	return 7
}

func FiniteLoopingDefer() int {
	defer func() {
		for i := 0; i < 2000; i++ {
			spin++
		}
	}()
	grow()
	return 1
}

func SleepingDefer() int {
	defer func() {
		time.Sleep(100 * time.Millisecond)
		released = 1
	}()
	grow()
	return 1
}

func EntryDeferBlocksInNative() int {
	defer func() { released = 1 }()
	defer func() { hostblock.Block() }()
	grow()
	return 1
}

func GoroutineDeferBlocksInNativeAtMainReturn() int {
	held := make(chan bool)
	go func() {
		defer func() { hostblock.Block() }()
		held <- true
		for {
			spin++
		}
	}()
	<-held
	return 1
}

func UnlocksThenStops() int {
	mu.Lock()
	mu.Unlock()
	grow()
	return 1
}
`

func unwindLimitsCompile(t *testing.T, options ...app.Option) (*app.Service, *program.CompiledFileSet, *hostBlock) {
	t.Helper()
	service := newTestService(t, append([]app.Option{app.WithMaxArenaSizeBytes(2 << 20)}, options...)...)
	require.NoError(t, service.UseSymbolProviders(stdlib.Providers()...))
	block := newHostBlock()
	t.Cleanup(block.unblock)
	service.RegisterPackage("hostblock", block.symbols())
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": unwindLimitsSource})
	require.NoError(t, err)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	return service, compiled, block
}

func TestUnwindLimitDefaultsLetBoundedDefersFinish(t *testing.T) {
	service, compiled, _ := unwindLimitsCompile(t)
	_, err := abortUnwindCall(t, service, compiled, "FiniteLoopingDefer")
	require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
	require.Equal(t, 2000, unwindRulesInt(t, service, compiled, "Spin"), "the default cost allowance covers the loop")
	_, err = abortUnwindCall(t, service, compiled, "SleepingDefer")
	require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
	require.Equal(t, 1, unwindRulesInt(t, service, compiled, "Released"), "the default wall allowance covers the sleep")
	_, err = abortUnwindCall(t, service, compiled, "UnlocksThenStops")
	require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
	requireLockFree(t, service, compiled)
}

func TestWithAbortUnwindCostCutsTheDefer(t *testing.T) {
	service, compiled, _ := unwindLimitsCompile(t, app.WithAbortUnwindCost(500))
	_, err := abortUnwindCall(t, service, compiled, "FiniteLoopingDefer")
	require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
	spin := unwindRulesInt(t, service, compiled, "Spin")
	require.Positive(t, spin, "the defer ran")
	require.Less(t, spin, 2000, "the defer was cut at the configured cost")
}

func TestWithAbortUnwindWallCutsTheDefer(t *testing.T) {
	for _, option := range []struct {
		name   string
		option app.Option
	}{
		{name: "total", option: app.WithAbortUnwindWall(20 * time.Millisecond)},
		{name: "per defer", option: app.WithAbortUnwindDeferWallMax(10 * time.Millisecond)},
	} {
		t.Run(option.name, func(t *testing.T) {
			service, compiled, _ := unwindLimitsCompile(t, option.option)
			_, err := abortUnwindCall(t, service, compiled, "SleepingDefer")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
			require.Zero(t, unwindRulesInt(t, service, compiled, "Released"), "the sleep was cut at the configured wall time")
		})
	}
}

func TestWithAbortUnwindNativeGraceWaitsForTheNativeCall(t *testing.T) {
	service, compiled, block := unwindLimitsCompile(t, app.WithAbortUnwindNativeGrace(time.Minute))
	release := time.AfterFunc(1500*time.Millisecond, block.unblock)
	defer release.Stop()
	_, err := abortUnwindCall(t, service, compiled, "EntryDeferBlocksInNative")
	require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
	require.NotErrorIs(t, err, fault.ErrUnwindBlocked, "the native call returned within the configured grace")
	select {
	case <-block.returned:
	default:
		require.FailNow(t, "the call returned before the native call did")
	}
	requireLockFree(t, service, compiled)
}

func TestWithLeakedUnwinderCapFailsTheServiceClosed(t *testing.T) {
	service, compiled, _ := unwindLimitsCompile(t, app.WithLeakedUnwinderCap(1))
	_, err := abortUnwindCall(t, service, compiled, "EntryDeferBlocksInNative")
	require.ErrorIs(t, err, fault.ErrUnwindBlocked)
	require.ErrorIs(t, err, fault.ErrUnwindLeakLimit, "the first leak reaches a cap of one")
	_, err = abortUnwindCall(t, service, compiled, "LockFree")
	require.ErrorIs(t, err, fault.ErrUnwindLeakLimit, "the service failed closed")
}

func TestWithGoroutineJoinUnwindCapStopsWaitingForAnUnwinding(t *testing.T) {
	service, compiled, block := unwindLimitsCompile(t,
		app.WithAbortUnwindNativeGrace(time.Minute), app.WithGoroutineJoinUnwindCap(50*time.Millisecond))
	started := time.Now()
	_, err := abortUnwindCall(t, service, compiled, "GoroutineDeferBlocksInNativeAtMainReturn")
	took := time.Since(started)
	require.ErrorIs(t, err, fault.ErrGoroutineJoinTimeout)
	require.Less(t, took, 5*time.Second, "the join stopped waiting at the configured cap, not the ten-second default")
	block.unblock()
	select {
	case <-block.returned:
	case <-time.After(abortUnwindHangAfter):
		require.FailNow(t, "the host call did not return after its release")
	}
}

func TestWithLockWalkMaxValuesFailsClosedWhenTheCheckIsCut(t *testing.T) {
	service, compiled, _ := unwindLimitsCompile(t, app.WithLockWalkMaxValues(1))
	_, err := abortUnwindCall(t, service, compiled, "UnlocksThenStops")
	require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
	_, err = abortUnwindCall(t, service, compiled, "LockFree")
	require.ErrorIs(t, err, fault.ErrLockAbandoned, "a check cut at one value fails the service closed")
	require.True(t, strings.Contains(err.Error(), "stopped at its budget"), "the report says the check was cut: %v", err)
}
