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
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const fusedJumpOffset int16 = 11

type fieldRunPixel struct {
	Red   uint8
	Green uint8
	Blue  uint8
	Alpha uint8
	Wide  uint16
	Count uint32
	Large uint64
}

const elementFieldRunReceiver = 6

func newFusedJumpFrame(t *testing.T, constants []int64) (*VM, *CallFrame, *Registers) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.intConstants = constants
	builder.body = append(builder.body, isa.NewTier1Instruction(isa.SubOpEqualInt64ConstantThenJumpIfFalse, 1, 0))
	low, high := isa.SplitOffset(fusedJumpOffset)
	builder.body = append(builder.body, isa.NewInstruction(isa.OpExtension, low, high, 0))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	vm, frame, registers := newFramedVM(t, builder.build())
	frame.ProgramCounter = 1
	return vm, frame, registers
}

func newFieldRunFrame(t *testing.T, receiver reflect.Value, fields []int) (*VM, *CallFrame, *Registers, isa.Instruction) {
	t.Helper()
	run := isa.NewTier1Instruction(isa.SubOpGetStructFieldUint64Run, 1, 1)
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.body = append(builder.body, run)
	for ordinal := range fields {
		count := uint8(0)
		if ordinal == 0 {
			count = uint8(len(fields))
		}
		builder.body = append(builder.body, isa.NewInstruction(isa.OpExtension, uint8(ordinal+2), uint8(ordinal), count))
	}
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	compiledFunction := builder.build()
	appendPixelLayouts(compiledFunction, fields)

	vm, frame, registers := newFramedVM(t, compiledFunction)
	frame.ProgramCounter = 1
	registers.General[1] = receiver
	return vm, frame, registers, run
}

