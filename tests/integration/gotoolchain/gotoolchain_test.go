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

package gotoolchain_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/sdk/stdlib/typeinfo"
)

type bucket string

const (
	bucketPass        bucket = "pass"
	bucketMismatch    bucket = "mismatch"
	bucketRuntime     bucket = "runtime"
	bucketTimeout     bucket = "timeout"
	bucketCompile     bucket = "compile"
	bucketPanic       bucket = "compiler-panic"
	bucketInternal    bucket = "internal"
	bucketUnsupported bucket = "unsupported"
	bucketSkipped     bucket = "skipped"
)

var bucketOrder = []bucket{
	bucketPanic, bucketInternal, bucketMismatch, bucketRuntime, bucketTimeout,
	bucketCompile, bucketUnsupported, bucketSkipped, bucketPass,
}

const (
	exitPanic    = 2
	exitInternal = 3
	exitTimeout  = 124
)

type runFile struct {
	name      string
	directive runDirective
}

type runDirective struct {
	args []string
	skip string
}

type outcome struct {
	file    string
	detail  string
	bucket  bucket
	elapsed time.Duration
}

type config struct {
	testDir      string
	version      string
	bin          string
	reportPath   string
	expectedPath string
	timeout      time.Duration
	jobs         int
	update       bool
}

func TestGoToolchainRunTests(t *testing.T) {
	if os.Getenv("PIPIT_GOTOOLCHAIN") == "" {
		t.Skip("set PIPIT_GOTOOLCHAIN=1 (make test-gotoolchain) to run the Go toolchain's test directory against pipit")
	}
	config := loadConfig(t)
	files := collectRunFiles(t, config.testDir)
	skips := readSkips(t, filepath.Join("testdata", "skip.txt"))
	registered := typeinfo.TypesPackages()

	var mu sync.Mutex
	results := make([]outcome, 0, len(files))
	semaphore := make(chan struct{}, config.jobs)
	t.Run("files", func(t *testing.T) {
		for _, file := range files {
			t.Run(file.name, func(t *testing.T) {
				t.Parallel()
				semaphore <- struct{}{}
				defer func() { <-semaphore }()
				result := judge(t, config, file, skips, registered)
				mu.Lock()
				results = append(results, result)
				mu.Unlock()
			})
		}
	})
	summarise(t, config, results)
}

func loadConfig(t *testing.T) config {
	t.Helper()
	goroot := os.Getenv("PIPIT_GOROOT")
	if goroot == "" {
		goroot = goEnv(t, "GOROOT")
	}
	testDir := filepath.Join(goroot, "test")
	info, err := os.Stat(testDir)
	require.NoError(t, err, "toolchain test directory")
	require.True(t, info.IsDir(), "%s is not a directory", testDir)
	version := majorMinor(goEnv(t, "GOVERSION"))
	bin := os.Getenv("PIPIT_BIN")
	if bin == "" {
		bin = buildPipit(t)
	}
	return config{
		testDir:      testDir,
		version:      version,
		bin:          bin,
		reportPath:   envOr("PIPIT_GOTOOLCHAIN_REPORT", filepath.Join(os.TempDir(), "pipit-gotoolchain-"+version+".txt")),
		expectedPath: filepath.Join("testdata", "expected", version+".txt"),
		timeout:      envDuration(t, "PIPIT_GOTOOLCHAIN_TIMEOUT", 30*time.Second),
		jobs:         envInt(t, "PIPIT_GOTOOLCHAIN_JOBS", min(max(runtime.GOMAXPROCS(0)/4, 2), 8)),
		update:       os.Getenv("PIPIT_GOTOOLCHAIN_UPDATE") != "",
	}
}

func goEnv(t *testing.T, key string) string {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), "go", "env", key).Output()
	require.NoError(t, err, "go env %s", key)
	return strings.TrimSpace(string(out))
}

func majorMinor(version string) string {
	parts := strings.SplitN(strings.TrimPrefix(version, "go"), ".", 3)
	if len(parts) < 2 {
		return version
	}
	return "go" + parts[0] + "." + parts[1]
}

func buildPipit(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "pipit")
	build := exec.CommandContext(context.Background(), "go", "build", "-o", bin, "pipit.sh/pipit/cmd/pipit")
	build.Dir = repoRoot(t)
	out, err := build.CombinedOutput()
	require.NoError(t, err, "building pipit: %s", out)
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.work")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "go.work not found above %s", dir)
		dir = parent
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envDuration(t *testing.T, key string, fallback time.Duration) time.Duration {
	t.Helper()
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	require.NoError(t, err, "%s", key)
	return value
}

