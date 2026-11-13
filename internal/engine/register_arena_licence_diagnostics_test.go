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

func TestLicenceDiagnosticsReadsEveryCounter(t *testing.T) {
	before := LicenceDiagnostics()
	foreignStringsKept.Add(1)
	mutableByteSlabGrows.Add(2)
	storedValuesWalkedOnce.Add(3)
	krefChunkCounters.slots.Add(4)
	optimisticCounts[OptimisticMake].Add(5)
	t.Cleanup(func() {
		foreignStringsKept.Add(-1)
		mutableByteSlabGrows.Add(-2)
		storedValuesWalkedOnce.Add(-3)
		krefChunkCounters.slots.Add(-4)
		optimisticCounts[OptimisticMake].Add(-5)
	})
	after := LicenceDiagnostics()
	require.Equal(t, int64(1), after.ForeignStringsKept-before.ForeignStringsKept)
	require.Equal(t, int64(2), after.MutableByteSlabs-before.MutableByteSlabs)
	require.Equal(t, int64(3), after.WalkedOnce-before.WalkedOnce)
	require.Equal(t, int64(4), after.KRefChunks.Slots-before.KRefChunks.Slots)
	require.Equal(t, int64(5), after.Optimistic[OptimisticMake]-before.Optimistic[OptimisticMake])
	require.Equal(t, abortedArenaCount(), after.AbortedArenas)
	require.Equal(t, abortUnwindStats(), after.AbortUnwind)
	require.Equal(t, licenceCheckStatistics(), after.LicenceCheck)
}
