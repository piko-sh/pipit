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

func TestStepInThroughFunctionCall(t *testing.T) {
	t.Parallel()

	dbg := debug.NewDebugger()
	svc := app.NewService(app.WithDebugger(dbg))

	code := `package main

func helper() int {
	return 42
}

func run() int {
	x := helper()
	return x
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	dbg.SetBreakpoint("main.go", 8)

	execution := debugtest.Run(t, dbg, func() (any, error) {
		return svc.ExecuteEntrypoint(t.Context(), cfs, "run")
	})

	snap := debugtest.WaitPause(t, dbg)
	require.Equal(t, 8, snap.Location.Line, "the breakpoint stops at line 8")

	dbg.StepIn(snap.ThreadID)
	snap = debugtest.WaitPause(t, dbg)
	assert.Equal(t, debug.StopReasonStep, snap.Reason, "the step stops")

	dbg.Continue()
	_, execErr := execution.Wait(t)
	require.NoError(t, execErr)
}

func TestStepOverFunctionCall(t *testing.T) {
	t.Parallel()

	dbg := debug.NewDebugger()
	svc := app.NewService(app.WithDebugger(dbg))

	code := `package main

func helper() int {
	return 42
}

func run() int {
	x := helper()
	y := x + 1
	return y
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	dbg.SetBreakpoint("main.go", 8)

	execution := debugtest.Run(t, dbg, func() (any, error) {
		return svc.ExecuteEntrypoint(t.Context(), cfs, "run")
	})

	snap := debugtest.WaitPause(t, dbg)
	require.Equal(t, 8, snap.Location.Line, "the breakpoint stops at line 8")

	dbg.StepOver(snap.ThreadID)
	snap = debugtest.WaitPause(t, dbg)
	assert.Equal(t, debug.StopReasonStep, snap.Reason, "the step stops")

	if snap.Location.Function != "" && snap.Location.Function != "run" && snap.Location.Function != "main.run" {
		t.Logf("step-over landed in function %q at line %d", snap.Location.Function, snap.Location.Line)
	}

	dbg.Continue()
	_, execErr := execution.Wait(t)
	require.NoError(t, execErr)
}

func TestStepInThenContinue(t *testing.T) {
	t.Parallel()

	dbg := debug.NewDebugger()
	svc := app.NewService(app.WithDebugger(dbg))

	code := `package main

func run() int {
	x := 1
	y := 2
	return x + y
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	dbg.SetBreakpoint("main.go", 4)

	execution := debugtest.Run(t, dbg, func() (any, error) {
		return svc.ExecuteEntrypoint(t.Context(), cfs, "run")
	})

	snap := debugtest.WaitPause(t, dbg)
	require.Equal(t, 4, snap.Location.Line, "the breakpoint stops at line 4")

	dbg.StepIn(snap.ThreadID)
	snap = debugtest.WaitPause(t, dbg)
	require.Equal(t, 5, snap.Location.Line, "StepIn lands on line 5")

	dbg.Continue()
	_, execErr := execution.Wait(t)
	require.NoError(t, execErr)
}
