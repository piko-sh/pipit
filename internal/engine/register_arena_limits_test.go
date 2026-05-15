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

func TestResetClearsTheConfiguredCapsSoAPooledArenaCarriesNoStaleBudget(t *testing.T) {
	t.Parallel()

	arena := newTestArena(t)
	arena.MaxArenaBytes = 4096
	arena.MaxAllocSize = 16

	arena.Reset()

	require.Zero(t, arena.MaxArenaBytes,
		"a pooled arena must not carry one virtual machine's memory budget into the next borrower")
	require.Zero(t, arena.MaxAllocSize,
		"a pooled arena must not carry one virtual machine's allocation cap into the next borrower")
	require.Equal(t, defaultMaxArenaBytes, arena.arenaBudgetLimit(),
		"a cleared budget falls back to the package default rather than an inherited one")
}

func TestAcquireArenaStampsTheLimitsTheVirtualMachineWasGiven(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	vm.Limits.MaxArenaBytes = 8192
	vm.Limits.MaxAllocSize = 32

	arena := vm.acquireArena()
	t.Cleanup(func() {
		arena.MaxArenaBytes = 0
		arena.MaxAllocSize = 0
		PutRegisterArena(arena)
	})

	require.Equal(t, uint64(8192), arena.MaxArenaBytes)
	require.Equal(t, 32, arena.MaxAllocSize)
	require.Equal(t, uint64(8192), arena.arenaBudgetLimit(),
		"the arena enforces the budget the host configured, not the package default")
}

func TestEnforceSingleAllocLimitUsesTheStampedCap(t *testing.T) {
	t.Parallel()

	t.Run("an arena with no cap admits any size", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		arena.MaxAllocSize = 0

		require.NotPanics(t, func() { arena.enforceSingleAllocLimit(1 << 20) })
	})

	t.Run("an arena with a cap refuses a larger request", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		arena.MaxAllocSize = 16

		require.Panics(t, func() { arena.enforceSingleAllocLimit(17) })
		require.NotPanics(t, func() { arena.enforceSingleAllocLimit(16) },
			"the cap is inclusive")
	})
}
