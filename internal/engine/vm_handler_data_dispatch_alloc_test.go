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

type allocPointee struct {
	Name  string
	Count int
}

func newAllocFrame(t *testing.T, pointeeType reflect.Type, heapCell uint8, arenaSafe bool) (*VM, *CallFrame, *Registers) {
	t.Helper()

	vm, frame, registers := newExtWordFrame(t, op(0, 0, heapCell))
	frame.Function.TypeTable = []reflect.Type{pointeeType}
	if arenaSafe {
		frame.Function.ArenaSafeAllocPCs = map[int]bool{0: true}
	}
	return vm, frame, registers
}

func TestAllocIndirectProducesATypedPointer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		pointeeType reflect.Type
		heapCell    uint8
		arenaSafe   bool
		load        func(*Registers)
		sourceKind  isa.RegisterKind
		inspect     func(*testing.T, reflect.Value)
	}{
		{
			name: "an int pointee carries the converted register value", pointeeType: reflect.TypeFor[int](),
			load: func(r *Registers) { r.Ints[1] = 5 }, sourceKind: isa.RegisterInt,
			inspect: func(t *testing.T, pointer reflect.Value) {
				require.Equal(t, 5, pointer.Elem().Interface())
			},
		},
		{
			name: "a heap cell is requested explicitly", pointeeType: reflect.TypeFor[int](), heapCell: isa.AllocIndirectHeapCell,
			load: func(r *Registers) { r.Ints[1] = 6 }, sourceKind: isa.RegisterInt,
			inspect: func(t *testing.T, pointer reflect.Value) {
				require.Equal(t, 6, pointer.Elem().Interface())
			},
		},
		{
			name: "an arena-safe pointer-free pointee still carries its value", pointeeType: reflect.TypeFor[int64](),
			arenaSafe: true, load: func(r *Registers) { r.Ints[1] = 7 }, sourceKind: isa.RegisterInt,
			inspect: func(t *testing.T, pointer reflect.Value) {
				require.Equal(t, int64(7), pointer.Elem().Interface())
			},
		},
		{
			name: "a struct pointee comes from the boundary snapshot chunk", pointeeType: reflect.TypeFor[allocPointee](),
			load:       func(r *Registers) { r.General[1] = reflect.ValueOf(allocPointee{Name: "n", Count: 2}) },
			sourceKind: isa.RegisterGeneral,
			inspect: func(t *testing.T, pointer reflect.Value) {
				require.Equal(t, allocPointee{Name: "n", Count: 2}, pointer.Elem().Interface())
			},
		},
		{
			name: "an array pointee comes from the boundary snapshot chunk", pointeeType: reflect.TypeFor[[2]int](),
			load: func(r *Registers) { r.General[1] = reflect.ValueOf([2]int{1, 2}) }, sourceKind: isa.RegisterGeneral,
			inspect: func(t *testing.T, pointer reflect.Value) {
				require.Equal(t, [2]int{1, 2}, pointer.Elem().Interface())
			},
		},
		{
			name: "a slice pointee comes from the arena slice-header slab", pointeeType: reflect.TypeFor[[]int64](),
			load: func(r *Registers) { r.SlicesInt[1] = []int64{3, 4} }, sourceKind: isa.RegisterSliceInt,
			inspect: func(t *testing.T, pointer reflect.Value) {
				require.Equal(t, []int64{3, 4}, pointer.Elem().Interface())
			},
		},
		{
			name: "a string pointee falls back to a plain heap cell", pointeeType: reflect.TypeFor[string](),
			load: func(r *Registers) { r.Strings[1] = "v" }, sourceKind: isa.RegisterString,
			inspect: func(t *testing.T, pointer reflect.Value) {
				require.Equal(t, "v", pointer.Elem().Interface())
			},
		},
		{
			name: "an unset source leaves the pointee zeroed", pointeeType: reflect.TypeFor[int](),
			load: func(*Registers) {}, sourceKind: isa.RegisterGeneral,
			inspect: func(t *testing.T, pointer reflect.Value) {
				require.Equal(t, 0, pointer.Elem().Interface())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newAllocFrame(t, tt.pointeeType, tt.heapCell, tt.arenaSafe)
			tt.load(registers)

			got := handleAllocIndirect(vm, frame, registers, op(0, 1, uint8(tt.sourceKind)))
			require.Equal(t, opContinue, got)
			require.Equal(t, 2, frame.ProgramCounter, "the type-index word must be stepped over")

			pointer := registers.General[0]
			require.Equal(t, reflect.Pointer, pointer.Kind(), "the destination holds a pointer")
			require.Equal(t, tt.pointeeType, pointer.Type().Elem())
			tt.inspect(t, pointer)
		})
	}
}

func TestAllocIndirectTakesTheAddressOfAnAddressableSource(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newAllocFrame(t, reflect.TypeFor[allocPointee](), 0, false)
	cell := reflect.New(reflect.TypeFor[allocPointee]()).Elem()
	cell.Set(reflect.ValueOf(allocPointee{Name: "existing", Count: 1}))
	registers.General[1] = cell

	got := handleAllocIndirect(vm, frame, registers, op(0, 1, uint8(isa.RegisterGeneral)))
	require.Equal(t, opContinue, got)
	require.Equal(t, cell.Addr().Pointer(), registers.General[0].Pointer(),
		"an addressable source of the right type needs no fresh cell")
}

func TestAllocIndirectRefusesATypeIndexPastTheTable(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newExtWordFrame(t, op(4, 0, 0))
	frame.Function.TypeTable = []reflect.Type{reflect.TypeFor[int]()}
	registers.Ints[1] = 1

	got := handleAllocIndirect(vm, frame, registers, op(0, 1, uint8(isa.RegisterInt)))
	require.Equal(t, opPanicError, got)
	require.Error(t, vm.evalError)
}

func TestMaterialiseAllocIndirectSourceHonoursTheEscapeVerdict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arenaSafe bool
		heapCell  bool
		wantSame  bool
	}{
		{name: "a proven-confined pointee keeps the arena value", arenaSafe: true, heapCell: false, wantSame: true},
		{name: "an unproven pointee is detached from the arena", arenaSafe: false, heapCell: false, wantSame: false},
		{name: "a heap cell is detached whatever the verdict", arenaSafe: true, heapCell: true, wantSame: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, _, _ := newStandardVM(t)
			source := reflect.ValueOf([]int{1, 2, 3})

			got := materialiseAllocIndirectSource(vm, source, tt.arenaSafe, tt.heapCell)
			require.Equal(t, source.Interface(), got.Interface(), "the value itself must survive either route")
			if tt.wantSame {
				require.Equal(t, source.Pointer(), got.Pointer(), "the confined route must not copy")
			}
		})
	}
}
