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
	"go/types"

	"pipit.sh/pipit/internal/compile/patterns"
	"pipit.sh/pipit/internal/engine/program"
)

const (
	// simdKernelPriority is the recogniser priority for the SIMD kernel matcher; sits above
	// the loop unroller (priority loopUnrollerPriority) so explicit SIMD shapes win when
	// both match a fingerprint bucket.
	simdKernelPriority = 100

	// simdAcceptedSignatureCapacity is the up-front capacity for the AcceptedSignatures
	// bucket. Sized to hold the cartesian product of init shape (2) x cond shape (2) x body
	// shape (4).
	simdAcceptedSignatureCapacity = 16

	// maxUint8Value is the largest value that fits in a uint8 operand byte. Used to gate the
	// small-const fast-path that packs the bound into a single instruction byte.
	maxUint8Value = 0xFF
)

// simdKernelKind enumerates the canonical numerical loop kernels. Each value corresponds
// to a tier-1 sub-opcode in the SIMD handler.
type simdKernelKind uint8

const (
	// simdKernelNone indicates no SIMD kernel was matched.
	simdKernelNone simdKernelKind = iota //nolint:unused // names the zero value

	// simdKernelDotProductFloat64 names the float64 dot-product kernel.
	simdKernelDotProductFloat64

	// simdKernelSumSliceFloat64 names the float64 sum-reduction kernel.
	simdKernelSumSliceFloat64

	// simdKernelAddSliceFloat64 names the float64 element-wise add kernel.
	simdKernelAddSliceFloat64

	// simdKernelScaleSliceFloat64 names the float64 in-place scale kernel.
	simdKernelScaleSliceFloat64
)

// simdKernelMatch is the opaque token a successful Match returns to Emit, carrying
// pre-extracted operand expressions.
type simdKernelMatch struct {
	// sliceA is the first slice operand expression (always an *ast.Ident; types are checked
	// in Match).
	sliceA ast.Expr

	// sliceB is the second slice operand, or nil for kernels with only one slice operand
	// (sum, scale, clear, fill).
	sliceB ast.Expr

	// destinationScalar is the scalar accumulator destination for reduction kernels (dot
	// product, sum). nil for non-reductions.
	destinationScalar ast.Expr

	// destinationSlice is the destination slice for element-wise kernels (add, sub, mul).
	// nil for reductions and in-place kernels (scale, clear, fill).
	destinationSlice ast.Expr

	// scalarOperand is the scalar coefficient for axpy/scale/fill kernels. nil otherwise.
	scalarOperand ast.Expr

	// boundSlice is the *ast.Ident the count comes from (the slice inside `len(slice)`) when
	// boundShape is simdBoundLenSlice; otherwise nil.
	boundSlice ast.Expr

	// boundConstValue is the constant iteration count when boundShape is simdBoundConst;
	// otherwise 0.
	boundConstValue int64

	// kind names which SIMD opcode Emit should produce.
	kind simdKernelKind

	// boundShape names how the loop's iteration count is computed at runtime.
	// simdBoundLenSlice means "len(boundSlice)"; simdBoundConst means "boundConstValue
	// elements (subject to runtime length check)".
	boundShape simdKernelBoundShape
}

// simdKernelBoundShape names the runtime origin of the iteration count.
type simdKernelBoundShape uint8

const (
	// simdBoundUnspecified indicates the bound shape has not been set.
	simdBoundUnspecified simdKernelBoundShape = iota //nolint:unused // names the zero value

	// simdBoundLenSlice indicates the count comes from len(slice).
	simdBoundLenSlice

	// simdBoundConst indicates the count is a compile-time constant.
	simdBoundConst
)

// simdKernelRecogniser matches canonical numerical loop shapes and emits the
// corresponding tier-1 SIMD sub-opcode in place of the scalar loop. It is conservative
// because a false positive would miscompile, whereas a false negative only misses the
// optimisation.
type simdKernelRecogniser struct{}

// Name returns the recogniser identifier for diagnostics + disassembler annotations.
//
// Returns "simd.kernels".
func (*simdKernelRecogniser) Name() string { return "simd.kernels" }

