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
//
// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package pipit_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
)

var errOutsideAllowedDirectory = errors.New("path outside the allowed directory")

type beneathHook struct {
	pipit.PermissiveCapabilityHook
	allowed string
}

func (hook beneathHook) beneath(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if absolute == hook.allowed || strings.HasPrefix(absolute, hook.allowed+string(filepath.Separator)) {
		return nil
	}
	return fmt.Errorf("%w: %s", errOutsideAllowedDirectory, path)
}

func (hook beneathHook) CheckFileOpen(_ context.Context, _, path string, _ int, _ os.FileMode) error {
	return hook.beneath(path)
}

func (hook beneathHook) CheckFileWrite(_ context.Context, _, path string) error {
	return hook.beneath(path)
}

func hostFilesystemSymbols() pipit.SymbolExports {
	return pipit.SymbolExports{"os": {
		"Chdir":     reflect.ValueOf(os.Chdir),
		"Chtimes":   reflect.ValueOf(os.Chtimes),
		"Link":      reflect.ValueOf(os.Link),
		"MkdirTemp": reflect.ValueOf(os.MkdirTemp),
		"Readlink":  reflect.ValueOf(os.Readlink),
		"Rename":    reflect.ValueOf(os.Rename),
		"Symlink":   reflect.ValueOf(os.Symlink),
		"WriteFile": reflect.ValueOf(os.WriteFile),
	}, "time": {
		"Unix": reflect.ValueOf(time.Unix),
		"Time": reflect.ValueOf((*time.Time)(nil)),
	}}
}

const confinedFilesystemScript = `package main

import (
	"os"
	"time"
)

func writeInside(allowed string) error { return os.WriteFile(allowed+"/inside.txt", []byte("inside"), 0o600) }

func renameOut(allowed, outside string) error {
	if err := os.WriteFile(allowed+"/moved.txt", []byte("moved"), 0o600); err != nil {
		return err
	}
	return os.Rename(allowed+"/moved.txt", outside+"/moved.txt")
}

func symlinkOut(allowed, outside string) error { return os.Symlink(allowed+"/inside.txt", outside+"/planted") }

func symlinkInToOutside(allowed, outside string) error { return os.Symlink(outside+"/secret.txt", allowed+"/link") }

func linkOut(allowed, outside string) error { return os.Link(outside+"/secret.txt", allowed+"/hard") }

func mkdirTempOut(allowed, outside string) error {
	_, err := os.MkdirTemp(outside, "made-*")
	return err
}

func chtimesOut(allowed, outside string) error {
	old := time.Unix(978307200, 0)
	return os.Chtimes(outside+"/secret.txt", old, old)
}

func readlinkOut(allowed, outside string) error {
	_, err := os.Readlink(outside + "/link")
	return err
}

func chdirOut(allowed, outside string) error { return os.Chdir(outside + "/missing") }
`

