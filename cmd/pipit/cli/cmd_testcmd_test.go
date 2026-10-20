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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/cmd/pipit/internal/clitest"
)

func captureTestRunner(t *testing.T) *[]string {
	t.Helper()
	previous := runTestMain
	var names []string
	runTestMain = func(matchString func(pat, str string) (bool, error), tests []testing.InternalTest) int {
		for _, test := range tests {
			names = append(names, test.Name)
		}
		if testing.RunTests(matchString, tests) {
			return 0
		}
		return 1
	}
	t.Cleanup(func() { runTestMain = previous })
	return &names
}

func writeTestPackage(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	return root
}

func TestPipitTestRunsPassingSuite(t *testing.T) {
	names := captureTestRunner(t)
	report := filepath.Join(t.TempDir(), "report.txt")
	root := writeTestPackage(t, map[string]string{
		"calc.go": "package calc\n\nfunc Add(a, b int) int { return a + b }\n\nvar seen []string\n",
		"calc_test.go": `package calc

import (
	"os"
	"strings"
	"testing"
)

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatalf("Add(2, 3) = %d", Add(2, 3))
	}
	seen = append(seen, t.Name())
}

func TestSubtests(t *testing.T) {
	for _, name := range []string{"one", "two"} {
		t.Run(name, func(t *testing.T) {
			t.Log("running", name)
			seen = append(seen, t.Name())
		})
	}
	if testing.Short() {
		t.Fatal("short mode was not requested")
	}
}

func TestReport(t *testing.T) {
	seen = append(seen, t.Name())
	if err := os.WriteFile(os.Getenv("REPORT"), []byte(strings.Join(seen, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
}

func Testify(t *testing.T) { t.Fatal("not a test: lower-case rune after the prefix") }
`,
	})
	t.Setenv("REPORT", report)
	result := clitest.Run(t.Context(), RunTest, []string{root}, "")
	require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)
	require.Equal(t, []string{"TestAdd", "TestSubtests", "TestReport"}, *names, "the suites run in source order")
	written, err := os.ReadFile(report)
	require.NoError(t, err, "the suite writes its report")
	require.Equal(t, "TestAdd\nTestSubtests/one\nTestSubtests/two\nTestReport", string(written))
}

func TestPipitTestReportsFailuresAndHonoursRunFilter(t *testing.T) {
	captureTestRunner(t)
	root := writeTestPackage(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
		"main_test.go": `package main

import "testing"

func TestPasses(t *testing.T) {}

func TestFails(t *testing.T) { t.Errorf("expected failure") }

func TestPanics(t *testing.T) { panic("boom") }
`,
	})
	result := clitest.Run(t.Context(), RunTest, []string{root}, "")
	require.Equal(t, 1, result.Code, "a failing suite exits 1: %q", result.Stderr)
	result = clitest.Run(t.Context(), RunTest, []string{"-run", "TestPasses$", root}, "")
	require.Equal(t, 0, result.Code, "-run selects only the passing test: %q", result.Stderr)
	result = clitest.Run(t.Context(), RunTest, []string{"-run", "TestPanics", root}, "")
	require.Equal(t, 1, result.Code, "an interpreted panic fails the test")
}

func TestPipitTestRefusesExternalTestPackagesAndBadSignatures(t *testing.T) {
	names := captureTestRunner(t)
	external := writeTestPackage(t, map[string]string{
		"calc.go":      "package calc\n",
		"calc_test.go": "package calc_test\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n",
	})
	result := clitest.Run(t.Context(), RunTest, []string{external}, "")
	require.Equal(t, 1, result.Code)
	require.Contains(t, result.Stderr, "external test packages")
	signature := writeTestPackage(t, map[string]string{
		"calc.go":      "package calc\n",
		"calc_test.go": "package calc\n\nfunc TestX() {}\n",
	})
	result = clitest.Run(t.Context(), RunTest, []string{signature}, "")
	require.Equal(t, 1, result.Code)
	require.Contains(t, result.Stderr, "wrong signature for TestX")
	require.Empty(t, *names, "no suite runs")
}

func TestPipitTestResolvesLocalModulePackages(t *testing.T) {
	names := captureTestRunner(t)
	root := writeLocalModule(t)
	testFile := "package main\n\nimport (\n\t\"testing\"\n\n\t\"example.com/app/internal/util\"\n)\n\nfunc TestGreet(t *testing.T) {\n\tif util.Greet(\"x\") != \"HELLO, X\" {\n\t\tt.Fatal(util.Greet(\"x\"))\n\t}\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "main_test.go"), []byte(testFile), 0o600))
	result := clitest.Run(t.Context(), RunTest, []string{root}, "")
	require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)
	require.Equal(t, []string{"TestGreet"}, *names)
}

func TestPipitTestGoEmbedPointsAtTheScriptDirectory(t *testing.T) {
	captureTestRunner(t)
	root := writeTestPackage(t, map[string]string{
		"calc.go":      "package calc\n\nimport \"embed\"\n\n//go:embed calc.go\nvar self embed.FS\n",
		"calc_test.go": "package calc\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n",
	})
	result := clitest.Run(t.Context(), RunTest, []string{root}, "")
	require.Equal(t, 1, result.Code, "stderr: %q", result.Stderr)
	require.Contains(t, result.Stderr, "//go:embed is not supported")
	require.Contains(t, result.Stderr, "PIPIT_SCRIPT_DIR")
}

func TestPipitTestSkipsFilesTheInterpreterExcludes(t *testing.T) {
	names := captureTestRunner(t)
	root := writeTestPackage(t, map[string]string{
		"calc.go":             "package calc\n\nfunc Add(a, b int) int { return a + b }\n",
		"calc_test.go":        "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"add\")\n\t}\n}\n",
		"native_test.go":      "//go:build !interp\n\npackage calc\n\nimport \"testing\"\n\nfunc TestNative(t *testing.T) { t.Fatal(\"compiled by the interpreter\") }\n",
		"external_test.go":    "//go:build !interp\n\npackage calc_test\n\nimport \"testing\"\n\nfunc TestExternal(t *testing.T) {}\n",
		"interp_only_test.go": "//go:build interp\n\npackage calc\n\nimport \"testing\"\n\nfunc TestInterp(t *testing.T) {}\n",
	})
	result := clitest.Run(t.Context(), RunTest, []string{root}, "")
	require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)
	require.Equal(t, []string{"TestAdd", "TestInterp"}, *names)
}