// Priority returns simdKernelPriority.
//
// Returns int which is the recogniser priority.
func (*simdKernelRecogniser) Priority() int { return simdKernelPriority }

// AcceptedSignatures lists every fingerprint signature accepted.
//
// Returns []patterns.FingerprintSignature which is the accepted set.
func (*simdKernelRecogniser) AcceptedSignatures() []patterns.FingerprintSignature {
	signatures := make([]patterns.FingerprintSignature, 0, simdAcceptedSignatureCapacity)
	for _, initShape := range []patterns.InitShape{patterns.ForInitConstZeroDecl, patterns.ForInitConstZeroAssign} {
		for _, condShape := range []patterns.CondShape{patterns.ForCondLtLen, patterns.ForCondLtConst} {
			for _, bodyShape := range []patterns.BodyShape{
				patterns.ForBodySingleAssignBinaryIndexIndex,
				patterns.ForBodySingleAssignIndex,
				patterns.ForBodySingleAssign,
			} {
				signatures = append(signatures, patterns.FingerprintSignature{
					InitShape: initShape,
					CondShape: condShape,
					PostShape: patterns.ForPostPlusPlus,
					BodyShape: bodyShape,
				})
			}
		}
	}
	return signatures
}

// Match validates a for-statement whose fingerprint matched one of AcceptedSignatures.
// Performs per-kernel checks on loop-variable consistency, slice-element types and
// aliasing, then dispatches to the appropriate per-kernel matcher.
//
// Takes ctx (patterns.RecogniseContext) which carries go/types info.
// Takes statement (*ast.ForStmt) which is the candidate loop.
// Takes fingerprint (patterns.Fingerprint) which the classifier pre-extracted.
//
// Returns the simdKernelMatch token and true on success.
func (*simdKernelRecogniser) Match(ctx patterns.RecogniseContext, statement *ast.ForStmt, fingerprint patterns.Fingerprint) (any, bool) {
	if !ctx.Options.SIMDKernels {
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
	var bound simdKernelBoundInfo
	switch fingerprint.CondShape {
	case patterns.ForCondLtLen:
		ident, ok := extractLenSliceIdent(fingerprint.KeyExprs[patterns.ForStmtKeyExprUpperBound])
		if !ok {
			return nil, false
		}
		bound = simdKernelBoundInfo{shape: simdBoundLenSlice, slice: ident, constValue: 0}
	case patterns.ForCondLtConst:
		if fingerprint.ConstUpperBound < 0 {
			return nil, false
		}
		bound = simdKernelBoundInfo{shape: simdBoundConst, constValue: fingerprint.ConstUpperBound, slice: nil}
	default:
		return nil, false
	}
	switch fingerprint.BodyShape {
	case patterns.ForBodySingleAssignBinaryIndexIndex:
		return matchBinaryIndexIndexKernel(ctx, statement, fingerprint, loopVar, bound)
	case patterns.ForBodySingleAssignIndex:
		return matchAssignIndexKernel(ctx, statement, fingerprint, loopVar, bound)
	case patterns.ForBodySingleAssign:
		return matchSingleAssignKernel(ctx, statement, fingerprint, loopVar, bound)
	default:
	}
	return nil, false
}

// Emit produces the bytecode for a matched SIMD kernel. Falls back to scalar compilation
// when an operand cannot be resolved to its typed bank.
//
// Takes emitter (patterns.Emitter) which is the compiler emitting the current function.
// Takes statement (*ast.ForStmt) which is the matched loop.
// Takes matchToken (any) which is the simdKernelMatch from Match.
//
// Returns VarLocation which is the destination of the kernel's result.
// Returns error when operand resolution or emission fails.
func (*simdKernelRecogniser) Emit(ctx context.Context, emitter patterns.Emitter, statement *ast.ForStmt, matchToken any) (program.VarLocation, error) {
	match, ok := matchToken.(simdKernelMatch)
	if !ok {
		return emitter.CompileForFallback(ctx, statement)
	}
	c := emitter
	outerLoopVar, needsWriteback, ok := resolveOuterLoopVarWriteTarget(c, statement)
	if !ok {
		return c.CompileForFallback(ctx, statement)
	}
	location, emitted, err := emitSimdKernelByKind(ctx, c, match)
	if err != nil {
		return program.VarLocation{}, err
	}
	if !emitted {
		return c.CompileForFallback(ctx, statement)
	}
	if needsWriteback {
		if err := emitSimdLoopVarWriteback(c, match, outerLoopVar.Register); err != nil {
			return program.VarLocation{}, err
		}
	}
	return location, nil
}

// simdKernelBoundInfo bundles the count-bound information extracted by Match.
type simdKernelBoundInfo struct {
	// slice is the slice identifier referenced by the cond's len() call when shape is
	// simdBoundLenSlice; otherwise nil.
	slice *ast.Ident

	// constValue is the compile-time iteration count when shape is simdBoundConst; otherwise
	// zero.
	constValue int64

	// shape names whether the count comes from len(slice) or a const.
	shape simdKernelBoundShape
}

// simdKernelBinarySources bundles the two source slices for binary SIMD kernels.
type simdKernelBinarySources struct {
	// sliceA is the first source slice identifier.
	sliceA *ast.Ident

	// sliceB is the second source slice identifier.
	sliceB *ast.Ident
}

// simdKernelOperands bundles the resolved operand expressions for a SIMD kernel.
type simdKernelOperands struct {
	// sliceA is the first slice operand expression, or nil.
	sliceA ast.Expr

	// sliceB is the second slice operand expression, or nil.
	sliceB ast.Expr

	// destinationScalar is the reduction scalar destination, or nil.
	destinationScalar ast.Expr

	// destinationSlice is the element-wise slice destination, or nil.
	destinationSlice ast.Expr

	// scalarOperand is the scalar coefficient operand, or nil.
	scalarOperand ast.Expr
}

// emitSimdKernelByKind dispatches to the per-kernel Emit routine.
//
// emitted=false means the caller (Emit) should fall back to the standard scalar emission
// path.
//
// Takes c (patterns.Emitter) which carries the active Emit state.
// Takes match (simdKernelMatch) which names the matched kernel.
//
// Returns VarLocation which is the kernel's location.
// Returns bool which is true when the SIMD opcode was written.
// Returns error when compilation fails. simdKernelNone is the zero value used for no
// match, so it is the default's job. Returning false there sends the loop back to
// ordinary compilation.
func emitSimdKernelByKind(ctx context.Context, c patterns.Emitter, match simdKernelMatch) (program.VarLocation, bool, error) {
	switch match.kind {
	case simdKernelDotProductFloat64:
		return emitSimdDotProductFloat64(c, match)
	case simdKernelSumSliceFloat64:
		return emitSimdSumSliceFloat64(c, match)
	case simdKernelAddSliceFloat64:
		return emitSimdAddSliceFloat64(c, match)
	case simdKernelScaleSliceFloat64:
		return emitSimdScaleSliceFloat64(ctx, c, match)
	default:
	}
	return program.VarLocation{}, false, nil
}

// matchBinaryIndexIndexKernel dispatches loops with binary-indexed RHS.
//
// The body is one assignment with a binary expression of two IndexExprs on the RHS. Two
// SIMD kernels share this body shape and disambiguate via the LHS: a scalar Ident selects
// the dot product kernel, an IndexExpr selects the element-wise op.
//
// Takes ctx (patterns.RecogniseContext) which carries go/types info.
// Takes statement (*ast.ForStmt) which is the candidate loop.
// Takes fingerprint (patterns.Fingerprint) which the classifier pre-extracted.
// Takes loopVar (*ast.Ident) which is the canonical loop variable.
// Takes bound (simdKernelBoundInfo) which carries count-bound info.
//
// Returns any which is the matched simdKernelMatch token.
// Returns bool which is true when the match succeeds.
func matchBinaryIndexIndexKernel(ctx patterns.RecogniseContext, statement *ast.ForStmt, fingerprint patterns.Fingerprint, loopVar *ast.Ident, bound simdKernelBoundInfo) (any, bool) {
	assign, ok := statement.Body.List[0].(*ast.AssignStmt)
	if !ok {
		return nil, false
	}
	binary, ok := fingerprint.KeyExprs[patterns.ForStmtKeyExprAssignRHS].(*ast.BinaryExpr)
	if !ok {
		return nil, false
	}
	sliceA, sliceB, ok := matchBinaryFloat64IndexOperands(ctx, binary, loopVar)
	if !ok {
		return nil, false
	}
	if bound.shape == simdBoundLenSlice && !isOneOfSlices(bound.slice, sliceA, sliceB) {
		return nil, false
	}
	sources := simdKernelBinarySources{sliceA: sliceA, sliceB: sliceB}
	switch leftHand := assign.Lhs[0].(type) {
	case *ast.Ident:
		return matchDotProductFloat64Kernel(ctx, assign, binary, leftHand, sources, bound)
	case *ast.IndexExpr:
		return matchElementwiseAddFloat64Kernel(ctx, assign, binary, leftHand, loopVar, sources, bound)
	}
	return nil, false
}

// matchBinaryFloat64IndexOperands resolves both binary operands.
//
// Treats each operand as a float64 slice index against the loop variable and returns the
// two source slices when the shape matches.
//
// Takes ctx (patterns.RecogniseContext) which carries go/types info.
// Takes binary (*ast.BinaryExpr) which is the candidate RHS.
// Takes loopVar (*ast.Ident) which is the canonical loop variable.
//
// Returns sliceA (*ast.Ident) which is the first source slice.
// Returns sliceB (*ast.Ident) which is the second source slice.
// Returns ok (bool) which is true when both operands match.
func matchBinaryFloat64IndexOperands(ctx patterns.RecogniseContext, binary *ast.BinaryExpr, loopVar *ast.Ident) (sliceA, sliceB *ast.Ident, ok bool) {
	sliceA, ok = indexedSliceWithLoopVar(binary.X, loopVar)
	if !ok {
		return nil, nil, false
	}
	sliceB, ok = indexedSliceWithLoopVar(binary.Y, loopVar)
	if !ok {
		return nil, nil, false
	}
	if !isFloat64Slice(ctx.Info, sliceA) || !isFloat64Slice(ctx.Info, sliceB) {
		return nil, nil, false
	}
	return sliceA, sliceB, true
}

// matchDotProductFloat64Kernel recognises the dot-product shape.
//
// The accepted shape is `scalar += a[i] * b[i]`.
//
// Takes ctx (patterns.RecogniseContext) which carries go/types info.
// Takes assign (*ast.AssignStmt) which is the body's assignment.
// Takes binary (*ast.BinaryExpr) which is the RHS multiplication.
// Takes leftHand (*ast.Ident) which is the scalar destination.
// Takes sources (simdKernelBinarySources) which carries the two source slices.
// Takes bound (simdKernelBoundInfo) which carries count-bound info.
//
// Returns any which is the matched simdKernelMatch token.
// Returns bool which is true when the match succeeds.
func matchDotProductFloat64Kernel(
	ctx patterns.RecogniseContext,
	assign *ast.AssignStmt,
	binary *ast.BinaryExpr,
	leftHand *ast.Ident,
	sources simdKernelBinarySources,
	bound simdKernelBoundInfo,
) (any, bool) {
	if assign.Tok != token.ADD_ASSIGN || binary.Op != token.MUL {
		return nil, false
	}
	if !isFloat64Scalar(ctx.Info, leftHand) {
		return nil, false
	}
	if isSameIdent(sources.sliceA, leftHand) || isSameIdent(sources.sliceB, leftHand) {
		return nil, false
	}
	return makeSimdKernelMatch(simdKernelDotProductFloat64, simdKernelOperands{sliceA: sources.sliceA,
		sliceB:            sources.sliceB,
		destinationScalar: leftHand, destinationSlice: nil, scalarOperand: nil}, bound), true
}

// matchElementwiseAddFloat64Kernel recognises the elementwise-add shape.
//
// The accepted shape is `dest[i] = a[i] + b[i]`.
//
// Takes ctx (patterns.RecogniseContext) which carries go/types info.
// Takes assign (*ast.AssignStmt) which is the body's assignment.
// Takes binary (*ast.BinaryExpr) which is the RHS addition.
// Takes leftHand (*ast.IndexExpr) which is the destination index.
// Takes loopVar (*ast.Ident) which is the canonical loop variable.
// Takes sources (simdKernelBinarySources) which carries the two source slices.
// Takes bound (simdKernelBoundInfo) which carries count-bound info.
//
// Returns any which is the matched simdKernelMatch token.
// Returns bool which is true when the match succeeds.
func matchElementwiseAddFloat64Kernel(
	ctx patterns.RecogniseContext,
	assign *ast.AssignStmt,
	binary *ast.BinaryExpr,
	leftHand *ast.IndexExpr,
	loopVar *ast.Ident,
	sources simdKernelBinarySources,
	bound simdKernelBoundInfo,
) (any, bool) {
	if assign.Tok != token.ASSIGN || binary.Op != token.ADD {
		return nil, false
	}
	destSlice, ok := indexedSliceWithLoopVar(leftHand, loopVar)
	if !ok {
		return nil, false
	}
	if !isFloat64Slice(ctx.Info, destSlice) {
		return nil, false
	}
	if isSameIdent(destSlice, sources.sliceA) || isSameIdent(destSlice, sources.sliceB) {
		return nil, false
	}
	return makeSimdKernelMatch(simdKernelAddSliceFloat64, simdKernelOperands{sliceA: sources.sliceA,
		sliceB:           sources.sliceB,
		destinationSlice: destSlice, destinationScalar: nil, scalarOperand: nil}, bound), true
}

// makeSimdKernelMatch composes a simdKernelMatch token from a resolved kernel kind,
// operand expressions, and the loop's bound info. Centralising token construction keeps
// the per-kernel matchers free of bookkeeping for the bound shape.
//
// Takes kind (simdKernelKind) which identifies the SIMD kernel variant.
// Takes operands (simdKernelOperands) which holds the resolved operand expressions.
// Takes bound (simdKernelBoundInfo) which describes the loop's iteration bound.
//
// Returns the populated simdKernelMatch.
func makeSimdKernelMatch(kind simdKernelKind, operands simdKernelOperands, bound simdKernelBoundInfo) simdKernelMatch {
	var boundSlice ast.Expr
	if bound.slice != nil {
		boundSlice = bound.slice
	}
	return simdKernelMatch{
		kind:              kind,
		sliceA:            operands.sliceA,
		sliceB:            operands.sliceB,
		destinationScalar: operands.destinationScalar,
		destinationSlice:  operands.destinationSlice,
		scalarOperand:     operands.scalarOperand,
		boundShape:        bound.shape,
		boundSlice:        boundSlice,
		boundConstValue:   bound.constValue,
	}
}

// matchAssignIndexKernel handles bodies whose RHS is a single IndexExpr (the
// sum-reduction shape `sum += a[i]`).
//
// Takes ctx (RecogniseContext) which carries compilation options and type info.
// Takes statement (*ast.ForStmt) which is the candidate for-loop.
// Takes fingerprint (Fingerprint) which holds the pre-extracted structural features.
// Takes loopVar (*ast.Ident) which is the loop induction variable.
// Takes bound (simdKernelBoundInfo) which describes the loop's iteration bound.
//
// Returns the matched token or false.
func matchAssignIndexKernel(ctx patterns.RecogniseContext, statement *ast.ForStmt, fingerprint patterns.Fingerprint, loopVar *ast.Ident, bound simdKernelBoundInfo) (any, bool) {
	assign, ok := statement.Body.List[0].(*ast.AssignStmt)
	if !ok {
		return nil, false
	}
	if assign.Tok != token.ADD_ASSIGN {
		return nil, false
	}
	destinationScalar, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return nil, false
	}
	if !isFloat64Scalar(ctx.Info, destinationScalar) {
		return nil, false
	}
	indexExpr, ok := fingerprint.KeyExprs[patterns.ForStmtKeyExprAssignRHS].(*ast.IndexExpr)
	if !ok {
		return nil, false
	}
	sliceA, ok := indexedSliceWithLoopVar(indexExpr, loopVar)
	if !ok {
		return nil, false
	}
	if !isFloat64Slice(ctx.Info, sliceA) {
		return nil, false
	}
	if bound.shape == simdBoundLenSlice && !isOneOfSlices(bound.slice, sliceA) {
		return nil, false
	}
	if isSameIdent(sliceA, destinationScalar) {
		return nil, false
	}
	return makeSimdKernelMatch(simdKernelSumSliceFloat64, simdKernelOperands{sliceA: sliceA,
		destinationScalar: destinationScalar, sliceB: nil, destinationSlice: nil, scalarOperand: nil}, bound), true
}