func TestWithCapabilityHookConfinesEveryPathOfAFileCall(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	outside := filepath.Join(base, "outside")
	require.NoError(t, os.MkdirAll(allowed, 0o700))
	require.NoError(t, os.MkdirAll(outside, 0o700))
	secret := filepath.Join(outside, "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("outside-secret"), 0o600))
	require.NoError(t, os.Symlink("/etc/hostname", filepath.Join(outside, "link")))
	before, err := os.Stat(secret)
	require.NoError(t, err)

	interpreter := pipit.NewInterpreterWithSymbols(hostFilesystemSymbols(), pipit.WithCapabilityHook(beneathHook{allowed: allowed}))
	t.Cleanup(func() { _ = interpreter.Close() })
	compiled, err := interpreter.CompileFileSet(t.Context(), map[string]string{"main.go": confinedFilesystemScript})
	require.NoError(t, err)

	_, err = interpreter.CallFunction(t.Context(), compiled, "writeInside", allowed)
	require.NoError(t, err, "a write inside the allowed directory is permitted")

	for _, name := range []string{"renameOut", "symlinkOut", "symlinkInToOutside", "linkOut", "mkdirTempOut", "chtimesOut", "readlinkOut", "chdirOut"} {
		result, err := interpreter.CallFunction(t.Context(), compiled, name, allowed, outside)
		if err == nil {
			returned, isError := result.(error)
			require.True(t, isError, "%s returned %v instead of a refusal", name, result)
			err = returned
		}
		require.ErrorIs(t, err, errOutsideAllowedDirectory, "%s reached outside the allowed directory", name)
	}

	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	require.ElementsMatch(t, []string{"secret.txt", "link"}, names, "nothing was created outside")
	after, err := os.Stat(secret)
	require.NoError(t, err)
	require.Equal(t, before.ModTime(), after.ModTime(), "the outside file keeps its times")
	_, err = os.Lstat(filepath.Join(allowed, "link"))
	require.ErrorIs(t, err, os.ErrNotExist, "no link to an outside file was planted")
}

func TestLimitOptionsStopRunawayPrograms(t *testing.T) {
	t.Parallel()
	nested := strings.Repeat("x + (", 64) + "x" + strings.Repeat(")", 64)
	tests := []struct {
		name    string
		option  pipit.Option
		body    string
		bounded bool
	}{
		{name: "execution time", option: pipit.WithMaxExecutionTime(50 * time.Millisecond), body: "x := 0\n\tfor {\n\t\tx++\n\t}"},
		{name: "cost budget", option: pipit.WithCostBudget(1000), body: "x := 0\n\tfor i := 0; i < 1000000; i++ {\n\t\tx += i\n\t}\n\treturn x", bounded: true},
		{name: "call depth", option: pipit.WithMaxCallDepth(16), body: "return depth(0)"},
		{name: "source size", option: pipit.WithMaxSourceSize(32), body: "return 1", bounded: true},
		{name: "allocation size", option: pipit.WithMaxAllocSize(64), body: "s := make([]int, 100000)\n\treturn len(s)", bounded: true},
		{name: "literal elements", option: pipit.WithMaxLiteralElements(2), body: "s := []int{1, 2, 3, 4}\n\treturn len(s)", bounded: true},
		{name: "expression depth", option: pipit.WithMaxExpressionDepth(8), body: "x := 1\n\treturn " + nested, bounded: true},
		{name: "string size", option: pipit.WithMaxStringSize(16), body: "s := \"\"\n\tfor i := 0; i < 64; i++ {\n\t\ts += \"x\"\n\t}\n\treturn len(s)", bounded: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := map[string]string{"main.go": "package main\n\nfunc depth(n int) int { return depth(n+1) + 1 }\n\nfunc run() int {\n\t" + tt.body + "\n}\n"}
			limited := pipit.NewInterpreter(tt.option, pipit.WithMaxExecutionTime(5*time.Second))
			t.Cleanup(func() { _ = limited.Close() })
			_, err := runEntrypoint(t, limited, source)
			require.Error(t, err, "the limit stops the program")

			if tt.bounded {
				unlimited := pipit.NewInterpreter(pipit.WithMaxExecutionTime(5 * time.Second))
				t.Cleanup(func() { _ = unlimited.Close() })
				_, err = runEntrypoint(t, unlimited, source)
				require.NoError(t, err, "the program itself is valid")
			}
		})
	}
}

func runEntrypoint(t *testing.T, interpreter *pipit.Interpreter, source map[string]string) (any, error) {
	t.Helper()
	compiled, err := interpreter.CompileFileSet(t.Context(), source)
	if err != nil {
		return nil, err
	}
	return interpreter.ExecuteEntrypoint(t.Context(), compiled, "run")
}

