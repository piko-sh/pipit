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
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var enumBlocks = map[string]Tier{"Opcode": TierMain, "SubOpcode": TierSub1, "SubOpcodeTier2": TierSub2, "SubOpcodeTier3": TierSub3}

var tierConstantPrefixes = [tierCount]string{"Op", "SubOp", "SubOpTier2", "SubOpTier3"}

var abbreviatedNameWords = map[string]string{
	"INC": "INCREMENT", "DEC": "DECREMENT", "SUB": "SUBTRACT", "MUL": "MULTIPLY", "DIV": "DIVIDE",
	"REM": "REMAINDER", "NEG": "NEGATE", "CONST": "CONSTANT", "ALLOC": "ALLOCATE",
	"DEREF": "DEREFERENCE", "EXT": "EXTENSION", "INIT": "INITIALISE", "UPVALUE": "CAPTURED_VARIABLE",
	"UPVALUES": "CAPTURED_VARIABLES", "EQ": "EQUAL", "NE": "NOT_EQUAL", "LT": "LESS_THAN",
	"LE": "LESS_OR_EQUAL", "GT": "GREATER_THAN", "GE": "GREATER_OR_EQUAL", "LEN": "LENGTH",
	"CAP": "CAPACITY", "ADDR": "ADDRESS", "TRUNC": "TRUNCATE", "NOP": "NO_OPERATION",
	"IMAG": "IMAGINARY_PART", "CONCAT": "CONCATENATE", "SYNC": "SYNCHRONISE",
	"INT": "INT64", "UINT": "UINT64", "FLOAT": "FLOAT64", "COMPLEX": "COMPLEX128",
}

var goFunctionNamePackages = []string{"MATH_", "STRINGS_", "STRCONV_", "UNSAFE_"}

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
			require.NotContainsf(t, constant, "Reserved", "%s must be a reserved row", constant)
			require.NotEmptyf(t, row.Name, "row for %s has no name", constant)
		}
		for code := len(constants); code < 256; code++ {
			_, ok := SpecAt(tier, uint8(code))
			require.Falsef(t, ok, "tier %d code %d has a row but no enum constant", tier, code)
		}
	}
}

