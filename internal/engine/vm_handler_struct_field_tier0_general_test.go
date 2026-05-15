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

type generalFields struct {
	Pointer *generalLeaf
	Boxed   any
	Values  []generalLeaf
	Fixed   [3]int64
	Leaf    generalLeaf
	Label   string
}

type generalLeaf struct {
	Count int64
	Ratio float64
}

func generalFieldLayout(t *testing.T, fieldIndex int) program.StructFieldLayout {
	t.Helper()

	field := reflect.TypeFor[generalFields]().Field(fieldIndex)
	layout := program.StructFieldLayout{
		Offset:         uint32(field.Offset),
		PathLength:     1,
		Kind:           uint8(field.Type.Kind()),
		RegisterKind:   uint8(isa.RegisterGeneral),
		TypeIndex:      0,
		FieldTypeIndex: uint16(fieldIndex + 1),
	}
	layout.Path[0] = uint8(fieldIndex)
	return layout
}

func newGeneralFieldFrame(t *testing.T, receiver any, extensions ...isa.Instruction) (*VM, *CallFrame, *Registers) {
	t.Helper()

	structType := reflect.TypeFor[generalFields]()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.Emit(isa.OpDrillTier1, 0, 0, 0)
	builder.body = append(builder.body, extensions...)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	layouts := make([]program.StructFieldLayout, 0, structType.NumField())
	typeTable := make([]reflect.Type, 0, structType.NumField()+1)
	typeTable = append(typeTable, structType)
	for index := range structType.NumField() {
		layouts = append(layouts, generalFieldLayout(t, index))
		typeTable = append(typeTable, structType.Field(index).Type)
	}
	builder.typeTable = typeTable

	compiledFunction := builder.build()
	compiledFunction.StructLayoutTable = layouts

	vm, frame, registers := newFramedVM(t, compiledFunction)
	frame.ProgramCounter = 1
	registers.General[1] = reflect.ValueOf(receiver)
	return vm, frame, registers
}

