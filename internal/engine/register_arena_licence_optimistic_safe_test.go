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

//go:build safe || (js && wasm)

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeBuildKeepsNoStringOptimistically(t *testing.T) {
	arena := safeArena(t)
	require.False(t, optimisticOwnValueStore(arena, reflect.ValueOf(new(string)).Elem(), reflect.ValueOf("x")))
	require.False(t, optimisticByteSlabRevoked(arena, "x"))
	require.False(t, optimisticStringSlotStore(arena, reflect.ValueOf(new(string)).Elem(), reflect.ValueOf("x")))
	optimisticCloneSliceStrings(arena, reflect.ValueOf([]string{"x"}))
	require.Equal(t, reflect.TypeFor[*int](), optimisticNew(arena, reflect.TypeFor[int]()).Type())
}

func TestSafeBuildNoteKeptStringRevokesTheEpoch(t *testing.T) {
	arena := safeArena(t)
	sequence := arena.licenceState.noteSeq
	noteKeptString(arena, "kept")
	require.Equal(t, sequence+1, arena.licenceState.noteSeq)
	require.True(t, arena.licenceState.epochRevoked)

	withoutLicence := NewRegisterArena()
	noteKeptString(withoutLicence, "kept")
	require.False(t, withoutLicence.licenceState.epochRevoked)
}
