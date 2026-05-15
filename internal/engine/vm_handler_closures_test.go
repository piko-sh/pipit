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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestGuardChannelOpConvertsAPanicIntoAReturnedValue(t *testing.T) {
	t.Parallel()

	t.Run("an operation that completes reports no panic", func(t *testing.T) {
		t.Parallel()
		ran := false

		recovered, panicked := guardChannelOp(func() { ran = true })

		require.True(t, ran)
		require.False(t, panicked)
		require.Nil(t, recovered)
	})

	t.Run("a panic is captured rather than unwound into the host", func(t *testing.T) {
		t.Parallel()
		recovered, panicked := guardChannelOp(func() { panic("send on closed channel") })

		require.True(t, panicked)
		require.Equal(t, "send on closed channel", recovered)
	})

	t.Run("a double close is captured", func(t *testing.T) {
		t.Parallel()
		channel := make(chan int)
		close(channel)

		_, panicked := guardChannelOp(func() { close(channel) })

		require.True(t, panicked, "closing a closed channel must not terminate the host")
	})
}

func TestChannelCloseReportsASecondClose(t *testing.T) {
	t.Parallel()

	t.Run("closing an open channel succeeds", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		channel := make(chan int, 1)
		registers.General[0] = reflect.ValueOf(channel)

		require.Equal(t, opContinue, handleChannelClose(vm, frame, registers, op(0, 0, 0)))

		_, open := <-channel
		require.False(t, open, "the channel must actually be closed")
	})

	t.Run("closing a closed channel raises an interpreted panic", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		channel := make(chan int, 1)
		close(channel)
		registers.General[0] = reflect.ValueOf(channel)

		got := handleChannelClose(vm, frame, registers, op(0, 0, 0))

		require.NotEqual(t, opContinue, got, "a second close must surface as an interpreted fault")
		require.Error(t, vm.evalError)
	})
}

func TestReadIndirectCellValueUnboxesIntoTheNamedBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		build func() reflect.Value
		check func(t *testing.T, registers *Registers)
		name  string
		kind  isa.RegisterKind
	}{
		{
			name: "an integer cell", kind: isa.RegisterInt,
			build: func() reflect.Value { value := int64(42); return reflect.ValueOf(&value) },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(42), registers.Ints[0]) },
		},
		{
			name: "a float cell", kind: isa.RegisterFloat,
			build: func() reflect.Value { value := 1.5; return reflect.ValueOf(&value) },
			check: func(t *testing.T, registers *Registers) { require.InDelta(t, 1.5, registers.Floats[0], 0) },
		},
		{
			name: "a string cell", kind: isa.RegisterString,
			build: func() reflect.Value { value := "pipit"; return reflect.ValueOf(&value) },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, "pipit", registers.Strings[0]) },
		},
		{
			name: "a boolean cell", kind: isa.RegisterBool,
			build: func() reflect.Value { value := true; return reflect.ValueOf(&value) },
			check: func(t *testing.T, registers *Registers) { require.True(t, registers.Bools[0]) },
		},
		{
			name: "an unsigned cell", kind: isa.RegisterUint,
			build: func() reflect.Value { value := uint64(42); return reflect.ValueOf(&value) },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, uint64(42), registers.Uints[0]) },
		},
		{
			name: "a complex cell", kind: isa.RegisterComplex,
			build: func() reflect.Value { value := 1 + 2i; return reflect.ValueOf(&value) },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, 1+2i, registers.Complex[0]) },
		},
		{
			name: "a general cell keeps the dereferenced value", kind: isa.RegisterGeneral,
			build: func() reflect.Value { value := leafCounter{Signed: 1}; return reflect.ValueOf(&value) },
			check: func(t *testing.T, registers *Registers) {
				require.Equal(t, reflect.Struct, registers.General[0].Kind())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			cell := &program.UpvalueCell{GeneralValue: tt.build(), IsIndirect: true}

			readIndirectCellValue(&registers, cell, 0, tt.kind)

			tt.check(t, &registers)
		})
	}

	t.Run("the pointer sentinel copies the pointer untouched", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		value := int64(42)
		pointer := reflect.ValueOf(&value)
		cell := &program.UpvalueCell{GeneralValue: pointer, IsIndirect: true}

		readIndirectCellValue(&registers, cell, 0, program.UpvalueKindAsPointer)

		require.Equal(t, reflect.Pointer, registers.General[0].Kind(),
			"the sentinel hands the closure the cell's pointer rather than its value")
	})

	t.Run("an invalid cell leaves the register alone", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.Ints[0] = -1
		cell := &program.UpvalueCell{IsIndirect: true}

		readIndirectCellValue(&registers, cell, 0, isa.RegisterInt)

		require.Equal(t, int64(-1), registers.Ints[0])
	})
}

func TestGetUpvalueReadsThroughTheFrameCellTable(t *testing.T) {
	t.Parallel()

	t.Run("a direct cell is copied into its bank", func(t *testing.T) {
		t.Parallel()
		_, frame, registers := newStandardVM(t)
		cell := &program.UpvalueCell{IntValue: 42, Kind: isa.RegisterInt}
		frame.upvalues = []upvalue{{Value: cell}}

		require.Equal(t, opContinue, handleGetUpvalue(nil, frame, registers, op(0, 0, uint8(isa.RegisterInt))))
		require.Equal(t, int64(42), registers.Ints[0])
	})

	t.Run("an indirect cell is dereferenced", func(t *testing.T) {
		t.Parallel()
		_, frame, registers := newStandardVM(t)
		value := int64(42)
		cell := &program.UpvalueCell{GeneralValue: reflect.ValueOf(&value), IsIndirect: true, Kind: isa.RegisterInt}
		frame.upvalues = []upvalue{{Value: cell}}

		require.Equal(t, opContinue, handleGetUpvalue(nil, frame, registers, op(0, 0, uint8(isa.RegisterInt))))
		require.Equal(t, int64(42), registers.Ints[0])
	})
}
