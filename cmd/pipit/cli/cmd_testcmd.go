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
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/modloader"
	"pipit.sh/pipit/cmd/pipit/internal/output"
	"pipit.sh/pipit/cmd/pipit/internal/runner"
)

// testFunctionPrefix is the name prefix that marks a test function, as in go test.
const testFunctionPrefix = "Test"

// runTestMain hands the discovered tests to the testing package. Tests replace this with
// a runner that returns instead of exiting.
var runTestMain = func(matchString func(pat, str string) (bool, error), tests []testing.InternalTest) int {
	if !flag.Parsed() {
		_ = flag.CommandLine.Parse(nil)
	}
	testing.Main(matchString, tests, nil, nil)
	return 0
}

// errExternalTestPackage reports a package x_test file, which pipit test does not run.
var errExternalTestPackage = errors.New("external test packages (package x_test) are not supported; put the tests in the package they test")

// testSuite is a compiled package together with the names of its test functions.
type testSuite struct {
	// compiled is the package, test files included.
	compiled *pipit.CompiledFileSet

	// tests lists the test function names in file-name then source order.
	tests []string
}

// internalTests wraps every test function as a testing.InternalTest whose body calls the
// compiled function with the host's *testing.T, so the script's t.Errorf, t.Fatal, t.Run
// and friends act on the real test.
//
// Takes interpreter (*pipit.Interpreter) which runs the functions.
//
// Returns []testing.InternalTest which lists the tests in discovery order.
func (suite *testSuite) internalTests(ctx context.Context, interpreter *pipit.Interpreter) []testing.InternalTest {
	tests := make([]testing.InternalTest, 0, len(suite.tests))
	for _, name := range suite.tests {
		tests = append(tests, testing.InternalTest{Name: name, F: func(t *testing.T) {
			if _, err := interpreter.CallFunction(ctx, suite.compiled, name, t); err != nil {
				t.Fatalf("%v", err)
			}
		}})
	}
	return tests
}

// RunTest handles `pipit test [flags] <file|directory>`. It compiles the package with its
// _test.go files and runs them through the host's testing package.
//
// Takes args ([]string) which holds the flags and target.
// Takes streams (output.IO) which supplies the diagnostic writers.
//
// Returns int which is the exit code when the suite could not be started.
func RunTest(ctx context.Context, args []string, streams output.IO) int {
	flags := flag.NewFlagSet("pipit test", flag.ContinueOnError)
	flags.SetOutput(streams.Stderr)
	options := registerRunFlags(flags)
	runPattern := flags.String("run", "", "Run only tests whose name matches this regular expression")
	verbose := flags.Bool("v", false, "Log every test as it runs")
	short := flags.Bool("short", false, "Tell long-running tests to shorten their run (testing.Short)")
	flags.Usage = func() {
		fmt.Fprintln(streams.Stderr, "Usage: pipit test [flags] <file|directory>")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return 1
	}
	target := flags.Arg(0)

	limits, err := runLimits(ctx, options)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	cache, err := modloader.ParseCacheSpec(*options.cacheSpec)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	modules := moduleRunOptions{
		Snapshot:     nil,
		Stderr:       streams.Stderr,
		CacheSpec:    cache,
		GoproxyURL:   *options.goproxyURL,
		AllowNetwork: *options.allowNetwork,
	}
	interpreter := runner.New(limits, pipit.WithArgs([]string{target}), pipit.WithEnv(buildScriptEnv(target, nil)))

	suite, err := loadTestSuite(ctx, interpreter, target, modules)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	if err := interpreter.ExecuteInits(ctx, suite.compiled); err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	restore, err := configureTestingFlags(*runPattern, *verbose, *short)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	defer restore()
	return runTestMain(matchTestName, suite.internalTests(ctx, interpreter))
}

