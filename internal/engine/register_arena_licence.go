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
	"iter"
	"reflect"
	"sync"
	"sync/atomic"

	"pipit.sh/pipit/internal/engine/program"
)

const (
	// licenceWalkLeafBudget bounds the values the revocation walks visit per epoch, across
	// all its hand-offs.
	licenceWalkLeafBudget = 1 << 16

	// licenceMarkTraceBudget bounds the extra values the licensed mark visits per cycle
	// inside containers the default mark leaves untraced.
	licenceMarkTraceBudget = 1 << 16

	// licenceMarkVisitBudget bounds the distinct referents (pointees, maps, slice backings,
	// closures and cells) the licensed mark traces per cycle.
	licenceMarkVisitBudget = 1 << 20

	// licenceMarkStepBudget bounds the steps the licensed mark takes per cycle. A step is a
	// value it marks, or an element or entry of a sequence or map it walks (drainMark).
	licenceMarkStepBudget = 1 << 24

	// noHandOffCause marks a revocation with no hand-off behind it, such as a re-entrant
	// release.
	noHandOffCause = numHandOffCauses
)

// licenceEpochReason says why a whole epoch was revoked.
type licenceEpochReason uint8

const (
	// epochProbeExhausted is a revocation walk that ran out of depth or leaf budget.
	epochProbeExhausted licenceEpochReason = iota

	// epochOpaqueFunction is a function value the walk cannot see into.
	epochOpaqueFunction

	// epochUpvalueCell is a reference into the upvalue cell or reference slabs.
	epochUpvalueCell

	// epochRegisterBank is a reference into a register bank.
	epochRegisterBank

	// epochInexactProbe is any revoking hand-off in a build whose ownership probes cannot
	// tell (safe and js/wasm).
	epochInexactProbe

	// epochReentrant is the release of a re-entrant VM's arena that holds escapable data.
	epochReentrant

	// epochWalkBudget is a walk that ran out of the leaf budget earlier walks of the epoch
	// had drawn down.
	epochWalkBudget

	// epochSharedContainer is a walk, or a release-time scan, that would have had to read
	// memory another goroutine may be writing.
	epochSharedContainer

	// epochBlindForeign is a blind hand-off by another arena during this arena's release
	// interval, which might have named any of its storage.
	epochBlindForeign

	// epochAborted is a limit or a handler panic that exited non-locally part-way through an
	// update of the arena (see markAborted).
	epochAborted

	// numLicenceEpochReasons is the number of reasons.
	numLicenceEpochReasons
)

// licenceFailClosedReason says why a minor collection skipped compaction.
type licenceFailClosedReason uint8

const (
	// failClosedBudget is a mark that ran out of one of its per-cycle budgets.
	failClosedBudget licenceFailClosedReason = iota + 1

	// failClosedChannel is a channel with buffered, reference-carrying elements.
	failClosedChannel

	// failClosedFunction is a function value the mark cannot see into.
	failClosedFunction

	// failClosedShared is a mark that would read memory another goroutine may be writing. It
	// is skipped because the arena does not mark while shared (sharedMarkApplies), or
	// stopped at such memory (readsPrivately).
	failClosedShared

	// numLicenceFailClosedReasons is one past the last reason.
	numLicenceFailClosedReasons
)

// licenceSlabClass is what an address inside the arena belongs to.
type licenceSlabClass uint8

const (
	// licenceSlabNone is an address outside the arena.
	licenceSlabNone licenceSlabClass = iota

	// licenceSlabData is a data slab revocable on its own.
	licenceSlabData

	// licenceSlabUpvalue is an upvalue cell or reference slab, which revokes the epoch.
	licenceSlabUpvalue

	// licenceSlabBank is a register bank, which revokes the epoch.
	licenceSlabBank

	// licenceSlabCell identifies closure chunks holding closures or their upvalue cells, as
	// implemented in register_arena_licence_closures.go.
	licenceSlabCell
)

// licenceEpochReasonNames holds the stable names reported for each reason.
var licenceEpochReasonNames = [numLicenceEpochReasons]string{
	epochProbeExhausted: "probeExhausted", epochOpaqueFunction: "opaqueFunction",
	epochUpvalueCell: "upvalueCell", epochRegisterBank: "registerBank",
	epochInexactProbe: "inexactProbe", epochReentrant: "reentrant", epochWalkBudget: "walkBudget",
	epochSharedContainer: "sharedContainer", epochBlindForeign: "blindForeign", epochAborted: "aborted",
}

// licenceFailClosedReasonNames holds the stable names reported for each reason.
var licenceFailClosedReasonNames = [numLicenceFailClosedReasons]string{
	failClosedBudget: "budget", failClosedChannel: "channel", failClosedFunction: "function",
	failClosedShared: "shared",
}

