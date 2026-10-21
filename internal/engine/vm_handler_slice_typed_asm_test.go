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

//go:build !safe && !(js && wasm) && (amd64 || arm64)

package engine

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

type elementRouteHolder struct {
	bytes    *[4]uint8
	readOnly []uint16
}

type namedUnsignedSlice []uint32

type namedUnsigned uint16

type elementRouteOutcome struct {
	failure string
	result  uint64
}

type elementStoreOutcome struct {
	failure string
	stored  string
}

func runElementRoute(t *testing.T, opcode isa.Opcode, collection reflect.Value, index int64, assembly bool) (outcome elementRouteOutcome) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.body = append(builder.body, isa.NewInstruction(opcode, 1, 2, 3))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	vm, frame, registers := newFramedVM(t, builder.build())
	registers.General[2] = collection
	registers.Ints[3] = index
	registers.Ints[1], registers.Uints[1] = -7, math.MaxUint64-7
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome.failure = fmt.Sprint("panic: ", recovered)
		}
		if opcode == isa.OpGetElementInt64 {
			outcome.result = uint64(registers.Ints[1])
		} else {
			outcome.result = registers.Uints[1]
		}
		if outcome.failure == "" && (vm.evalError != nil || vm.panicValue != nil) {
			outcome.failure = fmt.Sprint("error: ", vm.evalError, vm.panicValue != nil)
		}
	}()
	if assembly {
		runInAssembly(t, vm)
		return outcome
	}
	instruction := isa.NewInstruction(opcode, 1, 2, 3)
	if opcode == isa.OpGetElementInt64 {
		handleGetElementInt64(vm, frame, registers, instruction)
	} else {
		handleGetElementUint64(vm, frame, registers, instruction)
	}
	return outcome
}

func elementRouteCollections() map[string]reflect.Value {
	bytes := [4]uint8{1, 0x80, 0xfe, 0xff}
	array := [3]int16{-2, 0x7fff, -0x8000}
	holder := &elementRouteHolder{bytes: &bytes, readOnly: []uint16{9, 0xffff}}
	var nilArray *[4]uint8
	words := [2]uint64{11, math.MaxUint64}
	return map[string]reflect.Value{
		"[]uint8":                     reflect.ValueOf([]uint8{0, 0x7f, 0x80, 0xff}),
		"[]uint16":                    reflect.ValueOf([]uint16{1, 0x8000, 0xffff}),
		"[]uint32":                    reflect.ValueOf([]uint32{1, 0x80000000, 0xffffffff}),
		"[]uint64":                    reflect.ValueOf([]uint64{1, 1 << 63, math.MaxUint64}),
		"[]uint":                      reflect.ValueOf([]uint{3, math.MaxUint}),
		"[]uintptr":                   reflect.ValueOf([]uintptr{4, 1 << 40}),
		"[]int8":                      reflect.ValueOf([]int8{-1, 127, -128}),
		"[]int16":                     reflect.ValueOf([]int16{-1, 0x7fff, -0x8000}),
		"[]int32":                     reflect.ValueOf([]int32{-1, math.MaxInt32, math.MinInt32}),
		"[]int64":                     reflect.ValueOf([]int64{-1, math.MaxInt64, math.MinInt64}),
		"[]int":                       reflect.ValueOf([]int{-5, 6}),
		"named slice":                 reflect.ValueOf(namedUnsignedSlice{7, 0xfffffffe}),
		"named element":               reflect.ValueOf([]namedUnsigned{8, 0xfff0}),
		"empty slice":                 reflect.ValueOf([]uint8{}),
		"nil slice":                   reflect.ValueOf([]uint8(nil)),
		"array value":                 reflect.ValueOf(array),
		"addressable array":           reflect.ValueOf(&array).Elem(),
		"pointer to array":            reflect.ValueOf(&bytes),
		"pointer to array in a field": reflect.ValueOf(holder).Elem().Field(0),
		"read-only slice":             reflect.ValueOf(holder).Elem().Field(1),
		"nil pointer to array":        reflect.ValueOf(nilArray),
		"pointer to a short array":    reflect.ValueOf(&words),
		"short array":                 reflect.ValueOf(&words).Elem(),
		"[]float64":                   reflect.ValueOf([]float64{1.5, -2}),
		"[]bool":                      reflect.ValueOf([]bool{true}),
		"[]string":                    reflect.ValueOf([]string{"s"}),
		"[]*int":                      reflect.ValueOf([]*int{nil}),
		"[]any":                       reflect.ValueOf([]any{uint8(1)}),
		"map":                         reflect.ValueOf(map[int]uint8{0: 1}),
		"no value":                    {},
	}
}

