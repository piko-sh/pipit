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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRevokeByteSlabsFromRevokesTheChosenSlabs(t *testing.T) {
	arena := safeArena(t)
	arena.AllocStringBytes(8)
	current := licenceSlabAddress(arena.byteSlab)
	arena.revokeByteSlabsFrom(false)
	require.False(t, arena.licenceRevokedBase(current), "only the retired slabs, of which there are none")
	arena.revokeByteSlabsFrom(true)
	require.True(t, arena.licenceRevokedBase(current), "the current slab too")
}
