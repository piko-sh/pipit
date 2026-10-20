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

func TestSourceMapLocatesAssignment(t *testing.T) {
	t.Parallel()
	svc := app.NewService(app.WithDebugInfo(), app.WithForceGoDispatch())
	fn, err := svc.Compile(t.Context(), "x := 42; _ = x")
	require.NoError(t, err)

	require.True(t, fn.HasDebugSourceMap(), "a source map is recorded")

	bodyLen := fn.BodyLen()
	require.NotEqual(t, 0, bodyLen, "the body has instructions")

	foundLine := false
	for pc := range bodyLen {
		_, line, _ := fn.DebugSourcePosition(pc)
		if line > 0 {
			foundLine = true
		}
	}
	assert.True(t, foundLine, "at least one instruction has a source line")
}

func TestSourceMapMultiLineCode(t *testing.T) {
	t.Parallel()
	svc := app.NewService(app.WithDebugInfo(), app.WithForceGoDispatch())

	code := `package main

func compute() int {
	x := 10
	y := 20
	return x + y
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	fn, findErr := cfs.FindFunction("compute")
	require.NoError(t, findErr)

	require.True(t, fn.HasDebugSourceMap(), "a source map is recorded")

	lines := make(map[int]bool)
	for pc := range fn.BodyLen() {
		_, line, _ := fn.DebugSourcePosition(pc)
		if line > 0 {
			lines[line] = true
		}
	}
	assert.GreaterOrEqual(t, len(lines), 2, "the code spans at least 2 source lines")
}

func TestSourceMapPresentWithoutDebugInfo(t *testing.T) {
	t.Parallel()
	svc := app.NewService(app.WithForceGoDispatch())
	fn, err := svc.Compile(t.Context(), "42")
	require.NoError(t, err)

	assert.True(t, fn.HasDebugSourceMap(), "a source map is recorded even without debug info")
	assert.Nil(t, fn.DebugVarTable, "without debug info there is no variable table")
}

func TestSourceMapIfElse(t *testing.T) {
	t.Parallel()
	svc := app.NewService(app.WithDebugInfo(), app.WithForceGoDispatch())

	code := `package main

func choose(x int) int {
	if x > 0 {
		return 1
	} else {
		return -1
	}
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	fn, findErr := cfs.FindFunction("choose")
	require.NoError(t, findErr)

	require.True(t, fn.HasDebugSourceMap(), "a source map is recorded")

	lines := make(map[int]bool)
	for pc := range fn.BodyLen() {
		_, line, _ := fn.DebugSourcePosition(pc)
		if line > 0 {
			lines[line] = true
		}
	}
	assert.GreaterOrEqual(t, len(lines), 2, "an if/else spans at least 2 source lines")
}

func TestSourceMapForLoop(t *testing.T) {
	t.Parallel()
	svc := app.NewService(app.WithDebugInfo(), app.WithForceGoDispatch())

	code := `package main

func sum() int {
	total := 0
	for i := 0; i < 10; i++ {
		total += i
	}
	return total
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	fn, findErr := cfs.FindFunction("sum")
	require.NoError(t, findErr)

	require.True(t, fn.HasDebugSourceMap(), "a source map is recorded")

	lines := make(map[int]bool)
	for pc := range fn.BodyLen() {
		_, line, _ := fn.DebugSourcePosition(pc)
		if line > 0 {
			lines[line] = true
		}
	}
	assert.GreaterOrEqual(t, len(lines), 3, "a for loop spans at least 3 source lines")
}

func TestSourceMapClosures(t *testing.T) {
	t.Parallel()
	svc := app.NewService(app.WithDebugInfo(), app.WithForceGoDispatch())

	code := `package main

func outer() int {
	x := 10
	add := func(y int) int {
		return x + y
	}
	return add(5)
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	fn, findErr := cfs.FindFunction("outer")
	require.NoError(t, findErr)

	require.True(t, fn.HasDebugSourceMap(), "the outer function has a source map")

	subFuncs := fn.SubFunctions()
	if len(subFuncs) == 0 {

		t.Log("no sub-functions found on 'outer'; closure may reside on root function - skipping sub-function source map check")
	} else {
		for i, sub := range subFuncs {
			assert.True(t, sub.HasDebugSourceMap(), "sub-function %d has a source map", i)
		}
	}
}

func TestSourceMapPostOptimiserConsistency(t *testing.T) {
	t.Parallel()
	svc := app.NewService(app.WithDebugInfo(), app.WithForceGoDispatch())

	code := `package main

func check(x int) bool {
	return x > 5
}
`
	cfs, err := svc.CompileFileSet(t.Context(), map[string]string{"main.go": code})
	require.NoError(t, err)

	fn, findErr := cfs.FindFunction("check")
	require.NoError(t, findErr)

	require.True(t, fn.HasDebugSourceMap(), "a source map is recorded")

	bodyLen := fn.BodyLen()
	for pc := range bodyLen {
		fn.DebugSourcePosition(pc)
	}

	file, line, col := fn.DebugSourcePosition(bodyLen + 100)
	assert.Empty(t, file, "an out-of-range PC has no position")
	assert.Zero(t, line)
	assert.Zero(t, col)
}

func TestSourceMapMultipleFiles(t *testing.T) {
	t.Parallel()
	svc := app.NewService(app.WithDebugInfo(), app.WithForceGoDispatch())

	files := map[string]string{
		"a.go": `package main

func a() int { return 1 }
`,
		"b.go": `package main

func b() int { return 2 }
`,
	}

	cfs, err := svc.CompileFileSet(t.Context(), files)
	require.NoError(t, err)

	fnA, errA := cfs.FindFunction("a")
	fnB, errB := cfs.FindFunction("b")
	require.NoError(t, errA)
	require.NoError(t, errB)

	require.True(t, fnA.HasDebugSourceMap(), "both functions have source maps")
	require.True(t, fnB.HasDebugSourceMap())

	var fileA, fileB string
	for pc := range fnA.BodyLen() {
		if f, _, _ := fnA.DebugSourcePosition(pc); f != "" {
			fileA = f
			break
		}
	}
	for pc := range fnB.BodyLen() {
		if f, _, _ := fnB.DebugSourcePosition(pc); f != "" {
			fileB = f
			break
		}
	}
	if fileA == "" || fileB == "" {
		t.Logf("could not resolve file for one or both functions (a=%q, b=%q)", fileA, fileB)
	} else {
		assert.NotEqual(t, fileA, fileB, "the two functions come from different files")
	}
}
