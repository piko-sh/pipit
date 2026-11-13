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

func TestStackTraceSingleFrame(t *testing.T) {
	t.Parallel()

	dbg := debug.NewDebugger()
	svc := app.NewService(app.WithDebugger(dbg))

	code := `package main

func run() int {
	x := 42
	return x
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	dbg.SetBreakpoint("main.go", 4)

	execution := debugtest.Run(t, dbg, func() (any, error) {
		return svc.ExecuteEntrypoint(t.Context(), cfs, "run")
	})

	snap := debugtest.WaitPause(t, dbg)
	require.NotEmpty(t, debugtest.StackOf(t, dbg, snap), "the stack trace has frames")
	assert.NotEqual(t, 0, debugtest.StackOf(t, dbg, snap)[0].Line, "the top stack frame has a line")

	dbg.Continue()
	_, execErr := execution.Wait(t)
	require.NoError(t, execErr)
}

func TestStackTraceMultipleFrames(t *testing.T) {
	t.Parallel()

	dbg := debug.NewDebugger()
	svc := app.NewService(app.WithDebugger(dbg))

	code := `package main

func inner() int {
	return 42
}

func middle() int {
	v := inner()
	return v
}

func run() int {
	v := middle()
	return v
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	dbg.SetBreakpoint("main.go", 4)

	execution := debugtest.Run(t, dbg, func() (any, error) {
		return svc.ExecuteEntrypoint(t.Context(), cfs, "run")
	})

	snap := debugtest.WaitPause(t, dbg)
	assert.GreaterOrEqual(t, len(debugtest.StackOf(t, dbg, snap)), 3)

	dbg.Continue()
	_, execErr := execution.Wait(t)
	require.NoError(t, execErr)
}
