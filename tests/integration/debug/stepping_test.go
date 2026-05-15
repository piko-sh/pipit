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
	cfs, err := svc.CompileFileSet(context.Background(), map[string]string{"main.go": code})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	dbg.SetBreakpoint("main.go", 8)

	done := make(chan error, 1)
	go func() {
		_, execErr := svc.ExecuteEntrypoint(context.Background(), cfs, "run")
		done <- execErr
	}()

	snap := waitPause(t, dbg)
	if snap.Location.Line != 8 {
		t.Fatalf("expected breakpoint at line 8, got %d", snap.Location.Line)
	}

	dbg.StepIn(snap.ThreadID)
	snap = waitPause(t, dbg)
	if snap.Reason != debug.StopReasonStep {
		t.Errorf("expected step event, got %d", snap.Reason)
	}

	dbg.Continue()
	if execErr := <-done; execErr != nil {
		t.Fatalf("execute: %v", execErr)
	}
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
	cfs, err := svc.CompileFileSet(context.Background(), map[string]string{"main.go": code})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	dbg.SetBreakpoint("main.go", 8)

	done := make(chan error, 1)
	go func() {
		_, execErr := svc.ExecuteEntrypoint(context.Background(), cfs, "run")
		done <- execErr
	}()

	snap := waitPause(t, dbg)
	if snap.Location.Line != 8 {
		t.Fatalf("expected breakpoint at line 8, got %d", snap.Location.Line)
	}

	dbg.StepOver(snap.ThreadID)
	snap = waitPause(t, dbg)
	if snap.Reason != debug.StopReasonStep {
		t.Errorf("expected step event, got %d", snap.Reason)
	}

	if snap.Location.Function != "" && snap.Location.Function != "run" && snap.Location.Function != "main.run" {
		t.Logf("step-over landed in function %q at line %d", snap.Location.Function, snap.Location.Line)
	}

	dbg.Continue()
	if execErr := <-done; execErr != nil {
		t.Fatalf("execute: %v", execErr)
	}
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
	cfs, err := svc.CompileFileSet(context.Background(), map[string]string{"main.go": code})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	dbg.SetBreakpoint("main.go", 4)

	done := make(chan error, 1)
	go func() {
		_, execErr := svc.ExecuteEntrypoint(context.Background(), cfs, "run")
		done <- execErr
	}()

	snap := waitPause(t, dbg)
	if snap.Location.Line != 4 {
		t.Fatalf("expected breakpoint at line 4, got %d", snap.Location.Line)
	}

	dbg.StepIn(snap.ThreadID)
	snap = waitPause(t, dbg)
	if snap.Location.Line != 5 {
		t.Fatalf("expected StepIn to land on line 5, got %d", snap.Location.Line)
	}

	dbg.Continue()
	if execErr := <-done; execErr != nil {
		t.Fatalf("execute: %v", execErr)
	}
}
