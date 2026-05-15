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

package patterns

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

const (
	// maxFingerprintStmtCount caps the body statement count stored in the Fingerprint.
	// Bodies with more statements saturate at this value and are unlikely to match any
	// recogniser anyway.
	maxFingerprintStmtCount = 255
)

const (
	// ForStmtKeyExprUpperBound indexes the loop's upper-bound key expression slot.
	ForStmtKeyExprUpperBound = iota

	// forStmtKeyExprAssignLHS indexes the body assignment LHS slot.
	forStmtKeyExprAssignLHS

	// ForStmtKeyExprAssignRHS indexes the body assignment RHS slot.
	ForStmtKeyExprAssignRHS

	// forStmtKeyExprBinaryLHS indexes the body binary-expression LHS slot.
	forStmtKeyExprBinaryLHS

	// forStmtKeyExprBinaryRHS indexes the body binary-expression RHS slot.
	forStmtKeyExprBinaryRHS
)

// MatchLenCall reports whether expr is a call of the form `len(slice)` where slice is a
// bare identifier. Returns the slice identifier when matched.
//
// Takes expr (ast.Expr) which is the candidate expression.
//
// Returns the slice identifier and true when expr matches the shape; nil and false
// otherwise.
func MatchLenCall(expr ast.Expr) (*ast.Ident, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, false
	}
	fn, ok := call.Fun.(*ast.Ident)
	if !ok || fn.Name != "len" {
		return nil, false
	}
	argument, ok := call.Args[0].(*ast.Ident)
	if !ok {
		return nil, false
	}
	return argument, true
}

// classifyForStmt produces a coarse shape-fingerprint for the loop. Every for-statement
// produces a fingerprint even when no recogniser accepts it, so the dispatcher stays
// branch-free.
//
// Takes statement (*ast.ForStmt) which is the loop to classify.
// Takes info (*types.Info) which provides constant-folded values for condition bounds.
// May be nil, in which case ConstUpperBound stays zero.
//
// Returns the populated Fingerprint.
func classifyForStmt(statement *ast.ForStmt, info *types.Info) Fingerprint {
	var fingerprint Fingerprint
	fingerprint.initShape = classifyForStmtInit(statement.Init)
	fingerprint.CondShape = classifyForStmtCond(statement.Cond, info, &fingerprint)
	fingerprint.postShape = classifyForStmtPost(statement.Post)
	classifyForStmtBody(statement.Body, &fingerprint)
	return fingerprint
}

// classifyForStmtInit maps the init clause to its shape enum.
//
// Takes init (ast.Stmt) which is the loop init clause (may be nil).
//
// Returns the matched InitShape.
func classifyForStmtInit(init ast.Stmt) InitShape {
	if init == nil {
		return forInitNone
	}
	assign, ok := init.(*ast.AssignStmt)
	if !ok {
		return forInitOther
	}
	if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return forInitOther
	}
	if _, ok := assign.Lhs[0].(*ast.Ident); !ok {
		return forInitOther
	}
	if !isIntegerLiteralZero(assign.Rhs[0]) {
		return forInitOther
	}
	switch assign.Tok {
	case token.DEFINE:
		return ForInitConstZeroDecl
	case token.ASSIGN:
		return ForInitConstZeroAssign
	default:
		return forInitOther
	}
}

