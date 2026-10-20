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

package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExitReasonsAreUnique(t *testing.T) {
	t.Parallel()
	seen := map[int64]string{}
	for _, name := range []string{"dispatch_exit_codes.go", "dispatch_exit_codes_asm.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		require.NoError(t, err)
		before := len(seen)
		for _, decl := range file.Decls {
			general, ok := decl.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok || len(value.Names) != 1 || len(value.Values) != 1 || !strings.HasPrefix(value.Names[0].Name, "exit") {
					continue
				}
				literal, ok := value.Values[0].(*ast.BasicLit)
				require.True(t, ok, "%s must be a literal", value.Names[0].Name)
				number, err := strconv.ParseInt(literal.Value, 0, 64)
				require.NoError(t, err)
				previous, clash := seen[number]
				require.False(t, clash, "%s and %s share exit reason %d", previous, value.Names[0].Name, number)
				seen[number] = value.Names[0].Name
			}
		}
		require.Greater(t, len(seen), before, "%s declares exit reasons", name)
	}
}
