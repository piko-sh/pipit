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
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func cellHolding(vm *VM, live unsafe.Pointer) *program.UpvalueCell {
	cell := newCellFor(vm)
	cell.Kind = isa.RegisterGeneral
	cell.GeneralValue = reflect.NewAt(reflect.TypeFor[gcTracePair](), live)
	return cell
}

func TestSharedMarkReadsACellItsVMMade(t *testing.T) {
	vm := newGoroutineGCTestVM(t)
	arena := vm.Arena
	live := retirePair(t, arena)
	retirePair(t, arena)
	vm.CallStack[0].upvalues = []upvalue{{Value: cellHolding(vm, live)}}

	arena.MinorGC(vm)

	require.Len(t, arena.oldGenericByteSlabs, 1, "the slab nothing reaches is reclaimed")
	require.True(t, retainsGenericSlabHolding(arena, live), "the slab a cell the VM made reaches is kept")
}

func TestCellMakerNamesTheEpochWhoseVMMadeTheCell(t *testing.T) {
	vm := newGoroutineGCTestVM(t)
	other := newGoroutineGCTestVM(t)
	first, second := newCellFor(vm), newCellFor(vm)
	require.NotZero(t, first.Maker)
	require.Equal(t, first.Maker, second.Maker, "one epoch's cells share its tag")
	require.NotEqual(t, first.Maker, newCellFor(other).Maker, "another arena's epoch has its own tag")
	vm.Arena.licenceState.cellMaker = 0
	require.NotEqual(t, first.Maker, newCellFor(vm).Maker, "a later epoch has its own tag")
}

func TestSharedMarkReadsNoCellItCannotOwn(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		cell   func(vm *VM) *program.UpvalueCell
		closed bool
	}{
		{"a cell another epoch made", func(vm *VM) *program.UpvalueCell {
			return newCellFor(newGoroutineGCTestVM(t))
		}, false},
		{"a cell its VM made that a walk handed off", func(vm *VM) *program.UpvalueCell {
			cell := newCellFor(vm)
			cell.Kind = isa.RegisterGeneral
			cell.HandedOff.Store(1)
			return cell
		}, true},
		{"a cell its VM made while a callback's blind hand-off is pending", func(vm *VM) *program.UpvalueCell {
			cell := newCellFor(vm)
			vm.Arena.licenceState.chainBlind.all.Add(1)
			return cell
		}, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			vm := newGoroutineGCTestVM(t)
			arena := vm.Arena
			cell := testCase.cell(vm)
			cell.Kind = isa.RegisterGeneral
			vm.CallStack[0].upvalues = []upvalue{{Value: cell}}
			stop := make(chan struct{})
			var writer sync.WaitGroup
			writer.Go(func() {
				for round := 0; ; round++ {
					select {
					case <-stop:
						return
					default:
					}
					cell.GeneralValue = reflect.ValueOf(&gcTracePair{A: int64(round)})
				}
			})
			defer writer.Wait()
			defer close(stop)

			for round := range 12 {
				retirePair(t, arena)
				arena.MinorGC(vm)
				if testCase.closed {
					require.Len(t, arena.oldGenericByteSlabs, round+1, "the mark fails closed rather than read the cell")
				} else {
					require.Empty(t, arena.oldGenericByteSlabs, "the mark reclaims without reading the cell")
				}
			}
		})
	}
}
