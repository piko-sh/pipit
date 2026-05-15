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

//go:build !safe && !(js && wasm)

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func newIndirectCell(t *testing.T, box any) *program.UpvalueCell {
	t.Helper()
	cell := &program.UpvalueCell{IsIndirect: true, GeneralValue: reflect.ValueOf(box)}
	cacheIndirectTarget(cell)
	require.NotNil(t, cell.IndirectTarget)
	return cell
}

func TestClosureWriteTypedSliceNoBoxing(t *testing.T) {
	vm, registers := newHandlerTestVM(t)
	registers.SlicesInt = [][]int64{{1, 2, 3}}
	box := new([]int)
	cell := newIndirectCell(t, box)

	allocs := testing.AllocsPerRun(100, func() {
		writeRegisterToCell(vm, vm.Arena, cell, registers, isa.RegisterSliceInt, 0)
	})
	require.Zero(t, allocs)
	require.Equal(t, []int{1, 2, 3}, *box)
}

func TestClosureWriteScalarStoresThroughPointer(t *testing.T) {
	vm, registers := newHandlerTestVM(t)
	registers.Ints[0] = 1 << 40
	registers.Floats[0] = 2.5
	registers.Bools[0] = true
	registers.Uints[0] = 7
	intBox, floatBox, boolBox, uintBox := new(int), new(float64), new(bool), new(uint)
	cells := []*program.UpvalueCell{newIndirectCell(t, intBox), newIndirectCell(t, floatBox), newIndirectCell(t, boolBox), newIndirectCell(t, uintBox)}
	kinds := []isa.RegisterKind{isa.RegisterInt, isa.RegisterFloat, isa.RegisterBool, isa.RegisterUint}

	allocs := testing.AllocsPerRun(100, func() {
		for i, cell := range cells {
			writeRegisterToCell(vm, vm.Arena, cell, registers, kinds[i], 0)
		}
	})
	require.Zero(t, allocs)
	require.EqualValues(t, 1<<40, *intBox)
	require.InDelta(t, 2.5, *floatBox, 0)
	require.True(t, *boolBox)
	require.EqualValues(t, 7, *uintBox)
}

func TestClosureWriteNarrowScalarFallsBackToReflect(t *testing.T) {
	t.Parallel()
	vm, registers := newHandlerTestVM(t)
	registers.Ints[0] = 0x1234
	box := new(int8)
	cell := newIndirectCell(t, box)

	writeRegisterToCell(vm, vm.Arena, cell, registers, isa.RegisterInt, 0)
	require.EqualValues(t, int8(0x34), *box)
}

func TestClosureWriteArenaSliceBackingIsMaterialised(t *testing.T) {
	t.Parallel()
	vm, registers := newHandlerTestVM(t)
	backing := vm.Arena.AllocIntBacking(3)
	copy(backing, []int64{4, 5, 6})
	registers.SlicesInt = [][]int64{backing}
	box := new([]int)
	cell := newIndirectCell(t, box)

	writeRegisterToCell(vm, vm.Arena, cell, registers, isa.RegisterSliceInt, 0)
	require.Equal(t, []int{4, 5, 6}, *box)
	require.False(t, vm.Arena.ownsSliceBackingOf(reflect.ValueOf(*box)), "the cell must not point into the arena")
}