func TestSpecNamesAreUnique(t *testing.T) {
	t.Parallel()
	seen := map[string]OpSpec{}
	for _, row := range AllSpecs() {
		if row.Name == "" {
			continue
		}
		other, dup := seen[row.Name]
		assert.Falsef(t, dup, "%s and %s share the name %q; names are unique across every tier", other, row, row.Name)
		seen[row.Name] = row
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
	row, ok := SpecForInstruction(Instruction{Op: OpAddInt64, A: 0, B: 0, C: 0})
	require.True(t, ok)
	require.Equal(t, "ADD_INT64", row.Name)

	row, ok = SpecForInstruction(Instruction{Op: OpDrillTier1, A: uint8(SubOpJump), B: 0, C: 0})
	require.True(t, ok)
	require.Equal(t, TierSub1, row.Tier)
	require.True(t, row.Has(SpecJump))

	row, ok = SpecForInstruction(Instruction{Op: OpDrillTier1, A: uint8(SubOpDrillTier2), B: uint8(SubOpTier2DrillTier3), C: uint8(SubOpTier3NoOperation)})
	require.True(t, ok)
	require.Equal(t, TierSub3, row.Tier)
}

func TestSpecRowsClassifyTheirMemoryEffect(t *testing.T) {
	t.Parallel()
	for _, rows := range [][]OpSpec{tierMainSpecs, tierSub1Specs, tierSub2Specs, tierSub3Specs} {
		for _, row := range rows {
			if row.Has(specMeta) || row.Has(specReserved) {
				continue
			}
			mutates := row.Has(specMutatesMemory)
			pure := row.Has(specMemoryPure)
			assert.False(t, mutates && pure,
				"%s claims both SpecMutatesMemory and SpecMemoryPure", row.Name)
			assert.True(t, mutates || pure,
				"%s carries neither SpecMutatesMemory nor SpecMemoryPure. Every operation must "+
					"state whether it can mutate memory beyond its own register writes, because "+
					"passes.InstructionDirectlyMutatesHeap derives from this and treats an "+
					"unclassified operation as mutating. Add .mutates() or .pure() to its row.",
				row.Name)
		}
	}
}

func TestSpecMemoryClassificationSurvivesTheDrillCascade(t *testing.T) {
	t.Parallel()
	mutating := NewInstruction(OpSetElementInt64, 0, 0, 0)
	row, ok := SpecForInstruction(mutating)
	require.True(t, ok)
	mutates, classified := row.MutatesMemory()
	require.True(t, classified)
	require.True(t, mutates, "a typed element store mutates through its collection operand")

	pure := NewInstruction(OpAddInt64, 0, 0, 0)
	row, ok = SpecForInstruction(pure)
	require.True(t, ok)
	mutates, classified = row.MutatesMemory()
	require.True(t, classified)
	require.False(t, mutates)

	tier1 := NewInstruction(OpDrillTier1, uint8(SubOpSetStructFieldInt64Wide), 0, 0)
	row, ok = SpecForInstruction(tier1)
	require.True(t, ok)
	mutates, _ = row.MutatesMemory()
	require.True(t, mutates, "a tier-1 struct-field write resolves through the cascade")
}

func TestSpecUnclassifiedRowReportsAsMutating(t *testing.T) {
	t.Parallel()
	var unclassified OpSpec
	mutates, classified := unclassified.MutatesMemory()
	require.False(t, classified)
	require.True(t, mutates, "an unclassified operation fails closed")
}

func TestDispatchForInstructionFollowsTheRowExceptForTheSkippedNoOp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		instruction Instruction
		want        DispatchPath
	}{
		{name: "an assembly body", instruction: NewInstruction(OpAddInt64, 0, 1, 2), want: DispatchAsm},
		{name: "an in-loop shim", instruction: NewInstruction(OpMakeClosure, 0, 0, 0), want: DispatchShim},
		{name: "a dedicated exit", instruction: NewInstruction(OpGetMap, 0, 1, 2), want: DispatchExit},
		{name: "a generic exit", instruction: NewTier1Instruction(SubOpCallNative, 0, 0), want: DispatchGo},
		{name: "the no-op the dispatch sequence skips", instruction: NewInstruction(OpNoOperation, 0, 0, 0), want: DispatchAsm},
		{name: "a word with no row", instruction: NewInstruction(Opcode(math.MaxUint8), 0, 0, 0), want: DispatchNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, DispatchForInstruction(tt.instruction))
		})
	}

	row, ok := SpecForInstruction(NewInstruction(OpNoOperation, 0, 0, 0))
	require.True(t, ok)
	require.Equal(t, DispatchGo, row.Dispatch(),
		"the no-op row has no body of its own; if it gains one, DispatchForInstruction no longer needs its exception")
}

func TestTypedMakesDispatchThroughTheValidatedGoHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		subOp SubOpcode
	}{
		{name: "int", subOp: SubOpMakeSliceInt64},
		{name: "float", subOp: SubOpMakeSliceFloat64},
		{name: "string", subOp: SubOpMakeSliceString},
		{name: "bool", subOp: SubOpMakeSliceBool},
		{name: "uint", subOp: SubOpMakeSliceUint64},
		{name: "byte", subOp: SubOpMakeSliceByte},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			row, ok := SpecAt(TierSub1, uint8(tt.subOp))
			require.True(t, ok)
			require.Equal(t, DispatchShim, row.Dispatch(), "a hand-written assembly body would skip the handler's length, capacity and limit checks")
			require.Equal(t, "Handle"+strings.TrimPrefix(row.Handler, "handle"), row.ShimHandler, "the shim must reach the handler the safe build runs")
		})
	}
}

func TestSpecSymbolsDeriveFromThePrintedName(t *testing.T) {
	t.Parallel()
	constants := constantsPerTier(t)
	for _, row := range AllSpecs() {
		if row.Has(specReserved) {
			continue
		}
		require.Lessf(t, int(row.Code), len(constants[row.Tier]), "%s has no enum constant", row)
		constant := constants[row.Tier][row.Code]
		stem := symbolStem(row.Name)
		symbols := []struct {
			symbol string

			got string

			want string
		}{
			{symbol: "constant", got: constant, want: tierConstantPrefixes[row.Tier] + stem},
			{symbol: "handler", got: row.Handler, want: "handle" + stem},
			{symbol: "asm", got: row.AsmBody, want: "handler" + stem},
			{symbol: "shim", got: row.Shim, want: stem},
			{symbol: "shim handler", got: row.ShimHandler, want: "Handle" + stem},
			{symbol: "exit stub", got: row.ExitStub, want: "handler" + stem + "Exit"},
			{symbol: "exit reason", got: row.ExitReason, want: "exit" + stem},
		}
		for _, symbol := range symbols {
			if symbol.got == "" {
				continue
			}
			assert.Equalf(t, symbol.want, symbol.got,
				"%s: the %s must derive from the printed name %q; regenerate spec_names_generated.go with make generate-engine",
				row, symbol.symbol, row.Name)
		}
	}
}