// matchSingleAssignKernel handles bodies whose RHS does not directly contain an IndexExpr
// top-level. It matches the scale shape `s[i] *= k`.
//
// Takes ctx (RecogniseContext) which carries compilation options and type info.
// Takes statement (*ast.ForStmt) which is the candidate for-loop.
// Takes fingerprint (Fingerprint) which holds the pre-extracted structural features.
// Takes loopVar (*ast.Ident) which is the loop induction variable.
// Takes bound (simdKernelBoundInfo) which describes the loop's iteration bound.
//
// Returns the matched token or false.
func matchSingleAssignKernel(ctx patterns.RecogniseContext, statement *ast.ForStmt, fingerprint patterns.Fingerprint, loopVar *ast.Ident, bound simdKernelBoundInfo) (any, bool) {
	assign, ok := statement.Body.List[0].(*ast.AssignStmt)
	if !ok {
		return nil, false
	}
	if assign.Tok != token.MUL_ASSIGN {
		return nil, false
	}
	destinationIndex, ok := assign.Lhs[0].(*ast.IndexExpr)
	if !ok {
		return nil, false
	}
	destinationSlice, ok := indexedSliceWithLoopVar(destinationIndex, loopVar)
	if !ok {
		return nil, false
	}
	if !isFloat64Slice(ctx.Info, destinationSlice) {
		return nil, false
	}
	if bound.shape == simdBoundLenSlice && !isOneOfSlices(bound.slice, destinationSlice) {
		return nil, false
	}
	scalarIdent, ok := assign.Rhs[0].(*ast.Ident)
	if !ok {
		return nil, false
	}
	if !isFloat64Scalar(ctx.Info, scalarIdent) {
		return nil, false
	}
	if isSameIdent(destinationSlice, scalarIdent) {
		return nil, false
	}
	_ = fingerprint
	return makeSimdKernelMatch(simdKernelScaleSliceFloat64, simdKernelOperands{sliceA: destinationSlice,
		destinationSlice: destinationSlice,
		scalarOperand:    scalarIdent, sliceB: nil, destinationScalar: nil}, bound), true
}

