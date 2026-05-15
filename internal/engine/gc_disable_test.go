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
)

func TestDisableMinorGCSkipsCollection(t *testing.T) {
	tests := []struct {
		name            string
		disable         bool
		wantShouldRun   bool
		wantCollections uint64
	}{
		{name: "collection on", disable: false, wantShouldRun: true, wantCollections: 1},
		{name: "collection off", disable: true, wantShouldRun: false, wantCollections: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vm := newGCTestVM()
			arena := vm.Arena
			arena.disableMinorGC = tc.disable
			arena.bytesAllocated = gcInitialThreshold
			vm.checkpointFlags |= checkpointFlagGCPending
			before := arena.gcCount

			require.Equal(t, tc.wantShouldRun, arena.gcShouldRun())
			vm.runPendingCheckpoints()
			require.EqualValues(t, tc.wantCollections, uint64(arena.gcCount-before))
			require.Equal(t, tc.disable, vm.checkpointFlags&checkpointFlagGCPending != 0, "the pending flag is consumed only by a collection")
		})
	}
}

func TestAcquireArenaPropagatesDisableMinorGC(t *testing.T) {
	vm := newGCTestVM()
	vm.Limits.DisableMinorGC = true
	arena := vm.acquireArena()
	defer PutRegisterArena(arena)
	require.True(t, arena.disableMinorGC)
}
