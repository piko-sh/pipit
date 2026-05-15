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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

const fusedJumpOffset int16 = 11

func newFusedJumpFrame(t *testing.T, constants []int64) (*VM, *CallFrame, *Registers) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.intConstants = constants
	builder.body = append(builder.body, isa.NewTier1Instruction(isa.SubOpEqIntConstJumpFalse, 1, 0))
	low, high := isa.SplitOffset(fusedJumpOffset)
	builder.body = append(builder.body, isa.NewInstruction(isa.OpExt, low, high, 0))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	vm, frame, registers := newFramedVM(t, builder.build())
	frame.ProgramCounter = 1
	return vm, frame, registers
}

func TestFusedConstantComparisonsBranchOnTheirOwnCondition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler  opcodeHandler
		name     string
		register int64
		constant int64
		wantJump bool
	}{
		{name: "equality is false so the branch is taken", handler: handleEqIntConstJumpFalse, register: 1, constant: 2, wantJump: true},
		{name: "equality is true so the branch is skipped", handler: handleEqIntConstJumpFalse, register: 2, constant: 2, wantJump: false},
		{name: "equality is true so the true-branch is taken", handler: handleEqIntConstJumpTrue, register: 2, constant: 2, wantJump: true},
		{name: "equality is false so the true-branch is skipped", handler: handleEqIntConstJumpTrue, register: 1, constant: 2, wantJump: false},
		{name: "greater or equal is false so the branch is taken", handler: handleGeIntConstJumpFalse, register: 1, constant: 2, wantJump: true},
		{name: "greater or equal is true so the branch is skipped", handler: handleGeIntConstJumpFalse, register: 3, constant: 2, wantJump: false},
		{name: "greater or equal holds when equal", handler: handleGeIntConstJumpFalse, register: 2, constant: 2, wantJump: false},
		{name: "greater than is false so the branch is taken", handler: handleGtIntConstJumpFalse, register: 2, constant: 2, wantJump: true},
		{name: "greater than is true so the branch is skipped", handler: handleGtIntConstJumpFalse, register: 3, constant: 2, wantJump: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newFusedJumpFrame(t, []int64{tt.constant})
			registers.Ints[1] = tt.register

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 0)))

			want := 2
			if tt.wantJump {
				want += int(fusedJumpOffset)
			}
			require.Equal(t, want, frame.ProgramCounter,
				"the extension word is always consumed; the offset is added only when the branch is taken")
		})
	}

	t.Run("a constant index past the pool is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, []int64{1})

		got := handleEqIntConstJumpFalse(vm, frame, registers, op(0, 1, 9))

		require.Equal(t, opPanicError, got)
		require.Error(t, vm.evalError)
	})
}

func TestFusedArithmeticAndBranchCombinationsAdvanceTheCounter(t *testing.T) {
	t.Parallel()

	t.Run("an addition with a constant always branches", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, []int64{10})
		registers.Ints[1] = 32

		require.Equal(t, opContinue, handleAddIntJump(vm, frame, registers, op(0, 1, 0)))
		require.Equal(t, int64(42), registers.Ints[0])
		require.Equal(t, 2+int(fusedJumpOffset), frame.ProgramCounter,
			"the fused add-and-jump is an unconditional loop edge")
	})

	t.Run("an addition with a constant index past the pool is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, []int64{10})

		require.Equal(t, opPanicError, handleAddIntJump(vm, frame, registers, op(0, 1, 9)))
		require.Error(t, vm.evalError)
	})

	t.Run("an increment branches while it stays below the limit", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, nil)
		registers.Ints[0] = 1
		registers.Ints[1] = 5

		require.Equal(t, opContinue, handleIncIntJumpLt(vm, frame, registers, op(0, 1, 0)))
		require.Equal(t, int64(2), registers.Ints[0])
		require.Equal(t, 2+int(fusedJumpOffset), frame.ProgramCounter)
	})

	t.Run("an increment stops branching once it reaches the limit", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, nil)
		registers.Ints[0] = 4
		registers.Ints[1] = 5

		require.Equal(t, opContinue, handleIncIntJumpLt(vm, frame, registers, op(0, 1, 0)))
		require.Equal(t, int64(5), registers.Ints[0])
		require.Equal(t, 2, frame.ProgramCounter, "the loop edge is not taken once the counter reaches the limit")
	})

	t.Run("a string length comparison branches when the loop condition fails", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, nil)
		registers.Ints[0] = 5
		registers.Strings[1] = "pipit"

		require.Equal(t, opContinue, handleLenStringLtJumpFalse(vm, frame, registers, op(0, 1, 0)))
		require.Equal(t, 2+int(fusedJumpOffset), frame.ProgramCounter)
	})

	t.Run("a string length comparison continues while the counter is inside the string", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, nil)
		registers.Ints[0] = 2
		registers.Strings[1] = "pipit"

		require.Equal(t, opContinue, handleLenStringLtJumpFalse(vm, frame, registers, op(0, 1, 0)))
		require.Equal(t, 2, frame.ProgramCounter)
	})

	t.Run("a multiplication by a constant writes the destination", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, []int64{6})
		registers.Ints[1] = 7

		require.Equal(t, opContinue, handleMulIntConst(vm, frame, registers, op(0, 1, 0)))
		require.Equal(t, int64(42), registers.Ints[0])
	})

	t.Run("a multiplication with a constant index past the pool is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, []int64{6})

		require.Equal(t, opPanicError, handleMulIntConst(vm, frame, registers, op(0, 1, 9)))
		require.Error(t, vm.evalError)
	})
}

