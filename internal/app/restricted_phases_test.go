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

	"github.com/stretchr/testify/require"
)

func TestRestrictedPhases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		source          string
		entrypoint      string
		file            bool
		wantValue       string
		wantInitialised int
	}{
		{name: "expression", source: "6 * 7", wantValue: "42"},
		{name: "void expression", source: "println(\"hello\")"},
		{name: "compiled helper", source: "import \"math\"\nmath.Sqrt(49)", wantValue: "7"},
		{name: "mixed initialiser", source: "func unused() {}\nvar value = func() int { println(\"initialised\"); return 7 }()\nvalue", wantValue: "7", wantInitialised: 1},
		{name: "complete file", source: "package main\nvar value = 42\nfunc answer() int { return value }", entrypoint: "answer", file: true, wantValue: "42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			interpreter, err := NewRestrictedInterpreter(RestrictedConfig{Imports: []string{"math"}})
			require.NoError(t, err)
			boundaries := 0
			result, err := EvaluateRestrictedPhased(t.Context(), interpreter, tt.source, tt.entrypoint, tt.file,
				func() error { boundaries++; return nil })
			require.NoError(t, err)
			require.Equal(t, tt.wantValue, string(result.Value))
			require.Equal(t, 1, boundaries, "execution crosses the boundary once")
			require.Equal(t, tt.wantInitialised, strings.Count(result.Output, "initialised"), "the initialiser runs exactly once")
		})
	}
}

func TestRestrictedPhaseRefusal(t *testing.T) {
	t.Parallel()
	interpreter, err := NewRestrictedInterpreter(RestrictedConfig{})
	require.NoError(t, err)
	refused := errors.New("host refused execution")
	result, err := EvaluateRestrictedPhased(t.Context(), interpreter, "println(\"executed\")\n1", "", false,
		func() error { return refused })
	require.ErrorIs(t, err, refused, "a refused execution produces no result")
	require.Empty(t, result.Output)
	require.Nil(t, result.Value)
	called := false
	_, err = EvaluateRestrictedPhased(t.Context(), interpreter, "this is not Go", "", false,
		func() error { called = true; return nil })
	require.Error(t, err, "invalid source never reaches execution")
	require.False(t, called)
	_, err = interpreter.Eval(t.Context(), "1")
	require.NoError(t, err, "failed phases release admission")
}

func TestRestrictedPhasesRejectAuthority(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
	}{
		{name: "os import", source: "import \"os\"\nos.Getpid()"},
		{name: "goroutine", source: "go func() {}()"},
		{name: "channel", source: "make(chan int)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			interpreter, err := NewRestrictedInterpreter(RestrictedConfig{})
			require.NoError(t, err)
			called := false
			_, err = EvaluateRestrictedPhased(t.Context(), interpreter, tt.source, "", false,
				func() error { called = true; return nil })
			require.Error(t, err)
			require.False(t, called, "restricted source never reaches execution")
		})
	}
}

func TestRestrictedPhaseCancellationBeforeInitialisers(t *testing.T) {
	t.Parallel()
	interpreter, err := NewRestrictedInterpreter(RestrictedConfig{})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result, err := EvaluateRestrictedPhased(ctx, interpreter,
		"func unused() {}\nvar value = func() int { println(\"executed\"); return 7 }()\nvalue", "", false,
		func() error { cancel(); return nil })
	require.ErrorIs(t, err, context.Canceled, "a cancelled phase runs no initialiser")
	require.Empty(t, result.Output)
}

func TestRestrictedPhasesRetainAdmissionAtBoundary(t *testing.T) {
	t.Parallel()
	interpreter, err := NewRestrictedInterpreter(RestrictedConfig{})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
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
	_, err = interpreter.Eval(t.Context(), "2")
	require.ErrorIs(t, err, ErrRestrictedBusy, "a concurrent evaluation is refused")
	_, err = EvaluateRestrictedPhased(t.Context(), interpreter, "2", "", false, func() error { return nil })
	require.ErrorIs(t, err, ErrRestrictedBusy, "a concurrent compilation is refused")
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}