// classifyForStmtCond maps the cond clause to its shape enum and populates the
// upper-bound key expression (and constant value when applicable) on fingerprint.
//
// Takes cond (ast.Expr) which is the loop condition (may be nil).
// Takes info (*types.Info) which resolves constant bounds. May be nil, in which case
// constant bounds degrade to ForCondLtConst / forCondLeConst only when the bound is a
// bare *ast.BasicLit.
// Takes fingerprint (*Fingerprint) which receives the upper-bound key expression and (for
// constant bounds) the constant value.
//
// Returns the matched CondShape.
func classifyForStmtCond(cond ast.Expr, info *types.Info, fingerprint *Fingerprint) CondShape {
	if cond == nil {
		return forCondOther
	}
	binary, ok := cond.(*ast.BinaryExpr)
	if !ok {
		return forCondOther
	}
	if _, ok := binary.X.(*ast.Ident); !ok {
		return forCondOther
	}
	if _, lenOk := MatchLenCall(binary.Y); lenOk {
		fingerprint.KeyExprs[ForStmtKeyExprUpperBound] = binary.Y
		switch binary.Op {
		case token.LSS:
			return ForCondLtLen
		case token.LEQ:
			return forCondLeLen
		default:
		}
		return forCondOther
	}
	if value, ok := resolveIntegerConstant(binary.Y, info); ok {
		fingerprint.KeyExprs[ForStmtKeyExprUpperBound] = binary.Y
		fingerprint.ConstUpperBound = value
		switch binary.Op {
		case token.LSS:
			return ForCondLtConst
		case token.LEQ:
			return forCondLeConst
		default:
		}
		return forCondOther
	}
	return forCondOther
}

// classifyForStmtPost maps the post clause to its shape enum.
//
// Takes post (ast.Stmt) which is the loop post clause (may be nil).
//
// Returns the matched PostShape.
func classifyForStmtPost(post ast.Stmt) postShape {
	incDec, ok := post.(*ast.IncDecStmt)
	if !ok {
		return forPostOther
	}
	if _, ok := incDec.X.(*ast.Ident); !ok {
		return forPostOther
	}
	switch incDec.Tok {
	case token.INC:
		return ForPostPlusPlus
	case token.DEC:
		return forPostMinusMinus
	default:
	}
	return forPostOther
}

// classifyForStmtBody walks the loop body once, decides its coarse shape, and populates
// fingerprint with the body shape, statement count, and any key expressions the chosen
// shape exposes.
//
// Takes body (*ast.BlockStmt) which is the loop body (may be nil).
// Takes fingerprint (*Fingerprint) which is populated in place.
func classifyForStmtBody(body *ast.BlockStmt, fingerprint *Fingerprint) {
	if body == nil {
		fingerprint.BodyShape = ForBodyEmpty
		return
	}
	stmtCount := len(body.List)
	if stmtCount > maxFingerprintStmtCount {
		fingerprint.BodyStmtCount = maxFingerprintStmtCount
	} else {
		fingerprint.BodyStmtCount = uint8(stmtCount)
	}
	if stmtCount == 0 {
		fingerprint.BodyShape = ForBodyEmpty
		return
	}
	if stmtCount == 1 {
		switch only := body.List[0].(type) {
		case *ast.AssignStmt:
			fingerprint.BodyShape = classifyForBodyAssign(only, fingerprint)
			return
		case *ast.IfStmt:
			fingerprint.BodyShape = classifyForBodyIf(only, fingerprint)
			return
		}
	}
	fingerprint.BodyShape = ForBodyOther
}

// classifyForBodyAssign classifies a single-assignment loop body and extracts its operand
// key expressions.
//
// Takes assign (*ast.AssignStmt) which is the body's sole statement.
// Takes fingerprint (*Fingerprint) which receives the extracted
// destination/source/binary-operand key expressions.
//
// Returns the matched BodyShape.
func classifyForBodyAssign(assign *ast.AssignStmt, fingerprint *Fingerprint) BodyShape {
	if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return ForBodyOther
	}
	fingerprint.KeyExprs[forStmtKeyExprAssignLHS] = assign.Lhs[0]
	fingerprint.KeyExprs[ForStmtKeyExprAssignRHS] = assign.Rhs[0]
	switch rhs := assign.Rhs[0].(type) {
	case *ast.BinaryExpr:
		_, leftIsIndex := rhs.X.(*ast.IndexExpr)
		_, rightIsIndex := rhs.Y.(*ast.IndexExpr)
		if leftIsIndex && rightIsIndex {
			fingerprint.KeyExprs[forStmtKeyExprBinaryLHS] = rhs.X
			fingerprint.KeyExprs[forStmtKeyExprBinaryRHS] = rhs.Y
			return ForBodySingleAssignBinaryIndexIndex
		}
		if leftIsIndex || rightIsIndex {
			fingerprint.KeyExprs[forStmtKeyExprBinaryLHS] = rhs.X
			fingerprint.KeyExprs[forStmtKeyExprBinaryRHS] = rhs.Y
			return ForBodySingleAssignBinaryIndex
		}
		return ForBodySingleAssign
	case *ast.IndexExpr:
		return ForBodySingleAssignIndex
	}
	return ForBodySingleAssign
}