// loadTestSuite reads, checks and compiles the target's package with its test files.
//
// Takes interpreter (*pipit.Interpreter) which compiles the package.
// Takes target (string) which is the source file or directory.
// Takes modules (moduleRunOptions) which carries the module-fetch options.
//
// Returns *testSuite which is ready to run.
// Returns error when the sources cannot be read, a test has the wrong signature, an
// external test package is present, or compilation fails.
func loadTestSuite(ctx context.Context, interpreter *pipit.Interpreter, target string, modules moduleRunOptions) (*testSuite, error) {
	directory, sources, err := collectTestSources(target)
	if err != nil {
		return nil, err
	}
	tests, err := discoverTestFunctions(sources)
	if err != nil {
		return nil, err
	}
	compiled, err := compileTestPackage(ctx, interpreter, directory, sources, modules)
	if err != nil {
		return nil, err
	}
	return &testSuite{compiled: compiled, tests: tests}, nil
}

// collectTestSources reads the target package including its _test.go files. External test
// packages (package x_test) are refused.
//
// Takes target (string) which is the file or directory path.
//
// Returns string which is the package directory.
// Returns map[string]string which maps file name to source.
// Returns error when the target cannot be read or holds an external test package.
func collectTestSources(target string) (string, map[string]string, error) {
	directory, names, err := listTestFiles(target)
	if err != nil {
		return "", nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", nil, fmt.Errorf("cannot open %q: %w", directory, err)
	}
	defer root.Close()
	sources := make(map[string]string, len(names))
	for _, name := range names {
		body, err := root.ReadFile(name)
		if err != nil {
			return "", nil, fmt.Errorf("cannot read %q: %w", name, err)
		}
		if err := rejectExternalTestPackage(name, body); err != nil {
			return "", nil, err
		}
		sources[name] = string(body)
	}
	return directory, sources, nil
}

// listTestFiles names the Go files a target contributes: the file itself, or every .go
// file directly inside the directory.
//
// Takes target (string) which is the file or directory path.
//
// Returns string which is the package directory.
// Returns []string which lists the file names.
// Returns error when the target cannot be read or holds no Go files.
func listTestFiles(target string) (string, []string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return "", nil, fmt.Errorf("cannot stat %q: %w", target, err)
	}
	if !info.IsDir() {
		return filepath.Dir(target), []string{filepath.Base(target)}, nil
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return "", nil, fmt.Errorf("cannot read directory %q: %w", target, err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return "", nil, fmt.Errorf("no .go files in %q", target)
	}
	return target, names, nil
}

// rejectExternalTestPackage parses a file's package clause and refuses the _test suffix.
//
// Takes name (string) which is the file name for diagnostics.
// Takes body ([]byte) which is the file's source.
//
// Returns error when the package name ends in _test or the clause does not parse.
func rejectExternalTestPackage(name string, body []byte) error {
	file, err := parser.ParseFile(token.NewFileSet(), name, body, parser.PackageClauseOnly)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if strings.HasSuffix(file.Name.Name, "_test") {
		return fmt.Errorf("%s: %w", name, errExternalTestPackage)
	}
	return nil
}

// discoverTestFunctions finds the top-level Test functions of the package, in file-name
// then source order, and checks each has go test's signature.
//
// Takes sources (map[string]string) which maps file name to source.
//
// Returns []string which lists the test function names.
// Returns error when a Test function does not take exactly one *testing.T.
func discoverTestFunctions(sources map[string]string) ([]string, error) {
	var tests []string
	for _, name := range sortedFileNames(sources) {
		if !strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, sources[name], parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Recv != nil || !isTestName(function.Name.Name) {
				continue
			}
			if !takesTestingT(function.Type) {
				return nil, fmt.Errorf("%s: wrong signature for %s, must be: func %s(t *testing.T)", name, function.Name.Name, function.Name.Name)
			}
			tests = append(tests, function.Name.Name)
		}
	}
	return tests, nil
}

