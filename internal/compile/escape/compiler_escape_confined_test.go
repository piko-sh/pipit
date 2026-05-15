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

package escape

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func parsePackageFunctions(t *testing.T, source string) map[string]*ast.FuncDecl {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "escape_test.go", "package main\n\n"+source, parser.SkipObjectResolution)
	require.NoError(t, err)
	declarations := make(map[string]*ast.FuncDecl)
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Recv == nil {
			declarations[function.Name.Name] = function
		}
	}
	return declarations
}

const confinedCalleeSource = `
func generate(seed int) string {
	output := make([]byte, 0, 64)
	seed = fill(&output, seed, 3)
	return string(output)
}

func fill(output *[]byte, seed int, budget int) int {
	if budget == 0 {
		*output = append(*output, byte('0'+seed%10))
		return seed * 7
	}
	*output = append(*output, '(')
	seed = fill(output, seed, budget-1)
	*output = append(*output, ')')
	return other(output, seed)
}

func other(output *[]byte, seed int) int {
	if output == nil {
		return seed
	}
	*output = append(*output, ' ')
	if len(*output) > 100 {
		return seed
	}
	return fill(output, seed+1, 0)
}
`

func TestConfinedAddressArgumentKeepsCellAndBackingLocal(t *testing.T) {
	t.Parallel()
	declarations := parsePackageFunctions(t, confinedCalleeSource)
	body := declarations["generate"].Body

	verdict := classifyLocalEscape("output", body, declarations)

	require.False(t, verdict.escapes, "reason %q", verdict.reasonCode)
	require.False(t, sliceBackingEscapes("output", body, declarations))
}

func TestSliceBackingEscapeRules(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		use     string
		escapes bool
	}{
		{name: "appending to the local", use: "output = append(output, 1)", escapes: false},
		{name: "indexing the local", use: "output[0] = 2; _ = output[0]", escapes: false},
		{name: "ranging over the local", use: "for i := range output { _ = i }", escapes: false},
		{name: "measuring the local", use: "_ = len(output) + cap(output)", escapes: false},
		{name: "copying out of the local", use: "var dst [4]byte; copy(dst[:], output)", escapes: false},
		{name: "spreading the local into another append", use: "other = append(other, output...)", escapes: false},
		{name: "comparing the local with nil", use: "if output == nil { return \"\" }", escapes: false},
		{name: "converting the local to a string", use: "_ = string(output)", escapes: false},
		{name: "appending the local itself", use: "lists = append(lists, output)", escapes: true},
		{name: "passing the local by value", use: "consume(output)", escapes: true},
		{name: "aliasing the local", use: "alias := output; _ = alias", escapes: true},
		{name: "reslicing the local", use: "_ = output[1:]", escapes: true},
		{name: "returning the local", use: "return string(output[:0]) + string(output)", escapes: true},
		{name: "storing the local in a struct", use: "_ = holder{data: output}", escapes: true},
		{name: "capturing the local in a closure", use: "f := func() int { return len(output) }; _ = f()", escapes: true},
		{name: "address passed to a confined callee", use: "grow(&output)", escapes: false},
		{name: "address passed to a callee that keeps the pointer", use: "keep(&output)", escapes: true},
		{name: "address passed to a callee that copies the slice out", use: "leak(&output)", escapes: true},
		{name: "address passed to a callee reading a field", use: "field(&output)", escapes: true},
		{name: "address passed to a variadic callee", use: "spread(&output)", escapes: true},
		{name: "address passed to a shadowed callee", use: "grow := func(p *[]byte) {}; grow(&output)", escapes: true},
		{name: "address passed to a method", use: "var h holder; h.take(&output)", escapes: true},
		{name: "address stored", use: "saved = &output", escapes: true},
	}
	const prelude = `
var other []byte
var lists [][]byte
var saved *[]byte
var savedSlice []byte

type holder struct{ data []byte }

func (h holder) take(p *[]byte) {}
func consume(b []byte) {}
func grow(p *[]byte) { *p = append(*p, 1); if p != nil { _ = len(*p) } }
func keep(p *[]byte) { saved = p }
func leak(p *[]byte) { savedSlice = *p }
func field(p *[]byte) { _ = cap(*p); q := p; _ = q }
func spread(ps ...*[]byte) {}
`
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			source := prelude + "\nfunc run() string {\n\toutput := make([]byte, 0, 8)\n\t" + testCase.use + "\n\treturn \"\"\n}\n"
			declarations := parsePackageFunctions(t, source)
			body := declarations["run"].Body

			require.Equal(t, testCase.escapes, sliceBackingEscapes("output", body, declarations))
		})
	}
}

func TestCellVerdictAcceptsConfinedAddressAndCopyingReturn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		source  string
		escapes bool
		reason  string
	}{
		{name: "confined address and copying return", source: `
func grow(p *[]byte) { *p = append(*p, 1) }
func run() string { output := make([]byte, 0, 8); grow(&output); return string(output) }`},
		{name: "returned directly", source: `
func run() []byte { output := make([]byte, 0, 8); return output }`, escapes: true, reason: "returned"},
		{name: "address taken twice", source: `
func grow(p *[]byte) { *p = append(*p, 1) }
func run() string { output := make([]byte, 0, 8); grow(&output); grow(&output); return "" }`, escapes: true, reason: "multiple-address-of"},
		{name: "address handed to a callee that keeps it", source: `
var saved *[]byte
func keep(p *[]byte) { saved = p }
func run() string { output := make([]byte, 0, 8); keep(&output); return "" }`, escapes: true, reason: "address-of-non-deref"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			declarations := parsePackageFunctions(t, testCase.source)
			verdict := classifyLocalEscape("output", declarations["run"].Body, declarations)
			require.Equal(t, testCase.escapes, verdict.escapes)
			require.Equal(t, testCase.reason, verdict.reasonCode)
		})
	}
}

func TestParameterNameAtExpandsGroupedParameters(t *testing.T) {
	t.Parallel()
	declarations := parsePackageFunctions(t, `func f(a, b *int, c string, rest ...int) {}`)
	declaration := declarations["f"]
	for index, want := range []string{"a", "b", "c"} {
		name, ok := parameterNameAt(declaration, index)
		require.True(t, ok)
		require.Equal(t, want, name)
	}
	_, ok := parameterNameAt(declaration, 3)
	require.False(t, ok, "a variadic parameter is never confined")
	_, ok = parameterNameAt(declaration, 9)
	require.False(t, ok)
}
