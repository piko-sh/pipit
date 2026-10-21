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
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestStarAppendGrowsAHostSliceOnTheHeap(t *testing.T) {
	runWithLicence(t, func() any { return &generalFields{} }, func(t *testing.T, vm *VM, frame *CallFrame, registers *Registers) {
		var host []byte
		registers.General[3] = reflect.ValueOf(&host)
		for index := range 40 {
			registers.Uints[4] = uint64('a' + index%26)
			require.Equal(t, opContinue, handleAppendByteViaPointer(vm, frame, registers,
				isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpAppendByteViaPointer), 3, 4)))
		}
		require.Len(t, host, 40)
		require.False(t, vm.Arena.OwnsSliceBacking(unsafe.Pointer(unsafe.SliceData(host))),
			"a host slice appended to through a pointer must not grow into an arena backing")
		source := reflect.ValueOf([]byte("spread through a pointer to a host slice"))
		registers.General[5] = source
		require.Equal(t, opContinue, handleAppendSpreadByteViaPointer(vm, frame, registers,
			isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpAppendSpreadByteViaPointer), 3, 5)))
		require.False(t, vm.Arena.OwnsSliceBacking(unsafe.Pointer(unsafe.SliceData(host))),
			"a host slice spread into through a pointer must not grow into an arena backing")
	})
}

func TestStarAppendKeepsAnArenaHeaderInTheArena(t *testing.T) {
	vm, frame, registers := newGeneralFieldFrame(t, &generalFields{})
	header := vm.Arena.allocSliceHeader()
	registers.General[3] = reflect.NewAt(reflect.TypeFor[[]byte](), unsafe.Pointer(header))
	for index := range 40 {
		registers.Uints[4] = uint64('a' + index%26)
		require.Equal(t, opContinue, handleAppendByteViaPointer(vm, frame, registers,
			isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpAppendByteViaPointer), 3, 4)))
	}
	grown := *(*[]byte)(unsafe.Pointer(header))
	require.Len(t, grown, 40)
	require.True(t, vm.Arena.OwnsSliceBacking(unsafe.Pointer(unsafe.SliceData(grown))),
		"a local's header in the slice-header slab keeps the arena fast path")
}
