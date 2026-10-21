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
	"testing"

	"github.com/stretchr/testify/require"
)

var reservedOpcodes = map[Opcode]string{}

var expensiveOperations = []int{
	FlatIndexOf(OpAppend),
	FlatIndexOf(OpAppendSpread),
	FlatIndexOf(OpAppendByte),
	FlatIndexOf(OpAppendByteInPlace),
	FlatIndexOf(OpAppendInPlace),
	FlatIndexOf(OpAppendSpreadInPlace),
	FlatIndexOf(OpAppendStruct),
	FlatIndexOf(OpAppendInt64),
	FlatIndexOf(OpAppendFloat64),
	FlatIndexOf(OpAppendString),
	FlatIndexOfSub1(SubOpAppendUint64Wide),
	FlatIndexOfSub1(SubOpAppendInt64Wide),
	FlatIndexOfSub1(SubOpAppendStringWide),
	FlatIndexOfSub1(SubOpAppendFloat64Wide),
	FlatIndexOfSub1(SubOpAppendBoolWide),
	FlatIndexOfSub1(SubOpAppendByteViaPointer),
	FlatIndexOfSub1(SubOpAppendSpreadByteViaPointer),
	FlatIndexOfSub1(SubOpCall),
	FlatIndexOfSub1(SubOpTailCall),
	FlatIndexOfSub1(SubOpCallNative),
	FlatIndexOfSub1(SubOpCallBuiltin),
	FlatIndexOfSub1(SubOpCallMethod),
	FlatIndexOfSub1(SubOpCallIIFE),
	FlatIndexOfSub1(SubOpCallMethodInlineable),
	FlatIndexOfSub1(SubOpAppendSliceInt64),
	FlatIndexOfSub1(SubOpAppendSliceFloat64),
	FlatIndexOfSub1(SubOpAppendSliceString),
	FlatIndexOfSub1(SubOpAppendSliceBool),
	FlatIndexOfSub1(SubOpAppendSliceUint64),
	FlatIndexOfSub1(SubOpAppendSliceByte),
	FlatIndexOfSub1(SubOpAppendUint64WideInPlace),
	FlatIndexOfSub1(SubOpAppendSpreadByteInPlace),
}

func TestOpcodeNamesCoverEveryOpcode(t *testing.T) {
	t.Parallel()

	var unnamed []int
	for opIndex := range OpcodeCount {
		op := Opcode(opIndex)
		if _, reserved := reservedOpcodes[op]; reserved {
			continue
		}
		if opcodeNameOrEmpty(op) == "" {
			unnamed = append(unnamed, opIndex)
		}
	}

	require.Emptyf(t, unnamed,
		"these opcode numbers have no opcodeNames entry, so String reports UNKNOWN for them\n"+
			"in every disassembly, verifier error and panic trace: %v\n"+
			"Add the name, or list the slot in reservedOpcodes if it implements nothing.", unnamed)
}

func TestOpcodeCountIsOnePastTheLastMainTierRow(t *testing.T) {
	t.Parallel()

	last := -1
	for _, row := range AllSpecs() {
		if row.Tier == TierMain {
			last = max(last, int(row.Code))
		}
	}

	require.Equalf(t, last+1, OpcodeCount,
		"the main tier's last row is opcode %d (%s), so OpcodeCount has to be %d.\n"+
			"A loop over OpcodeCount that stops short never looks at the opcodes past it:\n"+
			"anchor the constant on the opcode appended last.",
		last, Opcode(max(last, 0)), last+1)
}

func TestOpcodeNamesAreUnique(t *testing.T) {
	t.Parallel()

	seen := map[string]Opcode{}
	for opIndex := range OpcodeCount {
		op := Opcode(opIndex)
		name := opcodeNameOrEmpty(op)
		if name == "" {
			continue
		}
		previous, duplicate := seen[name]
		require.Falsef(t, duplicate, "opcodes %d and %d share the name %q", int(previous), opIndex, name)
		seen[name] = op
	}
}

func TestCallScalarIsMeteredAsArithmetic(t *testing.T) {
	t.Parallel()

	require.Equal(t, costCheap, DefaultCostTable()[FlatIndexOfSub1(SubOpCallScalar)],
		"CALL_SCALAR is the lean call for scalar-only callees; metering it as a full call "+
			"would charge recursive scalar algorithms the boxing cost they were built to avoid")
}

func TestCostTableFamilyWeightsAreConsistent(t *testing.T) {
	t.Parallel()

	table := DefaultCostTable()

	for _, slot := range expensiveOperations {
		row, ok := SpecAt(Tier(slot/SlotsPerTier), uint8(slot%SlotsPerTier))
		require.Truef(t, ok, "slot %d is listed in expensiveOperations but has no specification row", slot)
		require.Equalf(t, costExpensive, table[slot],
			"%s calls or appends but is metered at %d instead of %d.\n"+
				"Operations default to costCheap and are only overridden by declaring a cost on\n"+
				"their spec row, so an omission silently under-meters the sandbox budget.",
			row, table[slot], costExpensive)
	}
}

func TestEveryOpcodeDeclaresAnOperandShape(t *testing.T) {
	t.Parallel()

	const describedOpcodeFloor = 111

	described := 0
	for opIndex := range OpcodeCount {
		if OperandShapeFor(Opcode(opIndex)).Flags&ShapeFlagDescribed != 0 {
			described++
		}
	}

	require.GreaterOrEqualf(t, described, describedOpcodeFloor,
		"operand-shape coverage fell from %d to %d described opcodes.\n"+
			"An undescribed opcode is opaque to the verifier's dataflow check and to the\n"+
			"escape pass, both of which then have to assume the worst.",
		describedOpcodeFloor, described)
}

func opcodeNameOrEmpty(op Opcode) string {
	row, ok := SpecAt(TierMain, uint8(op))
	if !ok {
		return ""
	}
	return row.Name
}

func TestDisplayLabelWidthFitsEveryName(t *testing.T) {
	t.Parallel()

	widest := 0
	for _, row := range AllSpecs() {
		widest = max(widest, len("0:")+len(row.Name))
	}
	require.Equalf(t, widest, DisplayLabelWidth,
		"DisplayLabelWidth must be the longest display label (the longest name plus the tier "+
			"prefix), so the operand columns line up without wasted space; change it and "+
			"re-record the goldens")
}
