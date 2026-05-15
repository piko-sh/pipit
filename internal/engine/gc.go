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

const (
	// gcInitialThreshold is the default value for nextGCAt on first allocation, sized to
	// roughly the initial byte-slab capacity so a short-running Execute that stays under the
	// threshold never pays for a GC cycle. Long-running scripts allocate past this and
	// trigger the first MinorGC, after which the adaptive tuner takes over.
	gcInitialThreshold int64 = 1 << 20

	// gcMinThresholdDelta is the minimum number of bytes that must accumulate between
	// consecutive MinorGC calls. Prevents pathological tight loops where a single allocation
	// immediately re-triggers GC because postGCBytes is already near the previous threshold.
	gcMinThresholdDelta int64 = 1 << 20

	// gcMaxThresholdDelta caps the upper bound on nextGCAt growth per cycle. Without it the
	// threshold could grow unboundedly on mostly-live arenas, hiding genuine bloat behind
	// ever-deferred GC cycles.
	gcMaxThresholdDelta int64 = int64(InitialByteSlabSize) * int64(maxArenaMultiplier) * 64

	// gcRatioMostlyLive is the live/total ratio above which the adaptive tuner backs off the
	// GC frequency. When most of the arena is reachable, GC is doing little useful work, so
	// we let allocation push further before trying again.
	gcRatioMostlyLive = 0.75

	// gcRatioMostlyGarbage is the live/total ratio below which the tuner keeps the GC
	// frequency tight. When the arena is mostly garbage each cycle is recovering significant
	// memory, so a modest threshold makes sense.
	gcRatioMostlyGarbage = 0.25

	// gcMostlyLiveGrowthMultiplier scales the observed allocation growth when the arena is
	// mostly live. A larger multiplier means the next GC is deferred further so allocation
	// can continue without thrashing.
	gcMostlyLiveGrowthMultiplier int64 = 4

	// gcBalancedGrowthMultiplier is the growth multiplier used when the live/total ratio is
	// between the mostly-live and mostly-garbage thresholds.
	gcBalancedGrowthMultiplier int64 = 2
)

// MinorGC runs a single stop-the-world arena garbage-collection cycle.
//
// Takes vm (*VM) which is the owning VM, used for root enumeration.
func (a *RegisterArena) MinorGC(vm *VM) {
	a.gcCount++
	preGCBytes := a.bytesAllocated
	state := acquireGCMarkState(a)
	defer releaseGCMarkState(state)
	vm.markPhase(state)
	reclaimed, recycled := a.compactPhase(state)
	postGCBytes := max(preGCBytes-reclaimed, 0)
	a.bytesAllocated = postGCBytes
	a.deductReclaimedFromBudget(reclaimed - recycled)
	a.updateNextGCAt(preGCBytes, postGCBytes)
	a.bytesAtLastGC = postGCBytes
}

// noteAlloc accounts for a just-completed data-slab bump allocation by adding its size to
// the arena's running byte total.
//
// Takes bytes (int64) which is the size of the just-completed allocation.
//
//go:nosplit
func (a *RegisterArena) noteAlloc(bytes int64) {
	a.bytesAllocated += bytes
}

// gcShouldRun reports whether MinorGC should run at the next safe point.
//
// Returns true when bytesAllocated has crossed the nextGCAt threshold (using
// gcInitialThreshold when nextGCAt is still zero).
//
//go:nosplit
func (a *RegisterArena) gcShouldRun() bool {
	if a.disableMinorGC {
		return false
	}
	threshold := a.nextGCAt
	if threshold == 0 {
		threshold = gcInitialThreshold
	}
	return a.bytesAllocated >= threshold
}

// deductReclaimedFromBudget reduces the arena's live-bytes counter by the capacity
// released during compactPhase. Negative or zero values are no-ops.
//
// Takes reclaimedBytes (int64) which is the byte capacity dropped by compactPhase.
func (a *RegisterArena) deductReclaimedFromBudget(reclaimedBytes int64) {
	if reclaimedBytes <= 0 {
		return
	}
	a.releaseArenaBytes(uint64(reclaimedBytes))
}

// updateNextGCAt sets the next GC trigger threshold based on the observed live ratio.
// Backs off when most data is live and tightens when most is garbage.
//
// Takes preGCBytes (int64) which is bytesAllocated at trigger time.
// Takes postGCBytes (int64) which is bytesAllocated after compaction.
func (a *RegisterArena) updateNextGCAt(preGCBytes, postGCBytes int64) {
	if preGCBytes <= 0 {
		a.nextGCAt = postGCBytes + gcInitialThreshold
		return
	}
	liveRatio := float64(postGCBytes) / float64(preGCBytes)
	growth := preGCBytes - a.bytesAtLastGC
	if growth <= 0 {
		growth = gcInitialThreshold
	}
	var delta int64
	switch {
	case liveRatio > gcRatioMostlyLive:
		delta = growth * gcMostlyLiveGrowthMultiplier
	case liveRatio < gcRatioMostlyGarbage:
		delta = growth
	default:
		delta = growth * gcBalancedGrowthMultiplier
	}
	delta = min(max(delta, gcMinThresholdDelta), gcMaxThresholdDelta)
	a.nextGCAt = postGCBytes + delta
}
