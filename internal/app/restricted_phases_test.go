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
	"strings"
	"testing"
)

func TestRestrictedPhases(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		source     string
		entrypoint string
		value      string
		file       bool
	}{
		{name: "expression", source: "6 * 7", value: "42"},
		{name: "void expression", source: "println(\"hello\")"},
		{name: "compiled helper", source: "import \"math\"\nmath.Sqrt(49)", value: "7"},
		{name: "mixed initialiser", source: "func unused() {}\nvar value = func() int { println(\"initialised\"); return 7 }()\nvalue", value: "7"},
		{name: "complete file", source: "package main\nvar value = 42\nfunc answer() int { return value }", entrypoint: "answer", value: "42", file: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			interpreter, err := NewRestrictedInterpreter(RestrictedConfig{Imports: []string{"math"}})
			if err != nil {
				t.Fatal(err)
			}
			boundaries := 0
			result, err := EvaluateRestrictedPhased(context.Background(), interpreter, test.source, test.entrypoint, test.file,
				func() error { boundaries++; return nil })
			if err != nil || string(result.Value) != test.value || boundaries != 1 {
				t.Fatalf("result=%+v boundaries=%d error=%v", result, boundaries, err)
			}
			if test.name == "mixed initialiser" && strings.Count(result.Output, "initialised") != 1 {
				t.Fatalf("initialiser did not run exactly once: %q", result.Output)
			}
		})
	}
}

func TestRestrictedPhaseRefusal(t *testing.T) {
	t.Parallel()
	interpreter, err := NewRestrictedInterpreter(RestrictedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	refused := errors.New("host refused execution")
	result, err := EvaluateRestrictedPhased(context.Background(), interpreter, "println(\"executed\")\n1", "", false,
		func() error { return refused })
	if !errors.Is(err, refused) || result.Output != "" || result.Value != nil {
		t.Fatalf("refused execution produced a result: %+v %v", result, err)
	}
	called := false
	_, err = EvaluateRestrictedPhased(context.Background(), interpreter, "this is not Go", "", false,
		func() error { called = true; return nil })
	if err == nil || called {
		t.Fatalf("invalid source entered execution: called=%v error=%v", called, err)
	}
	if _, err := interpreter.Eval(context.Background(), "1"); err != nil {
		t.Fatalf("failed phases leaked admission: %v", err)
	}
}

func TestRestrictedPhasesRejectAuthority(t *testing.T) {
	t.Parallel()
	interpreter, err := NewRestrictedInterpreter(RestrictedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{
		"import \"os\"\nos.Getpid()",
		"go func() {}()",
		"make(chan int)",
	} {
		called := false
		_, err := EvaluateRestrictedPhased(context.Background(), interpreter, source, "", false,
			func() error { called = true; return nil })
		if err == nil || called {
			t.Fatalf("restricted source reached execution: %q called=%v error=%v", source, called, err)
		}
	}
}

func TestRestrictedPhaseCancellationBeforeInitialisers(t *testing.T) {
	t.Parallel()
	interpreter, err := NewRestrictedInterpreter(RestrictedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, err := EvaluateRestrictedPhased(ctx, interpreter,
		"func unused() {}\nvar value = func() int { println(\"executed\"); return 7 }()\nvalue", "", false,
		func() error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) || result.Output != "" {
		t.Fatalf("cancelled phase ran an initialiser: %+v %v", result, err)
	}
}

func TestRestrictedPhasesRetainAdmissionAtBoundary(t *testing.T) {
	t.Parallel()
	interpreter, err := NewRestrictedInterpreter(RestrictedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := EvaluateRestrictedPhased(ctx, interpreter, "1", "", false, func() error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		})
		done <- err
	}()
	<-entered
	if _, err := interpreter.Eval(context.Background(), "2"); !errors.Is(err, ErrRestrictedBusy) {
		t.Fatalf("concurrent evaluation admitted: %v", err)
	}
	_, err = EvaluateRestrictedPhased(context.Background(), interpreter, "2", "", false, func() error { return nil })
	if !errors.Is(err, ErrRestrictedBusy) {
		t.Fatalf("concurrent compilation admitted: %v", err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