func TestTier0GeneralFieldReadsCoverEveryFieldShape(t *testing.T) {
	t.Parallel()

	leaf := &generalLeaf{Count: 4}

	tests := []struct {
		name     string
		field    uint8
		receiver any
		inspect  func(*testing.T, reflect.Value)
	}{
		{
			name: "a pointer field", field: 0, receiver: &generalFields{Pointer: leaf},
			inspect: func(t *testing.T, v reflect.Value) { require.Equal(t, leaf, v.Interface()) },
		},
		{
			name: "an interface field holding a value", field: 1, receiver: &generalFields{Boxed: 7},
			inspect: func(t *testing.T, v reflect.Value) { require.Equal(t, 7, v.Interface()) },
		},
		{
			name: "a nil interface field", field: 1, receiver: &generalFields{},
			inspect: func(t *testing.T, v reflect.Value) { require.False(t, v.IsValid()) },
		},
		{
			name: "a slice field", field: 2, receiver: &generalFields{Values: []generalLeaf{{Count: 1}}},
			inspect: func(t *testing.T, v reflect.Value) { require.Equal(t, 1, v.Len()) },
		},
		{
			name: "an array field", field: 3, receiver: &generalFields{Fixed: [3]int64{1, 2, 3}},
			inspect: func(t *testing.T, v reflect.Value) { require.Equal(t, [3]int64{1, 2, 3}, v.Interface()) },
		},
		{
			name: "a struct field", field: 4, receiver: &generalFields{Leaf: generalLeaf{Count: 5}},
			inspect: func(t *testing.T, v reflect.Value) { require.Equal(t, generalLeaf{Count: 5}, v.Interface()) },
		},
		{
			name: "a string field", field: 5, receiver: &generalFields{Label: "text"},
			inspect: func(t *testing.T, v reflect.Value) { require.Equal(t, "text", v.Interface()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newGeneralFieldFrame(t, tt.receiver)

			require.Equal(t, opContinue, handleGetStructFieldGeneralT0(vm, frame, registers, op(0, 1, tt.field)))
			tt.inspect(t, registers.General[0])
		})
	}
}

func TestTier0GeneralFieldReadsWalkANonPointerReceiver(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newGeneralFieldFrame(t, &generalFields{})
	registers.General[1] = reflect.ValueOf(generalFields{Label: "walked"})

	require.Equal(t, opContinue, handleGetStructFieldGeneralT0(vm, frame, registers, op(0, 1, 5)),
		"a receiver with no storage address still has to reach the field through reflect")
	require.Equal(t, "walked", registers.General[0].Interface())
}

func TestTier0GeneralFieldReadOnANilPointerRaisesADereferenceFault(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newGeneralFieldFrame(t, (*generalFields)(nil))

	got := handleGetStructFieldGeneralT0(vm, frame, registers, op(0, 1, 5))
	require.NotEqual(t, opContinue, got)
	require.Error(t, vm.evalError)
}

func TestTier0GeneralFieldWritesReachTheReceiver(t *testing.T) {
	t.Parallel()

	t.Run("a string field takes the general bank", func(t *testing.T) {
		t.Parallel()

		receiver := &generalFields{}
		vm, frame, registers := newGeneralFieldFrame(t, receiver)
		registers.General[2] = reflect.ValueOf("written")

		require.Equal(t, opContinue, handleSetStructFieldGeneralT0(vm, frame, registers, op(1, 2, 5)))
		require.Equal(t, "written", receiver.Label)
	})

	t.Run("an interface field takes any dynamic value", func(t *testing.T) {
		t.Parallel()

		receiver := &generalFields{}
		vm, frame, registers := newGeneralFieldFrame(t, receiver)
		registers.General[2] = reflect.ValueOf(11)

		require.Equal(t, opContinue, handleSetStructFieldGeneralT0(vm, frame, registers, op(1, 2, 1)))
		require.Equal(t, 11, receiver.Boxed)
	})

	t.Run("an unset source zeroes the field", func(t *testing.T) {
		t.Parallel()

		receiver := &generalFields{Label: "stale"}
		vm, frame, registers := newGeneralFieldFrame(t, receiver)

		require.Equal(t, opContinue, handleSetStructFieldGeneralT0(vm, frame, registers, op(1, 2, 5)))
		require.Empty(t, receiver.Label)
	})

	t.Run("a non-pointer receiver is written through the reflect walk", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newGeneralFieldFrame(t, &generalFields{})
		receiver := reflect.New(reflect.TypeFor[generalFields]()).Elem()
		registers.General[1] = receiver
		registers.General[2] = reflect.ValueOf("walked")

		require.Equal(t, opContinue, handleSetStructFieldGeneralT0(vm, frame, registers, op(1, 2, 5)))
		require.Equal(t, "walked", receiver.Interface().(generalFields).Label)
	})
}

func TestTier0CopyStructFieldMovesBetweenReceivers(t *testing.T) {
	t.Parallel()

	t.Run("a string field is copied field to field", func(t *testing.T) {
		t.Parallel()

		source := &generalFields{Label: "copied"}
		destination := &generalFields{}
		vm, frame, registers := newGeneralFieldFrame(t, source, op(5, 0, 0))
		registers.General[2] = reflect.ValueOf(destination)

		require.Equal(t, opContinue, handleCopyStructFieldGeneralT0(vm, frame, registers, op(1, 2, 5)))
		require.Equal(t, "copied", destination.Label)
	})

	t.Run("a non-pointer source falls back to the read-then-write sequence", func(t *testing.T) {
		t.Parallel()

		destination := &generalFields{}
		vm, frame, registers := newGeneralFieldFrame(t, &generalFields{}, op(5, 0, 0))
		registers.General[1] = reflect.ValueOf(generalFields{Label: "fallback"})
		registers.General[2] = reflect.ValueOf(destination)

		require.Equal(t, opContinue, handleCopyStructFieldGeneralT0(vm, frame, registers, op(1, 2, 5)))
		require.Equal(t, "fallback", destination.Label)
	})
}

func TestTier0LayoutIndexPastTheTableIsAnInvariantBreak(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newGeneralFieldFrame(t, &generalFields{})

	require.Panics(t, func() { handleGetStructFieldGeneralT0(vm, frame, registers, op(0, 1, 99)) },
		"a layout index the compiler cannot have emitted is an invariant break")
}