// arenaLicenceState holds the arena's licence for the epoch between two releases.
type arenaLicenceState struct {
	// revoked maps the start address of each revoked slab to the first cause that revoked
	// it.
	revoked map[uintptr]HandOffCause

	// blindSource holds the blind counters of the goroutine tree the arena was last acquired
	// for (see licenceBlindSource).
	blindSource *licenceBlindCounters

	// arenaLicenceExactState holds the walk's and the licence registry's buffers, which only
	// builds with exact probes have.
	arenaLicenceExactState

	// regrow is what the epoch whose release abandoned the data and upvalue slabs used of
	// each, for sizing their replacements at the next sizing.
	regrow regrowRecord

	// memoHintFor records the typed slab counts used to derive the memo size hint.
	memoHintFor program.TypedSlabCounts

	// lazyFloor holds, per byte-class kind dropRevokedSlabs left empty, the size its first
	// growth makes it at (lazyRegrowFloor).
	lazyFloor struct{ bytes, mutBytes, genericBytes, sliceHeaders int }

	// lastNoted identifies the value of the most recent revoking hand-off.
	lastNoted licenceNoteKey

	// chainBlind counts the blind hand-offs callback VMs serving this arena's native calls
	// counted on their served chain instead of process-wide
	// (register_arena_licence_chain.go); read with the tree's counters in blindTotals.
	chainBlind licenceBlindCounters

	// walkBudget is the leaf allowance left to the epoch's revocation walks.
	walkBudget int

	// blindMark is blindSource.all plus licenceBlindCallbacks.all plus chainBlind.all as the
	// previous licensed release, or the acquisition, read them (see blindTotals).
	blindMark uint64

	// blindByteMark is the same sum of the byte-only counters, read with blindMark. A
	// different value means a skipped walk might have named this arena's byte slabs.
	blindByteMark uint64

	// cbStarts counts callback entries for detecting callback activity.
	cbStarts atomic.Uint64

	// revision counts every change to what is revoked. It is never reset, so release can
	// tell whether anything was revoked after it detached.
	revision uint64

	// releasedRevision is revision as the latest detach left it.
	releasedRevision uint64

	// coverCleanBase and coverCleanRevision are the last slab licenceCoveredSlab found clean
	// and the revision it was found at, so a run of stores into one clean slab skips the
	// revoked-set lookup.
	coverCleanBase uintptr

	// coverCleanRevision is the licence revision of the cached clean-coverage result.
	coverCleanRevision uint64

	// coverCoveredBase records the last slab found covered by licenceCoveredSlab when
	// coverCoveredKnown is true.
	coverCoveredBase uintptr

	// memoHint records the size of the walk-memo table dropped at the arena's latest
	// release, while memoHintFor records the sizing of the run that grew it.
	memoHint int

	// lastNotedSeq is the note sequence number (noteSeq) the latest revoking note took;
	// lastNoted is the identity of its value. A second note of the value can skip its walk
	// (noteHandOffAfter, register_arena_licence_handoff.go).
	lastNotedSeq uint64

	// cellMaker is the tag written into every upvalue cell the epoch's VM makes, or zero
	// before its first (cellMaker).
	cellMaker uint64

	// revokedEpochRouteState holds the note sequence number, the revoked-epoch counts and
	// the flag the assembly pointer-store route reads and writes.
	revokedEpochRouteState

	// cbActive counts running callbacks that may reach this arena's storage during
	// parked-caller walks.
	cbActive atomic.Int32

	// epochCause is the first cause that revoked the epoch, or noHandOffCause.
	epochCause HandOffCause

	// epochReason is why the epoch was first revoked.
	epochReason licenceEpochReason

	// epochRevoked is true once anything revoked the whole epoch.
	epochRevoked bool

	// servingKnown and servingReentrant cache whether the VM the arena serves (its licence
	// VM, or else its owner) exists and runs guest code on behalf of a native call
	// (servingVMReentrant).
	servingKnown, servingPresent, servingReentrant bool

	// banksRevoked is true when a register bank was referenced; release then replaces the
	// register banks as well.
	banksRevoked bool

	// released is true once release has handled revoked storage, so Reset need not.
	released bool

	// blindMarked is true once blindMark and blindByteMark hold a reading of blindSource.
	blindMarked bool

	// coverCoveredKnown records whether the cached covered-storage result is valid.
	coverCoveredKnown bool

	// containerStore is true while recordContainerStore hands off a container stored into,
	// whose memo entry describes it before the store, so the memo must not settle it.
	containerStore bool

	// wholeArenaDetached is true once a release of a revoked epoch has left every data slab,
	// the upvalue slabs and the register banks to the collector.
	wholeArenaDetached bool

	// foreignLocked is true while this arena's goroutine holds licenceForeignRevokedMu, so
	// the revocations it makes meanwhile do not take it again.
	foreignLocked bool
}

