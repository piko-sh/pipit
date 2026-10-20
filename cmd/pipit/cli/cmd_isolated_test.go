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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/clitest"
	"pipit.sh/pipit/cmd/pipit/internal/output"
	"pipit.sh/pipit/internal/sandboxtest"
)

func TestRunIsolatedRefusesMissingBoundary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{name: "no worker", args: []string{"-e", "40 + 2"}},
		{name: "invalid worker digest", args: []string{"-worker", "/missing", "-worker-sha256", "invalid", "-e", "40 + 2"}},
		{name: "missing worker binary", args: []string{"-worker", filepath.Join(t.TempDir(), "missing"), "-worker-sha256", strings.Repeat("1", 64), "-cgroup-parent", t.TempDir(), "-e", "40 + 2"}},
		{name: "network requested", args: []string{"-allow-network", "-e", "40 + 2"}},
		{name: "check without a worker", args: []string{"--check"}},
		{name: "check with a missing worker", args: []string{"-worker", filepath.Join(t.TempDir(), "missing"), "-worker-sha256", strings.Repeat("1", 64), "-cgroup-parent", t.TempDir(), "--check"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := clitest.Run(t.Context(), RunIsolated, tt.args, "")
			require.Equal(t, 1, result.Code, "an isolated command never falls back to trusted execution")
			require.Empty(t, result.Stdout)
		})
	}
}

func TestRunIsolatedCheckRejectsSource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		selection []string
	}{
		{name: "expression", selection: []string{"-e", "1"}},
		{name: "empty expression", selection: []string{"-e", ""}},
		{name: "entrypoint", selection: []string{"-entrypoint", "main"}},
		{name: "empty entrypoint", selection: []string{"-entrypoint", ""}},
		{name: "stdin", selection: []string{"-"}},
		{name: "file", selection: []string{"file.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := make([]string, 0, 5+len(tt.selection))
			args = append(args, "--check", "-worker", "/missing", "-worker-sha256", strings.Repeat("1", 64))
			args = append(args, "-watchdog", "/missing-watchdog", "-watchdog-sha256", strings.Repeat("2", 64))
			args = append(args, "-state-dir", "/missing-state")
			args = append(args, tt.selection...)
			result := clitest.Run(t.Context(), RunIsolated, args, "1")
			require.Equal(t, 1, result.Code, "a probe refuses a conflicting source selection")
			require.Empty(t, result.Stdout)
			require.Contains(t, result.Stderr, "cannot be combined")
		})
	}
}

func TestIsolatedCheckSuppressesFailedProbe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result pipit.RestrictedResult
		err    error
	}{
		{name: "cleanup error", result: pipit.RestrictedResult{Value: []byte("1")}, err: errors.New("cleanup failed")},
		{name: "wrong value", result: pipit.RestrictedResult{Value: []byte("2")}},
		{name: "unexpected output", result: pipit.RestrictedResult{Value: []byte("1"), Output: "unexpected"}},
		{name: "truncated output", result: pipit.RestrictedResult{Value: []byte("1"), OutputTruncated: true}},
		{name: "no value", result: pipit.RestrictedResult{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			command := func(_ context.Context, _ []string, streams output.IO) int {
				return writeIsolatedCheck(tt.result, tt.err, streams)
			}
			result := clitest.Run(t.Context(), command, nil, "")
			require.Equal(t, 1, result.Code, "a failed probe does not report success")
			require.Empty(t, result.Stdout)
			require.NotEmpty(t, result.Stderr)
		})
	}
}

func TestIsolatedSourceIsBoundedAndUnambiguous(t *testing.T) {
	t.Parallel()
	reader := strings.NewReader(strings.Repeat("x", isolatedSourceLimit+2))
	_, err := isolatedSource("", []string{"-"}, reader)
	require.Error(t, err, "the source read is bounded")
	require.Equal(t, 1, reader.Len())
	source, err := isolatedSource("", []string{"-"}, strings.NewReader("1 + 2"))
	require.NoError(t, err, "valid stdin is accepted")
	require.Equal(t, "1 + 2", source)
	tests := []struct {
		name       string
		expression string
		args       []string
	}{
		{name: "expression and stdin", expression: "1", args: []string{"-"}},
		{name: "no source", expression: "", args: nil},
		{name: "a file", expression: "", args: []string{"file.go"}},
		{name: "stdin and an extra argument", expression: "", args: []string{"-", "extra"}},
		{name: "expression above the limit", expression: strings.Repeat("x", isolatedSourceLimit+1), args: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := isolatedSource(tt.expression, tt.args, strings.NewReader("1"))
			require.Error(t, err, "an invalid source selection is refused")
		})
	}
}

