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

	"pipit.sh/pipit/internal/isa"
)

type lengthHolder struct {
	items  []uint16
	hidden []string
}

func runLengthGeneral(t *testing.T, collection reflect.Value, assembly bool) (length int64, failure string) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	instruction := isa.NewTier1Instruction(isa.SubOpLengthGeneral, 1, 2)
	builder.body = append(builder.body, instruction)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	vm, frame, registers := newFramedVM(t, builder.build())
	registers.General[2] = collection
	registers.Ints[1] = -7
	defer func() {
		if recovered := recover(); recovered != nil {
			failure = fmt.Sprint("panic: ", recovered)
		}
	}()
	if assembly {
		runInAssembly(t, vm)
	} else {
		flatDispatchSwitch(vm, frame, registers, instruction)
	}
	if vm.evalError != nil {
		failure = vm.evalError.Error()
	}
	return registers.Ints[1], failure
}

func TestGeneralLengthIsTheLengthOfWhateverCollectionTheRegisterHolds(t *testing.T) {
	spec, known := isa.SpecAt(isa.TierSub1, uint8(isa.SubOpLengthGeneral))
	require.True(t, known)
	require.Equal(t, isa.DispatchAsm, spec.Dispatch(), "a slice's length is read without leaving the loop")

	holder := &lengthHolder{items: []uint16{1, 2, 3, 4, 5}, hidden: []string{"a", "b"}}
	var boxed any = []int{1, 2}
	filled := make(chan int, 3)
	filled <- 1
	filled <- 2
	tests := []struct {
		collection  reflect.Value
		name        string
		wantFailure string
		want        int64
	}{
		{name: "a slice", collection: reflect.ValueOf([]int{1, 2, 3}), want: 3},
		{name: "an empty slice", collection: reflect.ValueOf([]string{}), want: 0},
		{name: "a nil slice", collection: reflect.ValueOf([]float64(nil)), want: 0},
		{name: "a slice of structs", collection: reflect.ValueOf(make([]lengthHolder, 7)), want: 7},
		{name: "a slice with spare room", collection: reflect.ValueOf(make([]byte, 3, 64)), want: 3},
		{name: "a slice held in a field", collection: reflect.ValueOf(holder).Elem().Field(0), want: 5},
		{name: "a slice in a private field", collection: reflect.ValueOf(holder).Elem().Field(1), want: 2},
		{name: "a string", collection: reflect.ValueOf("hello"), want: 5},
		{name: "a map", collection: reflect.ValueOf(map[string]int{"a": 1, "b": 2}), want: 2},
		{name: "an array", collection: reflect.ValueOf([4]int{}), want: 4},
		{name: "a pointer to an array", collection: reflect.ValueOf(&[6]int{}), want: 6},
		{name: "an empty buffered channel", collection: reflect.ValueOf(make(chan int, 3)), want: 0},
		{name: "a buffered channel holding two values", collection: reflect.ValueOf(filled), want: 2},
		{name: "no value at all", collection: reflect.Value{}, want: 0},
		{name: "an interface over a slice, which has no length of its own", collection: reflect.ValueOf(&boxed).Elem(), wantFailure: "interface Value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, assembly := range dispatchRoutes() {
				length, failure := runLengthGeneral(t, tt.collection, assembly)
				if tt.wantFailure != "" {
					require.Containsf(t, failure, tt.wantFailure, "%s", routeName(assembly))
					continue
				}
				require.Emptyf(t, failure, "%s", routeName(assembly))
				require.Equalf(t, tt.want, length, "%s", routeName(assembly))
			}
		})
	}
}