// revokedEpochRouteState is the part of an arena's licence state that the assembly
// pointer-store route of SET_STRUCT_FIELD_GENERAL reads and writes, through the address
// the dispatch context publishes (arenaRouteState).
//
// It holds no field whose layout depends on the build, so its offsets (the ROUTE_*
// defines) are the same in every build that has assembly, whatever precedes it in
// RegisterArena and arenaLicenceState.
type revokedEpochRouteState struct {
	// noteSeq counts the arena's notes (see lastNotedSeq).
	noteSeq uint64

	// revokedEpochNotes holds the counts the revoked-epoch fast path's hand-offs have not
	// yet added to the shared counters (noteRevokedEpochHandOff, flushRevokedEpochNotes).
	revokedEpochNotes revokedEpochNoteCounts

	// revokedLocalBlind is true when revokedEpochLocalBlind last found the epoch revoked and
	// its blind hand-offs local, in a build whose assembly may take pointer stores in such
	// an epoch (revokedEpochStoreRouteBuilt), and nothing since has emptied it.
	revokedLocalBlind bool
}

// licenceForeignKey identifies a pending foreign revocation by its slab's starting
// address and the arena named as its holder by the registry entry.
type licenceForeignKey struct {
	// start is the revoked slab's first byte address.
	start uintptr

	// owner identifies the arena that held the slab (RegisterArena.slabOwner).
	owner uint64
}

// pendingForeignRevocations holds the foreign revocations pending against other arenas'
// slabs, indexed by the holder's identity, so dropping one arena's entries touches only
// those. licenceForeignRevokedMu guards it.
type pendingForeignRevocations struct {
	// byOwner maps a holder's identity to the revocations pending against its slabs. An
	// owner with no entry left is removed.
	byOwner map[uint64]ownerForeignRevocations

	// entries is the number of pending revocations.
	entries int
}

// ownerForeignRevocations is the revocations pending against one holder's slabs. The
// first is held inline, so a holder with one pending revocation, the usual case, costs no
// map of its own.
type ownerForeignRevocations struct {
	// more maps the starts of the holder's other revoked slabs to their causes; nil until a
	// second is recorded.
	more map[uintptr]HandOffCause

	// start is the first revoked slab's start.
	start uintptr

	// cause is the first revoked slab's cause.
	cause HandOffCause
}

var (
	// defaultBorrowRegistry classes native calls for licensed arenas that carry no counters
	// block.
	defaultBorrowRegistry = aliasAwareBorrowRegistry()

	// licenceForeignRevoked maps a slab of another arena, of any class, named by its start
	// and its holder, to the cause that revoked it. Each arena adopts its own entries at
	// release and acquisition; an entry is recorded only while the registry still names its
	// holder at its start (revokeForeignSlabLocked).
	licenceForeignRevoked pendingForeignRevocations

	// licenceForeignRevokedCount mirrors licenceForeignRevoked.len() for a lock-free check,
	// counting as well a revocation being recorded while revokeForeignSlabLocked checks the
	// registry. It changes only under licenceForeignRevokedMu, and reads zero only when
	// nothing is pending or being recorded.
	licenceForeignRevokedCount atomic.Int64

	// licenceForeignRevokedMu guards licenceForeignRevoked, serialises every increment of
	// licenceBlindHandOffs, and is held while Reset seals a release, so a foreign or blind
	// revocation lands either before the seal re-checks or after Reset has finished.
	licenceForeignRevokedMu sync.Mutex

	// licenceSealMu orders blind increments against seals. An increment holds it shared, so
	// increments by different arenas no longer serialise on each other; lockForeign (the
	// seal, acquisition and adoption) holds it exclusively together with
	// licenceForeignRevokedMu, so everything that excluded a blind increment before still
	// does.
	licenceSealMu sync.RWMutex

	// licenceBlindProcess counts the walks skipped by arenas acquired without a resource
	// tracker. A licensed arena whose release finds its blind counters changed since its
	// previous release revokes the storage the skipped walks might have named, standing in
	// for the foreign revocations they might have made.
	licenceBlindProcess licenceBlindCounters

	// licenceBlindCallbacks counts, process-wide, the walks skipped by arenas of callback
	// and bound-method VMs (servesNativeCaller), in addition to their own tree's counters.
	licenceBlindCallbacks licenceBlindCounters

	// reflectTypeDescriptorType is the dynamic type behind a reflect.Type interface. Type
	// descriptors are static, so the licence walks and the licensed mark stop at them rather
	// than wander through the runtime's type graph.
	reflectTypeDescriptorType = reflect.TypeOf(reflect.TypeFor[int]())
)

// licensesArenas reports whether the limits permit licensed arena reuse.
//
// Only the abandon fallback disables it, and those arenas reuse nothing.
//
// Returns bool which is true unless the limits abandon their arenas.
func (l *VMLimits) licensesArenas() bool {
	return !l.abandonsArenas()
}