func TestPrintedNamesSpellEveryWordOut(t *testing.T) {
	t.Parallel()
	for _, row := range AllSpecs() {
		if row.Name == "" || slices.ContainsFunc(goFunctionNamePackages, func(prefix string) bool {
			return strings.HasPrefix(row.Name, prefix)
		}) {
			continue
		}
		for word := range strings.SplitSeq(row.Name, "_") {
			if full, abbreviated := abbreviatedNameWords[word]; abbreviated {
				assert.Failf(t, "abbreviated word in a printed name",
					"%s abbreviates %s. Spell it %s, as the naming grammar in the package documentation says.", row, word, full)
			}
		}
	}
}

func TestSymbolStemSplitsThePrintedNameIntoWords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want string
	}{
		{name: "ADD_INT64", want: "AddInt64"},
		{name: "ADD_INT32", want: "AddInt32"},
		{name: "CALL_IIFE", want: "CallIIFE"},
		{name: "GET_MAP_INT64_INT64_OK_THEN_JUMP_IF_FALSE", want: "GetMapInt64Int64OkThenJumpIfFalse"},
		{name: "DRILL_TIER1", want: "DrillTier1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, symbolStem(tt.name))
		})
	}
}

func TestDerivedSymbolsFollowTheRowsDeclarations(t *testing.T) {
	t.Parallel()
	row := spec(TierSub1, uint8(SubOpLoadInt64Immediate)).asmIn("tier1JumpTable").shim().exit()
	deriveSymbols(&row)
	require.Equal(t, "LOAD_INT64_IMMEDIATE", row.Name)
	require.Equal(t, "handleLoadInt64Immediate", row.Handler)
	require.Equal(t, "handlerLoadInt64Immediate", row.AsmBody)
	require.Equal(t, "tier1JumpTable", row.AsmTable)
	require.Equal(t, "LoadInt64Immediate", row.Shim)
	require.Equal(t, "HandleLoadInt64Immediate", row.ShimHandler)
	require.Equal(t, "handlerLoadInt64ImmediateExit", row.ExitStub)
	require.Equal(t, "exitLoadInt64Immediate", row.ExitReason)

	plain := spec(TierMain, uint8(OpTypeSwitchCase)).noGoHandler()
	deriveSymbols(&plain)
	require.Equal(t, "TYPE_SWITCH_CASE", plain.Name)
	require.Empty(t, plain.Handler, "a table row has no Go handler")
	require.Empty(t, plain.AsmBody)
	require.Empty(t, plain.Shim)
	require.Empty(t, plain.ExitStub)

	marker := spec(TierMain, uint8(OpDrillTier1)).noGoHandler().flags(specMeta)
	deriveSymbols(&marker)
	require.Equal(t, "DRILL_TIER1", marker.Name)
	require.Empty(t, marker.Handler, "a drill marker has no Go handler")
}

func TestOnlyDrillMarkersAndTableRowsLackAGoHandler(t *testing.T) {
	t.Parallel()
	var without []string
	for _, row := range AllSpecs() {
		if row.Handler == "" && !row.Has(specReserved) {
			without = append(without, row.Name)
		}
	}
	require.ElementsMatch(t, []string{"DRILL_TIER1", "DRILL_TIER2", "DRILL_TIER3", "TYPE_SWITCH_CASE"}, without,
		"every operation the dispatch loop runs has a Go handler; a row without one must be a drill marker or a table row")
}

func TestInstructionMutatesMemoryAgreesWithTheSpecRow(t *testing.T) {
	t.Parallel()

	for slot := range FlatDispatchSlots {
		instr := instructionForFlatSlot(slot)
		if FlatIndexFor(instr) != slot {
			continue
		}
		row, found := SpecForInstruction(instr)
		mutates, classified := InstructionMutatesMemory(instr)
		if !found {
			require.True(t, mutates, "slot %d: an operation with no row is treated as mutating", slot)
			require.False(t, classified, "slot %d", slot)
			continue
		}
		wantMutates, wantClassified := row.MutatesMemory()
		require.Equal(t, wantMutates, mutates, "%s", row.Name)
		require.Equal(t, wantClassified, classified, "%s", row.Name)
	}
}
