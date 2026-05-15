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

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/clitest"
	"pipit.sh/pipit/cmd/pipit/internal/output"
)

func TestRunIsolatedRefusesMissingBoundary(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"-e", "40 + 2"},
		{"-worker", "/missing", "-worker-sha256", "invalid", "-e", "40 + 2"},
		{"-worker", filepath.Join(t.TempDir(), "missing"), "-worker-sha256", strings.Repeat("1", 64), "-cgroup-parent", t.TempDir(), "-e", "40 + 2"},
		{"-allow-network", "-e", "40 + 2"},
		{"--check"},
		{"-worker", filepath.Join(t.TempDir(), "missing"), "-worker-sha256", strings.Repeat("1", 64), "-cgroup-parent", t.TempDir(), "--check"},
	} {
		result := clitest.Run(context.Background(), RunIsolated, args, "")
		if result.Code != 1 || result.Stdout != "" {
			t.Fatalf("isolated command fell back to trusted execution: %+v", result)
		}
	}
}

func TestRunIsolatedCheckRejectsSource(t *testing.T) {
	t.Parallel()
	for _, selection := range [][]string{
		{"-e", "1"},
		{"-e", ""},
		{"-entrypoint", "main"},
		{"-entrypoint", ""},
		{"-"},
		{"file.go"},
	} {
		args := make([]string, 0, 5+len(selection))
		args = append(args, "--check", "-worker", "/missing", "-worker-sha256", strings.Repeat("1", 64))
		args = append(args, "-watchdog", "/missing-watchdog", "-watchdog-sha256", strings.Repeat("2", 64))
		args = append(args, "-state-dir", "/missing-state")
		args = append(args, selection...)
		result := clitest.Run(context.Background(), RunIsolated, args, "1")
		if result.Code != 1 || result.Stdout != "" || !strings.Contains(result.Stderr, "cannot be combined") {
			t.Fatalf("probe accepted conflicting source selection: %+v", result)
		}
	}
}

func TestIsolatedCheckSuppressesFailedProbe(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		result pipit.RestrictedResult
		err    error
	}{
		{result: pipit.RestrictedResult{Value: []byte("1")}, err: errors.New("cleanup failed")},
		{result: pipit.RestrictedResult{Value: []byte("2")}},
		{result: pipit.RestrictedResult{Value: []byte("1"), Output: "unexpected"}},
		{result: pipit.RestrictedResult{Value: []byte("1"), OutputTruncated: true}},
		{result: pipit.RestrictedResult{}},
	} {
		command := func(_ context.Context, _ []string, streams output.IO) int {
			return writeIsolatedCheck(test.result, test.err, streams)
		}
		result := clitest.Run(context.Background(), command, nil, "")
		if result.Code != 1 || result.Stdout != "" || result.Stderr == "" {
			t.Fatalf("failed probe published success: %+v", result)
		}
	}
}

func TestIsolatedSourceIsBoundedAndUnambiguous(t *testing.T) {
	t.Parallel()
	reader := strings.NewReader(strings.Repeat("x", isolatedSourceLimit+2))
	if _, err := isolatedSource("", []string{"-"}, reader); err == nil || reader.Len() != 1 {
		t.Fatalf("source read was not bounded: remaining=%d error=%v", reader.Len(), err)
	}
	for _, test := range []struct {
		expression string
		args       []string
	}{
		{expression: "1", args: []string{"-"}},
		{expression: "", args: nil},
		{expression: "", args: []string{"file.go"}},
		{expression: "", args: []string{"-", "extra"}},
		{expression: strings.Repeat("x", isolatedSourceLimit+1), args: nil},
	} {
		if _, err := isolatedSource(test.expression, test.args, strings.NewReader("1")); err == nil {
			t.Fatalf("invalid source selection accepted: %+v", test)
		}
	}
	if source, err := isolatedSource("", []string{"-"}, strings.NewReader("1 + 2")); err != nil || source != "1 + 2" {
		t.Fatalf("valid stdin rejected: source=%q error=%v", source, err)
	}
}

func TestRunIsolatedNative(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires an explicitly delegated cgroup-v2 parent")
	}
	executable := os.Getenv("PIPIT_TEST_WORKER_BINARY")
	if executable == "" {
		t.Fatal("requires a static worker fixture")
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.New()
	_, readErr := io.Copy(digest, source)
	if err := errors.Join(readErr, source.Close()); err != nil {
		t.Fatal(err)
	}
	watchdogPath := os.Getenv("PIPIT_TEST_HOST_WATCHDOG_BINARY")
	watchdogImage, err := os.ReadFile(watchdogPath)
	if err != nil {
		t.Fatal(err)
	}
	watchdogDigest := sha256.Sum256(watchdogImage)
	stateDirectory := t.TempDir()
	if err := os.Chmod(stateDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	flags := []string{
		"-worker", executable, "-worker-sha256", hex.EncodeToString(digest.Sum(nil)), "-cgroup-parent", parent,
		"-watchdog", watchdogPath, "-watchdog-sha256", hex.EncodeToString(watchdogDigest[:]),
		"-state-dir", stateDirectory, "-tenant", "cli-test",
	}
	t.Run("REPL idle input cancellation", func(t *testing.T) { testIsolatedReplCancellationNative(t, flags) })
	t.Run("recover with nothing to do", func(t *testing.T) {
		result := clitest.Run(context.Background(), RunIsolated, append([]string{"recover"}, flags...), "")
		if result.Code != 0 || result.Stdout != "Isolated recovery completed.\n" {
			t.Fatalf("unexpected recovery result: %+v", result)
		}
	})
	for _, test := range []struct {
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
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append(append([]string(nil), flags...), test.args...)
			result := clitest.Run(context.Background(), RunIsolated, args, test.input)
			if result.Code != test.code || result.Stdout != test.output {
				t.Fatalf("unexpected isolated CLI result: %+v", result)
			}
		})
	}
}
