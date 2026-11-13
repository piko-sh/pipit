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

//go:build pipit_testhooks && !safe && !(js && wasm)

package engine

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestPanicInTheBlindWindowLeavesTheReadingPending(t *testing.T) {
	arena := newArena(t)
	escaped := arenaString(arena, "named by a walk another arena of the tree skipped")
	base := unsafe.SliceData(arena.byteSlab)
	arena.blindCounters().all.Add(1)
	require.NoError(t, ArmAbortInjection("blind-window", 1))
	err := recoveredError(arena.releaseLicence)
	report := DisarmAbortInjection()
	require.True(t, report.Fired, "the release must reach the window")
	require.Error(t, err)
	require.True(t, arena.licenceState.epochRevoked, "the reading was applied before the window")
	require.True(t, arena.blindSinceReading(), "the interval must not restart before the reading is applied")
	arena.Reset()
	require.NotEqual(t, base, unsafe.SliceData(arena.byteSlab), "the release after the panic must still abandon what the reading covers")
	require.Equal(t, "named by a walk another arena of the tree skipped", escaped)
	runtime.KeepAlive(escaped)
}
