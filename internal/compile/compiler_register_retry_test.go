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

package compile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/frontend"
	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"

	"pipit.sh/pipit/internal/compile/passes"
)

func compileLoggingRetries(t *testing.T, source string) (*program.CompiledFunction, []string, error) {
	t.Helper()

	symbols := symtab.NewSymbolRegistry(nil)
	result, err := frontend.Typecheck(testPackageName, map[string]string{"main.go": source}, symbols.Importer(t.Context()))
	require.NoError(t, err, "the fixture source must type-check before it can be compiled")

	rootFunction := program.NewNamedFunction("<root>")
	config := CompilerConfig{
		FileSet:      result.FileSet,
		Info:         result.Info,
		Function:     rootFunction,
		RootFunction: rootFunction,
		Symbols:      symbols,
		Globals:      engine.NewGlobalStore(),
		ScopeName:    "<root>",
		Features:     policy.InterpFeaturesAll,
		Passes:       passes.DefaultOptions(),
	}
	var records bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&records, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := logging.WithLogger(t.Context(), logger)
	_, compileErr := CompileParsedPackage(ctx, config, result.Files, testPackageName, nil)

	var retried []string
	for line := range strings.SplitSeq(strings.TrimSpace(records.String()), "\n") {
		var record struct {
			Message  string `json:"msg"`
			Function string `json:"function"`
		}
		if line == "" || json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		if record.Message == "recompiling body under register pressure" {
			retried = append(retried, record.Function)
		}
	}
	return rootFunction, retried, compileErr
}

func wideCallSource(name string, first, arity int) string {
	var source strings.Builder
	fmt.Fprintf(&source, "func %s() int {\n", name)
	for index := range 300 {
		fmt.Fprintf(&source, "\tv%d := g(%d)\n", index, index)
	}
	fmt.Fprintf(&source, "\tt := sum%d(%s)\n", arity, repeatJoined(arity, ", ", func(i int) string { return fmt.Sprintf("v%d", first+i) }))
	for index := range 300 {
		fmt.Fprintf(&source, "\tt += v%d\n", index)
	}
	source.WriteString("\treturn t\n}\n")
	return source.String()
}

func sumSource(arity int) string {
	parameters := repeatJoined(arity, ", ", func(i int) string { return fmt.Sprintf("a%d", i) })
	return fmt.Sprintf("func sum%d(%s int) int { return %s }\n\n", arity, parameters,
		repeatJoined(arity, " + ", func(i int) string { return fmt.Sprintf("a%d", i) }))
}

func nestedWideCallBody(source *strings.Builder, levels int) {
	for index := range 300 {
		fmt.Fprintf(source, "\tv%d := g(%d)\n", index, index)
	}
	fmt.Fprintf(source, "\tt := sum40(%s)\n", repeatJoined(40, ", ", func(i int) string { return fmt.Sprintf("v%d", 220+i) }))
	for index := range 300 {
		fmt.Fprintf(source, "\tt += v%d\n", index)
	}
	if levels > 0 {
		source.WriteString("\tf := func() int {\n")
		nestedWideCallBody(source, levels-1)
		source.WriteString("\t}\n\tt += f()\n")
	}
	source.WriteString("\treturn t\n")
}

const retryPrelude = "package main\n\nfunc g(n int) int { return n }\n\n"

func TestRegisterRetryConvergesWhenArgumentsStraddleTheThreshold(t *testing.T) {
	t.Parallel()

	source := retryPrelude + sumSource(40) + wideCallSource("run", 220, 40)
	_, retried, err := compileLoggingRetries(t, source)
	require.NoError(t, err)
	require.Equal(t, []string{"run"}, retried, "one retry spills enough unread locals")
}

func TestRegisterRetryLeavesFunctionsThatFitUnchanged(t *testing.T) {
	t.Parallel()

	small := "func small(x int) int {\n\ty := x * 3\n\treturn y + sum12(x, y, x, y, x, y, x, y, x, y, x, y)\n}\n\n"
	alone, retried, err := compileLoggingRetries(t, retryPrelude+sumSource(12)+small)
	require.NoError(t, err)
	require.Empty(t, retried)
	withBig, retried, err := compileLoggingRetries(t, retryPrelude+sumSource(12)+small+wideCallSource("big", 280, 12))
	require.NoError(t, err)
	require.Equal(t, []string{"big"}, retried)

	before := findCompiledFunction(t, alone, "small")
	after := findCompiledFunction(t, withBig, "small")
	require.Equal(t, before.Body, after.Body)
	require.Equal(t, before.NumRegisters, after.NumRegisters)
}

