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
	"go/token"
	"go/types"
	"slices"
)

// bodyFacts holds what one walk of a function body finds out for the emitters.
type bodyFacts struct {
	// recovers is true when the body or a nested closure calls the predeclared recover.
	recovers bool

	// inspectsCallStack is true when the body or a closure refers to call-stack inspection.
	inspectsCallStack bool

	// defers is true when the body has a defer outside function literals.
	defers bool
}

// bodyShape holds the syntax that can affect body analysis.
//
// Each field errs towards true.
type bodyShape struct {
	// literals is true when the body holds a function literal.
	literals bool

	// addresses is true when the body applies the address operator.
	addresses bool

	// conversions is true when the body converts one value to a type.
	conversions bool

	// appends is true when the body calls something named append.
	appends bool
}

// bodyFactsScan is the state of one scanBodyFacts walk.
type bodyFactsScan struct {
	// info resolves identifiers, or is nil when type information is no longer needed.
	info *types.Info

	// facts accumulates the result.
	facts bodyFacts

	// shape accumulates the body's syntax.
	shape bodyShape
}

// visit tests each node against the facts not yet established and records its syntax.
//
// Takes node (ast.Node) which is the node entered.
// Takes stack ([]ast.Node) which holds the nodes enclosing it.
//
// Returns true always, so every subtree is walked.
func (scan *bodyFactsScan) visit(node ast.Node, stack []ast.Node) bool {
	if _, isLiteral := node.(*ast.FuncLit); isLiteral {
		scan.shape.literals = true
	}
	scan.noteShape(node)
	if _, isDefer := node.(*ast.DeferStmt); isDefer && !scan.facts.defers && !slices.ContainsFunc(stack, isFunctionLiteral) {
		scan.facts.defers = true
	}
	if !scan.facts.recovers && isUniverseRecoverCall(scan.info, node) {
		scan.facts.recovers = true
	}
	if !scan.facts.inspectsCallStack && refersToCallStackInspector(scan.info, node) {
		scan.facts.inspectsCallStack = true
	}
	return true
}

// noteShape records what node adds to the body's shape.
//
// A call counts as a conversion when it has one argument and its callee is a type, or
// when there is no type information to say otherwise.
//
// Takes node (ast.Node) which is the node entered.
func (scan *bodyFactsScan) noteShape(node ast.Node) {
	switch typed := node.(type) {
	case *ast.UnaryExpr:
		if typed.Op == token.AND {
			scan.shape.addresses = true
		}
	case *ast.CallExpr:
		if callee, named := ast.Unparen(typed.Fun).(*ast.Ident); named && callee.Name == "append" {
			scan.shape.appends = true
		}
		if len(typed.Args) == 1 && !scan.shape.conversions {
			scan.shape.conversions = scan.info == nil || scan.info.Types[typed.Fun].IsType()
		}
	}
}

// scanBodyFacts walks a function body once for the facts in bodyFacts.
//
// Without type information the two checks that need it answer true, the conservative
// answer.
//
// Takes info (*types.Info) which resolves identifiers, or nil.
// Takes body (*ast.BlockStmt) which is the body to scan, or nil.
//
// Returns bodyFacts which is all false for a nil body.
func scanBodyFacts(info *types.Info, body *ast.BlockStmt) bodyFacts {
	facts, _ := scanBody(info, body)
	return facts
}

// scanBody is scanBodyFacts() with the body's shape, which the same walk gathers.
//
// Takes info (*types.Info) which resolves identifiers, or nil.
// Takes body (*ast.BlockStmt) which is the body to scan, or nil.
//
// Returns bodyFacts which is all false for a nil body.
// Returns bodyShape which is all false for a nil body.
func scanBody(info *types.Info, body *ast.BlockStmt) (bodyFacts, bodyShape) {
	if body == nil {
		return bodyFacts{recovers: false, inspectsCallStack: false, defers: false},
			bodyShape{literals: false, addresses: false, conversions: false, appends: false}
	}
	scan := bodyFactsScan{
		info:  info,
		facts: bodyFacts{recovers: info == nil, inspectsCallStack: info == nil, defers: false},
		shape: bodyShape{literals: false, addresses: false, conversions: false, appends: false},
	}
	ast.PreorderStack(body, nil, scan.visit)
	return scan.facts, scan.shape
}

// isFunctionLiteral reports whether node is a function literal.
//
// Takes node (ast.Node) which is the node to test.
//
// Returns bool which is true for an *ast.FuncLit.
func isFunctionLiteral(node ast.Node) bool {
	_, ok := node.(*ast.FuncLit)
	return ok
}