func TestSliceGetElementRoutesReadAsTheGoHandlerDoes(t *testing.T) {
	indices := []int64{-1, 0, 1, 2, 3, 4, 1 << 40, math.MinInt64}
	for name, collection := range elementRouteCollections() {
		for _, opcode := range []isa.Opcode{isa.OpGetElementInt64, isa.OpGetElementUint64} {
			for _, index := range indices {
				label := fmt.Sprintf("%s %v [%d]", name, opcode, index)
				want := runElementRoute(t, opcode, collection, index, false)
				if collection.IsValid() {
					typedElementShapeSlot(reflectValueABIType(collection.Type())).Store(nil)
				}
				require.Equal(t, want, runElementRoute(t, opcode, collection, index, true), "%s: a front miss", label)
				require.Equal(t, want, runElementRoute(t, opcode, collection, index, true), "%s: a front hit", label)
				if collection.IsValid() {
					slot := typedElementShapeSlot(reflectValueABIType(collection.Type()))
					slot.Store(&typedElementShape{collectionType: unsafe.Pointer(slot), elementSize: 1, arrayLength: 1 << 20, elementKind: reflect.Uint8, container: typedContainerSlice})
					require.Equal(t, want, runElementRoute(t, opcode, collection, index, true), "%s: another type's shape in the slot", label)
				}
			}
		}
	}
}

func runElementStore(t *testing.T, opcode isa.Opcode, collection reflect.Value, index, value int64, assembly bool) (outcome elementStoreOutcome) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	instruction := isa.NewInstruction(opcode, 1, 2, 3)
	builder.body = append(builder.body, instruction)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	vm, frame, registers := newFramedVM(t, builder.build())
	registers.General[1] = collection
	registers.Ints[2] = index
	registers.Ints[3], registers.Uints[3] = value, uint64(value)
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome.failure = fmt.Sprint("panic: ", recovered)
		}
		outcome.stored = fmt.Sprint(collection)
		if outcome.failure == "" && (vm.evalError != nil || vm.panicValue != nil) {
			outcome.failure = fmt.Sprint("error: ", vm.evalError, vm.panicValue != nil)
		}
	}()
	if assembly {
		runInAssembly(t, vm)
		return outcome
	}
	if opcode == isa.OpSetElementInt64 {
		handleSetElementInt64(vm, frame, registers, instruction)
	} else {
		handleSetElementUint64(vm, frame, registers, instruction)
	}
	return outcome
}

func elementStoreCollections() map[string]func() reflect.Value {
	return map[string]func() reflect.Value{
		"[]uint8":           func() reflect.Value { return reflect.ValueOf([]uint8{0, 0x7f, 0x80, 0xff}) },
		"[]uint16":          func() reflect.Value { return reflect.ValueOf([]uint16{1, 0x8000, 0xffff}) },
		"[]uint32":          func() reflect.Value { return reflect.ValueOf([]uint32{1, 0x80000000, 0xffffffff}) },
		"[]uint64":          func() reflect.Value { return reflect.ValueOf([]uint64{1, 1 << 63, math.MaxUint64}) },
		"[]uint":            func() reflect.Value { return reflect.ValueOf([]uint{3, math.MaxUint}) },
		"[]uintptr":         func() reflect.Value { return reflect.ValueOf([]uintptr{4, 1 << 40}) },
		"[]int8":            func() reflect.Value { return reflect.ValueOf([]int8{-1, 127, -128}) },
		"[]int16":           func() reflect.Value { return reflect.ValueOf([]int16{-1, 0x7fff, -0x8000}) },
		"[]int32":           func() reflect.Value { return reflect.ValueOf([]int32{-1, math.MaxInt32, math.MinInt32}) },
		"[]int64":           func() reflect.Value { return reflect.ValueOf([]int64{-1, math.MaxInt64, math.MinInt64}) },
		"[]int":             func() reflect.Value { return reflect.ValueOf([]int{-5, 6}) },
		"named slice":       func() reflect.Value { return reflect.ValueOf(namedUnsignedSlice{7, 0xfffffffe}) },
		"named element":     func() reflect.Value { return reflect.ValueOf([]namedUnsigned{8, 0xfff0}) },
		"empty slice":       func() reflect.Value { return reflect.ValueOf([]uint8{}) },
		"nil slice":         func() reflect.Value { return reflect.ValueOf([]uint8(nil)) },
		"addressable array": func() reflect.Value { return reflect.ValueOf(&[3]int16{-2, 0x7fff, -0x8000}).Elem() },
		"pointer to array":  func() reflect.Value { return reflect.ValueOf(&[4]uint8{1, 0x80, 0xfe, 0xff}) },
		"pointer to array in a field": func() reflect.Value {
			return reflect.ValueOf(&elementRouteHolder{bytes: &[4]uint8{1, 2, 3, 4}}).Elem().Field(0)
		},
		"nil pointer to array":     func() reflect.Value { return reflect.ValueOf((*[4]uint8)(nil)) },
		"pointer to a short array": func() reflect.Value { return reflect.ValueOf(&[2]uint64{11, math.MaxUint64}) },
		"short array":              func() reflect.Value { return reflect.ValueOf(&[2]uint64{11, math.MaxUint64}).Elem() },
		"[]float64":                func() reflect.Value { return reflect.ValueOf([]float64{1.5, -2}) },
		"[]float32":                func() reflect.Value { return reflect.ValueOf([]float32{1.5, -2}) },
		"map":                      func() reflect.Value { return reflect.ValueOf(map[int]uint8{0: 1}) },
		"no value":                 func() reflect.Value { return reflect.Value{} },
	}
}

