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

	"pipit.sh/pipit/internal/compile/appendlower"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// inPlaceAppendMatch is the destructured return of inPlaceAppendCandidate. ok==false
// signals the candidate is unsafe or unclassifiable and the other fields are meaningless.
type inPlaceAppendMatch struct {
	// refusal names the gate that declined the candidate, "" when ok is true; read by the
	// lowering trace only.
	refusal string

	// callRHS is the matched append call expression.
	callRHS *ast.CallExpr

	// elementKind is the slice element's underlying basic-type name.
	elementKind string

	// location is the resolved LHS VarLocation.
	location program.VarLocation

	// spread is true when the append uses `...`.
	spread bool

	// ok is true when all preconditions hold.
	ok bool
}

// lookupInPlaceAppendTarget resolves a safe in-place append target.
//
// Resolves an LHS identifier to its VarLocation and verifies the safety preconditions for
// in-place arena-slot mutation: the location must sit in the general register bank
// (typed-slice banks already mutate the header by value), must not be captured by a
// closure, addressed via `&x`, stored as an upvalue, or spilled.
//
// Takes lhsIdent (*ast.Ident) which is the LHS identifier from the assignment.
//
// Returns VarLocation which is the resolved location.
// Returns bool which is true when all safety checks pass.
func (c *Compiler) lookupInPlaceAppendTarget(lhsIdent *ast.Ident) (program.VarLocation, bool) {
	location, found := c.Scopes.LookupVar(lhsIdent.Name)
	if !found {
		return program.VarLocation{}, false
	}
	if location.Kind != isa.RegisterGeneral {
		return program.VarLocation{}, false
	}
	if location.IsCaptured || location.IsIndirect || location.IsUpvalue || location.IsSpilled {
		return program.VarLocation{}, false
	}
	if c.inPlaceAppendAliases != nil && c.inPlaceAppendAliases[lhsIdent.Name] {
		return program.VarLocation{}, false
	}
	return location, true
}

// refusedInPlaceAppend returns the match that records why the candidate was refused.
//
// Takes reason (string) which names the failed gate for the lowering trace.
//
// Returns inPlaceAppendMatch with ok false and refusal set.
func refusedInPlaceAppend(reason string) inPlaceAppendMatch {
	return inPlaceAppendMatch{refusal: reason, callRHS: nil, elementKind: "", location: program.VarLocation{}, spread: false, ok: false}
}

// inPlaceAppendCandidate composes the in-place append predicates.
//
// Combines the AST shape match, identity check, target-safety lookup, and element-kind
// classification into a single helper. tryCompileInPlaceAppend calls this before deciding
// which in-place opcode to Emit.
//
// Takes leftHandSide (ast.Expr) which is the assignment LHS.
// Takes rightHandSide (ast.Expr) which is the assignment RHS.
//
// Returns inPlaceAppendMatch with ok set when the candidate is safe and classifiable; the
// destination location and element kind describe where and how to compile the in-place
// form.
func (c *Compiler) inPlaceAppendCandidate(leftHandSide, rightHandSide ast.Expr) inPlaceAppendMatch {
	lhsIdent, callRHS, shapeOK := appendlower.MatchShape(leftHandSide, rightHandSide)
	if !shapeOK {
		return refusedInPlaceAppend("shape")
	}
	if !appendlower.SameSlice(c.Info, lhsIdent, callRHS) {
		return refusedInPlaceAppend("same-slice")
	}
	location, locationOK := c.lookupInPlaceAppendTarget(lhsIdent)
	if !locationOK {
		return refusedInPlaceAppend("target")
	}
	elementKind, spread, kindOK := appendlower.ElementKind(c.Info, callRHS)
	if !kindOK {
		return refusedInPlaceAppend("element-kind")
	}
	return inPlaceAppendMatch{
		location:    location,
		callRHS:     callRHS,
		elementKind: elementKind,
		spread:      spread,
		ok:          true,
		refusal:     "",
	}
}
