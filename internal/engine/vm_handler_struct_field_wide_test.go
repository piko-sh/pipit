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

type wideFields struct {
	Signed   int64
	Unsigned uint64
	Ratio    float64
	Flag     bool
	Label    string
	Ints     []int64
	Floats   []float64
	Uints    []uint64
	Strings  []string
	Bools    []bool
	Bytes    []byte
}

func wideFieldLayout(t *testing.T, fieldIndex int) program.StructFieldLayout {
	t.Helper()

	field := reflect.TypeFor[wideFields]().Field(fieldIndex)
	layout := program.StructFieldLayout{
		Offset:         uint32(field.Offset),
		PathLength:     1,
		Kind:           uint8(field.Type.Kind()),
		RegisterKind:   uint8(isaBankForKind(field.Type.Kind())),
		TypeIndex:      0,
		FieldTypeIndex: uint16(fieldIndex + 1),
	}
	layout.Path[0] = uint8(fieldIndex)
	return layout
}

func newWideFieldFrame(t *testing.T, fieldIndex int, receiver any) (*VM, *CallFrame, *Registers) {
	t.Helper()

	structType := reflect.TypeFor[wideFields]()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.Emit(isa.OpDrillTier1, 0, 0, 0)
	builder.body = append(builder.body, isa.NewInstruction(isa.OpExtension, uint8(fieldIndex), 0, 0))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	layouts := make([]program.StructFieldLayout, 0, structType.NumField())
	typeTable := make([]reflect.Type, 0, structType.NumField()+1)
	typeTable = append(typeTable, structType)
	for index := range structType.NumField() {
		layouts = append(layouts, wideFieldLayout(t, index))
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

func TestWideStructFieldScalarReadsLandInTheirBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		field   int
		handler opcodeHandler
		inspect func(*testing.T, *Registers)
	}{
		{
			name: "a float field", field: 2, handler: handleGetStructFieldFloat64Wide,
			inspect: func(t *testing.T, r *Registers) {
				t.Helper()
				require.InDelta(t, 1.5, r.Floats[0], 0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newWideFieldFrame(t, tt.field, &wideFields{Ratio: 1.5})

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 0, 1)))
			require.Equal(t, 2, frame.ProgramCounter, "the layout word must be stepped over")
			tt.inspect(t, registers)
		})
	}
}

func TestWideStructFieldScalarWritesReachTheReceiver(t *testing.T) {
	t.Parallel()

	t.Run("a float field takes the float bank", func(t *testing.T) {
		t.Parallel()

		receiver := &wideFields{}
		vm, frame, registers := newWideFieldFrame(t, 2, receiver)
		registers.Floats[2] = 2.5

		require.Equal(t, opContinue, handleSetStructFieldFloat64Wide(vm, frame, registers, op(0, 1, 2)))
		require.InDelta(t, 2.5, receiver.Ratio, 0)
	})

	t.Run("a bool field takes the bool bank", func(t *testing.T) {
		t.Parallel()

		receiver := &wideFields{}
		vm, frame, registers := newWideFieldFrame(t, 3, receiver)
		registers.Bools[2] = true

		require.Equal(t, opContinue, handleSetStructFieldBoolWide(vm, frame, registers, op(0, 1, 2)))
		require.True(t, receiver.Flag)
	})

	t.Run("a string field takes the string bank", func(t *testing.T) {
		t.Parallel()

		receiver := &wideFields{}
		vm, frame, registers := newWideFieldFrame(t, 4, receiver)
		registers.Strings[2] = "written"

		require.Equal(t, opContinue, handleSetStructFieldStringWide(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, "written", receiver.Label)
	})
}

func TestWideStructFieldWritesFallBackOnANonPointerReceiver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		field   int
		handler opcodeHandler
		load    func(*Registers)
		inspect func(*testing.T, reflect.Value)
	}{
		{
			name: "a float field", field: 2, handler: handleSetStructFieldFloat64Wide,
			load: func(r *Registers) { r.Floats[2] = 3.5 },
			inspect: func(t *testing.T, v reflect.Value) {
				t.Helper()
				require.InDelta(t, 3.5, v.Interface().(wideFields).Ratio, 0)
			},
		},
		{
			name: "a bool field", field: 3, handler: handleSetStructFieldBoolWide,
			load: func(r *Registers) { r.Bools[2] = true },
			inspect: func(t *testing.T, v reflect.Value) {
				t.Helper()
				require.True(t, v.Interface().(wideFields).Flag)
			},
		},
		{
			name: "a string field", field: 4, handler: handleSetStructFieldStringWide,
			load: func(r *Registers) { r.Strings[2] = "walked" },
			inspect: func(t *testing.T, v reflect.Value) {
				t.Helper()
				require.Equal(t, "walked", v.Interface().(wideFields).Label)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newWideFieldFrame(t, tt.field, wideFields{})
			receiver := reflect.New(reflect.TypeFor[wideFields]()).Elem()
			registers.General[1] = receiver
			tt.load(registers)

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)),
				"a receiver the pointer path declines must still reach the field through reflect")
			tt.inspect(t, receiver)
		})
	}
}

func TestWideStructFieldAccessOnANilPointerRaisesADereferenceFault(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newWideFieldFrame(t, 2, (*wideFields)(nil))

	got := handleGetStructFieldFloat64Wide(vm, frame, registers, op(0, 0, 1))
	require.NotEqual(t, opContinue, got, "a nil receiver must not read through a null pointer")
	require.Error(t, vm.evalError)
}

