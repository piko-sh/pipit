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

//go:build integration

package snippets_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/sdk/stdlib"
)

type snippetSpec struct {
	KnownBug       string `json:"knownBug,omitempty"`
	RequiresUnsafe bool   `json:"requiresUnsafe,omitempty"`
}

var (
	sharedGoCache  string
	goRunSemaphore chan struct{}
)

func resolveGoRunParallelism() int {
	if raw := os.Getenv("PIPIT_PARITY_GORUN_PARALLELISM"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			return parsed
		}
	}
	return min(max(runtime.GOMAXPROCS(0)/4, 2), 8)
}

func TestParity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping parity tests in short mode")
	}

	testdataDir := filepath.Join("testdata")
	entries, err := os.ReadDir(testdataDir)
	require.NoError(t, err, "reading testdata directory")

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		directory := filepath.Join(testdataDir, name)

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			evalPath := filepath.Join(directory, "eval.go")
			requireFileExists(t, evalPath)

			specPath := filepath.Join(directory, "testspec.json")
			if data, err := os.ReadFile(specPath); err == nil {
				var spec snippetSpec
				require.NoError(t, json.Unmarshal(data, &spec), "parsing %s", specPath)
				if spec.KnownBug != "" {
					t.Skipf("known bug: %s", spec.KnownBug)
				}
				if spec.RequiresUnsafe && !unsafeLaneAvailable {
					t.Skip("requires the pointer-reinterpreting lane")
				}
			}

			snippet := readFile(t, evalPath)

			expected := parityExpectedOutput(t, name, snippet)

			for _, mode := range dispatchModes() {
				t.Run(mode.name, func(t *testing.T) {
					service := app.NewService(mode.options...)
					service.UseSymbolProviders(stdlib.Providers()...)
					result, evalErr := service.EvalFile(context.Background(), snippet, "run")
					require.NoError(t, evalErr, "EvalFile failed for %s in %s mode", name, mode.name)

					actual := strings.TrimSpace(fmt.Sprint(result))

					require.Equal(t, expected, actual,
						"parity mismatch for %s in %s mode\nsnippet:\n%s\ngo run: %q\neval:   %q",
						name, mode.name, snippet, expected, actual)
				})
			}
		})
	}
}

func parityExpectedOutput(t *testing.T, name, snippet string) string {
	t.Helper()
	dir := os.Getenv("PIPIT_SNIPPETS_EXPECTED_DIR")
	if dir == "" {
		return strings.TrimSpace(runGoSource(t, buildParityProgram(snippet)))
	}

	path := filepath.Join(dir, name+"-"+oracleCacheKey(snippet)+".txt")
	if data, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(data))
	}
	expected := strings.TrimSpace(runGoSource(t, buildParityProgram(snippet)))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(path, []byte(expected), 0o644))
	return expected
}

func oracleCacheKey(snippet string) string {
	sum := sha256.Sum256([]byte(runtime.Version() + "\x00" + snippet))
	return hex.EncodeToString(sum[:8])
}

func buildParityProgram(snippet string) string {

	lines := strings.SplitN(snippet, "\n", 2)
	var builder strings.Builder
	builder.WriteString(lines[0])
	if !snippetImportsFmt(snippet) {
		builder.WriteString("\n\nimport \"fmt\"\n")
	} else {
		builder.WriteString("\n")
	}
	if len(lines) > 1 {
		builder.WriteString(lines[1])
	}
	builder.WriteString("\nfunc main() {\n\tfmt.Println(run())\n}\n")
	return builder.String()
}

func snippetImportsFmt(snippet string) bool {
	file, err := parser.ParseFile(token.NewFileSet(), "snippet.go", snippet, parser.ImportsOnly)
	if err != nil {
		return false
	}
	for _, importSpec := range file.Imports {
		if importSpec.Path == nil || importSpec.Path.Value != `"fmt"` {
			continue
		}
		if importSpec.Name == nil || importSpec.Name.Name == "fmt" {
			return true
		}
	}
	return false
}

func runGoSource(t *testing.T, source string) string {
	t.Helper()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "main.go")
	goTmpDir := filepath.Join(tmpDir, "tmp")
	require.NoError(t, os.MkdirAll(goTmpDir, 0o755))

	err := os.WriteFile(tmpFile, []byte(source), 0o644)
	require.NoError(t, err)

	goModSource := "module snippetoracle\n\ngo " +
		strings.TrimPrefix(engine.InterpretedGoVersion, "go") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModSource), 0o644))

	ctx, cancel := context.WithTimeoutCause(
		context.Background(),
		30*time.Second,
		fmt.Errorf("parity: go run timed out"),
	)
	defer cancel()

	goRunSemaphore <- struct{}{}
	defer func() { <-goRunSemaphore }()

	command := exec.CommandContext(ctx, "go", "run", tmpFile)
	command.Dir = tmpDir
	command.Env = append(oracleEnviron(), "GOCACHE="+sharedGoCache, "GOTMPDIR="+goTmpDir, "GOWORK=off")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = 5 * time.Second

	out, err := command.CombinedOutput()
	require.NoError(t, err, "go run failed:\nsource:\n%s\noutput:\n%s", source, string(out))

	return string(out)
}

var oracleCleanedEnvVars = []string{"GOGC=", "GODEBUG="}

func oracleEnviron() []string {
	environ := os.Environ()
	cleaned := make([]string, 0, len(environ))

	for _, entry := range environ {
		if slices.ContainsFunc(oracleCleanedEnvVars, func(prefix string) bool {
			return strings.HasPrefix(entry, prefix)
		}) {
			continue
		}

		cleaned = append(cleaned, entry)
	}

	return cleaned
}

func requireFileExists(t *testing.T, path string) {
	t.Helper()

	_, err := os.Stat(path)
	require.NoError(t, err, "expected file %s to exist", path)
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	return strings.TrimSpace(string(data))
}

func TestEvalParity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping eval-parity tests in short mode")
	}

	testdataDir := filepath.Join("testdata_eval")
	entries, err := os.ReadDir(testdataDir)
	if err != nil {
		t.Skipf("no eval-parity fixtures: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		directory := filepath.Join(testdataDir, name)

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			evalPath := filepath.Join(directory, "body.txt")
			expectedPath := filepath.Join(directory, "expected.txt")
			requireFileExists(t, evalPath)
			requireFileExists(t, expectedPath)

			snippet := readFile(t, evalPath)
			expected := readFile(t, expectedPath)

			service := app.NewService()
			service.UseSymbolProviders(stdlib.Providers()...)
			result, evalErr := service.Eval(context.Background(), snippet)
			require.NoError(t, evalErr, "Service.Eval failed for %s", name)

			actual := fmt.Sprint(result)
			require.Equal(t, expected, actual,
				"eval-parity mismatch for %s\nsnippet:\n%s\nwant: %q\ngot:  %q",
				name, snippet, expected, actual)
		})
	}
}
