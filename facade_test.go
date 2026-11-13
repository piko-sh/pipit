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
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

const arenaReuseProgram = `
type item struct {
	name string
	tags []string
}

var kept []item
for i := 0; i < 64; i++ {
	name := ""
	for j := 0; j <= i%7; j++ {
		name += "x"
	}
	kept = append(kept, item{name: name, tags: []string{name, name + "!"}})
}
total := 0
for _, it := range kept {
	total += len(it.name) + len(it.tags[1])
}
total
`

func TestArenaReuseKeepsAProgramsResult(t *testing.T) {
	t.Parallel()

	interpreter := pipit.NewInterpreter()
	want, err := interpreter.Eval(t.Context(), arenaReuseProgram)
	require.NoError(t, err)
	for range 3 {
		got, err := interpreter.Eval(t.Context(), arenaReuseProgram)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

func TestInterpreterCloseFailsLaterEvaluations(t *testing.T) {
	interpreter := pipit.NewInterpreter()
	compiled, err := interpreter.CompileFileSet(t.Context(), map[string]string{"main.go": "package main\n\nvar table = []int{1, 2, 3}\n\nfunc run() int { return table[2] }\n"})
	require.NoError(t, err)
	result, err := interpreter.ExecuteEntrypoint(t.Context(), compiled, "run")
	require.NoError(t, err)
	require.EqualValues(t, 3, result)

	require.NoError(t, interpreter.Close())
	require.NoError(t, interpreter.Close(), "Close is idempotent")
	_, err = interpreter.ExecuteEntrypoint(t.Context(), compiled, "run")
	require.ErrorIs(t, err, pipit.ErrClosed)
	_, err = interpreter.Eval(t.Context(), "1 + 1")
	require.ErrorIs(t, err, pipit.ErrClosed)

	clone := interpreter.Clone()
	value, err := clone.Eval(t.Context(), "1 + 1")
	require.NoError(t, err, "a clone keeps its own lifecycle")
	require.EqualValues(t, 2, value)
	require.NoError(t, clone.Close())
}

func TestUnwindLimitOptions(t *testing.T) {
	t.Parallel()

	valid := pipit.NewInterpreter(
		pipit.WithAbortUnwindCost(1000),
		pipit.WithAbortUnwindWall(2*time.Second),
		pipit.WithAbortUnwindDeferWallMax(time.Second),
		pipit.WithAbortUnwindNativeGrace(time.Second),
		pipit.WithGoroutineJoinUnwindCap(time.Second),
		pipit.WithLeakedUnwinderCap(8),
		pipit.WithLockWalkMaxValues(1<<10),
	)
	value, err := valid.Eval(t.Context(), "1 + 1")
	require.NoError(t, err)
	require.EqualValues(t, 2, value)

	for _, option := range []pipit.Option{
		pipit.WithAbortUnwindCost(0),
		pipit.WithAbortUnwindWall(0),
		pipit.WithAbortUnwindDeferWallMax(-time.Second),
		pipit.WithAbortUnwindNativeGrace(0),
		pipit.WithGoroutineJoinUnwindCap(0),
		pipit.WithLeakedUnwinderCap(0),
		pipit.WithLockWalkMaxValues(-1),
	} {
		_, err := pipit.NewInterpreter(option).Eval(t.Context(), "1 + 1")
		require.ErrorIs(t, err, pipit.ErrInvalidOption)
		require.ErrorIs(t, err, pipit.ErrInvalidConfig)
	}
}

const twoInterfaceProgram = "package main\n\ntype Namer interface{ Name() string }\n\ntype Sizer interface{ Size() int }\n\nvar counter = 40\n\nfunc run() int { counter++; return counter }\n"

func TestUnloadCompiledReleasesALoadedProgram(t *testing.T) {
	t.Parallel()
	store, err := pipit.NewDirectoryBytecodeStore(t.TempDir())
	require.NoError(t, err)
	interpreter := pipit.NewInterpreter(pipit.WithBytecodeStore(store))
	t.Cleanup(func() { _ = interpreter.Close() })
	compiled, err := interpreter.CompileFileSet(t.Context(), map[string]string{"main.go": twoInterfaceProgram})
	require.NoError(t, err)
	require.NoError(t, interpreter.SaveCompiled(t.Context(), "prog", compiled))

	loaded, err := interpreter.LoadCompiled(t.Context(), "prog")
	require.NoError(t, err)
	require.NoError(t, interpreter.ExecuteInits(t.Context(), loaded))
	result, err := interpreter.ExecuteEntrypoint(t.Context(), loaded, "run")
	require.NoError(t, err)
	require.EqualValues(t, 41, result)

	require.NoError(t, interpreter.UnloadCompiled(loaded))
	require.NoError(t, interpreter.UnloadCompiled(loaded), "unloading twice does nothing")
	_, err = interpreter.ExecuteEntrypoint(t.Context(), loaded, "run")
	require.ErrorIs(t, err, pipit.ErrProgramUnloaded)
	require.ErrorIs(t, err, pipit.ErrClosed)

	require.ErrorIs(t, interpreter.UnloadCompiled(compiled), pipit.ErrProgramNotLoaded, "a compiled, never loaded, file set is refused")
	require.ErrorIs(t, interpreter.UnloadCompiled(nil), pipit.ErrProgramNotLoaded)
}

func TestWithMaxLoadedNamedInterfacesCapsLoads(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store, err := pipit.NewDirectoryBytecodeStore(directory)
	require.NoError(t, err)
	compiler := pipit.NewInterpreter(pipit.WithBytecodeStore(store))
	t.Cleanup(func() { _ = compiler.Close() })
	compiled, err := compiler.CompileFileSet(t.Context(), map[string]string{"main.go": twoInterfaceProgram})
	require.NoError(t, err)
	require.NoError(t, compiler.SaveCompiled(t.Context(), "prog", compiled))

	limited := pipit.NewInterpreter(pipit.WithBytecodeStore(store), pipit.WithMaxLoadedNamedInterfaces(1))
	t.Cleanup(func() { _ = limited.Close() })
	_, err = limited.LoadCompiled(t.Context(), "prog")
	require.ErrorIs(t, err, pipit.ErrNamedInterfaceLimit)

	roomy := pipit.NewInterpreter(pipit.WithBytecodeStore(store), pipit.WithMaxLoadedNamedInterfaces(2))
	t.Cleanup(func() { _ = roomy.Close() })
	_, err = roomy.LoadCompiled(t.Context(), "prog")
	require.NoError(t, err)

	_, err = pipit.NewInterpreter(pipit.WithMaxLoadedNamedInterfaces(0)).Eval(t.Context(), "1 + 1")
	require.ErrorIs(t, err, pipit.ErrInvalidOption)
}

func TestWithFinaliserTimeoutMustBePositive(t *testing.T) {
	t.Parallel()
	result, err := pipit.NewInterpreter(pipit.WithFinaliserTimeout(time.Second)).Eval(t.Context(), "1 + 1")
	require.NoError(t, err)
	require.EqualValues(t, 2, result)

	_, err = pipit.NewInterpreter(pipit.WithFinaliserTimeout(0)).Eval(t.Context(), "1 + 1")
	require.ErrorIs(t, err, pipit.ErrInvalidOption)
}

func TestWithMaxNamedCompositeTypesCapsAProgramsNamedTypes(t *testing.T) {
	t.Parallel()
	if !typemodel.NamedCompositeTypesSupported() {
		t.Skip("this build gives named composite types no runtime types of their own")
	}
	source := "package main\n\ntype facadeCells []int64\n\ntype facadeIndex map[string]facadeCells\n\n" +
		"func Main() int { return len(facadeIndex{\"a\": facadeCells{1, 2, 3}}[\"a\"]) }\n"

	_, err := pipit.NewInterpreter(pipit.WithMaxNamedCompositeTypes(1)).CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.ErrorIs(t, err, pipit.ErrNamedCompositeQuota)

	roomy := pipit.NewInterpreter(pipit.WithMaxNamedCompositeTypes(2))
	compiled, err := roomy.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoError(t, err)
	result, err := roomy.ExecuteEntrypoint(t.Context(), compiled, "Main")
	require.NoError(t, err)
	require.EqualValues(t, 3, result)

	_, err = pipit.NewInterpreter(pipit.WithMaxNamedCompositeTypes(0)).Eval(t.Context(), "1 + 1")
	require.ErrorIs(t, err, pipit.ErrInvalidOption)
}

func TestWithNamedCompositeProcessLimitSetsTheProcessCap(t *testing.T) {
	previous := typemodel.SetNamedCompositeProcessLimit(0)
	t.Cleanup(func() { typemodel.SetNamedCompositeProcessLimit(previous) })

	_, err := pipit.NewInterpreter(pipit.WithNamedCompositeProcessLimit(1<<17)).Eval(t.Context(), "1 + 1")
	require.NoError(t, err)
	require.Equal(t, 1<<17, typemodel.SetNamedCompositeProcessLimit(0))

	_, err = pipit.NewInterpreter(pipit.WithNamedCompositeProcessLimit(-1)).Eval(t.Context(), "1 + 1")
	require.ErrorIs(t, err, pipit.ErrInvalidOption)
	require.NotErrorIs(t, err, pipit.ErrNamedCompositeLimit)
}

func TestInterpreterCompileExecuteAndInspect(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter(pipit.WithCostBudget(1 << 30))
	t.Cleanup(func() { _ = interpreter.Close() })

	compiledFunction, err := interpreter.Compile(t.Context(), "x := 20\nx * 2 + 2")
	require.NoError(t, err)
	value, err := interpreter.Execute(t.Context(), compiledFunction)
	require.NoError(t, err)
	require.EqualValues(t, 42, value)
	require.Positive(t, interpreter.LastCostUsed(), "a run reports the cost it used")
	require.Zero(t, interpreter.LeakedGoroutines())
	require.Zero(t, interpreter.AbandonedLocks())
	_ = interpreter.FastPathStats()
	require.Contains(t, pipit.DisassembleFunctionAssembly(compiledFunction), "function <eval>")

	value, err = interpreter.EvalFile(t.Context(), "package main\n\nfunc answer() int { return 7 }\n", "answer")
	require.NoError(t, err)
	require.EqualValues(t, 7, value)

	compiled, err := interpreter.CompileFileSet(t.Context(), map[string]string{"main.go": "package main\n\nvar base = 10\n\nfunc init() { base++ }\n\nfunc add(n int) int { return base + n }\n"})
	require.NoError(t, err)
	interpreter.DeclareTopLevelCalls(compiled)
	require.NoError(t, interpreter.ExecuteInits(t.Context(), compiled))
	value, err = interpreter.CallFunction(t.Context(), compiled, "add", 5)
	require.NoError(t, err)
	require.EqualValues(t, 16, value, "init ran before the call")

	interpreter.Reset()
	value, err = interpreter.Eval(t.Context(), "3 * 3")
	require.NoError(t, err, "the interpreter evaluates again after Reset")
	require.EqualValues(t, 9, value)
}

func TestInterpreterCapabilityHookAccessors(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreterWithSymbols(pipit.SymbolExports{"os": {"Getenv": reflect.ValueOf(os.Getenv)}})
	t.Cleanup(func() { _ = interpreter.Close() })
	require.Nil(t, interpreter.CapabilityHook(), "an interpreter built without a hook reports none")

	interpreter.SetCapabilityHook(pipit.DenyCapabilityHook{})
	_, isDeny := interpreter.CapabilityHook().(pipit.DenyCapabilityHook)
	require.True(t, isDeny)
	compiled, err := interpreter.CompileFileSet(t.Context(), map[string]string{"main.go": "package main\n\nimport \"os\"\n\nfunc run() string { return os.Getenv(\"HOME\") }\n"})
	require.NoError(t, err)
	_, err = interpreter.ExecuteEntrypoint(t.Context(), compiled, "run")
	require.ErrorIs(t, err, pipit.ErrCapabilityDenied, "the hook installed later gates the environment read")
}

func TestSessionsKeepDeclarationsBetweenSubmissions(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter()
	t.Cleanup(func() { _ = interpreter.Close() })
	session := interpreter.NewSession()
	var stderr strings.Builder
	session.SetStderr(&stderr)

	_, err := session.Submit(t.Context(), "x := 40")
	require.NoError(t, err)
	value, err := session.Submit(t.Context(), "x + 2")
	require.NoError(t, err)
	require.EqualValues(t, 42, value)
	_, err = session.Submit(t.Context(), "println(\"from the session\")\nx")
	require.NoError(t, err)
	require.Contains(t, stderr.String(), "from the session")

	_, err = session.Submit(t.Context(), "func double(n int) int { return n * 2 }")
	require.NoError(t, err)
	value, err = session.Submit(t.Context(), "double(x)")
	require.NoError(t, err)
	require.EqualValues(t, 80, value, "a declared function persists into later submissions")

	state := session.Inspect()
	require.Equal(t, 5, state.SubmitCount)
	require.NotEmpty(t, state.Declarations)
	require.NotEmpty(t, session.CompiledFunctions())

	session.Reset()
	_, err = session.Submit(t.Context(), "x")
	require.Error(t, err, "Reset forgets the session's declarations")
	require.Zero(t, session.Inspect().SubmitCount)
}

func TestDeclareNativeBorrowClass(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter()
	t.Cleanup(func() { _ = interpreter.Close() })
	require.NoError(t, interpreter.DeclareNativeBorrowClass(strings.Repeat, pipit.BorrowBorrows))
	require.Error(t, interpreter.DeclareNativeBorrowClass("not a function", pipit.BorrowBorrows))
}

func TestStandardLibraryQueries(t *testing.T) {
	t.Parallel()
	require.NotEmpty(t, pipit.PureSurface())
	paths := pipit.StandardLibraryPaths()
	require.Contains(t, paths, "fmt")
	require.True(t, pipit.IsStandardLibraryPath("fmt"))
	require.False(t, pipit.IsStandardLibraryPath("example.com/not/stdlib"))
}

func TestIdentityTransparentFunctionRegistration(t *testing.T) {
	before := pipit.IdentityTransparentFunctionCount()
	pipit.RegisterIdentityTransparentFunction(func(a, b any) bool { return a == b })
	require.Equal(t, before+1, pipit.IdentityTransparentFunctionCount())
}
