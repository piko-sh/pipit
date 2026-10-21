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

// LicenceSnapshot holds the process-wide counters licensed reuse keeps, for tests and
// diagnostics. Most of them move only while the licence counters are on
// (SetLicenceCountingForTest).
type LicenceSnapshot struct {
	// Reuse holds the hand-off, revocation and release counts.
	Reuse LicenceCounts

	// AbortUnwind counts the unwindings of aborted runs.
	AbortUnwind AbortUnwindCounts

	// HostDeclarations counts host borrow declarations and reads-only byte views.
	HostDeclarations HostDeclarationStats

	// KRefPlacement counts the snapshot and pointee placements in KRef chunks.
	KRefPlacement KRefPlacementCounts

	// CallbackRelease counts callback and bound-method runs and their arena releases.
	CallbackRelease CallbackReleaseStats

	// CellWalks counts the closures and cells walks followed.
	CellWalks CellWalkStats

	// PageRevocation counts page-granular revocations and the clean runs releases kept.
	PageRevocation PageRevocationStats

	// Optimistic counts each optimistic placement rule's uses, indexed by OptimisticRule.
	Optimistic [numOptimisticRules]int64

	// BlindScope counts the blind hand-offs kept local and the shared memo hits.
	BlindScope BlindScopeStats

	// ClosureChunks counts closures and cells placed in closure chunks.
	ClosureChunks ClosureChunkStats

	// KRefChunks counts the KRef chunks made, detached and reused, and the slots handed out.
	KRefChunks KRefChunkCounts

	// LicenceCheck counts the programs and functions the licence checker examined.
	LicenceCheck LicenceCheckStats

	// ParkedCallers counts the walks that read a parked caller's storage.
	ParkedCallers ParkedCallerStats

	// NativeCopies counts the small strings copied for natives that would keep them.
	NativeCopies NativeCopyStats

	// ServedStores counts callback stores into a served arena that skipped the clone.
	ServedStores ServedStoreStats

	// ServedChain counts where callback blind hand-offs were counted.
	ServedChain ServedChainStats

	// AbortedArenas counts the arenas a limit or a recovered handler panic marked aborted.
	AbortedArenas int64

	// ForeignStringsKept counts foreign strings kept uncloned in own uncovered storage.
	ForeignStringsKept int64

	// MutableByteSlabs counts the mutable byte slabs installed.
	MutableByteSlabs int64

	// MutableByteSlabsDropped counts the retired mutable byte slabs collections dropped.
	MutableByteSlabsDropped int64

	// MutableByteBytesDropped is the capacity in bytes of those slabs.
	MutableByteBytesDropped int64

	// WalkedOnce counts the second notes settled without a walk.
	WalkedOnce int64

	// ExportGates counts the owner sections of VMs that exported guest code.
	ExportGates ExportGateStats
}

// LicenceDiagnostics returns the current values of the licensed-reuse counters.
//
// Returns LicenceSnapshot which is the snapshot.
func LicenceDiagnostics() LicenceSnapshot {
	return LicenceSnapshot{
		Reuse:                   licenceStatistics(),
		AbortUnwind:             abortUnwindStats(),
		HostDeclarations:        hostDeclarationStatistics(),
		KRefPlacement:           krefPlacementStatistics(),
		CallbackRelease:         callbackReleaseStatistics(),
		CellWalks:               cellWalkStatistics(),
		PageRevocation:          pageRevocationStatistics(),
		Optimistic:              optimisticStats(),
		BlindScope:              blindScopeStatistics(),
		ClosureChunks:           closureChunkStatistics(),
		KRefChunks:              krefChunkStatistics(),
		LicenceCheck:            licenceCheckStatistics(),
		ParkedCallers:           parkedCallerStatistics(),
		NativeCopies:            nativeCopyStatistics(),
		ServedStores:            servedStoreStatistics(),
		ServedChain:             servedChainStatistics(),
		AbortedArenas:           abortedArenaCount(),
		ForeignStringsKept:      foreignStringStatistics(),
		MutableByteSlabs:        mutableByteStatistics(),
		MutableByteSlabsDropped: mutableByteSlabsDropped.Load(),
		MutableByteBytesDropped: mutableByteBytesDropped.Load(),
		WalkedOnce:              walkedOnceStatistics(),
		ExportGates:             exportGateStatistics(),
	}
}