func TestStructLayoutAtWideRefusesAnIndexPastTheTable(t *testing.T) {
	t.Parallel()

	_, frame, _ := newWideFieldFrame(t, 0, &wideFields{})

	require.Panics(t, func() { structLayoutAtWide(frame, 99) },
		"a layout index the compiler cannot have emitted is an invariant break")
}

func TestStructFieldReflectWalkFindsTheLeaf(t *testing.T) {
	t.Parallel()

	t.Run("a read walks the layout path", func(t *testing.T) {
		t.Parallel()

		_, frame, registers := newWideFieldFrame(t, 0, &wideFields{Signed: 8})

		field, ok := structFieldReflectRead(registers, frame, 1, wideFieldLayout(t, 0))
		require.True(t, ok)
		require.Equal(t, int64(8), field.Int())
	})

	t.Run("a write walks to an addressable leaf", func(t *testing.T) {
		t.Parallel()

		receiver := &wideFields{}
		_, frame, registers := newWideFieldFrame(t, 0, receiver)

		field, ok := structFieldReflectWrite(registers, frame, 1, wideFieldLayout(t, 0))
		require.True(t, ok)
		field.SetInt(9)
		require.Equal(t, int64(9), receiver.Signed)
	})

	t.Run("a path step past the field count declines", func(t *testing.T) {
		t.Parallel()

		_, frame, registers := newWideFieldFrame(t, 0, &wideFields{})
		layout := wideFieldLayout(t, 0)
		layout.Path[0] = 99

		_, ok := structFieldReflectRead(registers, frame, 1, layout)
		require.False(t, ok, "the walk must decline rather than index past the struct")
	})

	t.Run("a receiver that is not a struct declines", func(t *testing.T) {
		t.Parallel()

		_, frame, registers := newWideFieldFrame(t, 0, &wideFields{})
		registers.General[1] = reflect.ValueOf(4)

		_, ok := structFieldReflectRead(registers, frame, 1, wideFieldLayout(t, 0))
		require.False(t, ok)
	})
}

func TestStructFieldReflectReadAgreesAcrossReceiverShapes(t *testing.T) {
	t.Parallel()

	receiver := &wideFields{Unsigned: 77}
	pointer := reflect.ValueOf(receiver)
	doublePointer := reflect.ValueOf(&receiver)
	var boxed any = receiver
	var nilReceiver *wideFields
	shapes := []struct {
		name  string
		value reflect.Value
		found bool
	}{
		{name: "a struct value", value: reflect.ValueOf(*receiver), found: true},
		{name: "an addressable struct", value: pointer.Elem(), found: true},
		{name: "a pointer to the struct", value: pointer, found: true},
		{name: "a pointer to that pointer", value: doublePointer, found: true},
		{name: "an interface holding the pointer", value: reflect.ValueOf(&boxed).Elem(), found: true},
		{name: "a nil pointer", value: reflect.ValueOf(nilReceiver), found: false},
		{name: "no value at all", value: reflect.Value{}, found: false},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()

			_, frame, registers := newWideFieldFrame(t, 0, &wideFields{})
			registers.General[1] = shape.value

			field, ok := structFieldReflectRead(registers, frame, 1, wideFieldLayout(t, 1))
			require.Equal(t, shape.found, ok)
			if ok {
				require.Equal(t, uint64(77), field.Uint())
			}
		})
	}
}

func TestStructFieldFallbackMissOnlyFaultsForNilPointers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		receiver  reflect.Value
		wantFault bool
	}{
		{name: "a nil pointer receiver faults", receiver: reflect.ValueOf((*wideFields)(nil)), wantFault: true},
		{name: "a live receiver just continues", receiver: reflect.ValueOf(&wideFields{}), wantFault: false},
		{name: "an unset register just continues", receiver: reflect.Value{}, wantFault: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newWideFieldFrame(t, 0, &wideFields{})
			registers.General[1] = tt.receiver

			got := structFieldFallbackMiss(vm, frame, registers, 1)
			if tt.wantFault {
				require.NotEqual(t, opContinue, got)
				require.Error(t, vm.evalError)
				return
			}
			require.Equal(t, opContinue, got)
		})
	}
}

func TestWideByteFieldReadPreservesHeadersAndBacking(t *testing.T) {
	t.Parallel()

	type octet uint8
	type bytes []byte
	for _, source := range []any{[]byte(nil), []byte{}, make([]byte, 0, 7), bytes{3, 5}, []octet{7, 11}} {
		value := reflect.ValueOf(source)
		t.Run(value.Type().String()+"/"+fmt.Sprint(value.Len(), "/", value.Cap()), func(t *testing.T) {
			t.Parallel()
			fieldType := reflect.StructOf([]reflect.StructField{{Name: "Bytes", Type: value.Type()}})
			receiver := reflect.New(fieldType).Elem()
			receiver.Field(0).Set(value)
			vm, frame, registers := newWideFieldFrame(t, 10, receiver.Addr().Interface())
			frame.Function.StructLayoutTable[10].Path[0] = 0
			frame.Function.StructLayoutTable[10].Offset = 0
			require.Equal(t, opContinue, handleGetStructFieldSliceByte(vm, frame, registers, op(0, 0, 1)))
			got := registers.slicesByte[0]
			require.Len(t, got, value.Len())
			require.Equal(t, value.Cap(), cap(got))
			require.Equal(t, value.IsNil(), got == nil)
			if len(got) > 0 {
				got[0] = 29
				require.Equal(t, uint64(29), value.Index(0).Uint())
			}
		})
	}
}
