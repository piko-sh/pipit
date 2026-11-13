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

package debug_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/debug/debugtest"
)

func TestBreakpointHitsCorrectLine(t *testing.T) {
	t.Parallel()

	dbg := debug.NewDebugger()
	svc := app.NewService(app.WithDebugger(dbg))

	code := `package main

func run() int {
	x := 10
	y := 20
	return x + y
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	dbg.SetBreakpoint("main.go", 5)

	execution := debugtest.Run(t, dbg, func() (any, error) {
		return svc.ExecuteEntrypoint(t.Context(), cfs, "run")
	})

	snap := debugtest.WaitPause(t, dbg)
	assert.Equal(t, 5, snap.Location.Line, "the breakpoint stops at line 5")
	assert.Equal(t, debug.StopReasonBreakpoint, snap.Reason)

	dbg.Continue()
	_, execErr := execution.Wait(t)
	require.NoError(t, execErr)
}

func TestBreakpointInLoop(t *testing.T) {
	t.Parallel()

	dbg := debug.NewDebugger()
	svc := app.NewService(app.WithDebugger(dbg))

	code := `package main

func run() int {
	sum := 0
	for i := 0; i < 3; i++ {
		sum += i
	}
	return sum
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	dbg.SetBreakpoint("main.go", 6)

	execution := debugtest.Run(t, dbg, func() (any, error) {
		return svc.ExecuteEntrypoint(t.Context(), cfs, "run")
	})

	hitCount := 0
	for range 3 {
		snap := debugtest.WaitPause(t, dbg)
		assert.Equal(t, 6, snap.Location.Line, "iteration %d stops at line 6", hitCount)
		hitCount++
		dbg.Continue()
	}

	assert.Equal(t, 3, hitCount)

	_, execErr := execution.Wait(t)
	require.NoError(t, execErr)
}

func TestMultipleBreakpoints(t *testing.T) {
	t.Parallel()

	dbg := debug.NewDebugger()
	svc := app.NewService(app.WithDebugger(dbg))

	code := `package main

func run() int {
	a := 1
	b := 2
	c := 3
	return a + b + c
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	dbg.SetBreakpoint("main.go", 4)
	dbg.SetBreakpoint("main.go", 6)

	execution := debugtest.Run(t, dbg, func() (any, error) {
		return svc.ExecuteEntrypoint(t.Context(), cfs, "run")
	})

	snap1 := debugtest.WaitPause(t, dbg)
	assert.Equal(t, 4, snap1.Location.Line, "the first breakpoint stops at line 4")
	dbg.Continue()

	snap2 := debugtest.WaitPause(t, dbg)
	assert.Equal(t, 6, snap2.Location.Line, "the second breakpoint stops at line 6")
	dbg.Continue()

	_, execErr := execution.Wait(t)
	require.NoError(t, execErr)
}

func TestClearAndResetBreakpoint(t *testing.T) {
	t.Parallel()

	dbg := debug.NewDebugger()
	svc := app.NewService(app.WithDebugger(dbg))

	code := `package main

func run() int {
	a := 1
	b := 2
	return a + b
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	dbg.SetBreakpoint("main.go", 4)
	dbg.ClearBreakpoint("main.go", 4)
	dbg.SetBreakpoint("main.go", 5)

	execution := debugtest.Run(t, dbg, func() (any, error) {
		return svc.ExecuteEntrypoint(t.Context(), cfs, "run")
	})

	snap := debugtest.WaitPause(t, dbg)
	assert.Equal(t, 5, snap.Location.Line, "the breakpoint stops at line 5, not 4")
	dbg.Continue()

	_, execErr := execution.Wait(t)
	require.NoError(t, execErr)
}
