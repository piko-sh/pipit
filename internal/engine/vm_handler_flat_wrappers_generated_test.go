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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func wrapperCallees(t *testing.T, path string) map[string]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	require.NoError(t, err)
	callees := map[string]string{}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		statement, ok := function.Body.List[len(function.Body.List)-1].(*ast.ReturnStmt)
		require.Truef(t, ok, "%s must end in a return", function.Name.Name)
		call, ok := statement.Results[0].(*ast.CallExpr)
		require.Truef(t, ok, "%s must return a handler call", function.Name.Name)
		callee, ok := call.Fun.(*ast.Ident)
		require.Truef(t, ok, "%s must call a handler by name", function.Name.Name)
		callees[function.Name.Name] = callee.Name
	}
	return callees
}

func TestWrappersAndTheirBodiesAreNamedAfterTheirOperation(t *testing.T) {
	t.Parallel()
	handlers := map[string]bool{}
	for _, row := range isa.AllSpecs() {
		if row.Handler != "" {
			handlers[row.Handler] = true
		}
	}

	flat := wrapperCallees(t, "vm_handler_flat_wrappers_generated.go")
	require.NotEmpty(t, flat)
	for wrapper, body := range flat {
		require.Truef(t, handlers[wrapper], "flat wrapper %s must be an operation's derived Go handler", wrapper)
		require.Equalf(t, wrapper+"Body", body,
			"the inner body of %s must be named after its operation (gen_kind_families.go)", wrapper)
	}

	adapters := wrapperCallees(t, "vm_handler_append_adapters_generated.go")
	require.NotEmpty(t, adapters)
	for adapter, inner := range adapters {
		require.Truef(t, handlers[adapter], "append adapter %s must be an operation's derived Go handler", adapter)
		require.Truef(t, handlers[inner] || inner == adapter+"Body",
			"append adapter %s must call a derived Go handler or its own body, not %s", adapter, inner)
	}
}
