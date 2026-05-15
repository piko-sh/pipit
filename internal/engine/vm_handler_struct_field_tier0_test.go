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

type tier0Fields struct {
	Signed   int64
	Unsigned uint64
	Ratio    float64
	Flag     bool
	Narrow   int32
	Values   []int64
}

func tier0Layout(fieldIndex int) program.StructFieldLayout {
	field := reflect.TypeFor[tier0Fields]().Field(fieldIndex)
	layout := program.StructFieldLayout{
		Offset:       uint32(field.Offset),
		PathLength:   1,
		Kind:         uint8(field.Type.Kind()),
		RegisterKind: uint8(isaBankForKind(field.Type.Kind())),
	}
	layout.Path[0] = uint8(fieldIndex)
	return layout
}

func isaBankForKind(kind reflect.Kind) isa.RegisterKind {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return isa.RegisterInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return isa.RegisterUint
	case reflect.Float32, reflect.Float64:
		return isa.RegisterFloat
	case reflect.Bool:
		return isa.RegisterBool
	default:
		return isa.RegisterGeneral
	}
}

func newTier0Frame(t *testing.T, receiver any) (*VM, *CallFrame, *Registers) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	compiledFunction := builder.build()

	fieldCount := reflect.TypeFor[tier0Fields]().NumField()
	layouts := make([]program.StructFieldLayout, 0, fieldCount)
	for index := range fieldCount {
		layouts = append(layouts, tier0Layout(index))
	}
	compiledFunction.StructLayoutTable = layouts

	vm, frame, registers := newFramedVM(t, compiledFunction)
	registers.General[1] = reflect.ValueOf(receiver)
	return vm, frame, registers
}

func TestTier0FieldReadsLandInTheirOwnBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		check   func(t *testing.T, registers *Registers)
		name    string
		field   uint8
	}{
		{
			name: "a signed field", handler: handleGetStructFieldIntT0, field: 0,
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(42), registers.Ints[0]) },
		},
		{
			name: "a narrow signed field widens", handler: handleGetStructFieldIntT0, field: 4,
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(-7), registers.Ints[0]) },
		},
		{
			name: "an unsigned field", handler: handleGetStructFieldUintT0, field: 1,
			check: func(t *testing.T, registers *Registers) { require.Equal(t, uint64(7), registers.Uints[0]) },
		},
		{
			name: "a float field", handler: handleGetStructFieldFloatT0, field: 2,
			check: func(t *testing.T, registers *Registers) { require.InDelta(t, 1.5, registers.Floats[0], 0) },
		},
		{
			name: "a boolean field", handler: handleGetStructFieldBoolT0, field: 3,
			check: func(t *testing.T, registers *Registers) { require.True(t, registers.Bools[0]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" through a pointer receiver", func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newTier0Frame(t, &tier0Fields{
				Signed: 42, Unsigned: 7, Ratio: 1.5, Flag: true, Narrow: -7,
			})

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, tt.field)))
			tt.check(t, registers)
		})

		t.Run(tt.name+" through a value receiver", func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newTier0Frame(t, tier0Fields{
				Signed: 42, Unsigned: 7, Ratio: 1.5, Flag: true, Narrow: -7,
			})

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, tt.field)))
			tt.check(t, registers)
		})
	}
}

func TestTier0FieldWritesReachTheReceiver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		seed    func(registers *Registers)
		check   func(t *testing.T, target *tier0Fields)
		name    string
		field   uint8
	}{
		{
			name: "a signed field", handler: handleSetStructFieldIntT0, field: 0,
			seed:  func(registers *Registers) { registers.Ints[2] = 42 },
			check: func(t *testing.T, target *tier0Fields) { require.Equal(t, int64(42), target.Signed) },
		},
		{
			name: "a narrow signed field", handler: handleSetStructFieldIntT0, field: 4,
			seed:  func(registers *Registers) { registers.Ints[2] = -7 },
			check: func(t *testing.T, target *tier0Fields) { require.Equal(t, int32(-7), target.Narrow) },
		},
		{
			name: "an unsigned field", handler: handleSetStructFieldUintT0, field: 1,
			seed:  func(registers *Registers) { registers.Uints[2] = 7 },
			check: func(t *testing.T, target *tier0Fields) { require.Equal(t, uint64(7), target.Unsigned) },
		},
		{
			name: "a float field", handler: handleSetStructFieldFloatT0, field: 2,
			seed:  func(registers *Registers) { registers.Floats[2] = 1.5 },
			check: func(t *testing.T, target *tier0Fields) { require.InDelta(t, 1.5, target.Ratio, 0) },
		},
		{
			name: "a boolean field", handler: handleSetStructFieldBoolT0, field: 3,
			seed:  func(registers *Registers) { registers.Bools[2] = true },
			check: func(t *testing.T, target *tier0Fields) { require.True(t, target.Flag) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			target := &tier0Fields{}
			vm, frame, registers := newTier0Frame(t, target)
			tt.seed(registers)

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(1, 2, tt.field)))
			tt.check(t, target)
		})
	}
}

func TestTier0FieldSliceLengthReadsTheHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []int64
		want   int64
	}{
		{name: "a populated slice", values: []int64{1, 2, 3}, want: 3},
		{name: "an empty slice", values: []int64{}, want: 0},
		{name: "a nil slice", values: nil, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newTier0Frame(t, &tier0Fields{Values: tt.values})

			require.Equal(t, opContinue, handleGetStructFieldSliceLen(vm, frame, registers, op(0, 1, 5)))
			require.Equal(t, tt.want, registers.Ints[0])
		})
	}
}

func TestTier0LayoutIndexPastTheTableIsReported(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newTier0Frame(t, &tier0Fields{})

	defer func() {
		recovered := recover()
		require.NotNil(t, recovered,
			"a layout index past the table must be reported rather than reading arbitrary memory")
	}()

	handleGetStructFieldIntT0(vm, frame, registers, op(0, 1, 99))
}

func TestTier0FieldReadOnANilPointerRaisesADereferenceFault(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newTier0Frame(t, (*tier0Fields)(nil))

	got := handleGetStructFieldIntT0(vm, frame, registers, op(0, 1, 0))

	require.NotEqual(t, opContinue, got,
		"reading a field through a nil pointer must raise the dereference the language promises")
	require.Error(t, vm.evalError)
}

func TestTier0FieldReadLeavesTheDestinationAloneOnAWalkMiss(t *testing.T) {
	t.Parallel()

	tests := []struct {
		receiver any
		name     string
	}{
		{name: "an integer receiver", receiver: 42},
		{name: "a string receiver", receiver: "not a struct"},
		{name: "a slice receiver", receiver: []int{1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newTier0Frame(t, tt.receiver)
			registers.Ints[0] = -999

			got := handleGetStructFieldIntT0(vm, frame, registers, op(0, 1, 0))

			require.Equal(t, opContinue, got)
			require.Equal(t, int64(-999), registers.Ints[0],
				"a walk that cannot reach the field must not write a value it did not read")
		})
	}
}
