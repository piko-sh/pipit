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

func TestSafeBuildPlacesNothingInKRefChunks(t *testing.T) {
	arena := NewRegisterArena()
	_, found := arena.krefFind(1)
	require.False(t, found)
	_, placed := krefMakeBacking(nil, reflect.TypeFor[[]*int](), 1, 1)
	require.False(t, placed)
	_, placed = krefPointee(nil, nil, 0, reflect.TypeFor[*int](), false)
	require.False(t, placed)
	_, placed = newTestVM(t).krefSnapshot(reflect.TypeFor[struct{ p *int }]())
	require.False(t, placed)
	arena.krefDropAll()
	arena.krefReset()
	arena.settlePointeePlacements()
	arena.settleSnapshotPlacements()
	require.Empty(t, arena.kref.sorted)
}
