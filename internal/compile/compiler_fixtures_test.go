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
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/frontend"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
)

const testPackageName = "main"

func compileSnippet(t *testing.T, source string) *program.CompiledFunction {
	t.Helper()
	root, err := tryCompileSnippet(t, source)
	require.NoError(t, err, "the fixture source must compile")
	return root
}

func tryCompileSnippet(t *testing.T, source string) (*program.CompiledFunction, error) {
	t.Helper()
	return tryCompileSnippetWithFeatures(t, source, policy.InterpFeaturesAll, 0, 0)
}

func tryCompileSnippetWithLimits(t *testing.T, source string, maxLiteralElements, maxExpressionDepth int) (*program.CompiledFunction, error) {
	return tryCompileSnippetWithFeatures(t, source, policy.InterpFeaturesAll, maxLiteralElements, maxExpressionDepth)
}

func tryCompileSnippetWithFeatures(t *testing.T, source string, features policy.InterpFeature, maxLiteralElements, maxExpressionDepth int) (*program.CompiledFunction, error) {
	t.Helper()

	symbols := symtab.NewSymbolRegistry(nil)
	result, err := frontend.Typecheck(testPackageName, map[string]string{"main.go": source}, symbols)
	require.NoError(t, err, "the fixture source must type-check before it can be compiled")

	rootFunction := program.NewNamedFunction("<root>")
	config := CompilerConfig{
		FileSet:            result.FileSet,
		Info:               result.Info,
		Function:           rootFunction,
		RootFunction:       rootFunction,
		FunctionTable:      nil,
		GlobalVariables:    nil,
		Symbols:            symbols,
		Globals:            engine.NewGlobalStore(),
		Patterns:           nil,
		ScopeName:          "<root>",
		Features:           features,
		Passes:             passes.DefaultOptions(),
		DebugEnabled:       false,
		MaxLiteralElements: maxLiteralElements,
		MaxExpressionDepth: maxExpressionDepth,
	}

	if _, err := CompileParsedPackage(context.Background(), config, result.Files, testPackageName, nil); err != nil {
		return nil, err
	}
	return rootFunction, nil
}

func findCompiledFunction(t *testing.T, root *program.CompiledFunction, name string) *program.CompiledFunction {
	t.Helper()
	for _, candidate := range root.SubFunctions() {
		if candidate.FunctionName() == name {
			return candidate
		}
	}
	names := make([]string, 0, len(root.SubFunctions()))
	for _, candidate := range root.SubFunctions() {
		names = append(names, candidate.FunctionName())
	}
	t.Fatalf("compiled function %q not found; the bundle holds %v", name, names)
	return nil
}

func bodyContainsOpcode(compiledFunction *program.CompiledFunction, op isa.Opcode) bool {
	for _, instruction := range compiledFunction.Body {
		if instruction.Op == op {
			return true
		}
	}
	return false
}

func bodyContainsTier1SubOp(compiledFunction *program.CompiledFunction, subOp isa.SubOpcode) bool {
	for _, instruction := range compiledFunction.Body {
		if isa.InstrIsTier1SubOp(instruction, subOp) {
			return true
		}
	}
	return false
}

func bodyContainsTier2SubOp(compiledFunction *program.CompiledFunction, subOp isa.SubOpcodeTier2) bool {
	for _, instruction := range compiledFunction.Body {
		if isa.InstrIsTier2SubOp(instruction, subOp) {
			return true
		}
	}
	return false
}

func countTier1SubOp(compiledFunction *program.CompiledFunction, subOp isa.SubOpcode) int {
	count := 0
	for _, instruction := range compiledFunction.Body {
		if isa.InstrIsTier1SubOp(instruction, subOp) {
			count++
		}
	}
	return count
}

func compileSnippetWithSymbols(t *testing.T, source string, symbols *symtab.SymbolRegistry) *program.CompiledFunction {
	t.Helper()
	result, err := frontend.Typecheck(testPackageName, map[string]string{"main.go": source}, symbols)
	require.NoError(t, err, "the fixture source must type-check before it can be compiled")

	rootFunction := program.NewNamedFunction("<root>")
	config := CompilerConfig{
		FileSet:            result.FileSet,
		Info:               result.Info,
		Function:           rootFunction,
		RootFunction:       rootFunction,
		FunctionTable:      nil,
		GlobalVariables:    nil,
		Symbols:            symbols,
		Globals:            engine.NewGlobalStore(),
		Patterns:           nil,
		ScopeName:          "<root>",
		Features:           policy.InterpFeaturesAll,
		Passes:             passes.DefaultOptions(),
		DebugEnabled:       false,
		MaxLiteralElements: 0,
		MaxExpressionDepth: 0,
	}

	_, err = CompileParsedPackage(context.Background(), config, result.Files, testPackageName, nil)
	require.NoError(t, err, "the fixture source must compile")
	return rootFunction
}
