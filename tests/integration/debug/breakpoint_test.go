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
	cfs, err := svc.CompileFileSet(context.Background(), map[string]string{"main.go": code})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	dbg.SetBreakpoint("main.go", 5)

	done := make(chan error, 1)
	go func() {
		_, execErr := svc.ExecuteEntrypoint(context.Background(), cfs, "run")
		done <- execErr
	}()

	snap := waitPause(t, dbg)
	if snap.Location.Line != 5 {
		t.Errorf("expected breakpoint at line 5, got line %d", snap.Location.Line)
	}
	if snap.Reason != debug.StopReasonBreakpoint {
		t.Errorf("expected DebugEventBreakpoint, got %d", snap.Reason)
	}

	dbg.Continue()
	if execErr := <-done; execErr != nil {
		t.Fatalf("execute: %v", execErr)
	}
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
	cfs, err := svc.CompileFileSet(context.Background(), map[string]string{"main.go": code})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	dbg.SetBreakpoint("main.go", 6)

	done := make(chan error, 1)
	go func() {
		_, execErr := svc.ExecuteEntrypoint(context.Background(), cfs, "run")
		done <- execErr
	}()

	hitCount := 0
	for range 3 {
		snap := waitPause(t, dbg)
		if snap.Location.Line != 6 {
			t.Errorf("iteration %d: expected line 6, got %d", hitCount, snap.Location.Line)
		}
		hitCount++
		dbg.Continue()
	}

	if hitCount != 3 {
		t.Errorf("expected 3 breakpoint hits, got %d", hitCount)
	}

	if execErr := <-done; execErr != nil {
		t.Fatalf("execute: %v", execErr)
	}
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
	cfs, err := svc.CompileFileSet(context.Background(), map[string]string{"main.go": code})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	dbg.SetBreakpoint("main.go", 4)
	dbg.SetBreakpoint("main.go", 6)

	done := make(chan error, 1)
	go func() {
		_, execErr := svc.ExecuteEntrypoint(context.Background(), cfs, "run")
		done <- execErr
	}()

	snap1 := waitPause(t, dbg)
	if snap1.Location.Line != 4 {
		t.Errorf("first breakpoint: expected line 4, got %d", snap1.Location.Line)
	}
	dbg.Continue()

	snap2 := waitPause(t, dbg)
	if snap2.Location.Line != 6 {
		t.Errorf("second breakpoint: expected line 6, got %d", snap2.Location.Line)
	}
	dbg.Continue()

	if execErr := <-done; execErr != nil {
		t.Fatalf("execute: %v", execErr)
	}
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
	cfs, err := svc.CompileFileSet(context.Background(), map[string]string{"main.go": code})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	dbg.SetBreakpoint("main.go", 4)
	dbg.ClearBreakpoint("main.go", 4)
	dbg.SetBreakpoint("main.go", 5)

	done := make(chan error, 1)
	go func() {
		_, execErr := svc.ExecuteEntrypoint(context.Background(), cfs, "run")
		done <- execErr
	}()

	snap := waitPause(t, dbg)
	if snap.Location.Line != 5 {
		t.Errorf("expected breakpoint at line 5 (not 4), got line %d", snap.Location.Line)
	}
	dbg.Continue()

	if execErr := <-done; execErr != nil {
		t.Fatalf("execute: %v", execErr)
	}
}