func TestSliceSetElementRoutesWriteAsTheGoHandlerDoes(t *testing.T) {
	indices := []int64{-1, 0, 1, 2, 3, 4, 1 << 40, math.MinInt64}
	values := []int64{0, 1, -1, 0x1FF, 0x1FFFF, 1 << 40, math.MinInt64, math.MaxInt64}
	for name, build := range elementStoreCollections() {
		for _, opcode := range []isa.Opcode{isa.OpSetElementInt64, isa.OpSetElementUint64} {
			spec, known := isa.SpecAt(isa.TierMain, uint8(opcode))
			require.True(t, known)
			require.Equal(t, isa.DispatchAsm, spec.Dispatch(), "the store has an assembly body")
			for _, index := range indices {
				for _, value := range values {
					label := fmt.Sprintf("%s %v [%d] = %d", name, opcode, index, value)
					want := runElementStore(t, opcode, build(), index, value, false)
					probe := build()
					if probe.IsValid() {
						typedElementShapeSlot(reflectValueABIType(probe.Type())).Store(nil)
					}
					require.Equal(t, want, runElementStore(t, opcode, build(), index, value, true), "%s: a front miss", label)
					require.Equal(t, want, runElementStore(t, opcode, build(), index, value, true), "%s: a front hit", label)
					if probe.IsValid() {
						slot := typedElementShapeSlot(reflectValueABIType(probe.Type()))
						slot.Store(&typedElementShape{collectionType: unsafe.Pointer(slot), elementSize: 1, arrayLength: 1 << 20, elementKind: reflect.Uint8, container: typedContainerSlice})
						require.Equal(t, want, runElementStore(t, opcode, build(), index, value, true), "%s: another type's shape in the slot", label)
						slot.Store(nil)
					}
				}
			}
		}
	}
}

func TestSliceSetElementLeavesValuesItMayNotWriteToTheShim(t *testing.T) {
	holder := &elementRouteHolder{readOnly: []uint16{9, 0xffff}}
	readOnly := reflect.ValueOf(holder).Elem().Field(1)
	unaddressable := reflect.ValueOf([3]int16{-2, 0x7fff, -0x8000})
	for name, collection := range map[string]reflect.Value{"a read-only slice": readOnly, "an array that cannot be addressed": unaddressable} {
		for _, opcode := range []isa.Opcode{isa.OpSetElementInt64, isa.OpSetElementUint64} {
			_ = runElementStore(t, opcode, collection, 1, 5, true)
			outcome := runElementStore(t, opcode, collection, 1, 5, true)
			require.NotEmpty(t, outcome.failure, "%s, %v: the store is refused as reflect refuses it", name, opcode)
		}
	}
	require.Equal(t, []uint16{9, 0xffff}, holder.readOnly, "nothing was written through the read-only value")
	require.Equal(t, "[-2 32767 -32768]", fmt.Sprint(unaddressable), "nothing was written into the array held by value")
}

func TestTypedElementWriteFlagsMatchTheAssemblyHeader(t *testing.T) {
	require.EqualValues(t, 0x60, licenceFlagRO, "FLAG_RO")
	require.EqualValues(t, 0x80, flagIndir, "FLAG_INDIR")
	require.EqualValues(t, 0x100, flagAddr, "FLAG_ADDR")
}

func TestTypedElementShapeOffsets(t *testing.T) {
	var shape typedElementShape
	require.EqualValues(t, 0, unsafe.Offsetof(shape.collectionType), "TES_COLLECTION_TYPE")
	require.EqualValues(t, 8, unsafe.Offsetof(shape.elementSize), "TES_ELEMENT_SIZE")
	require.EqualValues(t, 16, unsafe.Offsetof(shape.arrayLength), "TES_ARRAY_LENGTH")
	require.EqualValues(t, 24, unsafe.Offsetof(shape.elementKind), "TES_ELEMENT_KIND")
	require.EqualValues(t, 32, unsafe.Offsetof(shape.container), "TES_CONTAINER")
	require.EqualValues(t, 1, typedContainerSlice, "TES_CONTAINER_SLICE")
	require.EqualValues(t, 2, typedContainerArray, "TES_CONTAINER_ARRAY")
	require.EqualValues(t, 3, typedContainerArrayPointer, "TES_CONTAINER_ARRAY_POINTER")
	require.EqualValues(t, uint64(0x9E3779B97F4A7C15), uint64(typedElementShapeHashMultiplier), "TES_HASH_MULTIPLIER")
	require.EqualValues(t, 54, typedElementShapeHashBits-typedElementShapeSlotBits, "TES_SLOT_SHIFT")
	require.EqualValues(t, 8, unsafe.Sizeof(typedElementShapes[0]), "a front slot is one pointer word")
}
