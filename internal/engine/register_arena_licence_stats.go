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

// HostDeclarationStats counts what host borrow declarations, global reuse (variable
// initialisers skipped while the globals are unchanged) and reads-only []byte(s) views
// did since the process started.
type HostDeclarationStats struct {
	// Declared counts accepted borrow declarations.
	Declared int64

	// Refused counts refused borrow declarations.
	Refused int64

	// DirtyByNative counts global watches a native argument dirtied.
	DirtyByNative int64

	// Views counts []byte(s) arguments passed to reads-only natives as the string's own
	// bytes.
	Views int64

	// ViewFallbacks counts marked []byte(s) arguments copied because the callee is not
	// reads-only or the call shape was not confirmed.
	ViewFallbacks int64

	// Attestations counts reads-only natives whose first call passed the audit build's copy
	// check.
	Attestations int64

	// Violations counts reads-only natives seen writing an argument (audit build).
	Violations int64

	// Checksums counts reads-only calls whose bytes the audit build checksummed.
	Checksums int64
}

// licenceCounters accumulates licensed reuse's revocations and release outcomes, while
// licenceCounting is on. They are counts, not timings.
type licenceCounters struct {
	// handOffs counts revoking hand-offs seen by licensed arenas, per cause.
	handOffs [numHandOffCauses]atomic.Int64

	// revokedSlabs counts slabs newly revoked, per first cause.
	revokedSlabs [numHandOffCauses + 1]atomic.Int64

	// foreignRevokes counts revocations recorded against another arena's slab, of any class.
	foreignRevokes atomic.Int64

	// epochRevokes counts epochs revoked, per reason.
	epochRevokes [numLicenceEpochReasons]atomic.Int64

	// releases counts releases of licensed arenas.
	releases atomic.Int64

	// releasesNoData counts releases of arenas that allocated nothing escapable.
	releasesNoData atomic.Int64

	// releasesClean counts releases with escapable data that recycled every slab.
	releasesClean atomic.Int64

	// releasesPartial counts releases that abandoned some slabs and recycled the rest.
	releasesPartial atomic.Int64

	// releasesEpoch counts releases that abandoned every data slab.
	releasesEpoch atomic.Int64

	// blindHandOffs counts revoking hand-offs whose walk was skipped because the epoch was
	// already revoked, or stopped at memory another goroutine may be writing.
	blindHandOffs atomic.Int64

	// blindCallbackHandOffs counts the blind hand-offs of callback and bound-method VMs'
	// arenas, which are also counted process-wide (licenceBlindCallbacks).
	blindCallbackHandOffs atomic.Int64

	// blindByteHandOffs counts walks that skipped only strings in shared memory.
	blindByteHandOffs atomic.Int64

	// blindByteSlabReleases counts releases that revoked their byte slabs because some arena
	// skipped a walk during the release interval.
	blindByteSlabReleases atomic.Int64

	// partialByCause counts partial releases per cause that contributed an abandoned slab.
	partialByCause [numHandOffCauses + 1]atomic.Int64

	// epochByCause counts epoch releases per first cause.
	epochByCause [numHandOffCauses + 1]atomic.Int64

	// epochByReason counts epoch releases per first reason.
	epochByReason [numLicenceEpochReasons]atomic.Int64

	// slabsAbandoned counts data slabs abandoned at release.
	slabsAbandoned atomic.Int64

	// slabsRecycled counts data slabs kept for reuse at release.
	slabsRecycled atomic.Int64

	// compactions counts minor collections of licensed arenas.
	compactions atomic.Int64

	// compactionsFailedClosed counts minor collections that skipped compaction, per reason.
	compactionsFailedClosed [numLicenceFailClosedReasons]atomic.Int64

	// deadChunksKeptFromReuse counts dead chunks dropped rather than recycled because they
	// were revoked.
	deadChunksKeptFromReuse atomic.Int64

	// deferredCollections counts grows past the budget that deferred to a safepoint.
	deferredCollections atomic.Int64

	// safepointBudgetPanics counts safepoints that found the arena still over budget.
	safepointBudgetPanics atomic.Int64

	// overshootPanics counts grows that panicked past the overshoot allowance.
	overshootPanics atomic.Int64

	// sharedWalks counts walks that stopped at memory another goroutine may be writing and
	// revoked the epoch instead.
	sharedWalks atomic.Int64

	// lateDetaches counts releases that found revocations made after they detached, when
	// Reset sealed them.
	lateDetaches atomic.Int64

	// lateSlabsAbandoned counts slabs those late detaches abandoned.
	lateSlabsAbandoned atomic.Int64

	// acquireDetaches counts acquisitions that adopted revocations made after the previous
	// release and abandoned the slabs before reusing any.
	acquireDetaches atomic.Int64

	// closureRevokes counts slabs revoked because a revoked reference-bearing slab
	// referenced them at release.
	closureRevokes atomic.Int64

	// foreignDropped counts foreign revocations discarded because the recorded arena no
	// longer holds the slab.
	foreignDropped atomic.Int64
}