// extractCanonicalLoopVarIdent returns the *ast.Ident of the loop variable when init has
// the canonical shape `i := 0` or `i = 0`.
//
// Takes init (ast.Stmt) which is the for-statement's init clause.
//
// Returns the loop var identifier and ok=true on success.
func extractCanonicalLoopVarIdent(init ast.Stmt) (*ast.Ident, bool) {
	assign, ok := init.(*ast.AssignStmt)
	if !ok {
		return nil, false
	}
	if len(assign.Lhs) != 1 {
		return nil, false
	}
	ident, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return nil, false
	}
	return ident, true
}

// extractCondLeftIdent returns the *ast.Ident on the left side of the loop condition when
// it has shape `i < expr` or `i <= expr`.
//
// Takes condition (ast.Expr) which is the cond clause.
//
// Returns the identifier or nil.
func extractCondLeftIdent(condition ast.Expr) *ast.Ident {
	binary, ok := condition.(*ast.BinaryExpr)
	if !ok {
		return nil
	}
	ident, ok := binary.X.(*ast.Ident)
	if !ok {
		return nil
	}
	return ident
}

// extractPostIdent returns the *ast.Ident the post clause increments or decrements (`i++`
// / `i--`).
//
// Takes post (ast.Stmt) which is the post clause.
//
// Returns the identifier or nil.
func extractPostIdent(post ast.Stmt) *ast.Ident {
	incDec, ok := post.(*ast.IncDecStmt)
	if !ok {
		return nil
	}
	ident, ok := incDec.X.(*ast.Ident)
	if !ok {
		return nil
	}
	return ident
}

