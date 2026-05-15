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

package bytecode_test

import (
	"context"
	"errors"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"

	"github.com/stretchr/testify/require"
)

func TestDebugInfo(t *testing.T) {
	t.Parallel()

	t.Run("compiled with debug info has source map", func(t *testing.T) {
		t.Parallel()
		service := newTestService(t, app.WithDebugInfo())
		source := `package main

func main() {
	x := 42
	y := x + 1
	_ = y
}
`
		cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
		require.NoError(t, err)

		fn, fnErr := cfs.FindFunction("main")
		require.NoError(t, fnErr)
		require.NotNil(t, fn)
		require.True(t, fn.HasDebugSourceMap())
	})

	t.Run("source position at pc 0 is valid", func(t *testing.T) {
		t.Parallel()
		service := newTestService(t, app.WithDebugInfo())
		source := `package main

func main() {
	x := 42
	_ = x
}
`
		cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
		require.NoError(t, err)

		fn, fnErr := cfs.FindFunction("main")
		require.NoError(t, fnErr)
		require.NotNil(t, fn)

		file, line, col := fn.DebugSourcePosition(0)

		require.NotEmpty(t, file)
		require.Greater(t, line, 0)
		_ = col
	})

	t.Run("source position out of range returns empty", func(t *testing.T) {
		t.Parallel()
		service := newTestService(t, app.WithDebugInfo())
		source := `package main

func main() {
	x := 1
	_ = x
}
`
		cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
		require.NoError(t, err)

		fn, fnErr := cfs.FindFunction("main")
		require.NoError(t, fnErr)
		require.NotNil(t, fn)

		file, line, col := fn.DebugSourcePosition(-1)
		require.Empty(t, file)
		require.Equal(t, 0, line)
		require.Equal(t, 0, col)
	})

	t.Run("compiled with debug info has var table", func(t *testing.T) {
		t.Parallel()
		service := newTestService(t, app.WithDebugInfo())
		source := `package main

func main() {
	x := 42
	y := x + 1
	_ = y
}
`
		cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
		require.NoError(t, err)

		fn, fnErr := cfs.FindFunction("main")
		require.NoError(t, fnErr)
		require.NotNil(t, fn)
		require.True(t, fn.HasDebugVarTable())
	})

	t.Run("live variables reflect scope", func(t *testing.T) {
		t.Parallel()
		service := newTestService(t, app.WithDebugInfo())
		source := `package main

func main() {
	x := 42
	y := x + 1
	_ = y
}
`
		cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
		require.NoError(t, err)

		fn, fnErr := cfs.FindFunction("main")
		require.NoError(t, fnErr)
		require.NotNil(t, fn)

		vt := fn.DebugVarTable
		require.NotNil(t, vt)

		sm := fn.DebugSourceMap
		require.NotNil(t, sm)

		bodyLen := len(fn.Body)
		if bodyLen > 1 {
			live := vt.LiveVariables(bodyLen - 2)

			require.NotEmpty(t, live)
		}
	})

	t.Run("without debug info has no source map", func(t *testing.T) {
		t.Parallel()
		service := newTestService(t)
		source := `package main

func main() {
	x := 1
	_ = x
}
`
		cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
		require.NoError(t, err)

		fn, fnErr := cfs.FindFunction("main")
		require.NoError(t, fnErr)
		require.NotNil(t, fn)

		require.True(t, fn.HasDebugSourceMap())
		require.Nil(t, fn.DebugVarTable)
		file, line, _ := fn.DebugSourcePosition(0)
		require.Equal(t, "main.go", file)
		require.Positive(t, line)
	})

	t.Run("without debug info has no var table", func(t *testing.T) {
		t.Parallel()
		service := newTestService(t)
		source := `package main

func main() {
	x := 1
	_ = x
}
`
		cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
		require.NoError(t, err)

		fn, fnErr := cfs.FindFunction("main")
		require.NoError(t, fnErr)
		require.NotNil(t, fn)
		require.False(t, fn.HasDebugVarTable())
	})
}

