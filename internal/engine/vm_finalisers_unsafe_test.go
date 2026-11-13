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
)

func TestSliceHeadersAreGoAllocationsWhileTheProgramSetsFinalisers(t *testing.T) {
	for _, sets := range []bool{false, true} {
		vm := newFinaliserTestVM(t, sets)
		vm.bindFinaliserPlacement()
		require.Equal(t, sets, vm.Arena.heapSliceHeaders)
		header := vm.Arena.allocSliceHeader()
		require.Equal(t, !sets, vm.Arena.OwnsSliceHeaderPointer(unsafe.Pointer(header)),
			"a slab header only for a program without finalisers (sets=%v)", sets)
		vm.Arena.Reset()
		require.False(t, vm.Arena.heapSliceHeaders, "Reset clears the binding")
	}
}

func TestAProgramThatSetsFinalisersPlacesNothingInKRefChunks(t *testing.T) {
	for _, sets := range []bool{false, true} {
		vm := newFinaliserTestVM(t, sets)
		vm.Arena.attachLicencePolicy(&VMLimits{})
		require.Equal(t, !sets, krefPlacesFor(vm), "KRef placement only without finalisers (sets=%v)", sets)
		_, placed := vm.krefSnapshot(krefTestNodeType)
		require.Equal(t, !sets, placed, "a struct snapshot (sets=%v)", sets)
		vm.Arena.Reset()
	}
}

func TestAProgramThatSetsFinalisersWalksWithoutTheMemo(t *testing.T) {
	for _, sets := range []bool{false, true} {
		vm := newFinaliserTestVM(t, sets)
		arena := newArena(t)
		vm.Arena = arena
		arena.setLicenceVM(vm)
		host := make([]string, 2)
		noteHandOff(arena, handOffNativeArgUnknown, reflect.ValueOf(host))
		memoised := arena.licenceState.memo.peek(unsafe.Pointer(unsafe.SliceData(host)), reflectValueABIType(reflect.TypeFor[string]()), 2)
		require.Equal(t, !sets, memoised,
			"the memo keeps what it records reachable, so a program that sets finalisers records nothing (sets=%v)", sets)
		arena.setLicenceVM(nil)
		arena.Reset()
	}
}
