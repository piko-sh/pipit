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

package facade_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

func TestDeadlockOnProgramChannel(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithDeadlockGrace(50*time.Millisecond), pipit.WithMaxExecutionTime(10*time.Second))
	start := time.Now()
	_, err := interpreter.Eval(context.Background(), `
ch := make(chan int)
<-ch
`)
	if !errors.Is(err, pipit.ErrDeadlock) {
		t.Fatalf("expected ErrDeadlock, got %v", err)
	}
	if errors.Is(err, pipit.ErrExecutionCancelled) {
		t.Fatalf("a deadlock must not read as a timeout: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("deadlock took %s to report", elapsed)
	}
}

func TestDeadlockAcrossGoroutines(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithDeadlockGrace(50*time.Millisecond), pipit.WithMaxExecutionTime(10*time.Second))
	_, err := interpreter.Eval(context.Background(), `
a := make(chan int)
b := make(chan int)
done := make(chan bool)
go func() { <-a; b <- 1; done <- true }()
go func() { <-b; a <- 1; done <- true }()
<-done
<-done
`)
	if !errors.Is(err, pipit.ErrDeadlock) {
		t.Fatalf("expected ErrDeadlock, got %v", err)
	}
}

func TestNoDeadlockWhenAGoroutineCanWake(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithDeadlockGrace(20*time.Millisecond), pipit.WithMaxExecutionTime(10*time.Second))
	result, err := interpreter.Eval(context.Background(), `
import "time"
ch := make(chan int)
go func() {
	time.Sleep(150 * time.Millisecond)
	ch <- 42
}()
first := <-ch
<-time.After(120 * time.Millisecond)
first
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, ok := result.(int); !ok || got != 42 {
		t.Fatalf("expected 42, got %v (%T)", result, result)
	}
}

func TestSleepIsInterruptedByDeadline(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithMaxExecutionTime(100*time.Millisecond))
	start := time.Now()
	_, err := interpreter.Eval(context.Background(), `
import "time"
time.Sleep(30 * time.Second)
`)
	if !errors.Is(err, pipit.ErrExecutionCancelled) {
		t.Fatalf("expected ErrExecutionCancelled, got %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the context's own error must stay reachable: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("sleep ran %s past a 100ms deadline", elapsed)
	}
}

func TestUncaughtPanicIsClassified(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithMaxExecutionTime(5*time.Second))
	_, err := interpreter.Eval(context.Background(), "panic(\"boom\")\n1")
	if !errors.Is(err, pipit.ErrUncaughtPanic) {
		t.Fatalf("expected ErrUncaughtPanic, got %v", err)
	}
	value, ok := pipit.UncaughtPanicValue(err)
	if !ok || value != "boom" {
		t.Fatalf("expected panic value boom, got %v (%v)", value, ok)
	}
	if !strings.HasSuffix(err.Error(), "panic: boom") {
		t.Fatalf("panic text changed: %q", err.Error())
	}
}