func envInt(t *testing.T, key string, fallback int) int {
	t.Helper()
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	require.NoError(t, err, "%s", key)
	require.Positive(t, value, "%s", key)
	return value
}

func collectRunFiles(t *testing.T, testDir string) []runFile {
	t.Helper()
	var files []runFile
	err := filepath.WalkDir(testDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if strings.HasSuffix(entry.Name(), ".dir") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		directive, ok := parseRunDirectiveFile(path)
		if !ok {
			return nil
		}
		relative, relErr := filepath.Rel(testDir, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, runFile{name: filepath.ToSlash(relative), directive: directive})
		return nil
	})
	require.NoError(t, err)
	slices.SortFunc(files, func(a, b runFile) int { return strings.Compare(a.name, b.name) })
	require.NotEmpty(t, files, "no run-mode tests under %s", testDir)
	return files
}

func parseRunDirectiveFile(path string) (runDirective, bool) {
	file, err := os.Open(path)
	if err != nil {
		return runDirective{args: nil, skip: ""}, false
	}
	defer func() { _ = file.Close() }()
	return parseRunDirective(bufio.NewScanner(file))
}

func parseRunDirective(scanner *bufio.Scanner) (runDirective, bool) {
	none := runDirective{args: nil, skip: ""}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//go:build") || strings.HasPrefix(line, "// +build") {
			continue
		}
		if !strings.HasPrefix(line, "//") {
			return none, false
		}
		fields := strings.Fields(strings.TrimPrefix(line, "//"))
		if len(fields) == 0 || fields[0] != "run" {
			return none, false
		}
		directive := runDirective{args: nil, skip: ""}
		rest := fields[1:]
		for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
			if flag := rest[0]; flag == "-race" || strings.HasPrefix(flag, "-goexperiment") {
				directive.skip = "toolchain flag " + flag
			}
			rest = rest[1:]
		}
		if len(rest) > 0 {
			directive.args = rest
		}
		return directive, true
	}
	return none, false
}

func readSkips(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	skips := map[string]string{}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		file, reason, _ := strings.Cut(line, " ")
		skips[file] = strings.TrimSpace(reason)
	}
	return skips
}

func readExpected(t *testing.T, path string) (map[string]bool, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	require.NoError(t, err)
	expected := map[string]bool{}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			expected[line] = true
		}
	}
	return expected, true
}

func judge(t *testing.T, config config, file runFile, skips map[string]string, registered map[string]*typesPackage) outcome {
	t.Helper()
	if reason, skipped := skips[file.name]; skipped {
		return outcome{file: file.name, detail: reason, bucket: bucketSkipped, elapsed: 0}
	}
	if file.directive.skip != "" {
		return outcome{file: file.name, detail: file.directive.skip, bucket: bucketSkipped, elapsed: 0}
	}
	absolute := filepath.Join(config.testDir, filepath.FromSlash(file.name))
	source, err := os.ReadFile(absolute)
	require.NoError(t, err)
	if missing := unregisteredImport(source, registered); missing != "" {
		return outcome{file: file.name, detail: "import " + missing, bucket: bucketUnsupported, elapsed: 0}
	}
	start := time.Now()
	result := runToolchainFile(t, config, file, absolute)
	result.elapsed = time.Since(start)
	return result
}

type typesPackage = types.Package

func unregisteredImport(source []byte, registered map[string]*typesPackage) string {
	parsed, err := parser.ParseFile(token.NewFileSet(), "main.go", source, parser.ImportsOnly)
	if err != nil {
		return ""
	}
	for _, spec := range parsed.Imports {
		path, unquoteErr := strconv.Unquote(spec.Path.Value)
		if unquoteErr != nil {
			continue
		}
		if _, ok := registered[path]; !ok {
			return path
		}
	}
	return ""
}

