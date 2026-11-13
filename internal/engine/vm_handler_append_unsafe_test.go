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

func TestAppendInPlaceIntoAHostBackingRevokesTheElementsSlab(t *testing.T) {
	vm, frame, registers := newExtWordFrameWithLicence(t)
	host := make([][]string, 0, 4)
	header := vm.Arena.allocSliceHeader()
	header.Data = unsafe.Pointer(unsafe.SliceData(host[:1]))
	header.Cap = cap(host)
	registers.General[1] = reflect.NewAt(reflect.TypeFor[[][]string](), unsafe.Pointer(header)).Elem()
	require.True(t, vm.Arena.OwnsSliceHeaderPointer(unsafe.Pointer(header)))
	text := arenaString(vm.Arena, "appended in place into a host backing")
	registers.General[2] = reflect.ValueOf([]string{text})

	require.Equal(t, opContinue, handleAppendInPlace(vm, frame, registers, isa.NewInstruction(isa.OpAppendInPlace, 1, 1, 2)))

	require.Equal(t, text, host[:1][0][0], "the element lands in the host's backing")
	requireByteSlabRevoked(t, vm.Arena, text)
}

func TestAppendInPlaceThroughAPointerToAHostSliceRevokesTheElementsSlab(t *testing.T) {
	vm, frame, registers := newExtWordFrameWithLicence(t)
	host := make([][]string, 0, 4)
	registers.General[1] = reflect.ValueOf(&host)
	text := arenaString(vm.Arena, "appended through a pointer to a host slice")
	registers.General[2] = reflect.ValueOf([]string{text})

	require.Equal(t, opContinue, handleAppendInPlace(vm, frame, registers, isa.NewInstruction(isa.OpAppendInPlace, 1, 1, 2)))

	require.Equal(t, text, host[0][0], "the element lands in the host's slice")
	requireByteSlabRevoked(t, vm.Arena, text)
}