// extractLenSliceIdent returns the *ast.Ident of the slice that appears inside the cond's
// `len(slice)` call when condShape is forCondLtLen.
//
// Takes upperBound (ast.Expr) which is the cond's RHS expression the classifier
// extracted.
//
// Returns the slice identifier and ok=true on success.
func extractLenSliceIdent(upperBound ast.Expr) (*ast.Ident, bool) {
	return patterns.MatchLenCall(upperBound)
}

// isIdentNameEqual reports whether ident is non-nil and has the given name.
//
// Takes ident (*ast.Ident) which may be nil.
// Takes name (string) which is the expected name.
//
// Returns true when ident is non-nil and ident.Name == name.
func isIdentNameEqual(ident *ast.Ident, name string) bool {
	return ident != nil && ident.Name == name
}

// isSameIdent reports whether two AST expressions are both *ast.Ident with the same Name.
// Used for the no-aliasing rule: operand slices must be distinct from destinations.
//
// Takes a (ast.Expr).
// Takes b (ast.Expr).
//
// Returns true when both are identifiers and share a Name.
func isSameIdent(a, b ast.Expr) bool {
	aIdent, aOk := a.(*ast.Ident)
	bIdent, bOk := b.(*ast.Ident)
	if !aOk || !bOk {
		return false
	}
	return aIdent.Name == bIdent.Name
}