func newElementFieldRunFrame(t *testing.T, collection reflect.Value, index int64, fields []int, mode uint8) (*VM, *CallFrame, *Registers, isa.Instruction) {
	t.Helper()
	run := isa.NewTier1Instruction(isa.SubOpGetElementFieldUint64Run, 1, 0)
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.body = append(builder.body, run)
	for ordinal := range fields {
		third := uint8(0)
		switch ordinal {
		case 0:
			third = uint8(len(fields)) | mode<<isa.FieldRunModeShift
		case isa.ElementFieldRunScratchWord:
			third = elementFieldRunReceiver
		}
		builder.body = append(builder.body, isa.NewInstruction(isa.OpExtension, uint8(ordinal+2), uint8(ordinal), third))
	}
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	compiledFunction := builder.build()
	appendPixelLayouts(compiledFunction, fields)

	vm, frame, registers := newFramedVM(t, compiledFunction)
	frame.ProgramCounter = 1
	registers.General[1] = collection
	registers.Ints[0] = index
	return vm, frame, registers, run
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
		{name: "equality is false so the branch is taken", handler: handleEqualInt64ConstantThenJumpIfFalse, register: 1, constant: 2, wantJump: true},
		{name: "equality is true so the branch is skipped", handler: handleEqualInt64ConstantThenJumpIfFalse, register: 2, constant: 2, wantJump: false},
		{name: "equality is true so the true-branch is taken", handler: handleEqualInt64ConstantThenJumpIfTrue, register: 2, constant: 2, wantJump: true},
		{name: "equality is false so the true-branch is skipped", handler: handleEqualInt64ConstantThenJumpIfTrue, register: 1, constant: 2, wantJump: false},
		{name: "greater or equal is false so the branch is taken", handler: handleGreaterOrEqualInt64ConstantThenJumpIfFalse, register: 1, constant: 2, wantJump: true},
		{name: "greater or equal is true so the branch is skipped", handler: handleGreaterOrEqualInt64ConstantThenJumpIfFalse, register: 3, constant: 2, wantJump: false},
		{name: "greater or equal holds when equal", handler: handleGreaterOrEqualInt64ConstantThenJumpIfFalse, register: 2, constant: 2, wantJump: false},
		{name: "greater than is false so the branch is taken", handler: handleGreaterThanInt64ConstantThenJumpIfFalse, register: 2, constant: 2, wantJump: true},
		{name: "greater than is true so the branch is skipped", handler: handleGreaterThanInt64ConstantThenJumpIfFalse, register: 3, constant: 2, wantJump: false},
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

		got := handleEqualInt64ConstantThenJumpIfFalse(vm, frame, registers, op(0, 1, 9))

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

		require.Equal(t, opContinue, handleAddInt64ConstantThenJump(vm, frame, registers, op(0, 1, 0)))
		require.Equal(t, int64(42), registers.Ints[0])
		require.Equal(t, 2+int(fusedJumpOffset), frame.ProgramCounter,
			"the fused add-and-jump is an unconditional loop edge")
	})

	t.Run("an addition with a constant index past the pool is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, []int64{10})

		require.Equal(t, opPanicError, handleAddInt64ConstantThenJump(vm, frame, registers, op(0, 1, 9)))
		require.Error(t, vm.evalError)
	})

	t.Run("an increment branches while it stays below the limit", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, nil)
		registers.Ints[0] = 1
		registers.Ints[1] = 5

		require.Equal(t, opContinue, handleIncrementInt64ThenJumpIfLessThanBody(vm, frame, registers, op(0, 1, 0)))
		require.Equal(t, int64(2), registers.Ints[0])
		require.Equal(t, 2+int(fusedJumpOffset), frame.ProgramCounter)
	})

	t.Run("an increment stops branching once it reaches the limit", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, nil)
		registers.Ints[0] = 4
		registers.Ints[1] = 5

		require.Equal(t, opContinue, handleIncrementInt64ThenJumpIfLessThanBody(vm, frame, registers, op(0, 1, 0)))
		require.Equal(t, int64(5), registers.Ints[0])
		require.Equal(t, 2, frame.ProgramCounter, "the loop edge is not taken once the counter reaches the limit")
	})

	t.Run("a string length comparison branches when the loop condition fails", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, nil)
		registers.Ints[0] = 5
		registers.Strings[1] = "pipit"

		require.Equal(t, opContinue, handleLessThanLengthStringThenJumpIfFalseBody(vm, frame, registers, op(0, 1, 0)))
		require.Equal(t, 2+int(fusedJumpOffset), frame.ProgramCounter)
	})

	t.Run("a string length comparison continues while the counter is inside the string", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, nil)
		registers.Ints[0] = 2
		registers.Strings[1] = "pipit"

		require.Equal(t, opContinue, handleLessThanLengthStringThenJumpIfFalseBody(vm, frame, registers, op(0, 1, 0)))
		require.Equal(t, 2, frame.ProgramCounter)
	})

	t.Run("a multiplication by a constant writes the destination", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, []int64{6})
		registers.Ints[1] = 7

		require.Equal(t, opContinue, handleMultiplyInt64Constant(vm, frame, registers, op(0, 1, 0)))
		require.Equal(t, int64(42), registers.Ints[0])
	})

	t.Run("a multiplication with a constant index past the pool is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newFusedJumpFrame(t, []int64{6})

		require.Equal(t, opPanicError, handleMultiplyInt64Constant(vm, frame, registers, op(0, 1, 9)))
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

func TestRunsThatCountWordsPastTheEndOfTheBodyRaiseAnInvariantError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  isa.Instruction
		call func(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult
	}{
		{name: "a byte run", run: isa.NewTier1Instruction(isa.SubOpSetSliceByteElementRun, 0, 0), call: handleSetSliceByteElementRun},
		{name: "a field run", run: isa.NewTier1Instruction(isa.SubOpGetStructFieldUint64Run, 0, 0), call: handleGetStructFieldUint64Run},
		{name: "an element field run", run: isa.NewTier1Instruction(isa.SubOpGetElementFieldUint64Run, 0, 0), call: handleGetElementFieldUint64Run},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			vm := &VM{}
			registers := makeRegisters()
			frame := &CallFrame{Function: &program.CompiledFunction{
				Name: "truncated",
				Body: []isa.Instruction{test.run, isa.NewInstruction(isa.OpExtension, 0, 0, 4)},
			}}
			frame.ProgramCounter = 1

			require.Equal(t, opPanicError, test.call(vm, frame, &registers, test.run))
			require.ErrorContains(t, vm.evalError, "past the end of its body")
		})
	}
}