func TestRegisterRetryKeepsCreatedFunctionsConsistent(t *testing.T) {
	t.Parallel()

	var body strings.Builder
	body.WriteString("func run() int {\n\tfirst := func() int { return 1 }\n\tt := id(2) + first()\n")
	for index := range 300 {
		fmt.Fprintf(&body, "\tv%d := g(%d)\n", index, index)
	}
	fmt.Fprintf(&body, "\tt += sum20(%s)\n", repeatJoined(20, ", ", func(i int) string { return fmt.Sprintf("v%d", 280+i) }))
	body.WriteString("\tsecond := func() int { return 3 }\n\tt += second()\n")
	for index := range 300 {
		fmt.Fprintf(&body, "\tt += v%d\n", index)
	}
	body.WriteString("\treturn t\n}\n")
	identity := "func id[T any](v T) T {\n\tf := func() T { return v }\n\treturn f()\n}\n\n"

	root, retried, err := compileLoggingRetries(t, retryPrelude+sumSource(20)+identity+body.String())
	require.NoError(t, err)
	require.Contains(t, retried, "run")

	runtimeNames := make(map[string]bool)
	for _, function := range root.Functions {
		require.NotEqual(t, "<discarded>", function.Name, "every slot a discarded attempt freed is reused")
		if function.RuntimeName == "" {
			continue
		}
		require.False(t, runtimeNames[function.RuntimeName], "%s appears twice", function.RuntimeName)
		runtimeNames[function.RuntimeName] = true
	}
}

func TestRegisterRetryRecompilesOnlyTheOverflowingClosure(t *testing.T) {
	t.Parallel()

	closure := strings.Replace(wideCallSource("inner", 280, 20), "func inner() int {", "\tinner := func() int {", 1)
	source := retryPrelude + sumSource(20) + "func run() int {\n" + closure + "\treturn inner()\n}\n"
	_, retried, err := compileLoggingRetries(t, source)
	require.NoError(t, err)
	require.Equal(t, []string{"<closure>"}, retried)
}

func TestRegisterRetrySkipsAnOverflowNoSpillingCanFix(t *testing.T) {
	t.Parallel()

	source := "package main\n\nvar count = func(values ...int) int { return len(values) }\n\n" +
		"func run() int {\n\treturn count(" + repeatJoined(300, ", ", func(i int) string { return fmt.Sprintf("%d", i) }) + ")\n}\n"
	_, retried, err := compileLoggingRetries(t, source)
	require.Error(t, err)
	require.Empty(t, retried)
}

func TestRegisterRetryCompactsNestedCallArguments(t *testing.T) {
	t.Parallel()

	const arity = 60
	source := retryPrelude + sumSource(arity) +
		"func h(n int) int { return n }\n\nfunc g3(a, b, c int) int { return a + b + c }\n\n" +
		"func run(x int) int {\n\treturn sum60(" +
		repeatJoined(arity, ", ", func(i int) string { return fmt.Sprintf("g3(h(x+%d), h(x+1), h(x+2))", i) }) + ")\n}\n"
	_, retried, err := compileLoggingRetries(t, source)
	require.NoError(t, err)
	require.Equal(t, []string{"run"}, retried, "compaction alone fits the call")
}

func TestRegisterRetryOfNestedBodiesDoesNotMultiply(t *testing.T) {
	t.Parallel()

	const nestedClosures = 4
	var source strings.Builder
	source.WriteString(retryPrelude + sumSource(40) + "func run() int {\n")
	nestedWideCallBody(&source, nestedClosures)
	source.WriteString("}\n")

	_, retried, err := compileLoggingRetries(t, source.String())
	require.NoError(t, err)
	require.Len(t, retried, nestedClosures+1,
		"each body is retried once; a body compiled again for its parent starts from the settings it was kept with")
}

func TestRegisterCreatedFunctionReportsAFullTable(t *testing.T) {
	t.Parallel()

	root := program.NewNamedFunction("<root>")
	root.Functions = make([]*program.CompiledFunction, maxFunctionIndex)
	compiler := &Compiler{programContext: &programContext{RootFunction: root}}
	_, err := compiler.registerCreatedFunction(program.NewNamedFunction("<closure>"))
	require.ErrorIs(t, err, fault.ErrCompileFunctionTableFull)
}
