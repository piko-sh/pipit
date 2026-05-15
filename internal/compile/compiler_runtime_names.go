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
	"go/types"
)

// callStackInspectors names the host functions that read the call stack. A function using
// one must keep its own frame, so the compiler never turns a call to it into a tail call,
// and never inlines a function that calls it.
var callStackInspectors = map[string]map[string]bool{
	"runtime":       {"Caller": true, "Callers": true},
	"runtime/debug": {"Stack": true, "PrintStack": true},
}

// runtimeFunctionName renders a declaration's name as Go's runtime does: pkg.F, pkg.T.M
// for a value receiver, pkg.(*T).M for a pointer receiver, with [...] standing for the
// type arguments of a generic function or receiver.
//
// Takes declaration (*ast.FuncDecl) which is the function or method.
//
// Returns string which is the qualified name.
func (c *Compiler) runtimeFunctionName(declaration *ast.FuncDecl) string {
	qualifier := c.runtimePackageName
	if qualifier == "" {
		qualifier = "main"
	}
	name := declaration.Name.Name
	if declaration.Type.TypeParams != nil && len(declaration.Type.TypeParams.List) > 0 {
		name += "[...]"
	}
	if declaration.Recv == nil || len(declaration.Recv.List) != 1 {
		return qualifier + "." + name
	}
	receiver, pointer := receiverDeclarationName(declaration.Recv.List[0].Type)
	if receiver == "" {
		return qualifier + "." + name
	}
	if pointer {
		return qualifier + ".(*" + receiver + ")." + name
	}
	return qualifier + "." + receiver + "." + name
}

// bodyInspectsCallStack reports whether a function body refers to one of the call-stack
// inspection functions, as a call or as a function value.
//
// Takes info (*types.Info) which resolves selector uses; nil is treated as inspecting,
// the conservative answer.
// Takes body (*ast.BlockStmt) which is the body to scan.
//
// Returns bool which is true when the body reads the call stack.
func bodyInspectsCallStack(info *types.Info, body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	if info == nil {
		return true
	}
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found {
			return false
		}
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		function, ok := info.Uses[selector.Sel].(*types.Func)
		if ok && function.Pkg() != nil && callStackInspectors[function.Pkg().Path()][function.Name()] {
			found = true
			return false
		}
		return true
	})
	return found
}

// receiverDeclarationName reads the base type name out of a receiver expression, noting a
// pointer receiver and rendering type parameters as [...].
//
// Takes expression (ast.Expr) which is the receiver's type expression.
//
// Returns string which is the rendered type name, empty when unrecognised.
// Returns bool which is true for a pointer receiver.
func receiverDeclarationName(expression ast.Expr) (string, bool) {
	pointer := false
	if star, ok := expression.(*ast.StarExpr); ok {
		expression = star.X
		pointer = true
	}
	generic := false
	switch typed := expression.(type) {
	case *ast.IndexExpr:
		expression = typed.X
		generic = true
	case *ast.IndexListExpr:
		expression = typed.X
		generic = true
	default:
	}
	ident, ok := expression.(*ast.Ident)
	if !ok {
		return "", pointer
	}
	name := ident.Name
	if generic {
		name += "[...]"
	}
	return name, pointer
}
