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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

const loadZeroTarget = 2

func runLoadZero(t *testing.T, kind isa.RegisterKind, assembly bool) *Registers {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	instruction := isa.NewTier1Instruction(isa.SubOpLoadZero, loadZeroTarget, uint8(kind))
	builder.body = append(builder.body, instruction)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	vm, frame, registers := newFramedVM(t, builder.build())
	for register := range registers.Ints {
		registers.Ints[register], registers.Uints[register], registers.Floats[register] = -7, math.MaxUint64-7, 7.5
		registers.Bools[register], registers.Strings[register], registers.Complex[register] = true, "kept", complex(1, 2)
		registers.General[register] = reflect.ValueOf(register)
		registers.SlicesInt[register], registers.slicesFloat[register], registers.slicesString[register] = []int64{1}, []float64{1}, []string{"kept"}
		registers.slicesBool[register], registers.slicesUint[register], registers.slicesByte[register] = []bool{true}, []uint64{1}, []byte{1}
	}
	runOneInstruction(t, vm, frame, registers, instruction, assembly)
	require.NoError(t, vm.evalError)
	return registers
}

func TestLoadZeroClearsOneRegisterOfOneBankAndNothingElse(t *testing.T) {
	spec, known := isa.SpecAt(isa.TierSub1, uint8(isa.SubOpLoadZero))
	require.True(t, known)
	require.Equal(t, isa.DispatchAsm, spec.Dispatch(), "the scalar banks are zeroed without leaving the loop")

	tests := []struct {
		isZero func(registers *Registers, register int) bool
		name   string
		kind   isa.RegisterKind
	}{
		{name: "int", kind: isa.RegisterInt64, isZero: func(registers *Registers, register int) bool { return registers.Ints[register] == 0 }},
		{name: "uint", kind: isa.RegisterUint64, isZero: func(registers *Registers, register int) bool { return registers.Uints[register] == 0 }},
		{name: "float", kind: isa.RegisterFloat64, isZero: func(registers *Registers, register int) bool { return registers.Floats[register] == 0 }},
		{name: "bool", kind: isa.RegisterBool, isZero: func(registers *Registers, register int) bool { return !registers.Bools[register] }},
		{name: "string", kind: isa.RegisterString, isZero: func(registers *Registers, register int) bool { return registers.Strings[register] == "" }},
		{name: "general", kind: isa.RegisterGeneral, isZero: func(registers *Registers, register int) bool { return !registers.General[register].IsValid() }},
		{name: "complex", kind: isa.RegisterComplex128, isZero: func(registers *Registers, register int) bool { return registers.Complex[register] == 0 }},
		{name: "slice of int", kind: isa.RegisterSliceInt64, isZero: func(registers *Registers, register int) bool { return registers.SlicesInt[register] == nil }},
		{name: "slice of float", kind: isa.RegisterSliceFloat64, isZero: func(registers *Registers, register int) bool { return registers.slicesFloat[register] == nil }},
		{name: "slice of string", kind: isa.RegisterSliceString, isZero: func(registers *Registers, register int) bool { return registers.slicesString[register] == nil }},
		{name: "slice of bool", kind: isa.RegisterSliceBool, isZero: func(registers *Registers, register int) bool { return registers.slicesBool[register] == nil }},
		{name: "slice of uint", kind: isa.RegisterSliceUint64, isZero: func(registers *Registers, register int) bool { return registers.slicesUint[register] == nil }},
		{name: "slice of byte", kind: isa.RegisterSliceByte, isZero: func(registers *Registers, register int) bool { return registers.slicesByte[register] == nil }},
	}
	for _, zeroed := range tests {
		t.Run(zeroed.name, func(t *testing.T) {
			for _, assembly := range dispatchRoutes() {
				registers := runLoadZero(t, zeroed.kind, assembly)
				for _, bank := range tests {
					for register := range registers.Ints {
						want := bank.kind == zeroed.kind && register == loadZeroTarget
						require.Equalf(t, want, bank.isZero(registers, register),
							"%s: register %d of the %s bank after zeroing one %s register", routeName(assembly), register, bank.name, zeroed.name)
					}
				}
			}
		})
	}
	t.Run("a bank that does not exist", func(t *testing.T) {
		for _, assembly := range dispatchRoutes() {
			registers := runLoadZero(t, isa.RegisterKind(isa.NumRegisterKinds), assembly)
			for _, bank := range tests {
				for register := range registers.Ints {
					require.Falsef(t, bank.isZero(registers, register), "%s: nothing is zeroed, the %s bank included", routeName(assembly), bank.name)
				}
			}
		}
	})
}
