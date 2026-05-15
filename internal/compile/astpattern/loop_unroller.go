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

package astpattern

import (
	"context"
	"go/ast"
	"go/token"

	"pipit.sh/pipit/internal/compile/patterns"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// unrollMaxTripCount caps the constant N the unroller accepts. Larger trips keep the
	// standard scalar loop to avoid bytecode bloat.
	unrollMaxTripCount = 8

	// unrollMaxBodyStmts caps the number of statements per iteration the unroller accepts.
	unrollMaxBodyStmts = 8

	// unrollMaxTotalStmts caps the post-unroll bytecode footprint (trips * stmts). Cheaper
	// to reject conservatively than to risk bytecode bloat.
	unrollMaxTotalStmts = 64

	// loopUnrollerPriority is the recogniser priority for the loop unroller. Sits below the
	// SIMD kernel recogniser (priority simdKernelPriority) so explicit SIMD shapes win when
	// both match a fingerprint bucket.
	loopUnrollerPriority = 10
)

// loopUnrollRecogniser unrolls const-bound canonical for loops. It matches the `for i :=
// 0; i < N; i++ { body }` shape and emits N copies of the body with the loop variable
// pre-loaded per iteration.
type loopUnrollRecogniser struct{}

// Name returns the recogniser identifier for diagnostics + disassembler annotations.
//
// Returns "loop.unroll".
func (*loopUnrollRecogniser) Name() string { return "loop.unroll" }

// Priority returns the broad-fallback tiebreaker rank.
//
// Returns int which is the loopUnrollerPriority rank.
func (*loopUnrollRecogniser) Priority() int { return loopUnrollerPriority }

// AcceptedSignatures lists every fingerprint signature the unroller is willing to
// consider. Accepts every body shape - including forBodyOther for multi-statement bodies
// - gated on the canonical `i := 0` / `i < N` (const) / `i++` framing.
//
// Returns the full signature set.
func (*loopUnrollRecogniser) AcceptedSignatures() []patterns.FingerprintSignature {
	bodyShapes := []patterns.BodyShape{
		patterns.ForBodyEmpty,
		patterns.ForBodySingleAssign,
		patterns.ForBodySingleAssignBinaryIndex,
		patterns.ForBodySingleAssignBinaryIndexIndex,
		patterns.ForBodySingleAssignIndex,
		patterns.ForBodySingleIfMaxMin,
		patterns.ForBodyOther,
	}
	initShapes := []patterns.InitShape{patterns.ForInitConstZeroDecl, patterns.ForInitConstZeroAssign}
	signatures := make([]patterns.FingerprintSignature, 0, len(initShapes)*len(bodyShapes))
	for _, initShape := range initShapes {
		for _, bodyShape := range bodyShapes {
			signatures = append(signatures, patterns.FingerprintSignature{
				InitShape: initShape,
				CondShape: patterns.ForCondLtConst,
				PostShape: patterns.ForPostPlusPlus,
				BodyShape: bodyShape,
			})
		}
	}
	return signatures
}

// Match validates the per-pattern constraints that the fingerprint cannot capture: trip
// count and body size within the unroll budget, body free of control flow /
// address-of-loop-var / closures capturing loop var, body does not write to the loop var.
// Declines outright when the compilation has the loop unroller switched off.
//
// Takes ctx (RecogniseContext) which carries compilation options and type info.
// Takes statement (*ast.ForStmt) which is the candidate for-loop.
// Takes fingerprint (Fingerprint) which holds the pre-extracted structural features.
//
// Returns the loopUnrollMatch token and ok=true on success.
func (*loopUnrollRecogniser) Match(ctx patterns.RecogniseContext, statement *ast.ForStmt, fingerprint patterns.Fingerprint) (any, bool) {
	if !ctx.Options.LoopUnroll {
		return nil, false
	}
	if fingerprint.ConstUpperBound <= 0 || fingerprint.ConstUpperBound > unrollMaxTripCount {
		return nil, false
	}
	if fingerprint.BodyStmtCount == 0 || fingerprint.BodyStmtCount > unrollMaxBodyStmts {
		return nil, false
	}
	if fingerprint.ConstUpperBound*int64(fingerprint.BodyStmtCount) > unrollMaxTotalStmts {
		return nil, false
	}
	loopVar, ok := extractCanonicalLoopVarIdent(statement.Init)
	if !ok {
		return nil, false
	}
	if !isIdentNameEqual(extractCondLeftIdent(statement.Cond), loopVar.Name) {
		return nil, false
	}
	if !isIdentNameEqual(extractPostIdent(statement.Post), loopVar.Name) {
		return nil, false
	}
	if !unrollBodyIsSafe(statement.Body, loopVar) {
		return nil, false
	}
	return loopUnrollMatch{
		loopVarIdent: loopVar,
		tripCount:    int(fingerprint.ConstUpperBound),
	}, true
}