// lookup returns the cause pending against key.
//
// Takes key (licenceForeignKey) which is the slab's start and its holder.
//
// Returns HandOffCause which is the cause.
// Returns bool which is true when a revocation is pending.
func (p *pendingForeignRevocations) lookup(key licenceForeignKey) (HandOffCause, bool) {
	owner, found := p.byOwner[key.owner]
	if !found {
		return 0, false
	}
	if owner.start == key.start {
		return owner.cause, true
	}
	cause, found := owner.more[key.start]
	return cause, found
}

// record notes a revocation pending against key, replacing any cause already there.
//
// Takes key (licenceForeignKey) which is the slab's start and its holder.
// Takes cause (HandOffCause) which is the hand-off's cause.
func (p *pendingForeignRevocations) record(key licenceForeignKey, cause HandOffCause) {
	if p.byOwner == nil {
		p.byOwner = make(map[uint64]ownerForeignRevocations)
	}
	owner, found := p.byOwner[key.owner]
	switch {
	case !found:
		owner = ownerForeignRevocations{more: nil, start: key.start, cause: cause}
		p.entries++
	case owner.start == key.start:
		owner.cause = cause
	default:
		if owner.more == nil {
			owner.more = make(map[uintptr]HandOffCause)
		}
		if _, already := owner.more[key.start]; !already {
			p.entries++
		}
		owner.more[key.start] = cause
	}
	p.byOwner[key.owner] = owner
}

// forget removes the revocation pending against key.
//
// Takes key (licenceForeignKey) which is the slab's start and its holder.
//
// Returns bool which is true when one was pending.
func (p *pendingForeignRevocations) forget(key licenceForeignKey) bool {
	owner, found := p.byOwner[key.owner]
	if !found {
		return false
	}
	if owner.start != key.start {
		if _, pending := owner.more[key.start]; !pending {
			return false
		}
		delete(owner.more, key.start)
		p.entries--
		return true
	}
	p.entries--
	for start, cause := range owner.more {
		delete(owner.more, start)
		owner.start, owner.cause = start, cause
		p.byOwner[key.owner] = owner
		return true
	}
	delete(p.byOwner, key.owner)
	return true
}

// forgetOwner removes every revocation pending against owner's slabs.
//
// Takes owner (uint64) which identifies the holder.
//
// Returns int which is how many were removed.
func (p *pendingForeignRevocations) forgetOwner(owner uint64) int {
	pending, found := p.byOwner[owner]
	if !found {
		return 0
	}
	delete(p.byOwner, owner)
	removed := 1 + len(pending.more)
	p.entries -= removed
	return removed
}

// len returns the number of pending revocations.
//
// Returns int which is the count.
func (p *pendingForeignRevocations) len() int {
	return p.entries
}

// all yields every pending revocation from a snapshot, so the caller may forget any of
// them as it goes.
//
// Returns iter.Seq2[licenceForeignKey, HandOffCause] which yields each key and its cause.
func (p *pendingForeignRevocations) all() iter.Seq2[licenceForeignKey, HandOffCause] {
	keys := make([]licenceForeignKey, 0, p.entries)
	causes := make([]HandOffCause, 0, p.entries)
	for holder, owner := range p.byOwner {
		keys = append(keys, licenceForeignKey{start: owner.start, owner: holder})
		causes = append(causes, owner.cause)
		for start, cause := range owner.more {
			keys = append(keys, licenceForeignKey{start: start, owner: holder})
			causes = append(causes, cause)
		}
	}
	return func(yield func(licenceForeignKey, HandOffCause) bool) {
		for index, key := range keys {
			if !yield(key, causes[index]) {
				return
			}
		}
	}
}

// isTypeDescriptor reports whether an interface's dynamic value is a reflect type
// descriptor.
//
// Takes v (reflect.Value) which is the interface's element.
//
// Returns bool which is true for a type descriptor.
func isTypeDescriptor(v reflect.Value) bool {
	return v.Kind() == reflect.Pointer && v.Type() == reflectTypeDescriptorType
}

// attachLicencePolicy installs the current execution's reuse and collector settings on
// the arena.
//
// Takes limits (*VMLimits) which are the acquiring VM's limits.
func (a *RegisterArena) attachLicencePolicy(limits *VMLimits) {
	a.licenceState.revokedLocalBlind = false
	a.licensed = limits.licensesArenas()
	a.borrowDeclarations = limits.BorrowDeclarations
	a.disableMinorGC = limits.DisableMinorGC
	if !a.licensed {
		return
	}
	engageServedChain()
	a.clearLicence()
	a.adoptAtAcquisition(licenceBlindSource(limits.Tracker))
	a.publishLicenceSlabs()
}