// indexedSliceWithLoopVar returns the slice *ast.Ident when expr is `slice[loopVar]` for
// the given loop variable. Refuses any other index expression (constant index, computed
// index, nested index).
//
// Takes expr (ast.Expr) which is the candidate IndexExpr.
// Takes loopVar (*ast.Ident) which is the loop variable.
//
// Returns the slice identifier and ok=true on success.
func indexedSliceWithLoopVar(expr ast.Expr, loopVar *ast.Ident) (*ast.Ident, bool) {
	indexExpr, ok := expr.(*ast.IndexExpr)
	if !ok {
		return nil, false
	}
	sliceIdent, ok := indexExpr.X.(*ast.Ident)
	if !ok {
		return nil, false
	}
	indexIdent, ok := indexExpr.Index.(*ast.Ident)
	if !ok {
		return nil, false
	}
	if indexIdent.Name != loopVar.Name {
		return nil, false
	}
	return sliceIdent, true
}

// isFloat64Slice reports whether expr has type []float64 via go/types info.
//
// Takes info (*types.Info) which carries the type-checker output.
// Takes expr (ast.Expr) which is the candidate expression.
//
// Returns true when expr is a []float64.
func isFloat64Slice(info *types.Info, expr ast.Expr) bool {
	if info == nil {
		return false
	}
	typeAndValue, ok := info.Types[expr]
	if !ok || typeAndValue.Type == nil {
		return false
	}
	sliceType, ok := typeAndValue.Type.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	basic, ok := sliceType.Elem().Underlying().(*types.Basic)
	if !ok {
		return false
	}
	return basic.Kind() == types.Float64
}