// sortedFileNames returns the map's keys in ascending order.
//
// Takes sources (map[string]string) which maps file name to source.
//
// Returns []string which holds the sorted names.
func sortedFileNames(sources map[string]string) []string {
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// isTestName applies go test's rule: the name starts with Test and the next rune, if any,
// is not a lower-case letter, so TestX and Test_x count but Testify does not. TestMain is
// the package's optional entry hook, not a test.
//
// Takes name (string) which is the function name.
//
// Returns bool which is true for a test function name.
func isTestName(name string) bool {
	if name == "TestMain" || !strings.HasPrefix(name, testFunctionPrefix) {
		return false
	}
	if len(name) == len(testFunctionPrefix) {
		return true
	}
	next, _ := utf8.DecodeRuneInString(name[len(testFunctionPrefix):])
	return !unicode.IsLower(next)
}

// takesTestingT reports whether a function type is func(*testing.T) with no results.
//
// Takes functionType (*ast.FuncType) which is the declared signature.
//
// Returns bool which is true for go test's test signature.
func takesTestingT(functionType *ast.FuncType) bool {
	if functionType.Results != nil && len(functionType.Results.List) > 0 {
		return false
	}
	if functionType.Params == nil || len(functionType.Params.List) != 1 || len(functionType.Params.List[0].Names) > 1 {
		return false
	}
	star, ok := functionType.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	selector, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "testing" && selector.Sel.Name == "T"
}

// compileTestPackage compiles the package, resolving local and remote modules first when
// any file imports one.
//
// Takes interpreter (*pipit.Interpreter) which compiles the package.
// Takes directory (string) which locates the nearest go.mod.
// Takes sources (map[string]string) which maps file name to source.
// Takes modules (moduleRunOptions) which carries the module-fetch options.
//
// Returns *pipit.CompiledFileSet which is the compiled package.
// Returns error when resolution or compilation fails.
func compileTestPackage(ctx context.Context, interpreter *pipit.Interpreter, directory string, sources map[string]string, modules moduleRunOptions) (*pipit.CompiledFileSet, error) {
	needsResolution, err := scriptNeedsResolution(directory, sources, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing imports: %w", err)
	}
	if !needsResolution {
		return interpreter.CompileFileSet(ctx, sources)
	}
	options, cacheRoot := resolverOptions(filepath.Join(directory, "package"), modules)
	resolution, err := modloader.ResolveDirectory(ctx, directory, sources, options)
	if err != nil {
		return nil, err
	}
	return compileResolution(ctx, interpreter, resolution, cacheRoot, modules)
}

// configureTestingFlags registers the testing flags and sets the ones pipit test exposes,
// returning a function that restores their previous values.
//
// Takes pattern (string) which is the -run regular expression, empty for every test.
// Takes verbose (bool) which selects -v.
// Takes short (bool) which selects -short.
//
// Returns func() which restores the flags.
// Returns error when a flag value is rejected.
func configureTestingFlags(pattern string, verbose, short bool) (func(), error) {
	testing.Init()
	previous := make(map[string]string)
	set := func(name, value string) error {
		definition := flag.Lookup(name)
		if definition == nil {
			return fmt.Errorf("testing flag %q is not registered", name)
		}
		previous[name] = definition.Value.String()
		return flag.Set(name, value)
	}
	if err := errors.Join(
		set("test.run", pattern),
		set("test.v", strconv.FormatBool(verbose)),
		set("test.short", strconv.FormatBool(short)),
	); err != nil {
		return nil, err
	}
	return func() {
		for name, value := range previous {
			_ = flag.Set(name, value)
		}
	}, nil
}

// matchTestName is the pattern matcher the testing package applies to -run.
//
// Takes pattern (string) which is a regular expression.
// Takes name (string) which is the test or subtest name element.
//
// Returns bool which is true when the name matches.
// Returns error when the pattern does not compile.
func matchTestName(pattern, name string) (bool, error) {
	expression, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	return expression.MatchString(name), nil
}