func TestUnwrapToStructFollowsPointersAndInterfaces(t *testing.T) {
	t.Parallel()

	var boxed any = leafCounter{Signed: 1}
	interfaceValue := reflect.ValueOf(&boxed).Elem()

	tests := []struct {
		value reflect.Value
		name  string
		want  reflect.Kind
	}{
		{name: "a struct passes through", value: reflect.ValueOf(leafCounter{}), want: reflect.Struct},
		{name: "a pointer is followed", value: reflect.ValueOf(&leafCounter{}), want: reflect.Struct},
		{name: "an interface is unwrapped", value: interfaceValue, want: reflect.Struct},
		{name: "a nil pointer yields an invalid value", value: reflect.ValueOf((*leafCounter)(nil)), want: reflect.Invalid},
		{name: "an integer passes through unchanged", value: reflect.ValueOf(42), want: reflect.Int},
		{name: "an invalid value stays invalid", value: reflect.Value{}, want: reflect.Invalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, unwrapToStruct(tt.value).Kind())
		})
	}
}

func TestStructFieldReceiverIsNilPointerSpotsEveryNilLayer(t *testing.T) {
	t.Parallel()

	var nilBoxed any = (*leafCounter)(nil)
	interfaceOverNil := reflect.ValueOf(&nilBoxed).Elem()

	tests := []struct {
		value reflect.Value
		name  string
		want  bool
	}{
		{name: "a nil pointer is reported", value: reflect.ValueOf((*leafCounter)(nil)), want: true},
		{name: "an interface holding a nil pointer is reported", value: interfaceOverNil, want: true},
		{name: "a live pointer is not reported", value: reflect.ValueOf(&leafCounter{}), want: false},
		{name: "a struct value is not reported", value: reflect.ValueOf(leafCounter{}), want: false},
		{name: "an invalid value is not reported", value: reflect.Value{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, structFieldReceiverIsNilPointer(tt.value))
		})
	}
}

func TestMakeStructAddressableCopiesWhenItMust(t *testing.T) {
	t.Parallel()

	t.Run("a pointer receiver is already addressable", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		value := reflect.ValueOf(&leafCounter{Signed: 1})

		got, ok := makeStructAddressable(&registers, 0, value)

		require.True(t, ok)
		require.True(t, got.CanAddr())
	})

	t.Run("a value receiver is copied into an addressable slot", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		value := reflect.ValueOf(leafCounter{Signed: 1})

		got, ok := makeStructAddressable(&registers, 0, value)

		require.True(t, ok)
		require.True(t, got.CanAddr())
		require.True(t, registers.General[0].CanAddr(),
			"the register is rewritten so later writes reach the same copy")
	})

	t.Run("a nil pointer cannot be made addressable", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()

		_, ok := makeStructAddressable(&registers, 0, reflect.ValueOf((*leafCounter)(nil)))

		require.False(t, ok)
	})

	t.Run("a non-struct cannot be made addressable", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()

		_, ok := makeStructAddressable(&registers, 0, reflect.ValueOf(42))

		require.False(t, ok)
	})
}
