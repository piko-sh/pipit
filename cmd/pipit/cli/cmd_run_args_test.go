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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

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
	result := clitest.Run(t.Context(), RunRun, []string{file, "--", "-v", report, "beta"}, "")
	require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)
	written, err := os.ReadFile(report)
	require.NoError(t, err, "the script writes its report")
	lines := strings.Split(string(written), "\n")
	require.Len(t, lines, 4, "report: %q", written)
	require.Equal(t, "-v "+report+" beta", lines[0])
	require.Equal(t, "true "+report+",beta", lines[1])
	require.Equal(t, "3 -v still-here", lines[2], "host variables stay visible through the overlay")
	require.Equal(t, filepath.Dir(file), lines[3], "PIPIT_SCRIPT_DIR")
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
	result := clitest.Run(t.Context(), RunRun, []string{file}, "")
	require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)
	written, err := os.ReadFile(filepath.Join(filepath.Dir(file), "report.txt"))
	require.NoError(t, err, "PIPIT_SCRIPT_DIR points at the script directory")
	require.Equal(t, "0 1", string(written), "report: %q", written)
}
