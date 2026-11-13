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
	"go/ast"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"
)

func byteViewHostSymbols() *symtab.SymbolRegistry {
	return symtab.NewSymbolRegistry(symtab.SymbolExports{"host": {
		"Sum":   reflect.ValueOf(func(data []byte, scale int) int { return len(data) * scale }),
		"Two":   reflect.ValueOf(func(left, right []byte) int { return len(left) + len(right) }),
		"Join":  reflect.ValueOf(func(separator []byte, parts ...[]byte) int { return len(separator) + len(parts) }),
		"Any":   reflect.ValueOf(func(value any) int { return 0 }),
		"Scale": reflect.ValueOf(2),
	}})
}

func nativeByteViewMasks(t *testing.T, source string) []uint64 {
	t.Helper()
	root := compileSnippetWithSymbols(t, "package main\n\nimport \"host\"\n\n"+source+"\n", byteViewHostSymbols())
	compiled := findCompiledFunction(t, root, "f")
	var masks []uint64
	for _, site := range compiled.CallSites {
		if site.IsNative {
			masks = append(masks, site.ByteViewArguments)
		}
	}
	require.NotEmpty(t, masks, "f makes a native call")
	return masks
}

func TestNativeArgumentsConvertedFromStringsAreByteViews(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   uint64
	}{
		{name: "a local string", source: "func f(s string) int { return host.Sum([]byte(s), 3) }", want: 0b1},
		{name: "two conversions", source: "func f(a, b string) int { return host.Two([]byte(a), []byte(b)) }", want: 0b11},
		{name: "a package variable", source: "var name string\nfunc f() int { return host.Sum([]byte(name), 1) }", want: 0b1},
		{name: "a map element", source: "func f(m map[string]string) int { return host.Sum([]byte(m[\"k\"]), 1) }", want: 0b1},
		{name: "a struct field", source: "type pair struct{ s string }\nfunc f(p *pair) int { return host.Sum([]byte(p.s), 1) }", want: 0b1},
		{name: "a parenthesised string", source: "func f(s string) int { return host.Sum([]byte((s)), 1) }", want: 0b1},
		{name: "a concatenation", source: "func f(a, b string) int { return host.Sum([]byte(a+b), 1) }", want: 0b1},
		{name: "an element of a slice", source: "func f(xs []string, i int) int { return host.Sum([]byte(xs[i]), -i) }", want: 0b1},
		{name: "through a pointer", source: "func f(p *string) int { return host.Sum([]byte(*p), 1) }", want: 0b1},
		{name: "an address-taken variable", source: "func f(s string) int { p := &s; *p += \"!\"; return host.Sum([]byte(s), 1) }", want: 0b1},
		{name: "a host constant beside it", source: "func f(s string) int { return host.Sum([]byte(s), host.Scale) }", want: 0b1},
		{name: "a converted integer beside it", source: "func f(s string, n int8) int { return host.Sum([]byte(s), int(n)) }", want: 0b1},
		{name: "a call beside it", source: "func g() int { return 1 }\nfunc f(s string) int { return host.Sum([]byte(s), g()) }", want: 0},
		{name: "a receive beside it", source: "func f(s string, c chan int) int { return host.Sum([]byte(s), <-c) }", want: 0},
		{name: "a method value beside it", source: "type n int\nfunc (v n) get() int { return int(v) }\nfunc f(s string, v n) int { return host.Two([]byte(s), []byte(string(rune(v.get())))) }", want: 0},
		{name: "a composite literal beside it", source: "func f(s string) int { return host.Two([]byte(s), []byte{1}) }", want: 0},
		{name: "a variadic argument", source: "func f(s string) int { return host.Join(nil, []byte(s)) }", want: 0},
		{name: "a spread call", source: "func f(s string, parts [][]byte) int { return host.Join([]byte(s), parts...) }", want: 0},
		{name: "an interface parameter", source: "func f(s string) int { return host.Any([]byte(s)) }", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			masks := nativeByteViewMasks(t, tt.source)
			require.Equal(t, tt.want, masks[len(masks)-1])
		})
	}
}

func TestByteViewArgumentsFromCapturedStringsUseATemporary(t *testing.T) {
	t.Parallel()
	root := compileSnippetWithSymbols(t, `package main

import "host"

func f(s string) func() int {
	return func() int { return host.Sum([]byte(s), 1) }
}
`, byteViewHostSymbols())
	var marked []program.CallSite
	for _, function := range root.SubFunctions() {
		for _, site := range function.CallSites {
			if site.IsNative && site.ByteViewArguments != 0 {
				marked = append(marked, site)
			}
		}
	}
	require.Len(t, marked, 1, "the closure's call reads the captured string as a byte view")
}

func TestByteViewCandidatesNeedTheCallBeingCompiled(t *testing.T) {
	t.Parallel()
	compiler := &Compiler{}
	require.Zero(t, compiler.takeByteViewCandidates(&ast.CallExpr{}, nil), "another call than the native call being compiled")
}
