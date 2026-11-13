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

package cli

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/logging"
)

func TestSplitGlobalFlagsRecognisesEverySpelling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		args  []string
		level slog.Level
		rest  []string
	}{
		{name: "absent", args: []string{"run", "x.go"}, level: defaultLogLevel, rest: []string{"run", "x.go"}},
		{name: "inline", args: []string{"-log-level=debug", "run"}, level: slog.LevelDebug, rest: []string{"run"}},
		{name: "spaced", args: []string{"-log-level", "info", "run"}, level: slog.LevelInfo, rest: []string{"run"}},
		{name: "double dash", args: []string{"--log-level=error", "run"}, level: slog.LevelError, rest: []string{"run"}},
		{name: "mixed case", args: []string{"-log-level=WARN", "run"}, level: slog.LevelWarn, rest: []string{"run"}},
		{name: "no command", args: []string{"-log-level=debug"}, level: slog.LevelDebug, rest: []string{}},
		{name: "subcommand flag kept", args: []string{"test", "-v"}, level: defaultLogLevel, rest: []string{"test", "-v"}},
		{name: "another flag first", args: []string{"-v", "-log-level=debug", "run"}, level: defaultLogLevel, rest: []string{"-v", "-log-level=debug", "run"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			level, rest, err := splitGlobalFlags(tt.args)
			require.NoError(t, err)
			require.Equal(t, tt.level, level)
			require.Equal(t, tt.rest, rest)
		})
	}
}

func TestSplitGlobalFlagsRejectsBadValues(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"-log-level"}, {"-log-level=chatty", "run"}} {
		_, _, err := splitGlobalFlags(args)
		require.Error(t, err, "splitGlobalFlags(%q) fails", args)
	}
}

func TestTheInvocationContextCarriesTheConfiguredLogger(t *testing.T) {
	t.Parallel()

	buffer := &bytes.Buffer{}
	logger := newStderrLogger(slog.LevelDebug, buffer)
	ctx := invocationContext(t.Context(), newConfig([]Option{WithLogger(logger)}))
	_, bound := logging.From(ctx)
	require.Same(t, logger, bound, "library calls under the invocation's context log through the configured logger")

	bound.Debug("hello")
	require.Contains(t, buffer.String(), "hello", "the logger writes the record")
}
