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
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/debug/debugtest"
	"pipit.sh/pipit/internal/fault"
)

func TestFullDebugSession(t *testing.T) {
	t.Parallel()

	dbg := debug.NewDebugger()
	svc := app.NewService(app.WithDebugger(dbg))

	code := `package main

func run() int {
	a := 1
	b := 2
	c := a + b
	return c
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	dbg.SetBreakpoint("main.go", 5)

	execution := debugtest.Run(t, dbg, func() (any, error) {
		return svc.ExecuteEntrypoint(context.Background(), cfs, "run")
	})

	snap := debugtest.WaitPause(t, dbg)
	assert.Equal(t, 5, snap.Location.Line, "the step stops at line 5")

	debugtest.LocalsOf(t, dbg, snap, 0)

	dbg.StepIn(snap.ThreadID)
	snap = debugtest.WaitPause(t, dbg)

	assert.Greater(t, snap.Location.Line, 5, "StepIn moves past line 5")

	dbg.Continue()

	result, execErr := execution.Wait(t)
	require.NoError(t, execErr)
	require.EqualValues(t, 3, result)
}

func TestDebugActionStopTerminates(t *testing.T) {
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
		return svc.ExecuteEntrypoint(context.Background(), cfs, "run")
	})

	debugtest.WaitPause(t, dbg)
	dbg.Stop()

	_, execErr := execution.Wait(t)
	require.ErrorIs(t, execErr, fault.ErrDebuggerStop, "DebugActionStop ends the execution with an error")
}

func TestCancelContextDuringDebug(t *testing.T) {
	t.Parallel()

	dbg := debug.NewDebugger()
	svc := app.NewService(app.WithDebugger(dbg))

	code := `package main

func run() int {
	sum := 0
	for i := 0; i < 100000; i++ {
		sum += i
	}
	return sum
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	dbg.SetBreakpoint("main.go", 4)

	ctx, cancel := context.WithCancelCause(t.Context())

	execution := debugtest.Run(t, dbg, func() (any, error) {
		return svc.ExecuteEntrypoint(ctx, cfs, "run")
	})

	debugtest.WaitPause(t, dbg)
	cancel(errors.New("test cancellation"))

	_, execErr := execution.Wait(t)
	require.Error(t, execErr, "context cancellation ends the execution with an error")
}