var (
	// licenceCount holds licensed reuse's process-wide counters, which count only while
	// licenceCounting is on.
	licenceCount licenceCounters

	// licenceCounting turns licensed reuse's counters on. They exist for tests and
	// diagnostics; a production run leaves them off and pays one predictable branch per
	// count (SetLicenceCountingForTest).
	licenceCounting bool
)

// LicenceCounts is a snapshot of licensed reuse's process-wide counters, keyed by stable
// names. They are counts from correctness runs, not timings.
type LicenceCounts struct {
	// HandOffs counts revoking hand-offs per cause.
	HandOffs map[string]int64

	// RevokedSlabs counts slabs newly revoked per first cause.
	RevokedSlabs map[string]int64

	// EpochRevokes counts epochs revoked per reason.
	EpochRevokes map[string]int64

	// PartialByCause counts partial releases per contributing cause.
	PartialByCause map[string]int64

	// EpochByCause counts epoch releases per first cause.
	EpochByCause map[string]int64

	// EpochByReason counts epoch releases per first reason.
	EpochByReason map[string]int64

	// CompactionsFailedClosed counts minor collections that skipped compaction per reason.
	CompactionsFailedClosed map[string]int64

	// ForeignRevokes counts revocations recorded against another arena's slab.
	ForeignRevokes int64

	// Releases counts releases of licensed arenas.
	Releases int64

	// ReleasesNoData counts releases of arenas with nothing escapable.
	ReleasesNoData int64

	// ReleasesClean counts releases with data that recycled every slab.
	ReleasesClean int64

	// ReleasesPartial counts releases that abandoned some slabs.
	ReleasesPartial int64

	// ReleasesEpoch counts releases that abandoned every data slab.
	ReleasesEpoch int64

	// BlindHandOffs counts revoking hand-offs whose walk was skipped because the epoch was
	// already revoked.
	BlindHandOffs int64

	// BlindCallbackHandOffs counts the blind hand-offs of callback and bound-method VMs,
	// which every licensed arena sees whatever its goroutine tree.
	BlindCallbackHandOffs int64

	// BlindByteSlabReleases counts releases that revoked their storage, or their byte slabs,
	// because some arena of the tree skipped a walk during the release interval.
	BlindByteSlabReleases int64

	// BlindByteHandOffs counts walks that skipped only strings in shared memory.
	BlindByteHandOffs int64

	// SlabsAbandoned counts data slabs abandoned at release.
	SlabsAbandoned int64

	// SlabsRecycled counts data slabs kept for reuse at release.
	SlabsRecycled int64

	// Compactions counts minor collections of licensed arenas.
	Compactions int64

	// DeadChunksKeptFromReuse counts dead chunks dropped because they were revoked.
	DeadChunksKeptFromReuse int64

	// DeferredCollections counts grows past budget deferred to a safepoint.
	DeferredCollections int64

	// SafepointBudgetPanics counts safepoints that found the arena over budget.
	SafepointBudgetPanics int64

	// OvershootPanics counts grows that panicked past the overshoot allowance.
	OvershootPanics int64

	// SharedWalks counts walks and release-time scans that stopped at memory another
	// goroutine may be writing and revoked the epoch instead.
	SharedWalks int64

	// LateDetaches counts releases that Reset's seal found revoked further.
	LateDetaches int64

	// LateSlabsAbandoned counts the slabs those late detaches abandoned.
	LateSlabsAbandoned int64

	// AcquireDetaches counts acquisitions that abandoned slabs revoked after the previous
	// release.
	AcquireDetaches int64

	// ClosureRevokes counts slabs revoked because revoked storage referenced them.
	ClosureRevokes int64

	// ForeignDropped counts foreign revocations dropped with their slab, pending or not yet
	// recorded.
	ForeignDropped int64

	// ForeignPending is the number of foreign revocations still pending.
	ForeignPending int64
}

