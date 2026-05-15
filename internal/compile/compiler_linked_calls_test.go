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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/link"
	"pipit.sh/pipit/internal/symtab"
)

const linkedPackagePath = "example.com/linked"

func registryWithLinked(t *testing.T, exports map[string]reflect.Value) *symtab.SymbolRegistry {
	t.Helper()
	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{linkedPackagePath: exports})
	symbols.SynthesiseAll()
	return symbols
}

func TestCallingALinkedGenericLoadsItsSiblingAsAConstant(t *testing.T) {
	t.Parallel()

	oneTypeArgument := func(elementType reflect.Type, label string) reflect.Value {
		_ = label
		return reflect.New(elementType).Elem()
	}
	twoTypeArguments := func(keyType, valueType reflect.Type, key string) reflect.Value {
		_, _ = keyType, key
		return reflect.New(valueType).Elem()
	}

	tests := []struct {
		name    string
		exports map[string]reflect.Value
		source  string
	}{
		{
			name:    "one explicit type argument",
			exports: map[string]reflect.Value{"GetItem": reflect.ValueOf(link.Wrap(1, oneTypeArgument))},
			source:  "func f() string { return linked.GetItem[string](\"hello\") }",
		},
		{
			name:    "one explicit type argument of a different bank",
			exports: map[string]reflect.Value{"GetItem": reflect.ValueOf(link.Wrap(1, oneTypeArgument))},
			source:  "func f() int { return linked.GetItem[int](\"hello\") }",
		},
		{
			name:    "one explicit type argument of a slice type",
			exports: map[string]reflect.Value{"GetItem": reflect.ValueOf(link.Wrap(1, oneTypeArgument))},
			source:  "func f() int { return len(linked.GetItem[[]int](\"hello\")) }",
		},
		{
			name:    "one explicit type argument of a locally declared type",
			exports: map[string]reflect.Value{"GetItem": reflect.ValueOf(link.Wrap(1, oneTypeArgument))},
			source:  "type record struct{ N int }\nfunc f() int { return linked.GetItem[record](\"hello\").N }",
		},
		{
			name:    "two explicit type arguments",
			exports: map[string]reflect.Value{"Pick": reflect.ValueOf(link.Wrap(2, twoTypeArguments))},
			source:  "func f() string { return linked.Pick[string, int](\"k\") }",
		},
		{
			name:    "the result discarded",
			exports: map[string]reflect.Value{"GetItem": reflect.ValueOf(link.Wrap(1, oneTypeArgument))},
			source:  "func f() { _ = linked.GetItem[string](\"hello\") }",
		},
		{
			name:    "a call inside a loop body",
			exports: map[string]reflect.Value{"GetItem": reflect.ValueOf(link.Wrap(1, oneTypeArgument))},
			source:  "func f() int { n := 0; for i := 0; i < 2; i++ { n += len(linked.GetItem[string](\"hello\")) }; return n }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			symbols := registryWithLinked(t, tt.exports)
			source := "package main\n\nimport \"" + linkedPackagePath + "\"\n\n" + tt.source + "\n"

			root := compileSnippetWithSymbols(t, source, symbols)
			compiled := findCompiledFunction(t, root, "f")

			require.True(t, bodyContainsOpcode(compiled, isa.OpLoadGeneralConst),
				"the sibling is resolved at compile time and loaded as a constant, not looked up again at run time")
			require.True(t, bodyContainsTier1SubOp(compiled, isa.SubOpCallNative),
				"the sibling is host code, so the call leaves the interpreter through the native path")
		})
	}
}

func TestAPlainHostFunctionIsNotTreatedAsALinkedGeneric(t *testing.T) {
	t.Parallel()

	symbols := registryWithLinked(t, map[string]reflect.Value{
		"Plain": reflect.ValueOf(func(label string) string { return label }),
	})

	source := "package main\n\nimport \"" + linkedPackagePath + "\"\n\nfunc f() string { return linked.Plain(\"hello\") }\n"

	root := compileSnippetWithSymbols(t, source, symbols)
	compiled := findCompiledFunction(t, root, "f")

	require.True(t, bodyContainsTier1SubOp(compiled, isa.SubOpCallNative),
		"an ordinary host function still leaves the interpreter, just without a type-argument prefix")
	require.NotEmpty(t, compiled.Body)
}
