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

func TestBuildSelectSendValueReadsTheNamedBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		kind        isa.RegisterKind
		load        func(*Registers)
		elementType reflect.Type
		want        any
	}{
		{
			name: "the int bank", kind: isa.RegisterInt, load: func(r *Registers) { r.Ints[1] = 4 },
			elementType: reflect.TypeFor[int](), want: 4,
		},
		{
			name: "the float bank", kind: isa.RegisterFloat, load: func(r *Registers) { r.Floats[1] = 1.5 },
			elementType: reflect.TypeFor[float64](), want: 1.5,
		},
		{
			name: "the string bank", kind: isa.RegisterString, load: func(r *Registers) { r.Strings[1] = "s" },
			elementType: reflect.TypeFor[string](), want: "s",
		},
		{
			name: "the bool bank", kind: isa.RegisterBool, load: func(r *Registers) { r.Bools[1] = true },
			elementType: reflect.TypeFor[bool](), want: true,
		},
		{
			name: "the uint bank", kind: isa.RegisterUint, load: func(r *Registers) { r.Uints[1] = 6 },
			elementType: reflect.TypeFor[uint32](), want: uint32(6),
		},
		{
			name: "the complex bank", kind: isa.RegisterComplex, load: func(r *Registers) { r.Complex[1] = 3i },
			elementType: reflect.TypeFor[complex128](), want: 3i,
		},
		{
			name: "the int-slice bank", kind: isa.RegisterSliceInt, load: func(r *Registers) { r.SlicesInt[1] = []int64{1} },
			elementType: reflect.TypeFor[[]int64](), want: []int64{1},
		},
		{
			name: "the float-slice bank", kind: isa.RegisterSliceFloat,
			load:        func(r *Registers) { r.slicesFloat[1] = []float64{1.5} },
			elementType: reflect.TypeFor[[]float64](), want: []float64{1.5},
		},
		{
			name: "the string-slice bank", kind: isa.RegisterSliceString,
			load:        func(r *Registers) { r.slicesString[1] = []string{"s"} },
			elementType: reflect.TypeFor[[]string](), want: []string{"s"},
		},
		{
			name: "the bool-slice bank", kind: isa.RegisterSliceBool,
			load:        func(r *Registers) { r.slicesBool[1] = []bool{true} },
			elementType: reflect.TypeFor[[]bool](), want: []bool{true},
		},
		{
			name: "the uint-slice bank", kind: isa.RegisterSliceUint,
			load:        func(r *Registers) { r.slicesUint[1] = []uint64{2} },
			elementType: reflect.TypeFor[[]uint64](), want: []uint64{2},
		},
		{
			name: "the byte-slice bank", kind: isa.RegisterSliceByte,
			load:        func(r *Registers) { r.slicesByte[1] = []byte{3} },
			elementType: reflect.TypeFor[[]byte](), want: []byte{3},
		},
		{
			name: "the general bank", kind: isa.RegisterGeneral,
			load:        func(r *Registers) { r.General[1] = reflect.ValueOf("boxed") },
			elementType: reflect.TypeFor[string](), want: "boxed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, _, registers := newStandardVM(t)
			tt.load(registers)

			got := buildSelectSendValue(vm, registers, op(1, uint8(tt.kind), 0), tt.elementType)
			require.Equal(t, tt.want, got.Interface())
		})
	}
}

func TestBuildSelectSendValueZeroesAnUnsetGeneralRegister(t *testing.T) {
	t.Parallel()

	vm, _, registers := newStandardVM(t)

	got := buildSelectSendValue(vm, registers, op(1, uint8(isa.RegisterGeneral), 0), reflect.TypeFor[string]())
	require.Equal(t, "", got.Interface(), "an unset register sends the element type's zero value")
}

func TestWriteRegisterValueWritesTheNamedBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    isa.RegisterKind
		value   any
		inspect func(*testing.T, *Registers)
	}{
		{
			name: "the int bank", kind: isa.RegisterInt, value: int64(4),
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(4), r.Ints[2]) },
		},
		{
			name: "the float bank", kind: isa.RegisterFloat, value: 1.5,
			inspect: func(t *testing.T, r *Registers) { require.InDelta(t, 1.5, r.Floats[2], 0) },
		},
		{
			name: "the string bank", kind: isa.RegisterString, value: "s",
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "s", r.Strings[2]) },
		},
		{
			name: "the bool bank", kind: isa.RegisterBool, value: true,
			inspect: func(t *testing.T, r *Registers) { require.True(t, r.Bools[2]) },
		},
		{
			name: "the uint bank", kind: isa.RegisterUint, value: uint64(7),
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, uint64(7), r.Uints[2]) },
		},
		{
			name: "the complex bank", kind: isa.RegisterComplex, value: 2i,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, 2i, r.Complex[2]) },
		},
		{
			name: "the general bank", kind: isa.RegisterGeneral, value: "boxed",
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "boxed", r.General[2].Interface()) },
		},
		{
			name: "the int-slice bank", kind: isa.RegisterSliceInt, value: []int64{1, 2},
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []int64{1, 2}, r.SlicesInt[2]) },
		},
		{
			name: "the float-slice bank", kind: isa.RegisterSliceFloat, value: []float64{1.5},
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []float64{1.5}, r.slicesFloat[2]) },
		},
		{
			name: "the string-slice bank", kind: isa.RegisterSliceString, value: []string{"s"},
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []string{"s"}, r.slicesString[2]) },
		},
		{
			name: "the bool-slice bank", kind: isa.RegisterSliceBool, value: []bool{true},
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []bool{true}, r.slicesBool[2]) },
		},
		{
			name: "the uint-slice bank", kind: isa.RegisterSliceUint, value: []uint64{9},
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []uint64{9}, r.slicesUint[2]) },
		},
		{
			name: "the byte-slice bank", kind: isa.RegisterSliceByte, value: []byte{3},
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []byte{3}, r.slicesByte[2]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := standardRegisters()
			writeRegisterValue(&registers, 2, tt.kind, reflect.ValueOf(tt.value))
			tt.inspect(t, &registers)
		})
	}
}

func TestSliceFromReflectAsConvertsElementwise(t *testing.T) {
	t.Parallel()

	t.Run("an exact slice type is handed straight back", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, []int64{1, 2}, sliceFromReflectAs[int64](reflect.ValueOf([]int64{1, 2})))
	})

	t.Run("a convertible element type is converted one element at a time", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, []int64{1, 2}, sliceFromReflectAs[int64](reflect.ValueOf([]int32{1, 2})))
	})

	t.Run("an invalid value yields nothing", func(t *testing.T) {
		t.Parallel()

		require.Nil(t, sliceFromReflectAs[int64](reflect.Value{}))
	})

	t.Run("a value that is not a slice yields nothing", func(t *testing.T) {
		t.Parallel()

		require.Nil(t, sliceFromReflectAs[int64](reflect.ValueOf(4)))
	})

	t.Run("elements that cannot be converted are left as the zero value", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, []int64{0}, sliceFromReflectAs[int64](reflect.ValueOf([]string{"x"})))
	})
}

func TestSelectPicksTheReadyArm(t *testing.T) {
	t.Parallel()

	t.Run("a ready receive writes the value, the comma-ok flag and the chosen index", func(t *testing.T) {
		t.Parallel()

		channel := make(chan int, 1)
		channel <- 12

		vm, frame, registers := newExtWordFrame(t,
			op(isa.SelectDirectionReceive, 1, 1),
			op(3, uint8(isa.RegisterInt), 5),
		)
		registers.General[1] = reflect.ValueOf(channel)

		got := handleSelect(vm, frame, registers, op(0, 1, 6))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64(0), registers.Ints[6], "the only arm is arm zero")
		require.Equal(t, int64(12), registers.Ints[3])
		require.Equal(t, int64(1), registers.Ints[5], "the comma-ok flag reports an open channel")
	})

	t.Run("a receive from a closed channel reports a false comma-ok", func(t *testing.T) {
		t.Parallel()

		channel := make(chan int)
		close(channel)

		vm, frame, registers := newExtWordFrame(t,
			op(isa.SelectDirectionReceive, 1, 1),
			op(3, uint8(isa.RegisterInt), 5),
		)
		registers.General[1] = reflect.ValueOf(channel)

		got := handleSelect(vm, frame, registers, op(0, 1, 6))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64(0), registers.Ints[5])
	})

	t.Run("a ready send hands the register value to the channel", func(t *testing.T) {
		t.Parallel()

		channel := make(chan string, 1)

		vm, frame, registers := newExtWordFrame(t,
			op(isa.SelectDirectionSend, 1, 0),
			op(3, uint8(isa.RegisterString), 0),
		)
		registers.General[1] = reflect.ValueOf(channel)
		registers.Strings[3] = "sent"

		got := handleSelect(vm, frame, registers, op(0, 1, 6))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64(0), registers.Ints[6])
		require.Equal(t, "sent", <-channel)
	})

	t.Run("the default arm is taken when no channel is ready", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newExtWordFrame(t,
			op(isa.SelectDirectionReceive, 1, 0),
			op(3, uint8(isa.RegisterInt), 0),
			op(isa.SelectDirectionDefault, 0, 0),
		)
		registers.General[1] = reflect.ValueOf(make(chan int))

		got := handleSelect(vm, frame, registers, op(0, 2, 6))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64(1), registers.Ints[6], "the default is arm one")
		require.Zero(t, registers.Ints[3], "no receive happened, so nothing was written")
	})

	t.Run("a select with no ready arm aborts once the context is cancelled", func(t *testing.T) {
		t.Parallel()

		vm := newCancelledVM(t)
		builder := newBytecodeBuilder()
		builder.numRegisters = wideRegCounts(8)
		builder.Emit(isa.OpDrillTier1, 0, 0, 0)
		builder.body = append(builder.body,
			op(isa.SelectDirectionReceive, 1, 0),
			op(3, uint8(isa.RegisterInt), 0),
		)
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
		compiledFunction := builder.build()
		vm.prepareForExecution(compiledFunction)
		vm.PushFrame(compiledFunction)
		frame := &vm.CallStack[vm.FramePointer]
		frame.ProgramCounter = 1
		registers := &frame.Registers
		registers.General[1] = reflect.ValueOf(make(chan int))

		got := handleSelect(vm, frame, registers, op(0, 1, 6))
		require.Equal(t, opPanicError, got, "a select that can never proceed must abort rather than deadlock")
		require.Error(t, vm.evalError)
	})

	t.Run("an empty select blocks until the context is cancelled", func(t *testing.T) {
		t.Parallel()

		vm := newCancelledVM(t)
		require.Equal(t, opPanicError, handleSelectNoCases(vm))
		require.Error(t, vm.evalError)
	})
}

