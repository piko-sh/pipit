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

func TestResetDropsAHandedOnChargeTableOneRunGrewLarge(t *testing.T) {
	arena := NewRegisterArena()
	for key := range uintptr(4 * handedOnChargesKeptSize) {
		arena.noteChargeHandedOn(key+1, 1)
	}
	require.Len(t, arena.handedOnCharges, 4*handedOnChargesKeptSize)

	arena.Reset()

	require.Nil(t, arena.handedOnCharges, "a pooled arena keeps no large table from one run's heavy slab growth")
}

func TestResetKeepsASmallHandedOnChargeTableEmpty(t *testing.T) {
	arena := NewRegisterArena()
	arena.noteChargeHandedOn(1, 1)

	arena.Reset()

	require.NotNil(t, arena.handedOnCharges, "a small table is kept for the next run")
	require.Empty(t, arena.handedOnCharges, "and holds no stale record a slab at the same address could match")
}

func TestNoteChargeHandedOnRecordsOnlyARealHandOff(t *testing.T) {
	arena := NewRegisterArena()
	arena.noteChargeHandedOn(0, 8)
	arena.noteChargeHandedOn(8, 0)
	require.Empty(t, arena.handedOnCharges)
	arena.noteChargeHandedOn(8, 8)
	require.Contains(t, arena.handedOnCharges, uintptr(8))
}
