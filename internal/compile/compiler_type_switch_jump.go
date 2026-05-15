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
	"context"
	"go/ast"
	"go/types"
	"math"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// typeSwitchJumpTableMinCases is the smallest clause count for which a type switch is
	// compiled as a jump table instead of a chain of type-assertion probes. Below it the
	// chain's per-probe cost is comparable to the table walk.
	typeSwitchJumpTableMinCases = 5
)

// typeSwitchJumpTableTypes reports whether a type switch qualifies for jump-table
// dispatch: every clause names exactly one concrete, non-interface, non-parameterised
// type, none is nil, named basic types (which box under pool types) are absent, and there
// are at least typeSwitchJumpTableMinCases clauses.
//
// Takes cases ([]*ast.CaseClause) which are the non-default clauses.
//
// Returns the clause types in order and true when eligible, or nil and false otherwise.
func (c *Compiler) typeSwitchJumpTableTypes(cases []*ast.CaseClause) ([]types.Type, bool) {
	if len(cases) < typeSwitchJumpTableMinCases {
		return nil, false
	}
	caseTypes := make([]types.Type, 0, len(cases))
	for _, cc := range cases {
		if len(cc.List) != 1 {
			return nil, false
		}
		caseType := c.substitutedType(c.Info.Types[cc.List[0]].Type)
		if !typeSwitchJumpTableEligibleType(caseType) {
			return nil, false
		}
		caseTypes = append(caseTypes, caseType)
	}
	return caseTypes, true
}

// compileTypeSwitchJumpTable emits a type switch as a jump-table dispatch.
//
// Emits a isa.SubOpTypeSwitchJump head followed by one isa.OpTypeSwitchCase row per
// clause and a default jump, then the clause bodies. The clause types are interned in the
// type table first so a table whose indices overflow the row's 8-bit type-index field
// falls back to the probe chain before anything is emitted.
//
// Takes cases ([]*ast.CaseClause) which are the non-default clauses.
// Takes caseTypes ([]types.Type) which are the clause types in order.
// Takes defaultCase (*ast.CaseClause) which is the default clause, or nil.
// Takes sourceLocation (VarLocation) which holds the switched value in the general bank.
// Takes assignName (string) which is the bound variable name, or empty.
//
// Returns false when the table could not be used and the chain must be compiled instead,
// and any error from compiling the clause bodies.
func (c *Compiler) compileTypeSwitchJumpTable(ctx context.Context,
	cases []*ast.CaseClause,
	caseTypes []types.Type,
	defaultCase *ast.CaseClause,
	sourceLocation program.VarLocation,
	assignName string,
) (bool, error) {
	typeIndices := make([]uint16, 0, len(caseTypes))
	for _, caseType := range caseTypes {
		typeIndex, err := program.AddTypeRefWithMethods(c.Function, c.typeAssertReflectType(ctx, caseType), nil)
		if err != nil {
			return false, err
		}
		if typeIndex > math.MaxUint8 {
			return false, nil
		}
		typeIndices = append(typeIndices, typeIndex)
	}
	destinationRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpTypeSwitchJump), destinationRegister, sourceLocation.Register)
	rowPCs := make([]int, len(cases))
	for index, typeIndex := range typeIndices {
		rowPCs[index] = program.Emit(c.Function, isa.OpTypeSwitchCase, safeconv.Uint16ToUint8(typeIndex), 0, 0)
	}
	defaultJump := program.EmitTier1Jump(c.Function)
	endJumps := make([]int, 0, len(cases))
	for index, cc := range cases {
		program.PatchJump(c.Function, rowPCs[index])
		endJump, err := c.compileTypeSwitchCaseBody(ctx, cc, destinationRegister, assignName)
		if err != nil {
			return true, err
		}
		endJumps = append(endJumps, endJump)
	}
	program.PatchJump(c.Function, defaultJump)
	if defaultCase != nil {
		if err := c.compileTypeSwitchDefault(ctx, defaultCase, sourceLocation, assignName); err != nil {
			return true, err
		}
	}
	for _, pc := range endJumps {
		program.PatchJump(c.Function, pc)
	}
	return true, nil
}

// typeSwitchJumpTableEligibleType reports whether a clause type can be matched by runtime
// type identity alone.
//
// Takes t (types.Type) which is the clause's type.
//
// Returns true for concrete struct, pointer, slice, map, array, channel, function and
// unnamed basic types; false for nil, interfaces, type parameters and named basics.
func typeSwitchJumpTableEligibleType(t types.Type) bool {
	if t == nil {
		return false
	}
	if basic, ok := t.(*types.Basic); ok && basic.Kind() == types.UntypedNil {
		return false
	}
	switch t.Underlying().(type) {
	case *types.Interface, *types.TypeParam:
		return false
	case *types.Basic:
		_, named := types.Unalias(t).(*types.Named)
		return !named
	default:
		return true
	}
}