func TestTier0ArrayFieldIndexReadsTheElement(t *testing.T) {
	t.Parallel()

	const arrayLength, elementSize = 3, 8

	tests := []struct {
		name  string
		index int64
		want  any
	}{
		{name: "the first element", index: 0, want: int64(10)},
		{name: "the last element", index: 2, want: int64(30)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newGeneralFieldFrame(t, &generalFields{Fixed: [3]int64{10, 20, 30}},
				op(arrayLength, 0, 3),
				op(elementSize, 0, uint8(reflect.Int64)),
			)
			registers.Ints[3] = tt.index

			require.Equal(t, opContinue, handleGetStructFieldIndexGeneral(vm, frame, registers, op(0, 1, 3)))
			require.Equal(t, tt.want, registers.General[0].Interface())
		})
	}
}

func TestTier0ArrayFieldIndexRefusesAnOutOfRangeIndex(t *testing.T) {
	t.Parallel()

	const arrayLength, elementSize = 3, 8

	vm, frame, registers := newGeneralFieldFrame(t, &generalFields{Fixed: [3]int64{10, 20, 30}},
		op(arrayLength, 0, 3),
		op(elementSize, 0, uint8(reflect.Int64)),
	)
	registers.Ints[3] = 9

	got := handleGetStructFieldIndexGeneral(vm, frame, registers, op(0, 1, 3))
	requireRuntimePanic(t, vm, got, "index out of range [9] with length 3")
}

func TestTier0ArrayFieldIndexWritesTheElement(t *testing.T) {
	t.Parallel()

	const arrayLength, elementSize = 3, 8

	receiver := &generalFields{Fixed: [3]int64{10, 20, 30}}
	vm, frame, registers := newGeneralFieldFrame(t, receiver,
		op(arrayLength, 0, 3),
		op(elementSize, 0, uint8(reflect.Int64)),
	)
	registers.Ints[3] = 1
	registers.General[2] = reflect.ValueOf(int64(99))

	require.Equal(t, opContinue, handleSetStructFieldIndexGeneral(vm, frame, registers, op(1, 2, 3)))
	require.Equal(t, [3]int64{10, 99, 30}, receiver.Fixed)
}

func TestTier0SliceFieldScalarIndexReadsTheSubField(t *testing.T) {
	t.Parallel()

	const elementSize = 16

	tests := []struct {
		name      string
		subOffset uint8
		kind      isa.RegisterKind
		index     int64
		inspect   func(*testing.T, *Registers)
	}{
		{
			name: "the leading int sub-field", subOffset: 0, kind: isa.RegisterInt, index: 1,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(2), r.Ints[0]) },
		},
		{
			name: "the trailing float sub-field", subOffset: 8, kind: isa.RegisterFloat, index: 0,
			inspect: func(t *testing.T, r *Registers) { require.InDelta(t, 1.5, r.Floats[0], 0) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			receiver := &generalFields{Values: []generalLeaf{{Count: 1, Ratio: 1.5}, {Count: 2, Ratio: 2.5}}}
			vm, frame, registers := newGeneralFieldFrame(t, receiver,
				op(elementSize, 0, 3),
				op(tt.subOffset, 0, uint8(tt.kind)),
			)
			registers.Ints[3] = tt.index

			require.Equal(t, opContinue, handleGetStructFieldSliceIndexScalar(vm, frame, registers, op(0, 1, 2)))
			tt.inspect(t, registers)
		})
	}
}

func TestTier0SliceFieldScalarIndexRefusesAnOutOfRangeIndex(t *testing.T) {
	t.Parallel()

	const elementSize = 16

	receiver := &generalFields{Values: []generalLeaf{{Count: 1}}}
	vm, frame, registers := newGeneralFieldFrame(t, receiver,
		op(elementSize, 0, 3),
		op(0, 0, uint8(isa.RegisterInt)),
	)
	registers.Ints[3] = 4

	got := handleGetStructFieldSliceIndexScalar(vm, frame, registers, op(0, 1, 2))
	requireRuntimePanic(t, vm, got, "index out of range [4] with length 1")
}
