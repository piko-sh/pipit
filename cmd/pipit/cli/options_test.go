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
	"slices"
	"testing"
)

func TestSplitGlobalFlagsRecognisesEverySpelling(t *testing.T) {
	t.Parallel()

	cases := []struct {
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
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			level, rest, err := splitGlobalFlags(testCase.args)
			if err != nil {
				t.Fatalf("splitGlobalFlags(%q): %v", testCase.args, err)
			}
			if level != testCase.level {
				t.Fatalf("level: got %v, want %v", level, testCase.level)
			}
			if !slices.Equal(rest, testCase.rest) {
				t.Fatalf("rest: got %q, want %q", rest, testCase.rest)
			}
		})
	}
}

func TestSplitGlobalFlagsRejectsBadValues(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"-log-level"}, {"-log-level=chatty", "run"}} {
		if _, _, err := splitGlobalFlags(args); err == nil {
			t.Fatalf("splitGlobalFlags(%q): got nil error, want one", args)
		}
	}
}

func TestLoggerOptionsFollowTheConfiguredLogger(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	if got := loggerOptions(ctx); got != nil {
		t.Fatalf("loggerOptions with no config: got %d options, want none", len(got))
	}

	buffer := &bytes.Buffer{}
	logger := newStderrLogger(slog.LevelDebug, buffer)
	ctx = withConfig(ctx, newConfig([]Option{WithLogger(logger)}))
	if got := loggerFromContext(ctx); got != logger {
		t.Fatalf("loggerFromContext: got %v, want the configured logger", got)
	}
	if got := len(loggerOptions(ctx)); got != 1 {
		t.Fatalf("loggerOptions: got %d options, want 1", got)
	}

	logger.Debug("hello")
	if !bytes.Contains(buffer.Bytes(), []byte("hello")) {
		t.Fatalf("logger output: got %q, want it to hold the record", buffer.String())
	}
}