func TestRunIsolatedNative(t *testing.T) {
	parent := sandboxtest.CgroupParent(t)
	executable := sandboxtest.RequiredBinary(t, "PIPIT_TEST_WORKER_BINARY", "the static worker fixture")
	source, err := os.Open(executable)
	require.NoError(t, err)
	digest := sha256.New()
	_, readErr := io.Copy(digest, source)
	require.NoError(t, errors.Join(readErr, source.Close()))
	watchdogPath := os.Getenv("PIPIT_TEST_HOST_WATCHDOG_BINARY")
	watchdogImage, err := os.ReadFile(watchdogPath)
	require.NoError(t, err)
	watchdogDigest := sha256.Sum256(watchdogImage)
	stateDirectory := t.TempDir()
	require.NoError(t, os.Chmod(stateDirectory, 0700))
	flags := []string{
		"-worker", executable, "-worker-sha256", hex.EncodeToString(digest.Sum(nil)), "-cgroup-parent", parent,
		"-watchdog", watchdogPath, "-watchdog-sha256", hex.EncodeToString(watchdogDigest[:]),
		"-state-dir", stateDirectory, "-tenant", "cli-test",
	}
	t.Run("REPL idle input cancellation", func(t *testing.T) { testIsolatedReplCancellationNative(t, flags) })
	t.Run("recover with nothing to do", func(t *testing.T) {
		result := clitest.Run(t.Context(), RunIsolated, append([]string{"recover"}, flags...), "")
		require.Equal(t, 0, result.Code, "the recovery result")
		require.Equal(t, "Isolated recovery completed.\n", result.Stdout)
	})
	tests := []struct {
		name   string
		args   []string
		input  string
		output string
		code   int
	}{
		{name: "compiled helper", args: []string{"-imports", "math", "-e", "import \"math\"\nmath.Sqrt(49)"}, output: "7\n"},
		{name: "persistent REPL", args: []string{"--repl"}, input: "var value = 40\nvalue+2\n", output: "42\n"},
		{name: "REPL final submission", args: []string{"--repl"}, input: strings.Repeat("1\n", 1024), output: strings.Repeat("1\n", 1024)},
		{name: "REPL compiled helper", args: []string{"--repl", "-imports", "math"}, input: "import \"math\"\nmath.Sqrt(49)\n", output: "7\n"},
		{name: "multiline REPL", args: []string{"--repl"}, input: ":begin\nfunc answer() int {\nreturn 42\n}\n:end\nanswer()\n", output: "42\n"},
		{name: "terminal REPL failure", args: []string{"--repl"}, input: "import \"os\"\nprintln(\"must not run\")\n", code: 1},
		{name: "REPL no trusted hooks", args: []string{"--repl"}, input: ":load /etc/passwd\n", code: 1},
		{name: "live probe", args: []string{"--check"}, input: "invalid source is not read", output: "Native worker probe passed. Experimental; not a security certification.\n"},
		{name: "probe with registered import", args: []string{"--check", "-imports", "os"}, output: "Native worker probe passed. Experimental; not a security certification.\n"},
		{name: "probe with missing enforcement", args: []string{"--check", "-cgroup-parent", t.TempDir()}, code: 1},
		{name: "complete file", args: []string{"-entrypoint", "answer", "-"}, input: "package main\nfunc answer() int { return 42 }", output: "42\n"},
		{name: "bounded print", args: []string{"-print=false", "-e", "println(\"hello\")"}, output: "hello\n"},
		{name: "denied import", args: []string{"-e", "import \"os\"\nos.Getpid()"}, code: 1},
		{name: "registered import unused", args: []string{"-imports", "os", "-e", "1"}, output: "1\n"},
		{name: "rejected import", args: []string{"-imports", "pipit/fs", "-e", "1"}, code: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append(append([]string(nil), flags...), tt.args...)
			result := clitest.Run(t.Context(), RunIsolated, args, tt.input)
			require.Equal(t, tt.code, result.Code, "the isolated CLI result")
			require.Equal(t, tt.output, result.Stdout)
		})
	}
}
