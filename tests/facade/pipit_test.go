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
	"sync"
	"testing"
	"time"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

func TestInterpreterEvalArithmetic(t *testing.T) {
	t.Parallel()

	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithMaxExecutionTime(5*time.Second))
	result, err := interpreter.Eval(context.Background(), "1 + 2 * 3")
	if err != nil {
		t.Fatalf("eval returned error: %v", err)
	}
	if got, ok := result.(int); !ok || got != 7 {
		t.Fatalf("expected 7, got %v (%T)", result, result)
	}
}

func TestInterpreterEvalImport(t *testing.T) {
	t.Parallel()

	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithMaxExecutionTime(5*time.Second))
	const source = `
import "strings"
strings.ToUpper("pipit")
`
	result, err := interpreter.Eval(context.Background(), source)
	if err != nil {
		t.Fatalf("eval returned error: %v", err)
	}
	if result != "PIPIT" {
		t.Fatalf("expected PIPIT, got %v", result)
	}
}

func TestStandardLibraryExportsPopulated(t *testing.T) {
	t.Parallel()

	exports := stdlib.Exports()
	if len(exports) == 0 {
		t.Fatal("expected at least one package in the standard library")
	}
	if _, ok := exports["strings"]; !ok {
		t.Fatal("expected the strings package to be exported")
	}
}

func TestBareInterpreterRegistersNothing(t *testing.T) {
	t.Parallel()

	interpreter := pipit.NewInterpreter()
	if interpreter.HasRegisteredPackage("strings") {
		t.Fatal("expected a bare interpreter to register no packages")
	}
	if _, err := interpreter.Eval(context.Background(), "import \"strings\"\nstrings.ToUpper(\"x\")"); err == nil {
		t.Fatal("expected an unregistered import to fail")
	}
}

func TestPooledInterpretersEvaluateIndependently(t *testing.T) {
	t.Parallel()

	pool := sync.Pool{New: func() any { return pipit.NewInterpreter(stdlib.WithStandardLibrary()) }}

	for _, expression := range []string{"21 * 2", "6 * 7", "84 / 2"} {
		interpreter, ok := pool.Get().(*pipit.Interpreter)
		if !ok {
			t.Fatal("pool returned an unexpected type")
		}

		result, err := interpreter.Eval(context.Background(), expression)
		if err != nil {
			t.Fatalf("eval %q: %v", expression, err)
		}
		if got, isInt := result.(int); !isInt || got != 42 {
			t.Fatalf("eval %q: expected 42, got %v (%T)", expression, result, result)
		}

		pool.Put(interpreter)
	}
}

func TestFacadeErrorsAreIsAble(t *testing.T) {
	t.Parallel()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name    string
		options []pipit.Option
		ctx     context.Context
		source  string
		want    error
	}{
		{name: "parse", options: nil, ctx: context.Background(), source: "func(", want: pipit.ErrParse},
		{name: "typecheck", options: nil, ctx: context.Background(), source: `var x int = "s"; x`, want: pipit.ErrTypeCheck},
		{name: "cancelled", options: nil, ctx: cancelled, source: "1 + 1", want: pipit.ErrExecutionCancelled},
		{name: "cost budget", options: []pipit.Option{pipit.WithCostBudget(1)}, ctx: context.Background(), source: "s := 0; for i := 0; i < 1000000; i++ { s += i }; s", want: pipit.ErrCostBudgetExceeded},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			options := append([]pipit.Option{stdlib.WithStandardLibrary()}, testCase.options...)
			interpreter := pipit.NewInterpreter(options...)
			_, err := interpreter.Eval(testCase.ctx, testCase.source)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("errors.Is(%v, %v) is false", err, testCase.want)
			}
		})
	}
}
