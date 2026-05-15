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
	"context"
	"go/ast"
	"go/token"
	"go/types"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/compile/scope"
	"pipit.sh/pipit/internal/engine/program"
)

const (
	// maxForStmtFingerprintKeyExprs caps pre-extracted AST expressions. Increase only with
	// an audit of every recogniser to confirm the new slots stay zero-valued for unused
	// shapes.
	maxForStmtFingerprintKeyExprs = 6
)

// InitShape classifies the init clause of a for-statement. The classifier maps the AST
// shape to one of these values once; recognisers then read the value, never re-walk the
// AST.
type InitShape uint8

const (
	// forInitOther is the fallback for any init clause that does not match a recognised
	// pattern; recognisers should refuse on this.
	forInitOther InitShape = iota

	// forInitNone marks `for ; cond; post {}` (no init clause).
	forInitNone

	// ForInitConstZeroDecl marks `i := 0` (short variable declaration of a loop counter to
	// the integer literal 0).
	ForInitConstZeroDecl

	// ForInitConstZeroAssign marks `i = 0` (plain assignment of an already-declared loop
	// counter to 0).
	ForInitConstZeroAssign
)

// CondShape classifies the loop condition. Recognisers declare which condition shapes
// they accept via fingerprint signatures, so the classifier must produce stable enum
// values per distinct loop family.
type CondShape uint8

const (
	// forCondOther is the fallback for any condition that does not match a recognised
	// pattern.
	forCondOther CondShape = iota

	// ForCondLtLen marks `i < len(x)` where x is an identifier.
	ForCondLtLen

	// ForCondLtConst marks `i < N` where N resolves to an integer constant via go/types.
	ForCondLtConst

	// forCondLeLen marks `i <= len(x)`.
	forCondLeLen

	// forCondLeConst marks `i <= N`.
	forCondLeConst
)

// postShape classifies the post clause of a for-statement.
type postShape uint8

const (
	// forPostOther is the fallback for any post clause that does not match a recognised
	// pattern.
	forPostOther postShape = iota

	// ForPostPlusPlus marks `i++`.
	ForPostPlusPlus

	// forPostMinusMinus marks `i--`.
	forPostMinusMinus
)

// BodyShape classifies the body of a for-statement.
type BodyShape uint8

const (
	// ForBodyOther is the fallback for any body that does not match a recognised pattern.
	ForBodyOther BodyShape = iota

	// ForBodyEmpty marks an empty body (`for ... {}`).
	ForBodyEmpty

	// ForBodySingleAssign marks one assignment statement (`s = expr` or `s += expr`) whose
	// RHS does not contain an IndexExpr, used by simple-accumulator patterns where the loop
	// adds a constant or non-indexed value.
	ForBodySingleAssign

	// ForBodySingleAssignBinaryIndex marks one assignment whose RHS is a binary expression
	// with at least one IndexExpr operand; covers `sum += a[i]`, `destination[i] = a[i] *
	// 2`, etc.
	ForBodySingleAssignBinaryIndex

	// ForBodySingleAssignBinaryIndexIndex marks one assignment whose RHS is a binary
	// expression with TWO IndexExpr operands: the canonical dot-product / element-wise
	// pattern (`sum += a[i] * b[i]`, `destination[i] = a[i] + b[i]`).
	ForBodySingleAssignBinaryIndexIndex

	// ForBodySingleAssignIndex marks one assignment whose RHS is itself an IndexExpr: the
	// copy pattern (`destination[i] = source[i]`).
	ForBodySingleAssignIndex

	// ForBodySingleIfMaxMin marks one if-statement with a max/min-update shape (`if a[i] > m
	// { m = a[i] }`).
	ForBodySingleIfMaxMin
)

// Fingerprint summarises the shape of a for-statement in a fixed-size hashable structure
// produced by a single classification pass. It captures structural shape only, and each
// recogniser's Match validates semantic constraints.
type Fingerprint struct {
	// KeyExprs are AST expression pointers the classifier pre-extracted so recognisers do
	// not re-walk the loop.
	KeyExprs [maxForStmtFingerprintKeyExprs]ast.Expr

	// ConstUpperBound holds the integer constant N when CondShape is ForCondLtConst or
	// forCondLeConst; zero otherwise.
	ConstUpperBound int64

	// initShape is the classified init clause.
	initShape InitShape

	// CondShape is the classified loop condition.
	CondShape CondShape

	// postShape is the classified post clause.
	postShape postShape

	// BodyShape is the coarse body classification.
	BodyShape BodyShape

	// BodyStmtCount is the number of top-level statements in the loop body (capped at 255
	// for fingerprint compactness; bodies larger than 255 statements are unlikely to match
	// any recogniser and are floored at the cap).
	BodyStmtCount uint8
}