func TestExecutionModeOptionsKeepResults(t *testing.T) {
	t.Parallel()
	const program = `
type pair struct{ a, b int }
total := 0
values := map[string]pair{}
for i := 0; i < 50; i++ {
	values[string(rune('a'+i%26))] = pair{i, i * 2}
}
for _, value := range values {
	total += value.a + value.b
}
total`
	baseline, err := pipit.NewInterpreter().Eval(t.Context(), program)
	require.NoError(t, err)
	for name, option := range map[string]pipit.Option{
		"safe mode":             pipit.WithSafeMode(),
		"go dispatch":           pipit.WithForceGoDispatch(),
		"no verification":       pipit.WithBytecodeVerification(false),
		"yield interval":        pipit.WithYieldInterval(1),
		"callback timeout":      pipit.WithCallbackTimeout(time.Second),
		"deadlock grace":        pipit.WithDeadlockGrace(time.Second),
		"debug info":            pipit.WithDebugInfo(),
		"debugger":              pipit.WithDebugger(pipit.NewDebugger()),
		"arena size":            pipit.WithMaxArenaSizeBytes(1 << 30),
		"verifier iterations":   pipit.WithVerifierIterationLimit(1 << 20),
		"constant pool":         pipit.WithMaxConstantPoolSize(1 << 16),
		"specialisations":       pipit.WithMaxSpecialisations(1 << 10),
		"methods":               pipit.WithMaxMethods(1 << 10),
		"goroutines":            pipit.WithMaxGoroutines(8),
		"output size":           pipit.WithMaxOutputSize(1 << 20),
		"unpinned modules":      pipit.WithAllowUnpinnedModules(false),
		"logger":                pipit.WithLogger(nil),
		"build tags":            pipit.WithBuildTags("pro"),
		"interpreted arguments": pipit.WithArgs([]string{"script", "first"}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			interpreter := pipit.NewInterpreter(option)
			t.Cleanup(func() { _ = interpreter.Close() })
			got, err := interpreter.Eval(t.Context(), program)
			require.NoError(t, err)
			require.Equal(t, baseline, got)
		})
	}
}

func TestStderrOptionsCaptureInterpretedOutput(t *testing.T) {
	t.Parallel()
	var configured strings.Builder
	interpreter := pipit.NewInterpreter(pipit.WithStderr(&configured))
	t.Cleanup(func() { _ = interpreter.Close() })
	_, err := interpreter.Eval(t.Context(), "println(\"to the configured writer\")\n1")
	require.NoError(t, err)
	require.Contains(t, configured.String(), "to the configured writer")

	var replaced strings.Builder
	interpreter.SetStderr(&replaced)
	_, err = interpreter.Eval(t.Context(), "println(\"to the replacement\")\n1")
	require.NoError(t, err)
	require.Contains(t, replaced.String(), "to the replacement")
	require.NotContains(t, configured.String(), "to the replacement")
}

func environmentSymbols() pipit.SymbolExports {
	return pipit.SymbolExports{"os": {
		"Getenv": reflect.ValueOf(os.Getenv),
		"Setenv": reflect.ValueOf(os.Setenv),
	}}
}

func TestEnvironmentOptionsShapeWhatScriptsSee(t *testing.T) {
	t.Parallel()
	const program = "package main\n\nimport \"os\"\n\nfunc run() string {\n\t_ = os.Setenv(\"PIPIT_SCRIPT_SET\", \"set\")\n\treturn os.Getenv(\"PIPIT_OPTION_VALUE\") + \"/\" + os.Getenv(\"PIPIT_SCRIPT_SET\")\n}\n"
	for name, option := range map[string]pipit.Option{
		"overlay": pipit.WithEnv(map[string]string{"PIPIT_OPTION_VALUE": "overlay"}),
		"sealed":  pipit.WithSealedEnv(map[string]string{"PIPIT_OPTION_VALUE": "sealed"}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			interpreter := pipit.NewInterpreterWithSymbols(environmentSymbols(), option)
			t.Cleanup(func() { _ = interpreter.Close() })
			compiled, err := interpreter.CompileFileSet(t.Context(), map[string]string{"main.go": program})
			require.NoError(t, err)
			result, err := interpreter.ExecuteEntrypoint(t.Context(), compiled, "run")
			require.NoError(t, err)
			require.Equal(t, name+"/set", result)
			require.Empty(t, os.Getenv("PIPIT_SCRIPT_SET"), "a script's Setenv stays inside the interpreter")
		})
	}
}

