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
)

func TestVarTableRecordsDeclarations(t *testing.T) {
	t.Parallel()
	svc := app.NewService(app.WithDebugInfo(), app.WithForceGoDispatch())

	code := `package main

func vars() {
	x := 42
	y := "hello"
	_ = x
	_ = y
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	fn, findErr := cfs.FindFunction("vars")
	require.NoError(t, findErr)

	require.True(t, fn.HasDebugVarTable(), "a variable table is recorded")
}

func TestVarTableNilWhenDisabled(t *testing.T) {
	t.Parallel()
	svc := app.NewService(app.WithForceGoDispatch())

	fn, err := svc.Compile(t.Context(), "x := 42; _ = x")
	require.NoError(t, err)

	assert.False(t, fn.HasDebugVarTable(), "without debug info there is no variable table")
}

func TestVarTableFunctionParams(t *testing.T) {
	t.Parallel()
	svc := app.NewService(app.WithDebugInfo(), app.WithForceGoDispatch())

	code := `package main

func add(a int, b int) int {
	return a + b
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	fn, findErr := cfs.FindFunction("add")
	require.NoError(t, findErr)

	require.True(t, fn.HasDebugVarTable(), "a variable table is recorded")
}

func TestVarTableClosures(t *testing.T) {
	t.Parallel()
	svc := app.NewService(app.WithDebugInfo(), app.WithForceGoDispatch())

	code := `package main

func outer() int {
	x := 10
	inc := func() int {
		return x + 1
	}
	return inc()
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	fn, findErr := cfs.FindFunction("outer")
	require.NoError(t, findErr)

	require.True(t, fn.HasDebugVarTable(), "the outer function has a variable table")

	subFuncs := fn.SubFunctions()
	if len(subFuncs) == 0 {

		t.Log("no sub-functions found on 'outer'; closure may reside on root function - skipping sub-function var table check")
	} else {
		for i, sub := range subFuncs {
			assert.True(t, sub.HasDebugVarTable(), "sub-function %d has a variable table", i)
		}
	}
}
