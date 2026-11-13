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
	// krefChunkCounters counts the class's events, while the licence counters are on
	// (SetLicenceCountingForTest).
	krefChunkCounters struct {
		// chunks counts chunks made.
		chunks atomic.Int64

		// slots counts slots and runs handed out.
		slots atomic.Int64

		// detached counts chunks detached at release (revoked) or evicted at Reset.
		detached atomic.Int64

		// reused counts chunks kept by a Reset for the next epoch.
		reused atomic.Int64

		// refused counts placements refused (class cap, run too large, zero-size type).
		refused atomic.Int64
	}
)

// KRefChunkCounts is a snapshot of the KRef class's counters.
type KRefChunkCounts struct {
	// Chunks is the number of chunks made.
	Chunks int64

	// Slots is the number of slots and runs handed out.
	Slots int64

	// Detached is the number of chunks detached (revoked at release, or evicted).
	Detached int64

	// Reused is the number of chunks a Reset kept for reuse.
	Reused int64

	// Refused is the number of placements the class refused.
	Refused int64
}

// krefChunkStatistics returns the KRef class's counters since the process started, which
// move only while the licence counters are on (SetLicenceCountingForTest).
//
// Returns KRefChunkCounts which is the snapshot.
func krefChunkStatistics() KRefChunkCounts {
	return KRefChunkCounts{
		Chunks: krefChunkCounters.chunks.Load(), Slots: krefChunkCounters.slots.Load(),
		Detached: krefChunkCounters.detached.Load(), Reused: krefChunkCounters.reused.Load(),
		Refused: krefChunkCounters.refused.Load(),
	}
}

// krefChunkCount returns the number of KRef chunks the arena holds.
//
// Returns int which is the count.
func (a *RegisterArena) krefChunkCount() int { return len(a.kref.sorted) }

// krefHeldBytes returns the byte size of every KRef chunk the arena holds.
//
// Returns uint64 which is the total.
func (a *RegisterArena) krefHeldBytes() uint64 {
	total := uint64(0)
	for _, chunk := range a.kref.sorted {
		total += uint64(chunk.end - chunk.base)
	}
	return total
}
