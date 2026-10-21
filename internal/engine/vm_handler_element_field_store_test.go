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
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	elementFieldStoreValue   = 1
	elementFieldStoreSlice   = 2
	elementFieldStoreIndex   = 3
	elementFieldStoreScratch = 6
)

type elementFieldStoreTarget struct {
	Count  int64
	Ratio  float64
	Narrow int32
	Single float32
	Small  int8
	Plain  int
}

type elementFieldStoreLookalike struct {
	Count  int64
	Ratio  float64
	Narrow int32
	Single float32
	Small  int8
	Plain  int
}

const (
	elementFieldStoreCount = iota
	elementFieldStoreRatio
	elementFieldStoreNarrow
	elementFieldStoreSingle
	elementFieldStoreSmall
	elementFieldStorePlain
)

type elementFieldStoreCase struct {
	collection func() reflect.Value
	name       string
	want       string
	failure    string
	integer    int64
	float      float64
	index      int64
	field      int
}

func TestElementFieldStoreWritesTheFieldOfTheIndexedElement(t *testing.T) {
	t.Parallel()

	twoTargets := func() reflect.Value {
		return reflect.ValueOf([]elementFieldStoreTarget{{Count: 1, Ratio: 1.5}, {Count: 2, Ratio: 2.5}})
	}
	tests := []elementFieldStoreCase{
		{
			name: "a full-width integer field of the first element", collection: twoTargets,
			field: elementFieldStoreCount, index: 0, integer: math.MinInt64,
			want: "[{-9223372036854775808 1.5 0 0 0 0} {2 2.5 0 0 0 0}]",
		},
		{
			name: "a full-width integer field of the last element", collection: twoTargets,
			field: elementFieldStoreCount, index: 1, integer: 1<<40 + 5,
			want: "[{1 1.5 0 0 0 0} {1099511627781 2.5 0 0 0 0}]",
		},
		{
			name: "an int field", collection: twoTargets,
			field: elementFieldStorePlain, index: 1, integer: -9,
			want: "[{1 1.5 0 0 0 0} {2 2.5 0 0 0 -9}]",
		},
		{
			name: "a 32-bit field keeps the low 32 bits", collection: twoTargets,
			field: elementFieldStoreNarrow, index: 0, integer: 1<<40 + 5,
			want: "[{1 1.5 5 0 0 0} {2 2.5 0 0 0 0}]",
		},
		{
			name: "an 8-bit field keeps the low 8 bits", collection: twoTargets,
			field: elementFieldStoreSmall, index: 1, integer: 0x1FF,
			want: "[{1 1.5 0 0 0 0} {2 2.5 0 0 -1 0}]",
		},
		{
			name: "a float64 field", collection: twoTargets,
			field: elementFieldStoreRatio, index: 1, float: -0.25,
			want: "[{1 1.5 0 0 0 0} {2 -0.25 0 0 0 0}]",
		},
		{
			name: "a float64 field takes an infinity", collection: twoTargets,
			field: elementFieldStoreRatio, index: 0, float: math.Inf(1),
			want: "[{1 +Inf 0 0 0 0} {2 2.5 0 0 0 0}]",
		},
		{
			name: "a float32 field is rounded to its width", collection: twoTargets,
			field: elementFieldStoreSingle, index: 0, float: 0.1,
			want: "[{1 1.5 0 0.1 0 0} {2 2.5 0 0 0 0}]",
		},
		{
			name: "an index at the length", collection: twoTargets,
			field: elementFieldStoreCount, index: 2, integer: 7,
			want: "[{1 1.5 0 0 0 0} {2 2.5 0 0 0 0}]", failure: "index out of range [2] with length 2",
		},
		{
			name: "a negative index", collection: twoTargets,
			field: elementFieldStoreRatio, index: -1, float: 7,
			want: "[{1 1.5 0 0 0 0} {2 2.5 0 0 0 0}]", failure: "index out of range [-1]",
		},
		{
			name: "a nil slice",
			collection: func() reflect.Value {
				return reflect.ValueOf([]elementFieldStoreTarget(nil))
			},
			field: elementFieldStoreCount, index: 0, integer: 7,
			want: "[]", failure: "index out of range [0] with length 0",
		},
		{
			name: "an empty slice",
			collection: func() reflect.Value {
				return reflect.ValueOf([]elementFieldStoreTarget{})
			},
			field: elementFieldStoreRatio, index: 0, float: 7,
			want: "[]", failure: "index out of range [0] with length 0",
		},
		{
			name: "a slice cut out of a longer one is bounded by its own length",
			collection: func() reflect.Value {
				return reflect.ValueOf(make([]elementFieldStoreTarget, 4)[1:2])
			},
			field: elementFieldStoreCount, index: 1, integer: 7,
			want: "[{0 0 0 0 0 0}]", failure: "index out of range [1] with length 1",
		},
	}

	for _, tt := range tests {
		for _, assembly := range dispatchRoutes() {
			t.Run(tt.name+" by the "+routeName(assembly), func(t *testing.T) {
				t.Parallel()

				contents, failure, steppedOver := runElementFieldStoreRoute(t, tt, assembly)

				require.Equal(t, tt.want, contents)
				if tt.failure == "" {
					require.Empty(t, failure)
					require.True(t, steppedOver, "the store has to leave the program counter after its extension word")
					return
				}
				require.Contains(t, failure, tt.failure)
				require.False(t, steppedOver)
			})
		}
	}
}