// isFloat64Scalar reports whether expr has type float64.
//
// Takes info (*types.Info) which carries the type-checker output.
// Takes expr (ast.Expr) which is the candidate expression.
//
// Returns true when expr is a float64.
func isFloat64Scalar(info *types.Info, expr ast.Expr) bool {
	if info == nil {
		return false
	}
	typeAndValue, ok := info.Types[expr]
	if !ok || typeAndValue.Type == nil {
		return false
	}
	basic, ok := typeAndValue.Type.Underlying().(*types.Basic)
	if !ok {
		return false
	}
	return basic.Kind() == types.Float64
}

// isOneOfSlices reports whether candidate matches any comparison.
//
// Compares identifiers by Name. Used to verify that the loop's `len(slice)` upper bound
// names one of the operand slices, otherwise the loop's iteration count is not the
// operand slice length and the SIMD kernel would over- or under-iterate.
//
// Takes candidate (*ast.Ident) which is the slice named in len(slice); may be nil, in
// which case there is no bound slice to match and the answer is false.
// Takes comparisons (variadic *ast.Ident) which are the operand slices the kernel will
// work over.
//
// Returns true when candidate is non-nil and its Name matches any comparison's Name.
func isOneOfSlices(candidate *ast.Ident, comparisons ...*ast.Ident) bool {
	if candidate == nil {
		return false
	}
	for _, comparison := range comparisons {
		if comparison != nil && candidate.Name == comparison.Name {
			return true
		}
	}
	return false
}
