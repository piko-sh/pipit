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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

func TestInterpreterEvalArithmetic(t *testing.T) {
	t.Parallel()

	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithMaxExecutionTime(5*time.Second))
	result, err := interpreter.Eval(t.Context(), "1 + 2 * 3")
	require.NoError(t, err)
	require.Equal(t, 7, result)
}

func TestInterpreterEvalImport(t *testing.T) {
	t.Parallel()

	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithMaxExecutionTime(5*time.Second))
	const source = `
import "strings"
strings.ToUpper("pipit")
`
	result, err := interpreter.Eval(t.Context(), source)
	require.NoError(t, err)
	require.Equal(t, "PIPIT", result)
}

func TestStandardLibraryExportsPopulated(t *testing.T) {
	t.Parallel()

	exports := stdlib.Exports()
	require.NotEmpty(t, exports, "the standard library exports at least one package")
	_, ok := exports["strings"]
	require.True(t, ok, "the standard library exports strings")
}

func TestBareInterpreterRegistersNothing(t *testing.T) {
	t.Parallel()

	interpreter := pipit.NewInterpreter()
	require.False(t, interpreter.HasRegisteredPackage("strings"), "a bare interpreter registers no packages")
	_, err := interpreter.Eval(t.Context(), "import \"strings\"\nstrings.ToUpper(\"x\")")
	require.Error(t, err, "an unregistered import fails")
}

func TestPooledInterpretersEvaluateIndependently(t *testing.T) {
	t.Parallel()

	pool := sync.Pool{New: func() any { return pipit.NewInterpreter(stdlib.WithStandardLibrary()) }}

	for _, expression := range []string{"21 * 2", "6 * 7", "84 / 2"} {
		interpreter, ok := pool.Get().(*pipit.Interpreter)
		require.True(t, ok, "the pool holds interpreters")

		result, err := interpreter.Eval(t.Context(), expression)
		require.NoError(t, err, "eval %q", expression)
		require.Equal(t, 42, result)

		pool.Put(interpreter)
	}
}

func TestFacadeErrorsAreIsAble(t *testing.T) {
	t.Parallel()

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	tests := []struct {
		name    string
		options []pipit.Option
		ctx     context.Context
		source  string
		want    error
	}{
		{name: "parse", options: nil, ctx: t.Context(), source: "func(", want: pipit.ErrParse},
		{name: "typecheck", options: nil, ctx: t.Context(), source: `var x int = "s"; x`, want: pipit.ErrTypeCheck},
		{name: "cancelled", options: nil, ctx: cancelled, source: "1 + 1", want: pipit.ErrExecutionCancelled},
		{name: "cost budget", options: []pipit.Option{pipit.WithCostBudget(1)}, ctx: t.Context(), source: "s := 0; for i := 0; i < 1000000; i++ { s += i }; s", want: pipit.ErrCostBudgetExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			options := append([]pipit.Option{stdlib.WithStandardLibrary()}, tt.options...)
			interpreter := pipit.NewInterpreter(options...)
			_, err := interpreter.Eval(tt.ctx, tt.source)
			require.ErrorIs(t, err, tt.want)
		})
	}
}
