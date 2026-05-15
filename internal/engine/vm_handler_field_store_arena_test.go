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
