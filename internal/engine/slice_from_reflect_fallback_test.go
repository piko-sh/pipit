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
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab"
)

func TestReadTypedSliceFieldFallbackConvertsWidthIdenticalElements(t *testing.T) {
	t.Parallel()
	type holder struct {
		Ints    []int
		Strings []string
		Bytes   []byte
	}
	value := reflect.ValueOf(holder{Ints: []int{7, 8}, Strings: []string{"a"}, Bytes: []byte("xyz")})

	tests := []struct {
		name  string
		read  func(vm *VM, registers *Registers) (OpResult, any)
		want  any
		wantR OpResult
	}{
		{name: "[]int into the int bank", read: func(vm *VM, registers *Registers) (OpResult, any) {
			result := readTypedSliceFieldFallback(vm, vm.currentFrame(), registers, 0, value.Field(0), &registers.SlicesInt[0])
			return result, registers.SlicesInt[0]
		}, want: []int64{7, 8}, wantR: opContinue},
		{name: "[]string into the string bank", read: func(vm *VM, registers *Registers) (OpResult, any) {
			result := readTypedSliceFieldFallback(vm, vm.currentFrame(), registers, 0, value.Field(1), &registers.slicesString[0])
			return result, registers.slicesString[0]
		}, want: []string{"a"}, wantR: opContinue},
		{name: "[]byte into the byte bank", read: func(vm *VM, registers *Registers) (OpResult, any) {
			result := readTypedSliceFieldFallback(vm, vm.currentFrame(), registers, 0, value.Field(2), &registers.slicesByte[0])
			return result, registers.slicesByte[0]
		}, want: []byte("xyz"), wantR: opContinue},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vm := newFallbackTestVM(value)
			result, got := tc.read(vm, &vm.currentFrame().Registers)
			require.Equal(t, tc.wantR, result)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestReadTypedSliceFieldFallbackRejectsOtherWidths(t *testing.T) {
	t.Parallel()
	value := reflect.ValueOf(struct{ Halves []int32 }{Halves: []int32{1, 2}})
	vm := newFallbackTestVM(value)
	registers := &vm.currentFrame().Registers
	registers.SlicesInt[0] = []int64{99}
	result := readTypedSliceFieldFallback(vm, vm.currentFrame(), registers, 0, value.Field(0), &registers.SlicesInt[0])
	require.Equal(t, opPanicError, result)
	require.ErrorIs(t, vm.evalError, fault.ErrTypeMismatch)
	require.Equal(t, []int64{99}, registers.SlicesInt[0], "a mismatch must not touch the destination")

	miss := newFallbackTestVM(value)
	missRegisters := &miss.currentFrame().Registers
	require.Equal(t, opContinue, readTypedSliceFieldFallback(miss, miss.currentFrame(), missRegisters, 0, reflect.Value{}, &missRegisters.SlicesInt[0]))
}

func newFallbackTestVM(receiver reflect.Value) *VM {
	vm := NewVM(context.Background(), NewGlobalStore(), symtab.NewSymbolRegistry(nil))
	var counts [isa.NumRegisterKinds]uint32
	for kind := range counts {
		counts[kind] = 1
	}
	vm.CallStack = []CallFrame{{Function: program.NewNamedFunction("fallback"), Registers: NewRegisters(counts)}}
	vm.FramePointer = 0
	vm.CallStack[0].Registers.General[0] = receiver
	return vm
}
