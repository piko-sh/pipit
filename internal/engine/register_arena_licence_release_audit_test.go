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

//go:build pipit_reuse_audit && !safe && !(js && wasm)

package engine

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestReleaseClosurePageCheckAcceptsAMissWithBoxSlabBounds(t *testing.T) {
	arena := newArena(t)
	arena.complexBoxSlab = make([]complex128, 4)
	spans, ok := arena.appendLicenceDataSpans(nil)
	require.True(t, ok, "the arena's data slabs are pairwise disjoint")
	arena.licenceState.closureSpans, arena.licenceState.closureSpansOK = spans, true

	heap := make([]byte, 64)
	address := uintptr(unsafe.Pointer(&heap[0]))
	wantBase, _, wantOK := licenceDataSlabSpan(arena, address)
	require.False(t, wantOK, "a heap address lies in no data slab")
	require.NotZero(t, wantBase, "the miss carries the complex box slab's bounds")

	require.NotPanics(t, func() {
		_, _, found := arena.closureDataSlabSpan(address)
		require.False(t, found, "the table misses too")
	})
	require.NotNil(t, heap)
}