func TestSelectWakeArmsAreOmittedWhenADefaultExists(t *testing.T) {
	t.Parallel()

	t.Run("a default suppresses both wake arms", func(t *testing.T) {
		t.Parallel()

		vm := newCancelledVM(t)
		vm.selectCasesBuffer = make([]reflect.SelectCase, 3)

		cases, cancelIndex, panicIndex := vm.appendSelectWakeArms(1, true)
		require.Len(t, cases, 1, "a select with a default never blocks, so it needs no wake arm")
		require.Equal(t, -1, cancelIndex)
		require.Equal(t, -1, panicIndex)
	})

	t.Run("a cancellable context contributes a wake arm", func(t *testing.T) {
		t.Parallel()

		vm := newCancelledVM(t)
		vm.selectCasesBuffer = make([]reflect.SelectCase, 3)

		cases, cancelIndex, panicIndex := vm.appendSelectWakeArms(1, false)
		require.Len(t, cases, 2)
		require.Equal(t, 1, cancelIndex)
		require.Equal(t, -1, panicIndex)
	})
}

func TestApplySelectReceiveResultWritesOnlyWhatTheArmDeclares(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		info     selectCaseInfo
		receiver reflect.Value
		ok       bool
		inspect  func(*testing.T, *Registers)
	}{
		{
			name:     "a value arm writes the value alone",
			info:     selectCaseInfo{destinationRegister: 2, destinationKind: isa.RegisterInt, hasOk: false, okRegister: 3},
			receiver: reflect.ValueOf(int64(4)),
			ok:       true,
			inspect: func(t *testing.T, r *Registers) {
				require.Equal(t, int64(4), r.Ints[2])
				require.Zero(t, r.Ints[3], "an arm without a comma-ok binding writes no flag")
			},
		},
		{
			name:     "a comma-ok arm writes both",
			info:     selectCaseInfo{destinationRegister: 2, destinationKind: isa.RegisterInt, hasOk: true, okRegister: 3},
			receiver: reflect.ValueOf(int64(4)),
			ok:       true,
			inspect: func(t *testing.T, r *Registers) {
				require.Equal(t, int64(4), r.Ints[2])
				require.Equal(t, int64(1), r.Ints[3])
			},
		},
		{
			name:     "a closed channel clears the flag",
			info:     selectCaseInfo{destinationRegister: 2, destinationKind: isa.RegisterInt, hasOk: true, okRegister: 3},
			receiver: reflect.ValueOf(int64(0)),
			ok:       false,
			inspect: func(t *testing.T, r *Registers) {
				require.Equal(t, int64(0), r.Ints[3])
			},
		},
		{
			name:     "an invalid receiver writes no value",
			info:     selectCaseInfo{destinationRegister: 2, destinationKind: isa.RegisterInt, hasOk: true, okRegister: 3},
			receiver: reflect.Value{},
			ok:       false,
			inspect: func(t *testing.T, r *Registers) {
				require.Zero(t, r.Ints[2])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := standardRegisters()
			applySelectReceiveResult(&registers, tt.info, tt.receiver, tt.ok)
			tt.inspect(t, &registers)
		})
	}
}