func TestImportOptionsRefuseUnlistedPackages(t *testing.T) {
	t.Parallel()
	symbols := pipit.SymbolExports{
		"strings": {"ToUpper": reflect.ValueOf(strings.ToUpper)},
		"os":      {"Getenv": reflect.ValueOf(os.Getenv)},
	}
	usesOS := map[string]string{"main.go": "package main\n\nimport \"os\"\n\nfunc run() string { return os.Getenv(\"HOME\") }\n"}
	usesStrings := map[string]string{"main.go": "package main\n\nimport \"strings\"\n\nfunc run() string { return strings.ToUpper(\"x\") }\n"}

	allowlisted := pipit.NewInterpreterWithSymbols(symbols, pipit.WithImportAllowlist("strings"))
	t.Cleanup(func() { _ = allowlisted.Close() })
	_, err := allowlisted.CompileFileSet(t.Context(), usesOS)
	require.Error(t, err, "an import outside the allowlist is refused")
	_, err = allowlisted.CompileFileSet(t.Context(), usesStrings)
	require.NoError(t, err)

	denied := pipit.NewInterpreterWithSymbols(symbols, pipit.WithDeniedImports("os"))
	t.Cleanup(func() { _ = denied.Close() })
	_, err = denied.CompileFileSet(t.Context(), usesOS)
	require.Error(t, err, "a denied import is refused")
	require.True(t, denied.HasRegisteredPackage("strings"))
}

type fixedClock struct {
	pipit.Clock
	now time.Time
}

func (clock fixedClock) Now() time.Time { return clock.now }

type staticProvider pipit.SymbolExports

func (provider staticProvider) Exports() pipit.SymbolExports { return pipit.SymbolExports(provider) }

func TestSymbolProviderAndClockOptions(t *testing.T) {
	t.Parallel()
	provider := staticProvider{"time": {
		"Now":  reflect.ValueOf(time.Now),
		"Time": reflect.ValueOf((*time.Time)(nil)),
	}}
	moment := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	interpreter := pipit.NewInterpreter(pipit.WithSymbolProvider(provider), pipit.WithClock(fixedClock{Clock: pipit.WallClock, now: moment}))
	t.Cleanup(func() { _ = interpreter.Close() })
	compiled, err := interpreter.CompileFileSet(t.Context(), map[string]string{"main.go": "package main\n\nimport \"time\"\n\nfunc run() int { return time.Now().Year() }\n"})
	require.NoError(t, err)
	result, err := interpreter.ExecuteEntrypoint(t.Context(), compiled, "run")
	require.NoError(t, err)
	require.EqualValues(t, 2001, result, "interpreted code reads the injected clock")
}

func TestAContextLoggerWinsOverTheWithLoggerOption(t *testing.T) {
	var fallback, bound strings.Builder
	interpreter := pipit.NewInterpreter(pipit.WithLogger(slog.New(slog.NewTextHandler(&fallback, nil))))
	ctx := pipit.ContextWithLogger(t.Context(), slog.New(slog.NewTextHandler(&bound, nil)))
	_, err := interpreter.Eval(ctx, "panic(\"boom\")")
	require.Error(t, err)
	require.Empty(t, fallback.String(), "the option's logger is only the fallback")
	require.Equal(t, ctx, pipit.ContextWithLogger(ctx, nil), "a nil logger leaves the context unchanged")
}

func capabilityTestGreet(name string) string { return "Hello, " + name }

type identityHook struct {
	pipit.DenyCapabilityHook
	name string
}

func (hook identityHook) CheckFunctionCall(_ context.Context, _, name string, _ []reflect.Value) error {
	if name == hook.name {
		return nil
	}
	return pipit.ErrCapabilityDenied
}

func TestCheckFunctionCallReceivesTheRegisteredIdentity(t *testing.T) {
	hook := identityHook{DenyCapabilityHook: pipit.DenyCapabilityHook{}, name: "example.com/host.Greet"}
	interpreter := pipit.NewInterpreterWithSymbols(pipit.SymbolExports{"example.com/host": {"Greet": reflect.ValueOf(capabilityTestGreet)}}, pipit.WithCapabilityHook(hook))
	value, err := interpreter.Eval(t.Context(), "import \"example.com/host\"\nhost.Greet(\"Pipit\")")
	require.NoError(t, err, "the hook sees the import path and name the function was registered under")
	require.Equal(t, "Hello, Pipit", value)
}
