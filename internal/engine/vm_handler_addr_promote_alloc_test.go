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

const addrPromotionRuns = 512

const addrPromotionAllocationCeiling = 4

func TestPromoteAddressedValueEscapingPointeeUsesBoundarySlab(t *testing.T) {
	type pair struct {
		A, B int64
	}
	vm := newTestVM(t)
	vm.Arena = NewRegisterArena()
	frame := &CallFrame{Function: &program.CompiledFunction{}, ProgramCounter: 1}
	source := reflect.ValueOf(&pair{A: 1, B: 2}).Elem()

	var promoted []reflect.Value
	allocs := testing.AllocsPerRun(addrPromotionRuns, func() {
		promoted = append(promoted, promoteAddressedValue(vm, frame, source))
	})

	require.LessOrEqual(t, allocs, float64(addrPromotionAllocationCeiling))
	first, second := promoted[len(promoted)-2], promoted[len(promoted)-1]
	require.True(t, first.CanAddr() && second.CanAddr(), "boundary slots are addressable")
	require.NotEqual(t, first.Addr().Pointer(), second.Addr().Pointer(), "each pointee gets its own cell")
	require.Equal(t, int64(2), second.Field(1).Int(), "the copy carries the value")
}

func TestHandleAddrNonAddressableValueUsesBoundarySlab(t *testing.T) {
	vm := newTestVM(t)
	vm.Arena = NewRegisterArena()
	frame := &CallFrame{Function: &program.CompiledFunction{}, ProgramCounter: 1}
	registers := &Registers{General: make([]reflect.Value, 2)}
	instruction := isa.NewInstruction(isa.OpAddr, 0, 1, 0)

	allocs := testing.AllocsPerRun(addrPromotionRuns, func() {
		registers.General[1] = reflect.ValueOf(int64(7))
		require.Equal(t, opContinue, handleAddr(vm, frame, registers, instruction))
	})

	require.LessOrEqual(t, allocs, float64(addrPromotionAllocationCeiling))
	pointer := registers.General[0]
	require.Equal(t, reflect.Pointer, pointer.Kind())
	require.Equal(t, int64(7), pointer.Elem().Int())
	require.True(t, registers.General[1].CanAddr(), "the source register now names the shared cell")
	require.Equal(t, pointer.Pointer(), registers.General[1].Addr().Pointer(), "pointer and register share one cell")
}
