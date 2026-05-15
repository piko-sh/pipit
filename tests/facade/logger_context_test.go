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
	"log/slog"
	"reflect"
	"sync"
	"testing"

	"pipit.sh/pipit"
	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/sdk/stdlib"
)

type loggerProbe struct {
	pipit.PermissiveCapabilityHook
	mu   sync.Mutex
	seen *slog.Logger
}

func (probe *loggerProbe) CheckFunctionCall(ctx context.Context, _, _ string, _ []reflect.Value) error {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if probe.seen == nil {
		probe.seen = logging.LoggerFrom(ctx)
	}
	return nil
}

func (probe *loggerProbe) observed() *slog.Logger {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	return probe.seen
}

const loggerProbeExpr = "import \"os\"\nos.Getenv(\"PIPIT_LOGGER_PROBE\")"

const loggerProbeSource = `package main

import "os"

var probeVar = os.Getenv("PIPIT_LOGGER_PROBE_VAR")

func init() { _ = os.Getenv("PIPIT_LOGGER_PROBE_INIT") }

func probe() { _ = os.Getenv("PIPIT_LOGGER_PROBE_FUNC") }

func main() { probe() }
`

func TestHostLoggerReachesEveryEntryPoint(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		drive func(t *testing.T, interpreter *pipit.Interpreter)
	}{
		{
			name: "Eval",
			drive: func(t *testing.T, interpreter *pipit.Interpreter) {
				t.Helper()
				if _, err := interpreter.Eval(t.Context(), loggerProbeExpr); err != nil {
					t.Fatalf("Eval: %v", err)
				}
			},
		},
		{
			name: "EvalFile",
			drive: func(t *testing.T, interpreter *pipit.Interpreter) {
				t.Helper()
				if _, err := interpreter.EvalFile(t.Context(), loggerProbeSource, "main"); err != nil {
					t.Fatalf("EvalFile: %v", err)
				}
			},
		},
		{
			name: "Execute",
			drive: func(t *testing.T, interpreter *pipit.Interpreter) {
				t.Helper()
				compiled, err := interpreter.Compile(t.Context(), loggerProbeExpr)
				if err != nil {
					t.Fatalf("Compile: %v", err)
				}
				if _, err := interpreter.Execute(t.Context(), compiled); err != nil {
					t.Fatalf("Execute: %v", err)
				}
			},
		},
		{
			name: "ExecuteEntrypoint",
			drive: func(t *testing.T, interpreter *pipit.Interpreter) {
				t.Helper()
				cfs := compileProbeFileSet(t, interpreter)
				if _, err := interpreter.ExecuteEntrypoint(t.Context(), cfs, "main"); err != nil {
					t.Fatalf("ExecuteEntrypoint: %v", err)
				}
			},
		},
		{
			name: "CallFunction",
			drive: func(t *testing.T, interpreter *pipit.Interpreter) {
				t.Helper()
				cfs := compileProbeFileSet(t, interpreter)
				if _, err := interpreter.CallFunction(t.Context(), cfs, "probe"); err != nil {
					t.Fatalf("CallFunction: %v", err)
				}
			},
		},
		{
			name: "ExecuteInits",
			drive: func(t *testing.T, interpreter *pipit.Interpreter) {
				t.Helper()
				cfs := compileProbeFileSet(t, interpreter)
				if err := interpreter.ExecuteInits(t.Context(), cfs); err != nil {
					t.Fatalf("ExecuteInits: %v", err)
				}
			},
		},
		{
			name: "Session.Submit",
			drive: func(t *testing.T, interpreter *pipit.Interpreter) {
				t.Helper()
				session := interpreter.NewSession()
				if _, err := session.Submit(t.Context(), loggerProbeExpr); err != nil {
					t.Fatalf("Submit: %v", err)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			want := slog.New(slog.DiscardHandler)
			probe := &loggerProbe{PermissiveCapabilityHook: pipit.PermissiveCapabilityHook{}, mu: sync.Mutex{}, seen: nil}
			interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithLogger(want), pipit.WithCapabilityHook(probe))

			testCase.drive(t, interpreter)

			got := probe.observed()
			if got == nil {
				t.Fatal("capability hook was never consulted; the probe proves nothing")
			}
			if got != want {
				t.Errorf("entry point did not carry the host logger into its context:\n got %p\nwant %p", got, want)
			}
		})
	}
}

func TestHostLoggerReachesRestrictedTier(t *testing.T) {
	t.Parallel()

	want := slog.New(slog.DiscardHandler)
	probe := &loggerProbe{PermissiveCapabilityHook: pipit.PermissiveCapabilityHook{}, mu: sync.Mutex{}, seen: nil}

	interpreter, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{
		Hook:                  probe,
		Imports:               []string{"fmt"},
		Logger:                want,
		Timeout:               0,
		MaxSourceBytes:        0,
		MaxOutputBytes:        0,
		MaxReturnBytes:        0,
		MaxStringBytes:        0,
		MaxAllocationElements: 0,
		MaxCallDepth:          0,
		CostBudget:            0,
		MaxArenaBytes:         0,
	}, stdlib.Providers()...)
	if err != nil {
		t.Fatalf("NewRestrictedInterpreter: %v", err)
	}

	if _, err := interpreter.Eval(t.Context(), "import \"fmt\"\nfmt.Sprintf(\"%d\", 42)"); err != nil {
		t.Fatalf("Eval: %v", err)
	}

	got := probe.observed()
	if got == nil {
		t.Fatal("capability hook was never consulted; the probe proves nothing")
	}
	if got != want {
		t.Errorf("restricted tier did not carry the host logger into its context:\n got %p\nwant %p", got, want)
	}
}

func compileProbeFileSet(t *testing.T, interpreter *pipit.Interpreter) *pipit.CompiledFileSet {
	t.Helper()
	cfs, err := interpreter.CompileFileSet(t.Context(), map[string]string{"main.go": loggerProbeSource})
	if err != nil {
		t.Fatalf("CompileFileSet: %v", err)
	}
	return cfs
}