func runToolchainFile(t *testing.T, config config, file runFile, absolute string) outcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), config.timeout+5*time.Second)
	defer cancel()

	args := []string{"run", "-timeout", config.timeout.String(), absolute}
	if len(file.directive.args) > 0 {
		args = append(append(args, "--"), file.directive.args...)
	}
	cmd := exec.CommandContext(ctx, config.bin, args...)
	cmd.Dir = filepath.Dir(absolute)

	cmd.Env = append(os.Environ(), "GOROOT="+filepath.Dir(config.testDir))
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined
	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return outcome{file: file.name, detail: "no result within " + config.timeout.String(), bucket: bucketTimeout, elapsed: 0}
	}
	if runErr != nil {
		return classifyFailure(file.name, combined.String(), runErr)
	}
	expected := expectedOutput(t, absolute)
	actual := stripDiagnostics(strings.ReplaceAll(combined.String(), "\r\n", "\n"))
	if actual != expected {
		return outcome{file: file.name, detail: describeMismatch(expected, actual), bucket: bucketMismatch, elapsed: 0}
	}
	return outcome{file: file.name, detail: "", bucket: bucketPass, elapsed: 0}
}

const (
	textParse         = "parse failed"
	textTypeCheck     = "type check failed"
	textCompilation   = "compilation failed"
	textCompilePanic  = "internal Compiler panic"
	textNotAllowed    = "language feature not allowed"
	textNotRegistered = "package not registered with interpreter"
	textCancelled     = "execution cancelled"
	textDeadline      = "context deadline exceeded"
	compilePackage    = "pipit.sh/pipit/internal/compile"
)

var internalMarkers = []string{"interp:", "interpreter invariant violated", "undefined method", "zero reflect.Value", "internal error"}

var failurePrefixes = []string{"pipit:", "panic:", "fatal error:", "goroutine panicked"}

func classifyFailure(file, output string, runErr error) outcome {
	text := strings.TrimSpace(stripDiagnostics(strings.ReplaceAll(output, "\r\n", "\n")))
	detail := failureLine(text)
	if detail == "" {
		detail = runErr.Error()
	}
	code := exitCode(runErr)
	crashed := strings.Contains(text, "fatal error:") || strings.Contains(text, "panic:") ||
		strings.Contains(text, textCompilePanic)
	switch {
	case crashed && strings.Contains(text, compilePackage):
		return outcome{file: file, detail: crashDetail(text), bucket: bucketPanic, elapsed: 0}
	case code == exitInternal || containsAny(text, internalMarkers):
		return outcome{file: file, detail: detail, bucket: bucketInternal, elapsed: 0}
	case code == exitTimeout, strings.Contains(detail, textCancelled), strings.Contains(detail, textDeadline):
		return outcome{file: file, detail: detail, bucket: bucketTimeout, elapsed: 0}
	case strings.Contains(detail, textNotRegistered), strings.Contains(detail, textNotAllowed),
		strings.Contains(detail, textTypeCheck), strings.Contains(detail, textParse):
		return outcome{file: file, detail: detail, bucket: bucketUnsupported, elapsed: 0}
	case strings.Contains(detail, textCompilation):
		return outcome{file: file, detail: detail, bucket: bucketCompile, elapsed: 0}
	default:
		return outcome{file: file, detail: detail, bucket: bucketRuntime, elapsed: 0}
	}
}

func exitCode(runErr error) int {
	if exitErr, ok := errors.AsType[*exec.ExitError](runErr); ok {
		return exitErr.ExitCode()
	}
	return -1
}

