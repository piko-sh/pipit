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

package engine

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

type bankCrossingOutcome struct {
	integer  int64
	unsigned uint64
	truth    bool
}

func bankCrossingSource(reads isa.RegisterKind, value int64) bankCrossingOutcome {
	held := bankCrossingOutcome{integer: value + 1, unsigned: uint64(value) + 1, truth: value == 0}
	switch reads {
	case isa.RegisterInt64:
		held.integer = value
	case isa.RegisterUint64:
		held.unsigned = uint64(value)
	default:
		held.truth = value != 0
	}
	return held
}

func runBankCrossing(
	t *testing.T, subOpcode isa.SubOpcode, reads isa.RegisterKind, destination, source uint8, value int64, assembly bool,
) bankCrossingOutcome {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	instruction := isa.NewTier1Instruction(subOpcode, destination, source)
	builder.body = append(builder.body, instruction)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	vm, frame, registers := newFramedVM(t, builder.build())
	for register := range registers.Ints {
		registers.Ints[register], registers.Uints[register], registers.Bools[register] = -7, math.MaxUint64-7, register%2 == 0
	}
	held := bankCrossingSource(reads, value)
	registers.Ints[source], registers.Uints[source], registers.Bools[source] = held.integer, held.unsigned, held.truth
	runOneInstruction(t, vm, frame, registers, instruction, assembly)
	require.NoError(t, vm.evalError)
	return bankCrossingOutcome{integer: registers.Ints[destination], unsigned: registers.Uints[destination], truth: registers.Bools[destination]}
}

func TestBankCrossingConversionsWriteOneRegisterOfTheirDestinationBank(t *testing.T) {
	truth := func(holds bool) int64 {
		if holds {
			return 1
		}
		return 0
	}
	operations := []struct {
		write     func(outcome *bankCrossingOutcome, value int64)
		name      string
		subOpcode isa.SubOpcode
		reads     isa.RegisterKind
	}{
		{name: "not", subOpcode: isa.SubOpNotInt64, reads: isa.RegisterInt64, write: func(outcome *bankCrossingOutcome, value int64) { outcome.integer = truth(value == 0) }},
		{name: "bool to int", subOpcode: isa.SubOpConvertBoolToInt64, reads: isa.RegisterBool, write: func(outcome *bankCrossingOutcome, value int64) { outcome.integer = truth(value != 0) }},
		{name: "int to bool", subOpcode: isa.SubOpConvertInt64ToBool, reads: isa.RegisterInt64, write: func(outcome *bankCrossingOutcome, value int64) { outcome.truth = value != 0 }},
		{name: "int to uint", subOpcode: isa.SubOpConvertInt64ToUint64, reads: isa.RegisterInt64, write: func(outcome *bankCrossingOutcome, value int64) { outcome.unsigned = uint64(value) }},
		{name: "uint to int", subOpcode: isa.SubOpConvertUint64ToInt64, reads: isa.RegisterUint64, write: func(outcome *bankCrossingOutcome, value int64) { outcome.integer = value }},
	}
	values := []int64{0, 1, -1, 2, 255, 256, 1 << 32, 1 << 40, math.MinInt64, math.MaxInt64}
	registers := []struct{ destination, source uint8 }{{1, 2}, {3, 3}, {7, 0}}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			spec, known := isa.SpecAt(isa.TierSub1, uint8(operation.subOpcode))
			require.True(t, known)
			require.Equal(t, isa.DispatchAsm, spec.Dispatch(), "the operation has an assembly body, so it no longer leaves the loop")
			for _, pair := range registers {
				for _, value := range values {
					want := bankCrossingOutcome{integer: -7, unsigned: math.MaxUint64 - 7, truth: pair.destination%2 == 0}
					if pair.destination == pair.source {
						want = bankCrossingSource(operation.reads, value)
					}
					operation.write(&want, value)
					for _, assembly := range dispatchRoutes() {
						got := runBankCrossing(t, operation.subOpcode, operation.reads, pair.destination, pair.source, value, assembly)
						require.Equalf(t, want, got, "%s, value %d, destination %d, source %d", routeName(assembly), value, pair.destination, pair.source)
					}
				}
			}
		})
	}
}