func TestElementFieldStoreDoesWhatTheElementReadAndTheFieldStoreDo(t *testing.T) {
	t.Parallel()

	collections := []struct {
		build func() reflect.Value
		name  string
	}{
		{name: "a slice of the layout's type", build: func() reflect.Value {
			return reflect.ValueOf([]elementFieldStoreTarget{{Count: 1}, {Count: 2}, {Count: 3}})
		}},
		{name: "a slice of another type of the same shape", build: func() reflect.Value {
			return reflect.ValueOf([]elementFieldStoreLookalike{{Count: 1}, {Count: 2}, {Count: 3}})
		}},
		{name: "a nil slice", build: func() reflect.Value {
			return reflect.ValueOf([]elementFieldStoreTarget(nil))
		}},
		{name: "a slice of pointers", build: func() reflect.Value {
			return reflect.ValueOf([]*elementFieldStoreTarget{{Count: 1}, {Count: 2}, nil})
		}},
		{name: "a slice held in a struct field", build: func() reflect.Value {
			holder := &struct {
				Elements []elementFieldStoreTarget
			}{Elements: []elementFieldStoreTarget{{Count: 1}, {Count: 2}, {Count: 3}}}
			return reflect.ValueOf(holder).Elem().Field(0)
		}},
		{name: "an array reached through a pointer", build: func() reflect.Value {
			return reflect.ValueOf(&[3]elementFieldStoreTarget{{Count: 1}, {Count: 2}, {Count: 3}}).Elem()
		}},
	}

	for _, collection := range collections {
		for field := elementFieldStoreCount; field <= elementFieldStorePlain; field++ {
			for _, index := range []int64{-1, 0, 2, 3} {
				tt := elementFieldStoreCase{
					collection: collection.build, field: field, index: index,
					integer: 0x1FF_0000_01FF, float: 1.0 / 3,
				}
				name := fmt.Sprintf("%s, field %d, index %d", collection.name, field, index)
				t.Run(name, func(t *testing.T) {
					t.Parallel()

					wantContents, wantFailure := runElementReadThenFieldStore(t, tt)
					for _, assembly := range dispatchRoutes() {
						contents, failure, _ := runElementFieldStoreRoute(t, tt, assembly)

						require.Equal(t, wantContents, contents, routeName(assembly))
						require.Equal(t, wantFailure, failure, routeName(assembly))
					}
				})
			}
		}
	}
}

func elementFieldStoreLayouts() []program.StructFieldLayout {
	structType := reflect.TypeFor[elementFieldStoreTarget]()
	layouts := make([]program.StructFieldLayout, 0, structType.NumField())
	for index := range structType.NumField() {
		field := structType.Field(index)
		layout := program.StructFieldLayout{
			Offset:       uint32(field.Offset),
			PathLength:   1,
			Kind:         uint8(field.Type.Kind()),
			RegisterKind: uint8(isaBankForKind(field.Type.Kind())),
		}
		layout.Path[0] = uint8(index)
		layouts = append(layouts, layout)
	}
	return layouts
}

func elementFieldStoreIsFloat(field int) bool {
	return field == elementFieldStoreRatio || field == elementFieldStoreSingle
}