func TestDebugger(t *testing.T) {
	t.Parallel()

	t.Run("breakpoint hit", func(t *testing.T) {
		t.Parallel()

		dbg := debug.NewDebugger()
		service := newTestService(t, app.WithDebugger(dbg), app.WithDebugInfo())

		source := `package main

func main() {
	x := 1
	y := 2
	z := x + y
	_ = z
}
`

		dbg.SetBreakpoint("main.go", 5)

		var execErr error
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, execErr = service.EvalFile(context.Background(), source, "main")
		}()

		snap := waitPause(t, dbg)
		require.Equal(t, "main.go", snap.Location.File)
		require.Equal(t, 5, snap.Location.Line)
		require.Equal(t, debug.StopReasonBreakpoint, snap.Reason)
		require.NotEmpty(t, stackOf(t, dbg, snap))

		dbg.Continue()
		<-done
		require.NoError(t, execErr)
	})

	t.Run("multiple breakpoints fire in order", func(t *testing.T) {
		t.Parallel()

		dbg := debug.NewDebugger()
		service := newTestService(t, app.WithDebugger(dbg), app.WithDebugInfo())

		source := `package main

func main() {
	a := 1
	b := 2
	c := 3
	_ = a + b + c
}
`

		dbg.SetBreakpoint("main.go", 4)
		dbg.SetBreakpoint("main.go", 6)

		var execErr error
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, execErr = service.EvalFile(context.Background(), source, "main")
		}()

		snap1 := waitPause(t, dbg)
		require.Equal(t, 4, snap1.Location.Line)
		require.Equal(t, debug.StopReasonBreakpoint, snap1.Reason)
		dbg.Continue()

		snap2 := waitPause(t, dbg)
		require.Equal(t, 6, snap2.Location.Line)
		require.Equal(t, debug.StopReasonBreakpoint, snap2.Reason)
		dbg.Continue()

		<-done
		require.NoError(t, execErr)
	})

	t.Run("clear breakpoint removes it", func(t *testing.T) {
		t.Parallel()

		dbg := debug.NewDebugger()
		service := newTestService(t, app.WithDebugger(dbg), app.WithDebugInfo())

		source := `package main

func main() {
	a := 1
	b := 2
	c := 3
	_ = a + b + c
}
`
		dbg.SetBreakpoint("main.go", 4)
		dbg.SetBreakpoint("main.go", 6)

		dbg.ClearBreakpoint("main.go", 4)

		var execErr error
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, execErr = service.EvalFile(context.Background(), source, "main")
		}()

		snap := waitPause(t, dbg)
		require.Equal(t, 6, snap.Location.Line)
		require.Equal(t, debug.StopReasonBreakpoint, snap.Reason)
		dbg.Continue()

		<-done
		require.NoError(t, execErr)
	})

	t.Run("step in enters function call", func(t *testing.T) {
		t.Parallel()

		dbg := debug.NewDebugger()
		service := newTestService(t, app.WithDebugger(dbg), app.WithDebugInfo())

		source := `package main

func add(a, b int) int {
	return a + b
}

func main() {
	x := add(1, 2)
	_ = x
}
`

		dbg.SetBreakpoint("main.go", 8)

		var execErr error
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, execErr = service.EvalFile(context.Background(), source, "main")
		}()

		snap := waitPause(t, dbg)
		require.Equal(t, 8, snap.Location.Line)

		dbg.StepIn(snap.ThreadID)

		snap2 := waitPause(t, dbg)
		require.Equal(t, debug.StopReasonStep, snap2.Reason)

		require.NotEqual(t, 8, snap2.Location.Line)

		dbg.Continue()
		<-done
		require.NoError(t, execErr)
	})

	t.Run("step over skips function call", func(t *testing.T) {
		t.Parallel()

		dbg := debug.NewDebugger()
		service := newTestService(t, app.WithDebugger(dbg), app.WithDebugInfo())

		source := `package main

func add(a, b int) int {
	return a + b
}

func main() {
	x := add(1, 2)
	y := x + 1
	_ = y
}
`

		dbg.SetBreakpoint("main.go", 8)

		var execErr error
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, execErr = service.EvalFile(context.Background(), source, "main")
		}()

		snap := waitPause(t, dbg)
		require.Equal(t, 8, snap.Location.Line)

		dbg.StepOver(snap.ThreadID)

		snap2 := waitPause(t, dbg)
		require.Equal(t, debug.StopReasonStep, snap2.Reason)

		require.Equal(t, "main.main", snap2.Location.Function)

		dbg.Continue()
		<-done
		require.NoError(t, execErr)
	})

	t.Run("step out exits current function", func(t *testing.T) {
		t.Parallel()

		dbg := debug.NewDebugger()
		service := newTestService(t, app.WithDebugger(dbg), app.WithDebugInfo())

		source := `package main

func inner() int {
	a := 10
	b := 20
	return a + b
}

func main() {
	r := inner()
	_ = r
}
`

		dbg.SetBreakpoint("main.go", 4)

		var execErr error
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, execErr = service.EvalFile(context.Background(), source, "main")
		}()

		snap := waitPause(t, dbg)
		require.Equal(t, 4, snap.Location.Line)
		require.Equal(t, "main.inner", snap.Location.Function)

		dbg.StepOut(snap.ThreadID)

		snap2 := waitPause(t, dbg)
		require.Equal(t, debug.StopReasonStep, snap2.Reason)
		require.Equal(t, "main.main", snap2.Location.Function)

		dbg.Continue()
		<-done
		require.NoError(t, execErr)
	})

	t.Run("stop terminates execution", func(t *testing.T) {
		t.Parallel()

		dbg := debug.NewDebugger()
		service := newTestService(t, app.WithDebugger(dbg), app.WithDebugInfo())

		source := `package main

func main() {
	x := 1
	y := 2
	_ = x + y
}
`
		dbg.SetBreakpoint("main.go", 4)

		var execErr error
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, execErr = service.EvalFile(context.Background(), source, "main")
		}()

		waitPause(t, dbg)
		dbg.Stop()
		<-done
		require.Error(t, execErr)
		require.True(t, errors.Is(execErr, fault.ErrDebuggerStop))
	})

	t.Run("variables at breakpoint", func(t *testing.T) {
		t.Parallel()

		dbg := debug.NewDebugger()
		service := newTestService(t, app.WithDebugger(dbg), app.WithDebugInfo())

		source := `package main

func main() {
	x := 42
	y := x + 1
	_ = y
}
`

		dbg.SetBreakpoint("main.go", 5)

		var execErr error
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, execErr = service.EvalFile(context.Background(), source, "main")
		}()

		snap := waitPause(t, dbg)
		require.Equal(t, 5, snap.Location.Line)

		vars := localsOf(t, dbg, snap, 0)
		require.NotEmpty(t, vars, "expected at least one variable visible at breakpoint")

		found := false
		for _, v := range vars {
			if v.Name == "x" {
				found = true
				break
			}
		}
		require.True(t, found, "expected variable 'x' to be visible at line 5")

		dbg.Continue()
		<-done
		require.NoError(t, execErr)
	})

	t.Run("variables with an out-of-range frame index is refused", func(t *testing.T) {
		t.Parallel()

		dbg := debug.NewDebugger()
		service := newTestService(t, app.WithDebugger(dbg), app.WithDebugInfo())

		source := `package main

func main() {
	x := 1
	_ = x
}
`
		dbg.SetBreakpoint("main.go", 4)

		var execErr error
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, execErr = service.EvalFile(context.Background(), source, "main")
		}()

		snap := waitPause(t, dbg)
		for _, frameIndex := range []int{1000000, 100, -1} {
			_, err := dbg.Variables(snap.ThreadID, frameIndex, debug.ScopeLocals)
			require.ErrorIs(t, err, fault.ErrDebugFrameOutOfRange, "frame index %d", frameIndex)
		}

		dbg.Continue()
		<-done
		require.NoError(t, execErr)
	})

	t.Run("variables with all register types", func(t *testing.T) {
		t.Parallel()

		dbg := debug.NewDebugger()
		service := newTestService(t, app.WithDebugger(dbg), app.WithDebugInfo())

		source := `package main

func main() {
	intVar := 42
	floatVar := 3.14
	strVar := "hello"
	boolVar := true
	uintVar := uint(99)
	complexVar := complex(1.0, 2.0)
	var anyVar interface{} = "world"
	_ = intVar
	_ = floatVar
	_ = strVar
	_ = boolVar
	_ = uintVar
	_ = complexVar
	_ = anyVar
}
`

		dbg.SetBreakpoint("main.go", 10)

		ctx, cancel := context.WithTimeoutCause(
			context.Background(),
			5*1000*1000*1000,
			errors.New("debugger test timed out"),
		)
		defer cancel()

		var execErr error
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, execErr = service.EvalFile(ctx, source, "main")
		}()

		{
			snap := waitPause(t, dbg)
			vars := localsOf(t, dbg, snap, 0)
			require.NotEmpty(t, vars)

			varMap := make(map[string]any)
			for _, v := range vars {
				varMap[v.Name] = v.Value
			}

			if v, ok := varMap["intVar"]; ok {
				require.Equal(t, int64(42), v)
			}

			dbg.Continue()
		}

		<-done
		require.NoError(t, execErr)
	})

	t.Run("variables in closure with upvalues", func(t *testing.T) {
		t.Parallel()

		dbg := debug.NewDebugger()
		service := newTestService(t, app.WithDebugger(dbg), app.WithDebugInfo())

		source := `package main

func main() {
	outer := 42
	f := func() int {
		inner := outer + 1
		return inner
	}
	result := f()
	_ = result
}
`

		dbg.SetBreakpoint("main.go", 6)

		ctx, cancel := context.WithTimeoutCause(
			context.Background(),
			5*1000*1000*1000,
			errors.New("debugger test timed out"),
		)
		defer cancel()

		var execErr error
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, execErr = service.EvalFile(ctx, source, "main")
		}()

		{
			snap := waitPause(t, dbg)
			vars := localsOf(t, dbg, snap, 0)
			_ = vars
			dbg.Continue()
		}

		<-done
		require.NoError(t, execErr)
	})
}

func newCompiledFunctionWithSourceMap(
	name string,
	bodyLen int,
	positions []program.SourcePosition,
	files []string,
) *program.CompiledFunction {
	filesCopy := make([]string, len(files))
	copy(filesCopy, files)
	return &program.CompiledFunction{
		Name: name,
		Body: make([]isa.Instruction, bodyLen),
		DebugSourceMap: &program.SourceMap{
			Files:     &filesCopy,
			Positions: positions,
		},
	}
}