// signature returns the dispatch key for this fingerprint.
//
// Returns the four-shape tuple that the recogniser index uses to look up candidate
// recognisers.
func (fingerprint Fingerprint) signature() FingerprintSignature {
	return FingerprintSignature{
		InitShape: fingerprint.initShape,
		CondShape: fingerprint.CondShape,
		PostShape: fingerprint.postShape,
		BodyShape: fingerprint.BodyShape,
	}
}

// FingerprintSignature is the recogniser-registry hash key. Intentionally narrower than
// the full Fingerprint so recognisers gating on numeric fields share a bucket.
type FingerprintSignature struct {
	// InitShape is the classified init clause.
	InitShape InitShape

	// CondShape is the classified loop condition.
	CondShape CondShape

	// PostShape is the classified post clause.
	PostShape postShape

	// BodyShape is the coarse body classification.
	BodyShape BodyShape
}

// recogniser is the interface every for-statement AST-pattern consumer implements.
// Recognisers register at init time and dispatch via fingerprint-keyed lookup.
type recogniser interface {
	// Name returns a short stable identifier for diagnostics and disassembler annotations.
	//
	// Use a dotted namespace prefix (`simd.dot_product_f64`, `loop.unroll`) so multiple
	// recognisers from the same consumer share an obvious tag.
	//
	// Returns string which is the stable recogniser identifier.
	Name() string

	// Priority returns the bucket tiebreaker rank.
	//
	// Higher priority wins when multiple recognisers accept the same signature, used to
	// resolve overlap between, e.g., the SIMD kernel recogniser (narrow shape) and the loop
	// unroller (broader const-bound shape). Recommended ranges: 0 (default) for fallback
	// recognisers, 10 for general-purpose, 100 for narrow-specialised. Equal priorities are
	// ordered by registration time.
	//
	// Returns int which is the tiebreaker rank.
	Priority() int

	// AcceptedSignatures returns every fingerprint signature this recogniser is willing to
	// consider.
	//
	// The registry indexes recognisers by these signatures at registration time so dispatch
	// is O(1) per node regardless of how many recognisers are registered.
	//
	// Returns []FingerprintSignature which holds every signature the recogniser accepts.
	AcceptedSignatures() []FingerprintSignature

	// Match performs targeted per-pattern validation.
	//
	// Match must be conservative: false positives miscompile, false negatives just miss the
	// optimisation. Returns the opaque token the Emit step consumes when the pattern
	// applies.
	//
	// Takes ctx (RecogniseContext) which provides read-only state.
	// Takes statement (*ast.ForStmt) which is the matched node.
	// Takes fingerprint (Fingerprint) which carries the shape.
	//
	// Returns matchToken (any) which the Emit step consumes.
	// Returns ok (bool) which is true when the pattern applies.
	Match(ctx RecogniseContext, statement *ast.ForStmt, fingerprint Fingerprint) (matchToken any, ok bool)

	// Emit produces bytecode for the matched statement.
	//
	// Replaces the standard compile path. Receives the token Match returned and the
	// compiler's emission surface for threading into fallback paths
	// (Emitter.CompileForFallback, Emitter.CompileStmt).
	//
	// Takes emitter (Emitter) which is the compiler emitting the current function.
	// Takes statement (*ast.ForStmt) which is the node to emit.
	// Takes matchToken (any) which Match returned.
	//
	// Returns program.VarLocation which is the result destination.
	// Returns error when emission fails.
	Emit(ctx context.Context, emitter Emitter, statement *ast.ForStmt, matchToken any) (program.VarLocation, error)
}