// Emit produces the unrolled bytecode for the loop.
//
// Emits an `i := 0` declaration in a fresh scope, then for each k in 0..tripCount-1 a
// LOAD_INT_CONST of k into i followed by a compileStmt of the body. No back-edge jump, no
// cond evaluation, no post clause; the loop's semantics reduce to a flat sequence at
// compile time.
//
// Takes emitter (patterns.Emitter) which is the compiler emitting the current function.
// Takes statement (*ast.ForStmt) which is the matched loop.
// Takes matchToken (any) which is the loopUnrollMatch from Match.
//
// Returns VarLocation which is always the zero value for a for-stmt.
// Returns error when body compilation fails.
func (*loopUnrollRecogniser) Emit(ctx context.Context, emitter patterns.Emitter, statement *ast.ForStmt, matchToken any) (program.VarLocation, error) {
	match, ok := matchToken.(loopUnrollMatch)
	if !ok {
		return emitter.CompileForFallback(ctx, statement)
	}
	c := emitter
	outerLoopVar, needsWriteback, ok := resolveOuterLoopVarWriteTarget(c, statement)
	if !ok {
		return c.CompileForFallback(ctx, statement)
	}
	c.ScopeStack().PushScope()
	loopVarLocation := c.ScopeStack().DeclareVar(match.loopVarIdent.Name, isa.RegisterInt)
	if loopVarLocation.IsSpilled || loopVarLocation.IsIndirect {
		c.ScopeStack().PopScope()
		return c.CompileForFallback(ctx, statement)
	}
	for k := range match.tripCount {
		c.SetDebugPosition(ctx, statement.For)
		program.Emit(c.CurrentFunction(), isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), loopVarLocation.Register, safeconv.MustIntToUint8(k))
		if _, err := c.CompileStmt(ctx, statement.Body); err != nil {
			c.ScopeStack().PopScope()
			return program.VarLocation{}, err
		}
	}
	c.ScopeStack().PopScope()
	if needsWriteback {
		c.SetDebugPosition(ctx, statement.For)
		program.Emit(c.CurrentFunction(), isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), outerLoopVar.Register, safeconv.MustIntToUint8(match.tripCount))
	}
	return program.VarLocation{}, nil
}

// loopUnrollMatch is the opaque token Match returns to Emit.
type loopUnrollMatch struct {
	// loopVarIdent names the canonical loop variable to declare during emission.
	loopVarIdent *ast.Ident

	// tripCount is the validated constant iteration count.
	tripCount int
}

// unrollBodyIsSafe reports whether the body is safe to unroll. Refuses control-flow
// keywords, &loopVar, loop-variable mutation, and function literals, because Go 1.22+
// per-iteration scope semantics are not preserved by unrolling.
//
// Takes body (*ast.BlockStmt) which is the loop body.
// Takes loopVar (*ast.Ident) which is the canonical loop variable.
//
// Returns bool which is true when the body is safe to unroll.
func unrollBodyIsSafe(body *ast.BlockStmt, loopVar *ast.Ident) bool {
	if body == nil {
		return true
	}
	safe := true
	ast.Inspect(body, func(node ast.Node) bool {
		if !safe {
			return false
		}
		if !nodeIsSafeForUnroll(node, loopVar) {
			safe = false
			return false
		}
		return true
	})
	return safe
}

// nodeIsSafeForUnroll reports whether one node is unroll-compatible.
//
// Takes node (ast.Node) which is the AST node under inspection.
// Takes loopVar (*ast.Ident) which is the canonical loop variable.
//
// Returns bool which is true when node poses no unroll hazard.
func nodeIsSafeForUnroll(node ast.Node, loopVar *ast.Ident) bool {
	switch n := node.(type) {
	case *ast.BranchStmt:
		return !branchStmtBreaksControlFlow(n)
	case *ast.UnaryExpr:
		return !unaryTakesAddressOfLoopVar(n, loopVar)
	case *ast.AssignStmt:
		return !assignWritesLoopVar(n, loopVar)
	case *ast.IncDecStmt:
		return !incDecMutatesLoopVar(n, loopVar)
	case *ast.FuncLit:
		return false
	}
	return true
}

// branchStmtBreaksControlFlow reports whether the branch breaks unrolling.
//
// Takes n (*ast.BranchStmt) which is the branch statement under inspection.
//
// Returns bool indicating the branch breaks unrolling.
func branchStmtBreaksControlFlow(n *ast.BranchStmt) bool {
	switch n.Tok {
	case token.BREAK, token.CONTINUE, token.GOTO, token.FALLTHROUGH:
		return true
	default:
	}
	return false
}

// unaryTakesAddressOfLoopVar reports whether n is `&loopVar`, which would alias the loop
// variable to the heap and disqualify unrolling.
//
// Takes n (*ast.UnaryExpr) which is the unary expression under inspection.
// Takes loopVar (*ast.Ident) which is the canonical loop variable.
//
// Returns bool which is true when n addresses the loop variable.
func unaryTakesAddressOfLoopVar(n *ast.UnaryExpr, loopVar *ast.Ident) bool {
	if n.Op != token.AND {
		return false
	}
	ident, ok := n.X.(*ast.Ident)
	return ok && ident.Name == loopVar.Name
}

// assignWritesLoopVar reports whether the assignment writes the loop var.
//
// Takes n (*ast.AssignStmt) which is the assignment statement.
// Takes loopVar (*ast.Ident) which is the canonical loop variable.
//
// Returns bool which is true when any LHS binds the loop variable name.
func assignWritesLoopVar(n *ast.AssignStmt, loopVar *ast.Ident) bool {
	for _, lhs := range n.Lhs {
		if ident, ok := lhs.(*ast.Ident); ok && ident.Name == loopVar.Name {
			return true
		}
	}
	return false
}

// incDecMutatesLoopVar reports whether the inc/dec targets the loop var.
//
// Takes n (*ast.IncDecStmt) which is the inc/dec statement.
// Takes loopVar (*ast.Ident) which is the canonical loop variable.
//
// Returns bool which is true when n targets the loop variable.
func incDecMutatesLoopVar(n *ast.IncDecStmt, loopVar *ast.Ident) bool {
	ident, ok := n.X.(*ast.Ident)
	return ok && ident.Name == loopVar.Name
}
