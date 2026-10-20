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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

func TestRestrictedEvaluation(t *testing.T) {
	t.Parallel()
	interpreter, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{}, stdlib.Providers()...)
	require.NoError(t, err)
	result, err := interpreter.Eval(t.Context(), "6 * 7")
	require.NoError(t, err, "result=%+v", result)
	require.Equal(t, "42", string(result.Value))
	result, err = interpreter.EvalFile(t.Context(), `package main; func main() { println("hello") }`, "main")
	require.NoError(t, err, "result=%+v", result)
	require.Equal(t, "hello\n", result.Output)
}

func TestRestrictedDeniesAmbientAuthority(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
	}{
		{name: "process exit", source: `package main; import "os"; func main() { os.Exit(23) }`},
		{name: "environment", source: `package main; import "os"; func main() { println(os.Environ()) }`},
		{name: "directory file system", source: `package main; import ("os"; "io/fs"); func main() { fs.ReadFile(os.DirFS("/tmp"), "marker") }`},
		{name: "open in root", source: `package main; import "os"; func main() { os.OpenInRoot("/tmp", "marker") }`},
		{name: "network", source: `package main; import "net/http"; func main() { http.Get("https://example.com") }`},
		{name: "unsafe", source: `package main; import "unsafe"; func main() { println(unsafe.Sizeof(1)) }`},
		{name: "goroutine", source: `package main; func main() { go func() {}() }`},
		{name: "channel", source: `package main; func main() { make(chan int) }`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			interpreter, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{}, stdlib.Providers()...)
			require.NoError(t, err)
			_, err = interpreter.EvalFile(t.Context(), tt.source, "main")
			require.Error(t, err, "the restricted operation is refused")
		})
	}
}

func TestRestrictedManifestIsPrivate(t *testing.T) {
	t.Parallel()
	imports := []string{"math"}
	interpreter, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{Imports: imports}, stdlib.Providers()...)
	require.NoError(t, err)
	imports[0] = "os"

	result, err := interpreter.Eval(t.Context(), "import \"math\"\nmath.Sqrt(49)")
	require.NoError(t, err, "result=%+v", result)
	require.Equal(t, "7", string(result.Value))
	_, err = interpreter.Eval(t.Context(), "import \"math\"\nmath.Gamma(5)")
	require.NoError(t, err, "the full math package is available")

	_, err = interpreter.Eval(t.Context(), "import \"os\"\nos.Getpid()")
	require.Error(t, err, "an import outside the allowlist is refused")

	_, err = pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{Imports: []string{"definitely/not/a/package"}}, stdlib.Providers()...)
	require.ErrorIs(t, err, pipit.ErrInvalidRestrictedConfig)
}

func TestRestrictedAllowlistedCapabilityDenied(t *testing.T) {
	t.Parallel()
	interpreter, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{Imports: []string{"os"}}, stdlib.Providers()...)
	require.NoError(t, err)
	_, err = interpreter.Eval(t.Context(), "import \"os\"\nos.ReadFile(\"/etc/hostname\")")
	require.Error(t, err, "the default deny hook stops an allowlisted os.ReadFile")
}

func TestRestrictedLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config pipit.RestrictedConfig
		source string
	}{
		{name: "source", config: pipit.RestrictedConfig{MaxSourceBytes: 3}, source: "12345"},
		{name: "cost", config: pipit.RestrictedConfig{CostBudget: 100}, source: "for {}"},
		{name: "allocation", config: pipit.RestrictedConfig{MaxAllocationElements: 4}, source: "make([]byte, 128)"},
		{name: "output", config: pipit.RestrictedConfig{MaxOutputBytes: 4}, source: `println("12345678")`},
		{name: "return", config: pipit.RestrictedConfig{MaxReturnBytes: 4}, source: `"12345678"`},
		{name: "encoded return", config: pipit.RestrictedConfig{MaxReturnBytes: 4}, source: `"\x00"`},
		{name: "encoded number", config: pipit.RestrictedConfig{MaxReturnBytes: 1}, source: "12345"},
		{name: "object", config: pipit.RestrictedConfig{}, source: "make([]byte, 4)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			interpreter, err := pipit.NewRestrictedInterpreter(tt.config, stdlib.Providers()...)
			require.NoError(t, err)
			result, err := interpreter.Eval(t.Context(), tt.source)
			require.Error(t, err, "the limit is enforced (result %+v)", result)
			if tt.config.MaxOutputBytes > 0 {
				require.LessOrEqual(t, len(result.Output), tt.config.MaxOutputBytes, "the output stays within the limit")
			}
		})
	}
}

func TestRestrictedConfigRejectsNegativeLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config pipit.RestrictedConfig
	}{
		{name: "timeout", config: pipit.RestrictedConfig{Timeout: -1}},
		{name: "cost budget", config: pipit.RestrictedConfig{CostBudget: -1}},
		{name: "arena bytes", config: pipit.RestrictedConfig{MaxArenaBytes: -1}},
		{name: "source bytes", config: pipit.RestrictedConfig{MaxSourceBytes: -1}},
		{name: "output bytes", config: pipit.RestrictedConfig{MaxOutputBytes: -1}},
		{name: "return bytes", config: pipit.RestrictedConfig{MaxReturnBytes: -1}},
		{name: "string bytes", config: pipit.RestrictedConfig{MaxStringBytes: -1}},
		{name: "allocation elements", config: pipit.RestrictedConfig{MaxAllocationElements: -1}},
		{name: "call depth", config: pipit.RestrictedConfig{MaxCallDepth: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := pipit.NewRestrictedInterpreter(tt.config, stdlib.Providers()...)
			require.ErrorIs(t, err, pipit.ErrInvalidRestrictedConfig)
		})
	}
}

func TestRestrictedCancellationAndFreshState(t *testing.T) {
	t.Parallel()
	interpreter, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{}, stdlib.Providers()...)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = interpreter.Eval(ctx, "1")
	require.ErrorIs(t, err, context.Canceled)
	_, err = interpreter.Eval(t.Context(), "private := 42; _ = private")
	require.NoError(t, err)
	_, err = interpreter.Eval(t.Context(), "private")
	require.Error(t, err, "state from the previous run is gone")
	require.Contains(t, err.Error(), "private")
}

func TestRestrictedDeadlineAndFreshRun(t *testing.T) {
	t.Parallel()
	interpreter, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{
		Timeout:    20 * time.Millisecond,
		CostBudget: 100_000_000,
	}, stdlib.Providers()...)
	require.NoError(t, err)
	_, err = interpreter.Eval(t.Context(), "for {}")
	require.ErrorIs(t, err, context.DeadlineExceeded, "the deadline keeps its identity")
	result, err := interpreter.Eval(t.Context(), "42")
	require.NoError(t, err, "a fresh run succeeds (result %+v)", result)
	require.Equal(t, "42", string(result.Value))
}