func TestStructFieldRunReadsWhatSeparateFieldReadsDo(t *testing.T) {
	t.Parallel()

	pixel := fieldRunPixel{Red: 10, Green: 200, Blue: 255, Alpha: 1, Wide: 60000, Count: 4_000_000_000, Large: 1 << 63}
	receivers := []struct {
		build func() reflect.Value
		name  string
	}{
		{name: "a pointer to the struct", build: func() reflect.Value { held := pixel; return reflect.ValueOf(&held) }},
		{name: "an element of an addressable array", build: func() reflect.Value {
			return reflect.ValueOf(&[2]fieldRunPixel{{}, pixel}).Elem().Index(1)
		}},
		{name: "a struct value that cannot be addressed", build: func() reflect.Value { return reflect.ValueOf(pixel) }},
	}
	runs := [][]int{{0}, {0, 1, 2}, {2, 1, 0, 3}, {4, 5, 6}, {6, 0, 4, 5}}

	for _, receiver := range receivers {
		for _, fields := range runs {
			t.Run(fmt.Sprint(receiver.name, ", fields ", fields), func(t *testing.T) {
				t.Parallel()
				vm, frame, registers, run := newFieldRunFrame(t, receiver.build(), fields)

				require.Equal(t, opContinue, handleGetStructFieldUint64Run(vm, frame, registers, run))

				require.Equal(t, 1+len(fields), frame.ProgramCounter, "the run steps over one word for each read")
				separateVM, separateFrame, separate, _ := newFieldRunFrame(t, receiver.build(), fields)
				for ordinal := range fields {
					read := isa.NewInstruction(isa.OpGetStructFieldUint64, uint8(ordinal+2), 1, uint8(ordinal))
					require.Equal(t, opContinue, handleGetStructFieldUint64(separateVM, separateFrame, separate, read))
				}
				require.Equal(t, separate.Uints, registers.Uints)
				require.NotZero(t, registers.Uints[2], "the first field read is a non-zero field")
			})
		}
	}
}

func runElementReadThenFieldRun(vm *VM, frame *CallFrame, registers *Registers, mode uint8) OpResult {
	element := isa.NewInstruction(isa.OpGetElement, elementFieldRunReceiver, 1, 0)
	if result := handleGetElement(vm, frame, registers, element); result != opContinue {
		return result
	}
	if mode != MoveGeneralModeAlias {
		move := isa.NewInstruction(isa.OpMoveGeneral, elementFieldRunReceiver, elementFieldRunReceiver, mode)
		if result := handleMoveGeneral(vm, frame, registers, move); result != opContinue {
			return result
		}
	}
	run := isa.NewTier1Instruction(isa.SubOpGetStructFieldUint64Run, elementFieldRunReceiver, elementFieldRunReceiver)
	return handleGetStructFieldUint64Run(vm, frame, registers, run)
}

func elementFieldRunCollections() []struct {
	build func() reflect.Value
	name  string
} {
	pixel := fieldRunPixel{Red: 10, Green: 200, Blue: 255, Alpha: 1, Wide: 60000, Count: 4_000_000_000, Large: 1 << 63}
	other := fieldRunPixel{Red: 3, Green: 4, Blue: 5, Alpha: 6, Wide: 7, Count: 8, Large: 9}
	return []struct {
		build func() reflect.Value
		name  string
	}{
		{name: "a slice of structs", build: func() reflect.Value { return reflect.ValueOf([]fieldRunPixel{other, pixel, other}) }},
		{name: "an addressable array of structs", build: func() reflect.Value { return reflect.ValueOf(&[3]fieldRunPixel{other, pixel, other}).Elem() }},
		{name: "an array of structs held by value", build: func() reflect.Value { return reflect.ValueOf([3]fieldRunPixel{other, pixel, other}) }},
		{name: "a pointer to an array of structs", build: func() reflect.Value { return reflect.ValueOf(&[3]fieldRunPixel{other, pixel, other}) }},
		{name: "a slice of pointers to structs", build: func() reflect.Value { return reflect.ValueOf([]*fieldRunPixel{&other, &pixel, nil}) }},
		{name: "a slice of interfaces holding structs", build: func() reflect.Value { return reflect.ValueOf([]any{other, pixel, other}) }},
	}
}

func TestElementFieldRunReadsWhatTheElementReadAndTheFieldRunDo(t *testing.T) {
	t.Parallel()

	runs := [][]int{{0, 1}, {0, 1, 2}, {2, 1, 0, 3}, {4, 5, 6}, {6, 0, 4, 5}}
	modes := []uint8{MoveGeneralModeAlias, MoveGeneralModeDynamic, MoveGeneralModeSnapshot}
	for _, collection := range elementFieldRunCollections() {
		for _, fields := range runs {
			for _, mode := range modes {
				t.Run(fmt.Sprint(collection.name, ", fields ", fields, ", move mode ", mode), func(t *testing.T) {
					t.Parallel()
					vm, frame, registers, run := newElementFieldRunFrame(t, collection.build(), 1, fields, mode)

					require.Equal(t, opContinue, handleGetElementFieldUint64Run(vm, frame, registers, run))

					require.Equal(t, 1+len(fields), frame.ProgramCounter, "the run steps over one word for each read")
					separateVM, separateFrame, separate, _ := newElementFieldRunFrame(t, collection.build(), 1, fields, mode)
					require.Equal(t, opContinue, runElementReadThenFieldRun(separateVM, separateFrame, separate, mode))
					require.Equal(t, separate.Uints, registers.Uints)
					require.Equal(t, separateFrame.ProgramCounter, frame.ProgramCounter)
					require.NotZero(t, registers.Uints[2], "the first field read is a non-zero field")
				})
			}
		}
	}
}