func newElementFieldStoreFrame(t *testing.T, testCase elementFieldStoreCase, body ...isa.Instruction) (*VM, *CallFrame, *Registers, reflect.Value) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.typeTable = []reflect.Type{reflect.TypeFor[elementFieldStoreTarget]()}
	builder.body = append(builder.body, body...)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	function := builder.build()
	function.StructLayoutTable = elementFieldStoreLayouts()
	vm, frame, registers := newFramedVM(t, function)
	held := testCase.collection()
	registers.General[elementFieldStoreSlice] = held
	registers.Ints[elementFieldStoreIndex] = testCase.index
	registers.Ints[elementFieldStoreValue] = testCase.integer
	registers.Floats[elementFieldStoreValue] = testCase.float
	return vm, frame, registers, held
}

func elementFieldStoreOutcome(vm *VM, held reflect.Value, recovered any) (contents, failure string) {
	if recovered != nil {
		failure = fmt.Sprint("panic: ", recovered)
	}
	if failure == "" && vm.evalError != nil {
		failure = fmt.Sprint("error: ", vm.evalError)
	}
	return elementFieldStoreContents(held), failure
}

func elementFieldStoreContents(held reflect.Value) string {
	elements := make([]any, 0, held.Len())
	for index := range held.Len() {
		element := held.Index(index)
		if element.Kind() == reflect.Pointer && !element.IsNil() {
			element = element.Elem()
		}
		elements = append(elements, element.Interface())
	}
	return fmt.Sprint(elements)
}

func runElementFieldStoreRoute(t *testing.T, testCase elementFieldStoreCase, assembly bool) (contents, failure string, steppedOver bool) {
	t.Helper()
	const witness, fellThrough = 5, 11
	opcode := isa.OpSetElementFieldInt64
	handler := handleSetElementFieldInt64
	if elementFieldStoreIsFloat(testCase.field) {
		opcode, handler = isa.OpSetElementFieldFloat64, handleSetElementFieldFloat64
	}
	instruction := isa.NewInstruction(opcode, elementFieldStoreValue, elementFieldStoreSlice, elementFieldStoreIndex)
	vm, frame, registers, held := newElementFieldStoreFrame(t, testCase,
		instruction,
		isa.NewInstruction(isa.OpExtension, uint8(testCase.field), 0, elementFieldStoreScratch),
		isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, witness, fellThrough))
	defer func() {
		contents, failure = elementFieldStoreOutcome(vm, held, recover())
	}()
	kept := reflect.ValueOf("held across the store")
	registers.General[elementFieldStoreScratch] = kept
	if assembly {
		runInAssembly(t, vm)
		steppedOver = registers.Ints[witness] == fellThrough
	} else {
		frame.ProgramCounter = 1
		result := handler(vm, frame, registers, instruction)
		steppedOver = result == opContinue && frame.ProgramCounter == len(frame.Function.Body)-2
	}
	if steppedOver {
		require.Equal(t, kept.Interface(), registers.General[elementFieldStoreScratch].Interface(),
			"a store that succeeds leaves the register it passes the element through as it found it")
	}
	return contents, failure, steppedOver
}

func runElementReadThenFieldStore(t *testing.T, testCase elementFieldStoreCase) (contents, failure string) {
	t.Helper()
	store := isa.NewTier1Instruction(isa.SubOpSetStructFieldInt64Wide, elementFieldStoreScratch, elementFieldStoreValue)
	storeHandler := handleSetStructFieldInt64Wide
	if elementFieldStoreIsFloat(testCase.field) {
		store = isa.NewTier1Instruction(isa.SubOpSetStructFieldFloat64Wide, elementFieldStoreScratch, elementFieldStoreValue)
		storeHandler = handleSetStructFieldFloat64Wide
	}
	read := isa.NewInstruction(isa.OpGetElement, elementFieldStoreScratch, elementFieldStoreSlice, elementFieldStoreIndex)
	vm, frame, registers, held := newElementFieldStoreFrame(t, testCase,
		read, store, isa.NewInstruction(isa.OpExtension, uint8(testCase.field), 0, 0))
	defer func() {
		contents, failure = elementFieldStoreOutcome(vm, held, recover())
	}()
	frame.ProgramCounter = 1
	if handleGetElement(vm, frame, registers, read) != opContinue {
		return contents, failure
	}
	frame.ProgramCounter = 2
	storeHandler(vm, frame, registers, store)
	return contents, failure
}