// Emitter is the compiler's emission surface as a recogniser's Emit sees it.
type Emitter interface {
	// CurrentFunction returns the function whose body is being emitted.
	//
	// Returns *program.CompiledFunction which is the function being built.
	CurrentFunction() *program.CompiledFunction

	// ScopeStack returns the scope stack and register allocator of the current function.
	//
	// Returns *scope.ScopeStack which is the active scope.
	ScopeStack() *scope.ScopeStack

	// SetDebugPosition records pos as the source position of the instructions emitted next.
	//
	// Takes pos (token.Pos) which is the source position.
	SetDebugPosition(ctx context.Context, pos token.Pos)

	// CompileExpression compiles expression through the standard walker.
	//
	// Takes expression (ast.Expr) which is the AST node.
	//
	// Returns program.VarLocation which is the result destination.
	// Returns error when compilation fails.
	CompileExpression(ctx context.Context, expression ast.Expr) (program.VarLocation, error)

	// CompileStmt compiles statement through the standard walker.
	//
	// Takes statement (ast.Stmt) which is the AST node.
	//
	// Returns program.VarLocation which is the result destination.
	// Returns error when compilation fails.
	CompileStmt(ctx context.Context, statement ast.Stmt) (program.VarLocation, error)

	// CompileForFallback compiles a for statement through the scalar path, bypassing
	// recognition.
	//
	// Emit() calls it when its specialised emission declines partway.
	//
	// Takes statement (*ast.ForStmt) which is the for statement AST node.
	//
	// Returns program.VarLocation which is the result destination.
	// Returns error when compilation fails.
	CompileForFallback(ctx context.Context, statement *ast.ForStmt) (program.VarLocation, error)
}

// RecogniseContext bundles the read-only state Match needs.
type RecogniseContext struct {
	// Info is the go/types info table for the package being compiled. Recognisers use it to
	// resolve identifier types and extract constant values via Info.Types[expr].Value.
	Info *types.Info

	// Function is the current CompiledFunction being built. Recognisers may read it but
	// Match must not write.
	Function *program.CompiledFunction

	// Options selects which optimisations the compilation runs. Every recogniser is
	// registered unconditionally, so a recogniser whose optimisation is switched off must
	// decline in Match.
	Options passes.Options
}

// Registry indexes for-statement recognisers by fingerprint signature, built once and
// treated as immutable so dispatch is lock-free and a nil *Registry recognises nothing.
type Registry struct {
	// forStmt maps fingerprint signatures to recogniser buckets, highest priority first.
	forStmt map[FingerprintSignature][]recogniser
}

// NewRegistry returns an empty registry.
//
// Returns *Registry which is ready for Register.
func NewRegistry() *Registry {
	return new(Registry{forStmt: make(map[FingerprintSignature][]recogniser)})
}

// Register inserts a recogniser into the index.
//
// Indexes r under every signature it reports via AcceptedSignatures. Registration order
// determines candidate order within a bucket among equal priorities; the first recogniser
// whose Match returns true wins.
//
// Takes r (Recogniser) which is the recogniser to register.
func (registry *Registry) Register(r recogniser) {
	for _, signature := range r.AcceptedSignatures() {
		bucket := registry.forStmt[signature]
		insertAt := len(bucket)
		for i, existing := range bucket {
			if existing.Priority() < r.Priority() {
				insertAt = i
				break
			}
		}
		bucket = append(bucket, nil)
		copy(bucket[insertAt+1:], bucket[insertAt:])
		bucket[insertAt] = r
		registry.forStmt[signature] = bucket
	}
}

// TryRecogniseForStmt fingerprints statement and asks the recognisers indexed under its
// signature, in priority order, whether they match.
//
// Takes ctx (RecogniseContext) which provides read-only state.
// Takes statement (*ast.ForStmt) which is the loop to recognise.
//
// Returns recogniser which matched the loop.
// Returns any which is the opaque match token.
// Returns bool which is false when no recogniser matched.
func (registry *Registry) TryRecogniseForStmt(ctx RecogniseContext, statement *ast.ForStmt) (recogniser, any, bool) {
	if registry == nil || len(registry.forStmt) == 0 {
		return nil, nil, false
	}
	fingerprint := classifyForStmt(statement, ctx.Info)
	bucket := registry.forStmt[fingerprint.signature()]
	if len(bucket) == 0 {
		return nil, nil, false
	}
	for _, recogniser := range bucket {
		if matchToken, ok := recogniser.Match(ctx, statement, fingerprint); ok {
			return recogniser, matchToken, true
		}
	}
	return nil, nil, false
}