// classifyForBodyIf classifies a single-if loop body.
//
// Recognises the canonical max/min shape `if a[i] > m { m = a[i] }` (or its < flip).
// Bodies that match get key expressions populated; non-matching shapes fall through to
// ForBodyOther.
//
// Takes ifStmt (*ast.IfStmt) which is the body's sole statement.
// Takes fingerprint (*Fingerprint) which receives the extracted operand/destination key
// expressions.
//
// Returns the matched BodyShape.
func classifyForBodyIf(ifStmt *ast.IfStmt, fingerprint *Fingerprint) BodyShape {
	if ifStmt.Init != nil || ifStmt.Else != nil {
		return ForBodyOther
	}
	cond, ok := ifStmt.Cond.(*ast.BinaryExpr)
	if !ok {
		return ForBodyOther
	}
	switch cond.Op {
	case token.GTR, token.LSS, token.GEQ, token.LEQ:
	default:
		return ForBodyOther
	}
	if _, ok := cond.X.(*ast.IndexExpr); !ok {
		return ForBodyOther
	}
	if _, ok := cond.Y.(*ast.Ident); !ok {
		return ForBodyOther
	}
	if ifStmt.Body == nil || len(ifStmt.Body.List) != 1 {
		return ForBodyOther
	}
	bodyAssign, ok := ifStmt.Body.List[0].(*ast.AssignStmt)
	if !ok {
		return ForBodyOther
	}
	if len(bodyAssign.Lhs) != 1 || len(bodyAssign.Rhs) != 1 || bodyAssign.Tok != token.ASSIGN {
		return ForBodyOther
	}
	fingerprint.KeyExprs[forStmtKeyExprAssignLHS] = bodyAssign.Lhs[0]
	fingerprint.KeyExprs[ForStmtKeyExprAssignRHS] = bodyAssign.Rhs[0]
	fingerprint.KeyExprs[forStmtKeyExprBinaryLHS] = cond.X
	fingerprint.KeyExprs[forStmtKeyExprBinaryRHS] = cond.Y
	return ForBodySingleIfMaxMin
}

// isIntegerLiteralZero reports whether expr is the integer literal 0 (the only init RHS
// classifyForStmtInit accepts). Constant-folded expressions that evaluate to zero are
// intentionally NOT accepted: the classifier stays cheap and shape-driven; recognisers
// needing stronger predicates use info.Types directly in Match.
//
// Takes expr (ast.Expr) which is the candidate expression.
//
// Returns true when expr is literally "0".
func isIntegerLiteralZero(expr ast.Expr) bool {
	literal, ok := expr.(*ast.BasicLit)
	if !ok {
		return false
	}
	return literal.Kind == token.INT && literal.Value == "0"
}

// resolveIntegerConstant resolves expr to an integer constant when the type-checker has
// folded it. Used to populate ConstUpperBound for the loop unroller's gating decision.
//
// Takes expr (ast.Expr) which is the candidate expression.
// Takes info (*types.Info) which provides constant-folded values; may be nil, in which
// case only bare *ast.BasicLit integers are resolved.
//
// Returns the integer value and true when resolved; 0 and false otherwise.
func resolveIntegerConstant(expr ast.Expr, info *types.Info) (int64, bool) {
	if info != nil {
		if typeAndValue, ok := info.Types[expr]; ok && typeAndValue.Value != nil {
			if value, ok := constant.Int64Val(typeAndValue.Value); ok {
				return value, true
			}
		}
	}
	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind != token.INT {
		return 0, false
	}
	value, ok := constant.Int64Val(constant.MakeFromLiteral(literal.Value, token.INT, 0))
	if !ok {
		return 0, false
	}
	return value, true
}
