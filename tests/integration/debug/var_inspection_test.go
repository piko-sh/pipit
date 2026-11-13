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
	"pipit.sh/pipit/internal/engine"
)

func TestVarInspectionInt(t *testing.T) {
	t.Parallel()

	dbg := debug.NewDebugger()
	svc := app.NewService(app.WithDebugger(dbg))

	code := `package main

func run() int {
	x := 42
	y := x + 1
	return y
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	dbg.SetBreakpoint("main.go", 5)

	execution := debugtest.Run(t, dbg, func() (any, error) {
		return svc.ExecuteEntrypoint(t.Context(), cfs, "run")
	})

	snap := debugtest.WaitPause(t, dbg)

	vars := debugtest.LocalsOf(t, dbg, snap, 0)
	found := false
	for _, v := range vars {
		if v.Name == "x" {
			found = true
			assert.Equal(t, int64(42), v.Value)
		}
	}
	assert.True(t, found, "the inspection lists x")

	dbg.Continue()
	_, execErr := execution.Wait(t)
	require.NoError(t, execErr)
}

func TestVarInspectionMultipleTypes(t *testing.T) {
	t.Parallel()

	dbg := debug.NewDebugger()
	svc := app.NewService(app.WithDebugger(dbg))

	code := `package main

func run() int {
	n := 100
	s := "hello"
	b := true
	_ = s
	_ = b
	return n
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	dbg.SetBreakpoint("main.go", 9)

	execution := debugtest.Run(t, dbg, func() (any, error) {
		return svc.ExecuteEntrypoint(t.Context(), cfs, "run")
	})
	snap := debugtest.WaitPause(t, dbg)
	vars := debugtest.LocalsOf(t, dbg, snap, 0)

	varMap := make(map[string]engine.VariableInfo)
	for _, v := range vars {
		varMap[v.Name] = v
	}

	if v, ok := varMap["n"]; ok {
		assert.Equal(t, int64(100), v.Value)
	}
	if v, ok := varMap["s"]; ok {
		assert.Equal(t, "hello", v.Value)
	}
	if v, ok := varMap["b"]; ok {
		assert.Equal(t, true, v.Value)
	}

	dbg.Continue()
	_, execErr := execution.Wait(t)
	require.NoError(t, execErr)
}

func TestVarInspectionAfterReassignment(t *testing.T) {
	t.Parallel()

	dbg := debug.NewDebugger()
	svc := app.NewService(app.WithDebugger(dbg))

	code := `package main

func run() int {
	x := 10
	x = 20
	return x
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	dbg.SetBreakpoint("main.go", 6)

	execution := debugtest.Run(t, dbg, func() (any, error) {
		return svc.ExecuteEntrypoint(t.Context(), cfs, "run")
	})
	snap := debugtest.WaitPause(t, dbg)
	vars := debugtest.LocalsOf(t, dbg, snap, 0)

	for _, v := range vars {
		if v.Name == "x" {
			assert.Equal(t, int64(20), v.Value, "x after reassignment")
		}
	}

	dbg.Continue()
	_, execErr := execution.Wait(t)
	require.NoError(t, execErr)
}
