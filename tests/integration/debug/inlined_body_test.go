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
	"testing"

	"pipit.sh/pipit/internal/debug"

	"pipit.sh/pipit/internal/app"

	"github.com/stretchr/testify/require"
)

func TestInlinedBodyPositionsResolve(t *testing.T) {
	t.Parallel()

	dbg := debug.NewDebugger()
	svc := app.NewService(app.WithDebugger(dbg))

	code := `package main

type pair struct {
	a int
	b int
}

func sum(p pair) int {
	return p.a + p.b
}

func run() int {
	p := pair{a: 1, b: 2}
	total := 0
	for i := 0; i < 10; i++ {
		total = total + sum(p)
	}
	return total
}
`
	cfs, err := svc.CompileFileSet(context.Background(), map[string]string{"main.go": code})
	require.NoError(t, err)

	dbg.SetBreakpoint("main.go", 9)

	done := make(chan error, 1)
	go func() {
		_, execErr := svc.ExecuteEntrypoint(context.Background(), cfs, "run")
		done <- execErr
	}()

	snap := waitPause(t, dbg)
	require.Equal(t, debug.StopReasonBreakpoint, snap.Reason)
	require.Equal(t, 9, snap.Location.Line, "breakpoint inside the inlined callee resolves to the callee's line")
	require.Equal(t, "main.go", snap.Location.File)
	require.NotEmpty(t, stackOf(t, dbg, snap))
	require.Equal(t, 9, stackOf(t, dbg, snap)[0].Line)
	require.Equal(t, "main.run", stackOf(t, dbg, snap)[0].Function, "the inlined body executes in the caller's frame")

	dbg.ClearBreakpoint("main.go", 9)
	dbg.Continue()
	require.NoError(t, <-done)
}
