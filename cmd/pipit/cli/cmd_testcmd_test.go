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
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
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
	result := clitest.Run(context.Background(), RunTest, []string{root}, "")
	if result.Code != 0 {
		t.Fatalf("exit code = %d (stderr=%q)", result.Code, result.Stderr)
	}
	if strings.Join(*names, ",") != "TestAdd,TestSubtests,TestReport" {
		t.Fatalf("discovered %v", *names)
	}
	written, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("the suite did not write its report: %v", err)
	}
	if got := string(written); got != "TestAdd\nTestSubtests/one\nTestSubtests/two\nTestReport" {
		t.Fatalf("report = %q", got)
	}
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
	if result := clitest.Run(context.Background(), RunTest, []string{root}, ""); result.Code != 1 {
		t.Fatalf("a failing suite must exit 1, got %d (stderr=%q)", result.Code, result.Stderr)
	}
	if result := clitest.Run(context.Background(), RunTest, []string{"-run", "TestPasses$", root}, ""); result.Code != 0 {
		t.Fatalf("-run must select only the passing test, got %d (stderr=%q)", result.Code, result.Stderr)
	}
	if result := clitest.Run(context.Background(), RunTest, []string{"-run", "TestPanics", root}, ""); result.Code != 1 {
		t.Fatalf("an interpreted panic must fail the test, got %d", result.Code)
	}
}

func TestPipitTestRefusesExternalTestPackagesAndBadSignatures(t *testing.T) {
	names := captureTestRunner(t)
	external := writeTestPackage(t, map[string]string{
		"calc.go":      "package calc\n",
		"calc_test.go": "package calc_test\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n",
	})
	result := clitest.Run(context.Background(), RunTest, []string{external}, "")
	if result.Code != 1 || !strings.Contains(result.Stderr, "external test packages") {
		t.Fatalf("external package: code=%d stderr=%q", result.Code, result.Stderr)
	}
	signature := writeTestPackage(t, map[string]string{
		"calc.go":      "package calc\n",
		"calc_test.go": "package calc\n\nfunc TestX() {}\n",
	})
	result = clitest.Run(context.Background(), RunTest, []string{signature}, "")
	if result.Code != 1 || !strings.Contains(result.Stderr, "wrong signature for TestX") {
		t.Fatalf("signature: code=%d stderr=%q", result.Code, result.Stderr)
	}
	if len(*names) != 0 {
		t.Fatalf("no suite should have run, got %v", *names)
	}
}

func TestPipitTestResolvesLocalModulePackages(t *testing.T) {
	names := captureTestRunner(t)
	root := writeLocalModule(t)
	testFile := "package main\n\nimport (\n\t\"testing\"\n\n\t\"example.com/app/internal/util\"\n)\n\nfunc TestGreet(t *testing.T) {\n\tif util.Greet(\"x\") != \"HELLO, X\" {\n\t\tt.Fatal(util.Greet(\"x\"))\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte(testFile), 0o600); err != nil {
		t.Fatal(err)
	}
	result := clitest.Run(context.Background(), RunTest, []string{root}, "")
	if result.Code != 0 {
		t.Fatalf("exit code = %d (stderr=%q)", result.Code, result.Stderr)
	}
	if strings.Join(*names, ",") != "TestGreet" {
		t.Fatalf("discovered %v", *names)
	}
}
