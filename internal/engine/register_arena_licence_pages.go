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
	"sync/atomic"
)

var (
	// pageCounters counts what page-granular revocation did, while the licence counters are
	// on (SetLicenceCountingForTest).
	pageCounters struct {
		pagesRevoked, runsKept, runsTooShort, pagesRetired, capacityFallbacks atomic.Int64
	}
)

// PageRevocationStats counts what page-granular revocation did since the process started.
type PageRevocationStats struct {
	// PagesRevoked counts pages newly revoked by page-granular hand-offs.
	PagesRevoked int64

	// RunsKept counts releases that kept a run of clean pages of a revoked slab.
	RunsKept int64

	// RunsTooShort counts revoked slabs replaced whole because their longest clean run held
	// less than half the slab, or less than the epoch used of it.
	RunsTooShort int64

	// PagesRetired counts the pages kept runs left behind.
	PagesRetired int64

	// CapacityFallbacks counts extents that left the slab holding their start, revoked as
	// the whole slab (the run-time check that host access stays within the extent).
	CapacityFallbacks int64
}

// pageRevocationStatistics returns the page revocation counters, which move only while
// the licence counters are on (SetLicenceCountingForTest).
//
// Returns PageRevocationStats which is a snapshot.
func pageRevocationStatistics() PageRevocationStats {
	counters := &pageCounters
	return PageRevocationStats{
		PagesRevoked: counters.pagesRevoked.Load(), RunsKept: counters.runsKept.Load(), RunsTooShort: counters.runsTooShort.Load(),
		PagesRetired: counters.pagesRetired.Load(), CapacityFallbacks: counters.capacityFallbacks.Load(),
	}
}
