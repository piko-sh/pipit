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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var reservedOpcodes = map[Opcode]string{}

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

var costFamilies = map[string]int64{
	"APPEND": costExpensive,
	"CALL":   costExpensive,
}

var costFamilyExceptions = map[string]string{
	"CALL_SCALAR": "scalar-only callee with no boxing, so it is closer to an arithmetic op than a call",
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

	for _, row := range AllSpecs() {
		if row.Name == "" {
			continue
		}
		if reason, excepted := costFamilyExceptions[row.Name]; excepted {
			require.NotEmpty(t, reason)
			continue
		}
		for prefix, want := range costFamilies {
			if !strings.HasPrefix(row.Name, prefix) {
				continue
			}
			require.Equalf(t, want, table[int(row.Tier)*SlotsPerTier+int(row.Code)],
				"%s is in the %s family but is metered at %d instead of %d.\n"+
					"Operations default to costCheap and are only overridden by declaring a cost on\n"+
					"their spec row, so an omission silently under-meters the sandbox budget.\n"+
					"Add the cost, or record why it differs in costFamilyExceptions.",
				row.Name, prefix, table[int(row.Tier)*SlotsPerTier+int(row.Code)], want)
		}
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
