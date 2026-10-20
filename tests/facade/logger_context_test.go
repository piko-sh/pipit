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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	tests := []struct {
		name  string
		drive func(t *testing.T, interpreter *pipit.Interpreter)
	}{
		{
			name: "Eval",
			drive: func(t *testing.T, interpreter *pipit.Interpreter) {
				t.Helper()
				_, err := interpreter.Eval(t.Context(), loggerProbeExpr)
				require.NoError(t, err, "eval")
			},
		},
		{
			name: "EvalFile",
			drive: func(t *testing.T, interpreter *pipit.Interpreter) {
				t.Helper()
				_, err := interpreter.EvalFile(t.Context(), loggerProbeSource, "main")
				require.NoError(t, err, "EvalFile")
			},
		},
		{
			name: "Execute",
			drive: func(t *testing.T, interpreter *pipit.Interpreter) {
				t.Helper()
				compiled, err := interpreter.Compile(t.Context(), loggerProbeExpr)
				require.NoError(t, err, "compile")
				_, err = interpreter.Execute(t.Context(), compiled)
				require.NoError(t, err, "Execute")
			},
		},
		{
			name: "ExecuteEntrypoint",
			drive: func(t *testing.T, interpreter *pipit.Interpreter) {
				t.Helper()
				cfs := compileProbeFileSet(t, interpreter)
				_, err := interpreter.ExecuteEntrypoint(t.Context(), cfs, "main")
				require.NoError(t, err, "ExecuteEntrypoint")
			},
		},
		{
			name: "CallFunction",
			drive: func(t *testing.T, interpreter *pipit.Interpreter) {
				t.Helper()
				cfs := compileProbeFileSet(t, interpreter)
				_, err := interpreter.CallFunction(t.Context(), cfs, "probe")
				require.NoError(t, err, "CallFunction")
			},
		},
		{
			name: "ExecuteInits",
			drive: func(t *testing.T, interpreter *pipit.Interpreter) {
				t.Helper()
				cfs := compileProbeFileSet(t, interpreter)
				require.NoError(t, interpreter.ExecuteInits(t.Context(), cfs), "ExecuteInits")
			},
		},
		{
			name: "Session.Submit",
			drive: func(t *testing.T, interpreter *pipit.Interpreter) {
				t.Helper()
				session := interpreter.NewSession()
				_, err := session.Submit(t.Context(), loggerProbeExpr)
				require.NoError(t, err, "submit")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			want := slog.New(slog.DiscardHandler)
			probe := &loggerProbe{PermissiveCapabilityHook: pipit.PermissiveCapabilityHook{}, mu: sync.Mutex{}, seen: nil}
			interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithLogger(want), pipit.WithCapabilityHook(probe))

			tt.drive(t, interpreter)

			got := probe.observed()
			require.NotNil(t, got, "the capability hook is consulted")
			assert.Same(t, want, got, "the entry point carries the host logger into its context")
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
	require.NoError(t, err, "NewRestrictedInterpreter")

	_, err = interpreter.Eval(t.Context(), "import \"fmt\"\nfmt.Sprintf(\"%d\", 42)")
	require.NoError(t, err, "eval")

	got := probe.observed()
	require.NotNil(t, got, "the capability hook is consulted")
	assert.Same(t, want, got, "the restricted tier carries the host logger into its context")
}

func compileProbeFileSet(t *testing.T, interpreter *pipit.Interpreter) *pipit.CompiledFileSet {
	t.Helper()
	cfs, err := interpreter.CompileFileSet(t.Context(), map[string]string{"main.go": loggerProbeSource})
	require.NoError(t, err, "CompileFileSet")
	return cfs
}