func containsAny(text string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func failureLine(text string) string {
	for line := range strings.Lines(text) {
		trimmed := strings.TrimSpace(line)
		for _, prefix := range failurePrefixes {
			if strings.HasPrefix(trimmed, prefix) {
				return trimmed
			}
		}
	}
	return firstLine(text)
}

func crashDetail(text string) string {
	summary := firstLine(text)
	for line := range strings.Lines(text) {
		if strings.Contains(line, compilePackage) {
			return summary + " at " + strings.TrimSpace(line)
		}
	}
	return summary
}

var diagnosticLine = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} (WARN|INFO|ERROR|DEBUG) `)

func stripDiagnostics(output string) string {
	if !strings.Contains(output, "WARN ") && !strings.Contains(output, "ERROR ") &&
		!strings.Contains(output, "INFO ") && !strings.Contains(output, "DEBUG ") {
		return output
	}
	var kept strings.Builder
	for line := range strings.Lines(output) {
		if diagnosticLine.MatchString(line) {
			continue
		}
		kept.WriteString(line)
	}
	return kept.String()
}

func expectedOutput(t *testing.T, absolute string) string {
	t.Helper()
	data, err := os.ReadFile(strings.TrimSuffix(absolute, ".go") + ".out")
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	require.NoError(t, err)
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func describeMismatch(expected, actual string) string {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")
	for i := range max(len(expectedLines), len(actualLines)) {
		var want, got string
		if i < len(expectedLines) {
			want = expectedLines[i]
		}
		if i < len(actualLines) {
			got = actualLines[i]
		}
		if want != got {
			return fmt.Sprintf("line %d: want %q, got %q", i+1, truncate(want), truncate(got))
		}
	}
	return "outputs differ"
}

func truncate(s string) string {
	const limit = 80
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func summarise(t *testing.T, config config, results []outcome) {
	t.Helper()
	slices.SortFunc(results, func(a, b outcome) int {
		if a.bucket != b.bucket {
			return slices.Index(bucketOrder, a.bucket) - slices.Index(bucketOrder, b.bucket)
		}
		return strings.Compare(a.file, b.file)
	})
	counts := map[bucket]int{}
	passing := map[string]bool{}
	for _, result := range results {
		counts[result.bucket]++
		if result.bucket == bucketPass {
			passing[result.file] = true
		}
	}
	writeReport(t, config, results, counts)
	summary := fmt.Sprintf("gotoolchain %s: %d files:", config.version, len(results))
	for _, b := range bucketOrder {
		summary += fmt.Sprintf(" %s %d", b, counts[b])
	}
	summary += fmt.Sprintf(" pool-exhausted %d", countDetails(results, poolExhaustedMarker))
	t.Logf("%s\nreport: %s", summary, config.reportPath)

	if config.update {
		writeExpected(t, config, passing)
		return
	}
	expected, recorded := readExpected(t, config.expectedPath)
	if !recorded {
		t.Logf("no expectations recorded for %s; run `make test-gotoolchain-update` to record %d passing files at %s",
			config.version, len(passing), config.expectedPath)
		return
	}
	compareExpected(t, results, expected, passing)
}

func compareExpected(t *testing.T, results []outcome, expected, passing map[string]bool) {
	t.Helper()
	for _, result := range results {
		if expected[result.file] && result.bucket != bucketPass {
			t.Errorf("regression: %s was recorded as passing, now %s: %s", result.file, result.bucket, result.detail)
		}
	}
	var fresh []string
	for file := range passing {
		if !expected[file] {
			fresh = append(fresh, file)
		}
	}
	if len(fresh) > 0 {
		slices.Sort(fresh)
		t.Logf("%d files pass that are not recorded (run `make test-gotoolchain-update` to record them):\n  %s",
			len(fresh), strings.Join(fresh, "\n  "))
	}
}

func writeReport(t *testing.T, config config, results []outcome, counts map[bucket]int) {
	t.Helper()
	var builder strings.Builder
	fmt.Fprintf(&builder, "# pipit against $GOROOT/test, %s: %d files\n", config.version, len(results))
	for _, b := range bucketOrder {
		fmt.Fprintf(&builder, "# %-14s %d\n", b, counts[b])
	}
	builder.WriteString("#\n# bucket\tfile\telapsed\tdetail\n")
	for _, result := range results {
		fmt.Fprintf(&builder, "%s\t%s\t%s\t%s\n", result.bucket, result.file, result.elapsed.Round(time.Millisecond), result.detail)
	}
	require.NoError(t, os.WriteFile(config.reportPath, []byte(builder.String()), 0o644))
}

func writeExpected(t *testing.T, config config, passing map[string]bool) {
	t.Helper()
	files := make([]string, 0, len(passing))
	for file := range passing {
		files = append(files, file)
	}
	slices.Sort(files)
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Files under $GOROOT/test that pass on %s. Recorded by `make test-gotoolchain-update`;\n", config.version)
	builder.WriteString("# a listed file that stops passing fails `make test-gotoolchain`.\n")
	for _, file := range files {
		builder.WriteString(file + "\n")
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(config.expectedPath), 0o755))
	require.NoError(t, os.WriteFile(config.expectedPath, []byte(builder.String()), 0o644))
	t.Logf("recorded %d passing files at %s", len(files), config.expectedPath)
}

const poolExhaustedMarker = "named-scalar pool exhausted"

func countDetails(results []outcome, marker string) int {
	count := 0
	for _, result := range results {
		if strings.Contains(result.detail, marker) {
			count++
		}
	}
	return count
}
