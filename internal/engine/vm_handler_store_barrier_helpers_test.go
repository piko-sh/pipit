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
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func requirePointeeHandedOff(t *testing.T, arena *RegisterArena, pointee unsafe.Pointer) {
	t.Helper()
	require.True(t, arena.licensed, "test harness: the arena is licensed")
	base, class := arena.licenceSlabOf(uintptr(pointee))
	require.Equal(t, licenceSlabData, class, "the pointee must lie in one of the arena's data slabs")
	require.True(t, arena.licenceState.epochRevoked || containsSlab(arena.licenceState.revoked, base),
		"the store must revoke the slab the pointee lives in")
}

func containsSlab[M ~map[uintptr]V, V any](revoked M, base uintptr) bool {
	_, ok := revoked[base]
	return ok
}

func runWithLicence(t *testing.T, receiver func() any, body func(t *testing.T, vm *VM, frame *CallFrame, registers *Registers)) {
	t.Helper()
	vm, frame, registers := newGeneralFieldFrame(t, receiver())

	vm.Arena.licenceState.blindMarked = false
	vm.Arena.attachLicencePolicy(&VMLimits{})
	require.True(t, vm.Arena.licensed)
	require.NotEmpty(t, registers.General, "test harness: attaching the licence kept the frame's register banks")
	body(t, vm, frame, registers)
}

func newExtWordFrameWithLicence(t *testing.T) (*VM, *CallFrame, *Registers) {
	t.Helper()
	vm, frame, registers := newExtWordFrame(t, op(3, 0, 0))

	vm.Arena.licenceState.blindMarked = false
	vm.Arena.attachLicencePolicy(&VMLimits{})
	require.True(t, vm.Arena.licensed)
	require.NotEmpty(t, registers.General, "test harness: attaching the licence kept the frame's register banks")
	return vm, frame, registers
}
