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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
)

func TestMapSizeHintHonoursTheSingleAllocationCap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		maxAllocSize int
		hintLog      uint8
		refused      bool
	}{
		{name: "no configured cap admits any hint", maxAllocSize: 0, hintLog: 20},
		{name: "a hint of zero is never refused", maxAllocSize: 4, hintLog: 0},
		{name: "a hint inside the cap is admitted", maxAllocSize: 16, hintLog: 4},
		{name: "a hint at the cap is admitted", maxAllocSize: 8, hintLog: 3},
		{name: "a hint one step past the cap is refused", maxAllocSize: 8, hintLog: 4, refused: true},
		{name: "a large hint against a small cap is refused", maxAllocSize: 16, hintLog: 30, refused: true},
		{name: "a hint past the encoding cap is still refused", maxAllocSize: 16, hintLog: 255, refused: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm := newTestVM(t)
			t.Cleanup(vm.ReleaseArena)
			vm.Limits.MaxAllocSize = tt.maxAllocSize

			got, refused := refuseMapSizeHint(vm, tt.hintLog)

			require.Equal(t, tt.refused, refused)
			if !tt.refused {
				require.Equal(t, opContinue, got)
				return
			}
			require.Equal(t, opPanicError, got)
			require.ErrorIs(t, vm.evalError, fault.ErrAllocationLimit,
				"a refused reservation reports the allocation limit, matching make slice and make chan")
		})
	}
}

func TestEveryUpFrontReservationConsultsTheAllocationCap(t *testing.T) {
	t.Parallel()

	const allocationCap = 8

	vm := newTestVM(t)
	t.Cleanup(vm.ReleaseArena)
	vm.Limits.MaxAllocSize = allocationCap

	_, refused := refuseMapSizeHint(vm, 4)

	require.True(t, refused,
		"a hint of sixteen entries must be refused against a cap of eight, as the slice and channel paths would be")
}
