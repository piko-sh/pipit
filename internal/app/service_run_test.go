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

package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"bytes"
	"fmt"
	"pipit.sh/pipit/internal/fault"
	"strings"
)

func TestEvalCancelledContext(t *testing.T) {
	t.Parallel()
	service := NewService()

	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("test: pre-cancelled eval context"))

	_, err := service.Eval(ctx, `1 + 2`)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !errors.Is(err, fault.ErrExecutionCancelled) {
		t.Fatalf("expected errExecutionCancelled, got: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled in chain, got: %v", err)
	}
}

func TestEvalTimeout(t *testing.T) {
	t.Parallel()
	service := NewService()
	service.UseSymbols(newOSSymbols())

	ctx, cancel := context.WithTimeoutCause(context.Background(), 100*time.Millisecond, errors.New("test: eval timeout"))
	defer cancel()

	_, err := service.Eval(ctx, `
import (
	"os"
)
x := 0
for {
	x++
	os.Getenv("PATH")
}
`)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
}

func TestCompileCancelledContext(t *testing.T) {
	t.Parallel()
	service := NewService()

	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("test: pre-cancelled compile context"))

	_, err := service.Compile(ctx, `1 + 2`)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !errors.Is(err, fault.ErrExecutionCancelled) {
		t.Fatalf("expected errExecutionCancelled, got: %v", err)
	}
}

func TestExecuteEntrypointCancelledContext(t *testing.T) {
	t.Parallel()
	service := NewService()

	source := `package main
func entrypoint() int { return 42 }
`
	cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("test: pre-cancelled entrypoint context"))

	_, err = service.ExecuteEntrypoint(ctx, cfs, "entrypoint")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !errors.Is(err, fault.ErrExecutionCancelled) {
		t.Fatalf("expected errExecutionCancelled, got: %v", err)
	}
}

func TestMaxExecutionTime(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxExecutionTime(100 * time.Millisecond))
	service.UseSymbols(newOSSymbols())

	_, err := service.Eval(context.Background(), `
import (
	"os"
)
x := 0
for {
	x++
	os.Getenv("PATH")
}
`)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
}

func TestEvalCancelDuringExecution(t *testing.T) {
	t.Parallel()
	service := NewService()
	service.UseSymbols(newOSSymbols())

	ctx, cancel := context.WithCancelCause(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel(errors.New("test: cancel during execution"))
	}()

	_, err := service.Eval(ctx, `
import (
	"os"
)
x := 0
for {
	x++
	os.Getenv("PATH")
}
`)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestCompileDefersGlobalInitialisation(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	service := NewService(WithSafeMode(), WithStderr(&output))
	compiled, err := service.Compile(context.Background(), `
func unused() {}
var value = func() int {
	println("initialised")
	return 7
}()
value
`)
	if err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("compilation executed script code: %q", output.String())
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Execute(cancelled, compiled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled execution: %v", err)
	}
	if output.Len() != 0 {
		t.Fatal("cancelled execution ran the initialiser")
	}
	for iteration := 1; iteration <= 2; iteration++ {
		result, err := service.Execute(context.Background(), compiled)
		if err != nil || fmt.Sprint(result) != "7" {
			t.Fatalf("execution %d: result=%v error=%v", iteration, result, err)
		}
		if bytes.Count(output.Bytes(), []byte("initialised")) != iteration {
			t.Fatalf("initialiser did not run once per execution: %q", output.String())
		}
	}
}

func TestCompileDefersFailingInitialiser(t *testing.T) {
	t.Parallel()
	service := NewService(WithSafeMode())
	compiled, err := service.Compile(context.Background(), `
func unused() {}
var value = func() int {
	panic("initialiser failure")
	return 7
}()
value
`)
	if err != nil {
		t.Fatalf("runtime initialiser failure escaped into compilation: %v", err)
	}
	if _, err := service.Execute(context.Background(), compiled); err == nil {
		t.Fatal("execution ignored the initialiser failure")
	}
}

func TestGoroutineLimitIsWiredThroughService(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxGoroutines(4))
	if service.limits.MaxGoroutines != 4 {
		t.Fatalf("limits.maxGoroutines = %d, want 4", service.limits.MaxGoroutines)
	}
}

func TestErrGoroutineLimitIsExported(t *testing.T) {
	t.Parallel()
	if fault.ErrGoroutineLimit == nil {
		t.Fatalf("errGoroutineLimit unexported or nil")
	}
	wrapped := errors.Join(errors.New("wrapper"), fault.ErrGoroutineLimit)
	if !errors.Is(wrapped, fault.ErrGoroutineLimit) {
		t.Fatalf("errors.Is failed to unwrap errGoroutineLimit")
	}
	if !strings.Contains(fault.ErrGoroutineLimit.Error(), "goroutine") {
		t.Fatalf("errGoroutineLimit message %q lacks 'goroutine'", fault.ErrGoroutineLimit.Error())
	}
}

func TestWithCapabilityHookAndForceGoDispatchCompose(t *testing.T) {
	t.Parallel()
	hook := &recordingHook{}
	service := NewService(
		WithCapabilityHook(hook),
		WithForceGoDispatch(),
		WithMaxGoroutines(8),
		WithMaxCallDepth(1024),
	)
	if service.CapabilityHook() != hook {
		t.Fatalf("CapabilityHook not preserved across option list")
	}
	if service.limits.MaxGoroutines != 8 {
		t.Fatalf("maxGoroutines = %d, want 8", service.limits.MaxGoroutines)
	}
	if service.limits.MaxCallDepth != 1024 {
		t.Fatalf("maxCallDepth = %d, want 1024", service.limits.MaxCallDepth)
	}
}
