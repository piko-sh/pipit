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

//go:build !safe

package engine

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type sliceOwner struct {
	Numbers []int64
}

func storeArenaSliceIntoField(t *testing.T, annotated map[int]bool) (fieldData, arenaData uintptr) {
	t.Helper()
	vm := newTestVM(t)
	vm.Arena = NewRegisterArena()
	backing := vm.Arena.AllocIntBacking(8)
	receiver := reflect.New(reflect.TypeFor[sliceOwner]())
	registers := &Registers{General: []reflect.Value{receiver, reflect.ValueOf(backing)}}
	frame := &CallFrame{Function: &program.CompiledFunction{FieldStoreArenaSafePCs: annotated}, ProgramCounter: 1}

	require.Equal(t, opContinue, handleSetField(vm, frame, registers, isa.NewInstruction(isa.OpSetField, 0, 0, 1)))

	stored := receiver.Elem().Field(0)
	require.Equal(t, 8, stored.Len())
	return stored.Pointer(), reflect.ValueOf(backing).Pointer()
}

func TestSetFieldKeepsArenaValueOnlyWhenAnnotated(t *testing.T) {
	fieldData, arenaData := storeArenaSliceIntoField(t, map[int]bool{0: true})
	require.Equal(t, arenaData, fieldData, "an annotated store keeps the arena backing")

	fieldData, arenaData = storeArenaSliceIntoField(t, nil)
	require.NotEqual(t, arenaData, fieldData, "an unannotated store copies the backing out of the arena")
}

func arenaGeneralLeaves(t *testing.T, arena *RegisterArena) []generalLeaf {
	t.Helper()
	size := unsafe.Sizeof(generalLeaf{})
	pointer := arena.AllocBytes(2*size, unsafe.Alignof(generalLeaf{}))
	leaves := unsafe.Slice((*generalLeaf)(pointer), 2)
	leaves[0], leaves[1] = generalLeaf{Count: 1}, generalLeaf{Count: 2}
	require.True(t, arena.ownsAnyStorage(unsafe.Pointer(unsafe.SliceData(leaves))), "the leaves must be arena storage")
	return leaves
}

func TestFusedFieldCopyHasNoStoreBarrier(t *testing.T) {
	const valuesField = 2

	t.Run("the ungranted SET copies the arena backing out", func(t *testing.T) {
		destination := &generalFields{}
		vm, frame, registers := newGeneralFieldFrame(t, destination)
		vm.Arena = NewRegisterArena()
		leaves := arenaGeneralLeaves(t, vm.Arena)
		registers.General[2] = reflect.ValueOf(leaves)

		require.Equal(t, opContinue, handleSetStructFieldGeneral(vm, frame, registers, op(1, 2, valuesField)))
		require.Len(t, destination.Values, 2)
		require.False(t, vm.Arena.ownsAnyStorage(unsafe.Pointer(unsafe.SliceData(destination.Values))),
			"an ungranted field store must not leave arena storage in a heap struct")
	})

	t.Run("the fused copy keeps the arena backing", func(t *testing.T) {
		source := &generalFields{}
		destination := &generalFields{}
		vm, frame, registers := newGeneralFieldFrame(t, source, op(valuesField, 0, 0))
		vm.Arena = NewRegisterArena()
		source.Values = arenaGeneralLeaves(t, vm.Arena)
		registers.General[2] = reflect.ValueOf(destination)

		require.Equal(t, opContinue, handleCopyStructFieldGeneral(vm, frame, registers, op(1, 2, valuesField)))
		require.Len(t, destination.Values, 2)
		require.True(t, vm.Arena.ownsAnyStorage(unsafe.Pointer(unsafe.SliceData(destination.Values))),
			"the fused copy moves the arena slice header into the heap struct with no barrier")
	})
}