func TestElementFieldRunFailsAsTheElementReadAndTheFieldRunFail(t *testing.T) {
	t.Parallel()

	pixels := func() reflect.Value { return reflect.ValueOf([]fieldRunPixel{{Red: 1}, {Red: 2}}) }
	tests := []struct {
		build func() reflect.Value
		name  string
		index int64
	}{
		{name: "an index one past the end", build: pixels, index: 2},
		{name: "a negative index", build: pixels, index: -1},
		{name: "a nil slice", build: func() reflect.Value { return reflect.ValueOf([]fieldRunPixel(nil)) }},
		{name: "an empty register", build: func() reflect.Value { return reflect.Value{} }},
		{name: "a nil pointer to an array", build: func() reflect.Value { return reflect.ValueOf((*[2]fieldRunPixel)(nil)) }},
		{name: "a nil pointer element", build: func() reflect.Value { return reflect.ValueOf([]*fieldRunPixel{nil}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fields := []int{0, 1, 2}
			vm, frame, registers, run := newElementFieldRunFrame(t, tt.build(), tt.index, fields, MoveGeneralModeAlias)
			separateVM, separateFrame, separate, _ := newElementFieldRunFrame(t, tt.build(), tt.index, fields, MoveGeneralModeAlias)

			result := handleGetElementFieldUint64Run(vm, frame, registers, run)
			want := runElementReadThenFieldRun(separateVM, separateFrame, separate, MoveGeneralModeAlias)

			require.Equal(t, want, result)
			require.NotEqual(t, opContinue, result, "the case is one that fails")
			require.Equal(t, fmt.Sprint(separateVM.evalError), fmt.Sprint(vm.evalError))
			require.Equal(t, separate.Uints, registers.Uints)
		})
	}
}

func TestElementFieldRunWritesItsReceiverOnlyForADebugger(t *testing.T) {
	t.Parallel()

	collection := func() reflect.Value { return reflect.ValueOf([]fieldRunPixel{{Red: 1}, {Red: 2, Green: 3}}) }
	fields := []int{0, 1}
	probeFrame := &CallFrame{Function: &program.CompiledFunction{Name: "probe"}}
	probe := collection()
	addressed := readStructElementUintFields(probeFrame, &Registers{}, &probe, 1, nil, 0)

	vm, frame, registers, run := newElementFieldRunFrame(t, collection(), 1, fields, MoveGeneralModeDebugSnapshot)
	require.Equal(t, opContinue, handleGetElementFieldUint64Run(vm, frame, registers, run))
	require.Equal(t, []uint64{2, 3}, registers.Uints[2:4])
	require.Equal(t, !addressed, registers.General[elementFieldRunReceiver].IsValid(),
		"with no debugger attached the element's storage is read where it can be addressed, and no value is made for the element")

	watched, watchedFrame, watchedRegisters, _ := newElementFieldRunFrame(t, collection(), 1, fields, MoveGeneralModeDebugSnapshot)
	watched.Limits.Debug = NewDebugSession()
	require.Equal(t, opContinue, handleGetElementFieldUint64Run(watched, watchedFrame, watchedRegisters, run))
	require.Equal(t, []uint64{2, 3}, watchedRegisters.Uints[2:4])
	require.Equal(t, fieldRunPixel{Red: 2, Green: 3}, watchedRegisters.General[elementFieldRunReceiver].Interface(),
		"a debugger is shown the element in the register the copy was kept in")
}

func appendPixelLayouts(compiledFunction *program.CompiledFunction, fields []int) {
	pixelType := reflect.TypeFor[fieldRunPixel]()
	for _, fieldIndex := range fields {
		field := pixelType.Field(fieldIndex)
		layout := program.StructFieldLayout{Offset: uint32(field.Offset), PathLength: 1, Kind: uint8(field.Type.Kind()), RegisterKind: uint8(isa.RegisterUint64)}
		layout.Path[0] = uint8(fieldIndex)
		compiledFunction.StructLayoutTable = append(compiledFunction.StructLayoutTable, layout)
	}
}
