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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pipit.sh/pipit/cmd/pipit/internal/clitest"
)

func TestRunScriptArgumentsReachOsArgsFlagAndEnv(t *testing.T) {
	t.Setenv("PIPIT_TEST_HOST_VAR", "still-here")
	file := writeScript(t, `package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	verbose := flag.Bool("v", false, "verbose")
	flag.Parse()
	lines := []string{
		strings.Join(os.Args[1:], " "),
		fmt.Sprint(*verbose, " ", strings.Join(flag.Args(), ",")),
		fmt.Sprint(os.Getenv("PIPIT_ARGC"), " ", os.Getenv("PIPIT_ARG_0"), " ", os.Getenv("PIPIT_TEST_HOST_VAR")),
		os.Getenv("PIPIT_SCRIPT_DIR"),
	}
	if err := os.WriteFile(flag.Arg(0), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		panic(err)
	}
}
`)
	report := filepath.Join(t.TempDir(), "report.txt")
	result := clitest.Run(context.Background(), RunRun, []string{file, "--", "-v", report, "beta"}, "")
	if result.Code != 0 {
		t.Fatalf("exit code = %d (stderr=%q)", result.Code, result.Stderr)
	}
	written, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("the script did not write its report: %v", err)
	}
	lines := strings.Split(string(written), "\n")
	if len(lines) != 4 {
		t.Fatalf("report = %q", written)
	}
	if lines[0] != "-v "+report+" beta" {
		t.Fatalf("os.Args[1:] = %q", lines[0])
	}
	if lines[1] != "true "+report+",beta" {
		t.Fatalf("flag parsing = %q", lines[1])
	}
	if lines[2] != "3 -v still-here" {
		t.Fatalf("env overlay = %q (host variables must stay visible)", lines[2])
	}
	if lines[3] != filepath.Dir(file) {
		t.Fatalf("PIPIT_SCRIPT_DIR = %q, want %q", lines[3], filepath.Dir(file))
	}
}

func TestRunWithoutArgumentsStillSetsScriptDir(t *testing.T) {
	t.Parallel()
	file := writeScript(t, `package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	report := filepath.Join(os.Getenv("PIPIT_SCRIPT_DIR"), "report.txt")
	if err := os.WriteFile(report, []byte(fmt.Sprint(os.Getenv("PIPIT_ARGC"), " ", len(os.Args))), 0o644); err != nil {
		panic(err)
	}
}
`)
	result := clitest.Run(context.Background(), RunRun, []string{file}, "")
	if result.Code != 0 {
		t.Fatalf("exit code = %d (stderr=%q)", result.Code, result.Stderr)
	}
	written, err := os.ReadFile(filepath.Join(filepath.Dir(file), "report.txt"))
	if err != nil {
		t.Fatalf("PIPIT_SCRIPT_DIR did not point at the script directory: %v", err)
	}
	if string(written) != "0 1" {
		t.Fatalf("report = %q", written)
	}
}
