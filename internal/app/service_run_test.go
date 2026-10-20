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
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab"
)

func TestEvalCancelledContext(t *testing.T) {
	t.Parallel()
	service := NewService()

	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errors.New("test: pre-cancelled eval context"))

	_, err := service.Eval(ctx, `1 + 2`)
	require.ErrorIs(t, err, fault.ErrExecutionCancelled, "a cancelled context stops execution")
	require.ErrorIs(t, err, context.Canceled)
}

func TestEvalTimeout(t *testing.T) {
	t.Parallel()
	service := NewService()
	service.UseSymbols(newOSSymbols())

	ctx, cancel := context.WithTimeoutCause(t.Context(), 100*time.Millisecond, errors.New("test: eval timeout"))
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
	require.ErrorIs(t, err, context.DeadlineExceeded, "the deadline stops execution")
}

func TestCompileCancelledContext(t *testing.T) {
	t.Parallel()
	service := NewService()

	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errors.New("test: pre-cancelled compile context"))

	_, err := service.Compile(ctx, `1 + 2`)
	require.ErrorIs(t, err, fault.ErrExecutionCancelled, "a cancelled context stops execution")
}

func TestExecuteEntrypointCancelledContext(t *testing.T) {
	t.Parallel()
	service := NewService()

	source := `package main
func entrypoint() int { return 42 }
`
	cfs, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoError(t, err)

	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errors.New("test: pre-cancelled entrypoint context"))

	_, err = service.ExecuteEntrypoint(ctx, cfs, "entrypoint")
	require.ErrorIs(t, err, fault.ErrExecutionCancelled, "a cancelled context stops execution")
}

func TestMaxExecutionTime(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxExecutionTime(100 * time.Millisecond))
	service.UseSymbols(newOSSymbols())

	_, err := service.Eval(t.Context(), `
import (
	"os"
)
x := 0
for {
	x++
	os.Getenv("PATH")
}
`)
	require.ErrorIs(t, err, context.DeadlineExceeded, "the deadline stops execution")
}

func TestEvalCancelDuringExecution(t *testing.T) {
	t.Parallel()
	service := NewService()
	service.UseSymbols(newOSSymbols())

	ctx, cancel := context.WithCancelCause(t.Context())
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
	require.ErrorIs(t, err, context.Canceled, "cancellation stops execution")
}

func TestCompileDefersGlobalInitialisation(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	service := NewService(WithSafeMode(), WithStderr(&output))
	compiled, err := service.Compile(t.Context(), `
func unused() {}
var value = func() int {
	println("initialised")
	return 7
}()
value
`)
	require.NoError(t, err)
	require.Empty(t, output.String(), "compilation runs no script code")
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.Execute(cancelled, compiled)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, output.Len(), "a cancelled execution runs no initialiser")
	for iteration := 1; iteration <= 2; iteration++ {
		result, err := service.Execute(t.Context(), compiled)
		require.NoError(t, err)
		require.Equal(t, "7", fmt.Sprint(result))
		require.Equal(t, iteration, bytes.Count(output.Bytes(), []byte("initialised")), "the initialiser runs once per execution")
	}
}

func TestEntrypointKeepsStringsVariableInitialisersBuilt(t *testing.T) {
	t.Parallel()
	const entrypoint = `
func run() string {
	other := string(host.Bytes("XXXX"))
	return value + other[:0]
}`
	tests := []struct {
		name   string
		source string
	}{
		{name: "a conversion in the initialiser", source: `var value = string(host.Bytes("kept"))`},
		{name: "a conversion in a function the initialiser calls", source: `var value = load()
func load() string { data := host.Bytes("kept"); return string(data) }`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := NewService()
			service.UseSymbols(symtab.NewSymbolRegistry(symtab.SymbolExports{"host": {
				"Bytes": reflect.ValueOf(func(s string) []byte { return []byte(s) }),
			}}))
			cfs, err := service.CompileFileSet(t.Context(), map[string]string{
				"main.go": "package main\nimport \"host\"\n" + tt.source + "\n" + entrypoint,
			})
			require.NoError(t, err)

			value, err := service.ExecuteEntrypoint(t.Context(), cfs, "run")

			require.NoError(t, err)
			require.Equal(t, "kept", value, "the global must not point into the initialiser's released arena")
		})
	}
}

func TestCompileDefersFailingInitialiser(t *testing.T) {
	t.Parallel()
	service := NewService(WithSafeMode())
	compiled, err := service.Compile(t.Context(), `
func unused() {}
var value = func() int {
	panic("initialiser failure")
	return 7
}()
value
`)
	require.NoError(t, err, "a runtime initialiser failure does not surface during compilation")
	_, err = service.Execute(t.Context(), compiled)
	require.Error(t, err, "execution reports the initialiser failure")
}

func TestGoroutineLimitIsWiredThroughService(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxGoroutines(4))
	require.Equal(t, int32(4), service.limits.MaxGoroutines)
}

func TestErrGoroutineLimitIsExported(t *testing.T) {
	t.Parallel()
	require.Error(t, fault.ErrGoroutineLimit)
	wrapped := errors.Join(errors.New("wrapper"), fault.ErrGoroutineLimit)
	require.ErrorIs(t, wrapped, fault.ErrGoroutineLimit)
	require.Contains(t, fault.ErrGoroutineLimit.Error(), "goroutine")
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
	require.Same(t, hook, service.CapabilityHook(), "the hook survives the rest of the option list")
	require.Equal(t, int32(8), service.limits.MaxGoroutines)
	require.Equal(t, 1024, service.limits.MaxCallDepth)
}

func TestEvalAndSessionsReportSeveralTypeErrors(t *testing.T) {
	t.Parallel()

	const snippet = "x := missingOne\ny := missingTwo\n_, _ = x, y\n"
	_, err := NewService().Eval(t.Context(), snippet)
	require.ErrorIs(t, err, fault.ErrTypeCheck)
	require.Contains(t, err.Error(), "missingOne")
	require.Contains(t, err.Error(), "missingTwo")

	_, err = NewService().NewSession().Submit(t.Context(), snippet)
	require.ErrorIs(t, err, fault.ErrTypeCheck)
	require.Contains(t, err.Error(), "missingTwo")
}
