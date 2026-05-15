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
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func classifyBody(t *testing.T, source string) map[string]isa.RegisterKind {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "main.go", source, 0)
	require.NoError(t, err)
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	_, err = (&types.Config{}).Check("main", fileSet, []*ast.File{file}, info)
	require.NoError(t, err)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "f" {
			continue
		}
		return ClassifyTypedSliceLocals(&Context{Info: info, Function: &program.CompiledFunction{}}, function.Body)
	}
	t.Fatal("no func f in source")
	return nil
}

func TestClassifyTypedSliceLocalsAdmitsMapLookups(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want map[string]isa.RegisterKind
	}{
		{
			name: "string slice from a define lookup",
			body: `m := map[string][]string{}; s := m["k"]; _ = len(s); _ = s[0]`,
			want: map[string]isa.RegisterKind{"s": isa.RegisterSliceString},
		},
		{
			name: "comma-ok define pairs the value side only",
			body: `m := map[int][]int{}; s, ok := m[1]; _ = ok; _ = s[0]`,
			want: map[string]isa.RegisterKind{"s": isa.RegisterSliceInt},
		},
		{
			name: "byte and float elements route to their banks",
			body: `m := map[string][]byte{}; n := map[string][]float64{}; b := m["k"]; x := n["k"]; _ = b[0]; _ = x[0]`,
			want: map[string]isa.RegisterKind{"b": isa.RegisterSliceByte, "x": isa.RegisterSliceFloat},
		},
		{
			name: "named slice element is refused",
			body: `type words []string; m := map[string]words{}; s := m["k"]; _ = s[0]`,
			want: nil,
		},
		{
			name: "narrow element is refused",
			body: `m := map[string][]int32{}; s := m["k"]; _ = s[0]`,
			want: nil,
		},
		{
			name: "reassignment disqualifies",
			body: `m := map[string][]string{}; s := m["k"]; s = m["j"]; _ = s[0]`,
			want: nil,
		},
		{
			name: "blank comma-ok value declares nothing",
			body: `m := map[string][]string{}; _, ok := m["k"]; _ = ok`,
			want: nil,
		},
		{
			name: "var form is not classified",
			body: `m := map[string][]string{}; var s []string = m["k"]; _ = s[0]`,
			want: nil,
		},
		{
			name: "slice index on a non-map is not a lookup",
			body: `outer := [][]string{}; s := outer[0]; _ = s[0]`,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyBody(t, "package main\n\nfunc f() {\n"+tt.body+"\n}\n")
			if tt.want == nil {
				require.Empty(t, got)
				return
			}
			require.Equal(t, tt.want, got)
		})
	}
}