// hostDeclarationStatistics returns the host declaration counters, which move only while
// the licence counters are on (SetLicenceCountingForTest).
//
// Returns HostDeclarationStats which is a snapshot.
func hostDeclarationStatistics() HostDeclarationStats {
	counters := &hostDeclarationCounters
	return HostDeclarationStats{
		Declared: counters.declared.Load(), Refused: counters.refused.Load(), DirtyByNative: counters.dirtyByNative.Load(),
		Views: counters.views.Load(), ViewFallbacks: counters.viewFallbacks.Load(),
		Attestations: counters.attestations.Load(), Violations: counters.violations.Load(), Checksums: counters.checksums.Load(),
	}
}

// causeName names a cause slot, including noHandOffCause.
//
// Takes cause (HandOffCause) which is the slot.
//
// Returns string which is the name.
func causeName(cause HandOffCause) string {
	if cause == noHandOffCause {
		return "none"
	}
	return cause.String()
}

// licenceStats returns licensed reuse's counters.
//
// Returns *licenceCounters which is the process-wide block.
func licenceStats() *licenceCounters {
	return &licenceCount
}

// stats returns licensed reuse's counters the arena adds to.
//
// Returns *licenceCounters which is the process-wide block.
func (*RegisterArena) stats() *licenceCounters {
	return &licenceCount
}

// licenceTotal returns licensed reuse's counters for reading.
//
// Returns *licenceCounters which is the process-wide block.
func licenceTotal() *licenceCounters {
	return &licenceCount
}

// SetLicenceCountingForTest turns licensed reuse's counters on or off and returns the
// previous setting. Test binaries turn them on in TestMain.
//
// Takes on (bool) which is the new setting.
//
// Returns bool which is the previous one.
func SetLicenceCountingForTest(on bool) bool {
	previous := licenceCounting
	licenceCounting = on
	return previous
}

// licenceStatistics returns licensed reuse's process-wide counters.
//
// Returns LicenceCounts which is a snapshot.
func licenceStatistics() LicenceCounts {
	counters := licenceTotal()
	causes := func(slots []atomic.Int64) map[string]int64 {
		out := map[string]int64{}
		for index := range slots {
			if value := slots[index].Load(); value != 0 {
				out[causeName(HandOffCause(index))] = value
			}
		}
		return out
	}
	reasons := func(slots []atomic.Int64) map[string]int64 {
		out := map[string]int64{}
		for index := range slots {
			if value := slots[index].Load(); value != 0 {
				out[licenceEpochReasonNames[index]] = value
			}
		}
		return out
	}
	failed := map[string]int64{}
	for index := range counters.compactionsFailedClosed {
		if value := counters.compactionsFailedClosed[index].Load(); value != 0 {
			failed[licenceFailClosedReasonNames[index]] = value
		}
	}
	return LicenceCounts{
		HandOffs: causes(counters.handOffs[:]), RevokedSlabs: causes(counters.revokedSlabs[:]),
		EpochRevokes: reasons(counters.epochRevokes[:]), PartialByCause: causes(counters.partialByCause[:]),
		EpochByCause: causes(counters.epochByCause[:]), EpochByReason: reasons(counters.epochByReason[:]),
		CompactionsFailedClosed: failed, ForeignRevokes: counters.foreignRevokes.Load(),
		Releases: counters.releases.Load(), ReleasesNoData: counters.releasesNoData.Load(),
		ReleasesClean: counters.releasesClean.Load(), ReleasesPartial: counters.releasesPartial.Load(),
		ReleasesEpoch: counters.releasesEpoch.Load(), SlabsAbandoned: counters.slabsAbandoned.Load(),
		BlindHandOffs: counters.blindHandOffs.Load(), BlindCallbackHandOffs: counters.blindCallbackHandOffs.Load(),
		BlindByteSlabReleases: counters.blindByteSlabReleases.Load(),
		SlabsRecycled:         counters.slabsRecycled.Load(), Compactions: counters.compactions.Load(),
		DeadChunksKeptFromReuse: counters.deadChunksKeptFromReuse.Load(),
		DeferredCollections:     counters.deferredCollections.Load(),
		SafepointBudgetPanics:   counters.safepointBudgetPanics.Load(),
		OvershootPanics:         counters.overshootPanics.Load(),
		SharedWalks:             counters.sharedWalks.Load(),
		BlindByteHandOffs:       counters.blindByteHandOffs.Load(),
		LateDetaches:            counters.lateDetaches.Load(),
		LateSlabsAbandoned:      counters.lateSlabsAbandoned.Load(),
		AcquireDetaches:         counters.acquireDetaches.Load(),
		ClosureRevokes:          counters.closureRevokes.Load(),
		ForeignDropped:          counters.foreignDropped.Load(),
		ForeignPending:          licenceForeignRevokedCount.Load(),
	}
}
