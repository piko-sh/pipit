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

package isa

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var enumBlocks = map[string]Tier{"Opcode": TierMain, "SubOpcode": TierSub1, "SubOpcodeTier2": TierSub2, "SubOpcodeTier3": TierSub3}

func constantsPerTier(t *testing.T) map[Tier][]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "opcode.go", nil, 0)
	require.NoError(t, err)
	out := map[Tier][]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST || len(gen.Specs) == 0 {
			continue
		}
		first, ok := gen.Specs[0].(*ast.ValueSpec)
		if !ok || first.Type == nil {
			continue
		}
		ident, ok := first.Type.(*ast.Ident)
		if !ok {
			continue
		}
		tier, ok := enumBlocks[ident.Name]
		if !ok || len(first.Values) != 1 {
			continue
		}
		if value, isIdent := first.Values[0].(*ast.Ident); !isIdent || value.Name != "iota" {
			continue
		}
		for _, spec := range gen.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				out[tier] = append(out[tier], name.Name)
			}
		}
	}
	return out
}

func TestSpecHasOneRowPerEnumConstant(t *testing.T) {
	t.Parallel()
	for tier, constants := range constantsPerTier(t) {
		require.NotEmptyf(t, constants, "tier %d has no enum block", tier)
		for index, constant := range constants {
			row, ok := SpecAt(tier, uint8(index))
			require.Truef(t, ok, "%s (tier %d code %d) has no spec row", constant, tier, index)
			if row.Has(specReserved) {
				require.Containsf(t, constant, "Reserved", "%s is a reserved row but its constant does not say so", constant)
				continue
			}
			require.Falsef(t, strings.Contains(constant, "Reserved"), "%s must be a reserved row", constant)
			require.NotEmptyf(t, row.Name, "row for %s has no name", constant)
		}
		for code := len(constants); code < 256; code++ {
			_, ok := SpecAt(tier, uint8(code))
			require.Falsef(t, ok, "tier %d code %d has a row but no enum constant", tier, code)
		}
	}
}

func TestSpecNamesAreUniqueWithinTier(t *testing.T) {
	t.Parallel()
	seen := map[Tier]map[string]uint8{}
	for _, row := range AllSpecs() {
		if row.Name == "" {
			continue
		}
		if seen[row.Tier] == nil {
			seen[row.Tier] = map[string]uint8{}
		}
		if other, dup := seen[row.Tier][row.Name]; dup {
			t.Errorf("tier %d: codes %d and %d share the name %q", row.Tier, other, row.Code, row.Name)
		}
		seen[row.Tier][row.Name] = row.Code
	}
}

func TestSpecCostsMatchDefaultCostTable(t *testing.T) {
	t.Parallel()
	table := DefaultCostTable()
	for _, row := range AllSpecs() {
		require.Equalf(t, table[int(row.Tier)*SlotsPerTier+int(row.Code)], row.Cost, "cost of %s", row)
	}
}

func TestSpecRowsAreConsistent(t *testing.T) {
	t.Parallel()
	for _, row := range AllSpecs() {
		require.Falsef(t, row.AsmBody != "" && row.ExitStub != "", "%s has both an assembly body and an exit stub", row)
		require.Equalf(t, row.Shim == "", row.ShimHandler == "", "%s must name a shim handler exactly when it names a shim", row)
		require.Equalf(t, row.ExitStub == "", row.ExitReason == "", "%s must name an exit reason exactly when it names a stub", row)
		if row.Has(SpecShimNarrow) || row.Has(SpecShimSuppressed) {
			require.NotEmptyf(t, row.Shim, "%s carries shim flags without a shim", row)
		}
		if row.Has(SpecShimSuppressed) {
			require.NotEmptyf(t, row.AsmBody, "%s suppresses its shim without an assembly body to own the slot", row)
		}
		if row.Has(specMeta) {
			require.Zerof(t, row.Code, "%s is a drill marker but not at code 0", row)
		}
		if row.Has(specReserved) {
			require.Emptyf(t, row.Handler, "reserved %s must have no handler", row)
			require.Emptyf(t, row.AsmBody, "reserved %s must have no assembly body", row)
		}
	}
}

func TestSpecForInstructionResolvesTheDrillCascade(t *testing.T) {
	t.Parallel()
	row, ok := SpecForInstruction(Instruction{Op: OpAddInt, A: 0, B: 0, C: 0})
	require.True(t, ok)
	require.Equal(t, "ADD_INT", row.Name)

	row, ok = SpecForInstruction(Instruction{Op: OpDrillTier1, A: uint8(SubOpJump), B: 0, C: 0})
	require.True(t, ok)
	require.Equal(t, TierSub1, row.Tier)
	require.True(t, row.Has(SpecJump))

	row, ok = SpecForInstruction(Instruction{Op: OpDrillTier1, A: uint8(SubOpDrillTier2), B: uint8(SubOpTier2DrillTier3), C: uint8(SubOpTier3Nop)})
	require.True(t, ok)
	require.Equal(t, TierSub3, row.Tier)
}

func TestSpecRowsClassifyTheirMemoryEffect(t *testing.T) {
	for _, rows := range [][]OpSpec{tierMainSpecs, tierSub1Specs, tierSub2Specs, tierSub3Specs} {
		for _, row := range rows {
			if row.Has(specMeta) || row.Has(specReserved) {
				continue
			}
			mutates := row.Has(specMutatesMemory)
			pure := row.Has(specMemoryPure)
			require.False(t, mutates && pure,
				"%s claims both SpecMutatesMemory and SpecMemoryPure", row.Name)
			require.True(t, mutates || pure,
				"%s carries neither SpecMutatesMemory nor SpecMemoryPure. Every operation must "+
					"state whether it can mutate memory beyond its own register writes, because "+
					"passes.InstructionDirectlyMutatesHeap derives from this and treats an "+
					"unclassified operation as mutating. Add .mutates() or .pure() to its row.",
				row.Name)
		}
	}
}

func TestSpecMemoryClassificationSurvivesTheDrillCascade(t *testing.T) {
	mutating := NewInstruction(OpSliceSetInt, 0, 0, 0)
	row, ok := SpecForInstruction(mutating)
	require.True(t, ok)
	mutates, classified := row.MutatesMemory()
	require.True(t, classified)
	require.True(t, mutates, "a typed element store mutates through its collection operand")

	pure := NewInstruction(OpAddInt, 0, 0, 0)
	row, ok = SpecForInstruction(pure)
	require.True(t, ok)
	mutates, classified = row.MutatesMemory()
	require.True(t, classified)
	require.False(t, mutates)

	tier1 := NewInstruction(OpDrillTier1, uint8(SubOpSetStructFieldInt), 0, 0)
	row, ok = SpecForInstruction(tier1)
	require.True(t, ok)
	mutates, _ = row.MutatesMemory()
	require.True(t, mutates, "a tier-1 struct-field write must resolve through the cascade")
}

func TestSpecUnclassifiedRowReportsAsMutating(t *testing.T) {
	var unclassified OpSpec
	mutates, classified := unclassified.MutatesMemory()
	require.False(t, classified)
	require.True(t, mutates, "an unclassified operation must fail closed")
}
