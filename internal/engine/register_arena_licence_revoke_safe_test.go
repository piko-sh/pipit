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

func TestSafeBuildRevokesTheEpochForEveryWalk(t *testing.T) {
	arena := safeArena(t)
	require.False(t, arena.revokeTrivially(reflect.ValueOf("x"), false))
	arena.walkHandOff(reflect.ValueOf("x"), false, handOffNativeArgRetains, false)
	require.True(t, arena.licenceState.epochRevoked)
	arena.adoptForeignRevocationsLocked()
}

func TestLicenceStringNamesNothingOnlyWhenEmpty(t *testing.T) {
	require.True(t, licenceStringNamesNothing(nil, ""))
	require.False(t, licenceStringNamesNothing(nil, "x"))
}

func TestLicenceCoveredAddressNeedsACoveredSlab(t *testing.T) {
	require.False(t, licenceCoveredAddress(NewRegisterArena(), 1), "an arena without a licence")
	arena := safeArena(t)
	require.False(t, licenceCoveredAddress(arena, 0))
	bytes := arena.AllocStringBytes(8)
	address := licenceSlabAddress(bytes)
	require.False(t, licenceCoveredAddress(arena, address), "nothing is covered yet")
	arena.revokeSlab(licenceSlabAddress(arena.byteSlab), handOffNativeArgRetains)
	require.Equal(t, arena.licenceCoveredSlab(licenceSlabAddress(arena.byteSlab)), licenceCoveredAddress(arena, address))
	require.False(t, licenceCoveredAddress(arena, licenceSlabAddress(make([]byte, 1))), "an address outside the arena")
}

func TestLicenceRevokedSlabReadsTheSlabsBase(t *testing.T) {
	arena := safeArena(t)
	require.False(t, licenceRevokedSlab[byte](arena, nil))
	arena.AllocStringBytes(8)
	require.False(t, licenceRevokedSlab(arena, arena.byteSlab))
	arena.revokeSlab(licenceSlabAddress(arena.byteSlab), handOffNativeArgRetains)
	require.True(t, licenceRevokedSlab(arena, arena.byteSlab))
}
