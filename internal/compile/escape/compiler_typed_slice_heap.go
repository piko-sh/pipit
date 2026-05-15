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

	"pipit.sh/pipit/internal/isa"
)

// ClassifyHeapTypedSliceLocals names the typed-slice locals whose backing must live on
// the Go heap because the slice header flows out of the frame. Placing them on the heap
// up front avoids an O(n) arena-backing copy at the escape barrier.
//
// Takes body (*ast.BlockStmt) which is the function body to inspect.
// Takes typedLocals (map[string]isa.RegisterKind) which lists the surviving typed-slice
// locals; names outside it are ignored.
//
// Returns the set of escaping local names, or nil when there are none.
func ClassifyHeapTypedSliceLocals(body *ast.BlockStmt, typedLocals map[string]isa.RegisterKind) map[string]bool {
	if body == nil || len(typedLocals) == 0 {
		return nil
	}
	escaping := make(map[string]bool)
	ast.Inspect(body, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.AssignStmt:
			if assignIsContainerWrite(statement) {
				markTypedSliceIdent(statement.Rhs[0], typedLocals, escaping)
			}
		case *ast.ReturnStmt:
			for _, result := range statement.Results {
				markTypedSliceIdent(result, typedLocals, escaping)
			}
		case *ast.SendStmt:
			markTypedSliceIdent(statement.Value, typedLocals, escaping)
		case *ast.GoStmt:
			markTypedSliceCallArgs(statement.Call, typedLocals, escaping)
		case *ast.DeferStmt:
			markTypedSliceCallArgs(statement.Call, typedLocals, escaping)
		case *ast.FuncLit:
			markTypedSliceCaptures(statement, typedLocals, escaping)
			return false
		}
		return true
	})
	if len(escaping) == 0 {
		return nil
	}
	return escaping
}

// markTypedSliceIdent records expression in escaping when it names a typed-slice local.
//
// Takes expression (ast.Expr) which is the candidate use site.
// Takes typedLocals (map[string]isa.RegisterKind) which lists the typed-slice locals.
// Takes escaping (map[string]bool) which receives the name.
func markTypedSliceIdent(expression ast.Expr, typedLocals map[string]isa.RegisterKind, escaping map[string]bool) {
	identifier, ok := ast.Unparen(expression).(*ast.Ident)
	if !ok {
		return
	}
	if _, typed := typedLocals[identifier.Name]; typed {
		escaping[identifier.Name] = true
	}
}

// markTypedSliceCallArgs records every typed-slice local passed directly as an argument
// of call.
//
// Takes call (*ast.CallExpr) which is the go or defer call.
// Takes typedLocals (map[string]isa.RegisterKind) which lists the typed-slice locals.
// Takes escaping (map[string]bool) which receives the names.
func markTypedSliceCallArgs(call *ast.CallExpr, typedLocals map[string]isa.RegisterKind, escaping map[string]bool) {
	if call == nil {
		return
	}
	for _, argument := range call.Args {
		markTypedSliceIdent(argument, typedLocals, escaping)
	}
}

// markTypedSliceCaptures records every typed-slice local referenced inside literal's
// body, since a captured local is hoisted into a heap cell that outlives the frame.
//
// Takes literal (*ast.FuncLit) which is the closure to scan.
// Takes typedLocals (map[string]isa.RegisterKind) which lists the typed-slice locals.
// Takes escaping (map[string]bool) which receives the names.
func markTypedSliceCaptures(literal *ast.FuncLit, typedLocals map[string]isa.RegisterKind, escaping map[string]bool) {
	if literal == nil || literal.Body == nil {
		return
	}
	ast.Inspect(literal.Body, func(node ast.Node) bool {
		if identifier, ok := node.(*ast.Ident); ok {
			markTypedSliceIdent(identifier, typedLocals, escaping)
		}
		return true
	})
}
