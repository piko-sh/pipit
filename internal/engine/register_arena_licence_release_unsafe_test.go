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

//go:build !safe && !(js && wasm)

package engine

import (
	"math/rand/v2"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestReleaseDetachesARevokedByteSlab(t *testing.T) {
	arena := newArena(t)
	escaped := arenaString(arena, "escaped through a revoking hand-off")
	before := unsafe.SliceData(arena.byteSlab)
	noteHandOffString(arena, handOffUntracedContainer, escaped)
	require.Contains(t, arena.licenceState.revoked, uintptr(unsafe.Pointer(before)))

	arena.Reset()
	require.NotEqual(t, uintptr(unsafe.Pointer(before)), uintptr(unsafe.Pointer(unsafe.SliceData(arena.byteSlab))),
		"release must install a fresh byte slab in place of the revoked one")
	require.Equal(t, "escaped through a revoking hand-off", escaped, "the revoked slab must be left uncleared")
	require.False(t, arena.licensed, "Reset clears the licence for the pool's next owner")
	require.Empty(t, arena.licenceState.revoked)
}

func TestReleaseRecyclesACleanArena(t *testing.T) {
	arena := newArena(t)
	arenaString(arena, "never handed off")
	before := unsafe.SliceData(arena.byteSlab)
	noteHandOff(arena, handOffNativeArgBorrows, reflect.ValueOf(arenaString(arena, "borrowed only")))
	arena.Reset()
	require.Equal(t, uintptr(unsafe.Pointer(before)), uintptr(unsafe.Pointer(unsafe.SliceData(arena.byteSlab))), "a clean slab must be kept for reuse")
}

func TestReleaseRevokesByteSlabsAfterAnotherArenasBlindHandOff(t *testing.T) {
	lender := newArena(t)
	lent := arenaString(lender, "lent to a borrowing callee")
	lenderSlab := uintptr(unsafe.Pointer(unsafe.SliceData(lender.byteSlab)))

	blind := newArena(t)
	noteHandOff(blind, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	noteHandOffString(blind, handOffUntracedContainer, lent)
	require.False(t, foreignPending(lenderSlab), "the skipped walk names no foreign slab itself")

	lender.Reset()
	require.NotEqual(t, lenderSlab, uintptr(unsafe.Pointer(unsafe.SliceData(lender.byteSlab))),
		"the lender must abandon the byte slab a skipped walk might have named")
	require.Equal(t, "lent to a borrowing callee", lent, "the abandoned slab must be left uncleared")

	quiet := newArena(t)
	arenaString(quiet, "no blind hand-off since")
	quiet.Reset()
	quietSlab := uintptr(unsafe.Pointer(unsafe.SliceData(quiet.byteSlab)))
	quiet.attachLicencePolicy(&VMLimits{})
	arenaString(quiet, "recycled")
	quiet.Reset()
	require.Equal(t, quietSlab, uintptr(unsafe.Pointer(unsafe.SliceData(quiet.byteSlab))),
		"without a blind hand-off in the interval the clean byte slab is recycled")
}

func TestReleaseReplacesAnOvergrownBackingAtItsInitialLength(t *testing.T) {
	arena := newArena(t)
	t.Cleanup(arena.Reset)
	revoked := func(count int) []int64 {
		slab := make([]int64, count)
		if arena.licenceState.revoked == nil {
			arena.licenceState.revoked = map[uintptr]HandOffCause{}
		}
		arena.licenceState.revoked[uintptr(unsafe.Pointer(unsafe.SliceData(slab)))] = handOffNativeArgUnknown
		return slab
	}
	tally := &licenceReleaseTally{}
	overgrown := revoked(InitialIntBackingSize*maxArenaMultiplier + 1)
	require.Len(t, keepUnlessRevokedSized(arena, overgrown, tally, InitialIntBackingSize), InitialIntBackingSize)
	within := revoked(InitialIntBackingSize * 2)
	require.Len(t, keepUnlessRevokedSized(arena, within, tally, InitialIntBackingSize), InitialIntBackingSize*2)
	require.Len(t, keepUnlessRevoked(arena, revoked(InitialIntBackingSize*maxArenaMultiplier+1), tally), InitialIntBackingSize*maxArenaMultiplier+1, "without a shrink rule the length is kept")
}

func TestReleaseClosureSpansAnswerAsLicenceDataSlabSpan(t *testing.T) {
	arena := newArena(t)
	for range 3 {
		arena.GrowByteSlab(len(arena.byteSlab) + 1)
		arena.growStringBackingSlab(len(arena.stringBackingSlab) + 1)
		arena.AllocIntBacking(len(arena.intBackingSlab) + 1)
	}
	spans, ok := arena.appendLicenceDataSpans(nil)
	require.True(t, ok, "the arena's data slabs are pairwise disjoint")
	require.NotEmpty(t, arena.oldByteSlabs, "the byte slab has retired slabs")
	require.NotEmpty(t, arena.oldStringBackings, "the string backing has retired slabs")

	var independent []licenceDataSpan
	independent = appendLicenceListSpans(independent, arena.byteSlab, arena.oldByteSlabs)
	independent = appendLicenceListSpans(independent, arena.stringBackingSlab, arena.oldStringBackings)
	independent = appendLicenceListSpans(independent, arena.intBackingSlab, arena.oldIntBackings)
	independent = appendLicenceListSpans(independent, arena.sliceHeaderSlab, arena.oldSliceHeaderSlabs)
	independent = appendLicenceListSpans(independent, arena.genericBytesSlab, arena.oldGenericByteSlabs)
	independent = append(independent, spans...)
	probes := make([]uintptr, 0, 6*len(independent)+4)
	for _, span := range independent {
		probes = append(probes, span.base-1, span.base, span.base+1, (span.base+span.end)/2, span.end-1, span.end)
	}
	heap := make([]byte, 64)
	probes = append(probes, uintptr(unsafe.Pointer(&heap[0])), 0, 1, ^uintptr(0))
	for _, address := range probes {
		wantBase, wantEnd, wantOK := licenceDataSlabSpan(arena, address)
		base, end, found := findLicenceDataSpan(spans, address)
		require.Equal(t, wantOK, found, "%#x: found", address)
		if !found {
			continue
		}
		require.Equal(t, wantBase, base, "%#x: base", address)
		require.Equal(t, wantEnd, end, "%#x: end", address)
	}
	require.NotNil(t, heap)
}

func blindCallbackArena(t *testing.T) *RegisterArena {
	t.Helper()
	callback := newArena(t)
	noteHandOff(callback, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	require.True(t, callback.licenceState.epochRevoked, "test harness: the callback's epoch must be revoked")
	return callback
}

func TestReuseHazardEarlyReturnDiscardsABlindReading(t *testing.T) {
	observations := &hazardObservations{}
	caller := newArena(t)
	callback := blindCallbackArena(t)
	slot := callerBankPointer(t, caller)
	blind := licenceTotal().blindHandOffs.Load()
	noteHandOff(callback, handOffUntracedContainer, reflect.ValueOf(slot))
	observations.note("callback hand-off counted blind=%v", licenceTotal().blindHandOffs.Load() > blind)
	callback.Reset()
	require.False(t, caller.holdsEscapableData(), "test harness: the caller must hold no escapable data")
	noData := licenceTotal().releasesNoData.Load()
	reuseCallerBank(caller, slot, observations)
	observations.note("caller's release took the early return=%v", licenceTotal().releasesNoData.Load() > noData)
	assertNoHazard(t, observations, "")
}

func TestReuseHazardBlindHandOffInARevokedEpochSparesTheBanks(t *testing.T) {
	observations := &hazardObservations{}
	caller := newArena(t)
	callback := blindCallbackArena(t)
	slot := callerBankPointer(t, caller)
	noteHandOff(callback, handOffUntracedContainer, reflect.ValueOf(slot))
	callback.Reset()
	cells := caller.allocUpvalueCells(1)
	noteHandOff(caller, handOffSpawn, reflect.ValueOf(&cells[0]))
	require.True(t, caller.licenceState.epochRevoked, "test harness: the caller's epoch must be revoked")
	observations.note("caller banks revoked before release=%v", caller.licenceState.banksRevoked)
	reuseCallerBank(caller, slot, observations)
	assertNoHazard(t, observations, "")
}

func TestReuseHazardForeignRevocationPendingAtANoDataReleaseIsAdopted(t *testing.T) {
	observations := &hazardObservations{}
	const text = "bytes allocated in the owner's previous epoch"
	owner := newArena(t)
	early := arenaString(owner, text)
	ownerSlab := slabBase(owner.byteSlab)
	owner.Reset()
	relicence(owner)
	require.Equal(t, ownerSlab, slabBase(owner.byteSlab), "test harness: the clean byte slab must be recycled")
	t.Cleanup(func() { dropForeignPending(ownerSlab) })

	witness := newArena(t)
	noteHandOffString(witness, handOffUntracedContainer, early)
	require.True(t, foreignPending(ownerSlab), "test harness: the witness must record a foreign revocation")
	require.False(t, owner.holdsEscapableData(), "test harness: the owner must hold no data this epoch")

	noData := licenceTotal().releasesNoData.Load()
	owner.Reset()
	earlyReturn := licenceTotal().releasesNoData.Load() == noData+1
	pendingAfterNoData := foreignPending(ownerSlab)
	observations.note("no-data release took the early return=%v; foreign entry still pending after it=%v", earlyReturn, pendingAfterNoData)
	if !earlyReturn || !pendingAfterNoData {
		observations.note("the no-data release adopted the entry itself")
	}

	relicence(owner)
	overwriteStrings(owner, len(text))
	owner.Reset()
	adoptedLater := !foreignPending(ownerSlab)
	abandonedLater := slabBase(owner.byteSlab) != ownerSlab
	observations.note("next data-holding release adopted the entry=%v and abandoned the slab=%v", adoptedLater, abandonedLater)

	late := &hazardObservations{}
	late.compareString("late-witnessed string after the owner's next epoch", text, early)
	observations.note("late-witness precondition (an earlier release recycled a slab the string still references, itself a breach of the licence) gives: %s",
		strings.Join(late.details, ", "))
	if !adoptedLater || !abandonedLater {
		observations.fire("pending-entry-lost", "the deferred foreign revocation was never adopted")
	}
	runtime.KeepAlive(early)
	assertNoHazard(t, observations, "")
}

func TestReuseHazardRandomisedReleaseAgainstAForeignHandOff(t *testing.T) {
	iterations := 2000
	if testing.Short() {
		iterations = 200
	}
	const text = "lent bytes raced against the owner's release"
	observations := &hazardObservations{}
	changed := map[string]int{}
	recycled := map[string]int{}
	late, unhonoured, unadopted := map[string]int{}, map[string]int{}, map[string]int{}
	for iteration := range iterations {
		mode := "blind"
		if iteration%2 == 1 {
			mode = "foreign"
		}
		owner := newArena(t)
		lent := arenaString(owner, text)
		ownerSlab := slabBase(owner.byteSlab)
		other := newArena(t)
		if mode == "blind" {
			noteHandOff(other, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
		}
		start := make(chan struct{})
		var group sync.WaitGroup
		ownerDelay, otherDelay := rand.IntN(2000), rand.IntN(2000)
		group.Go(func() {
			<-start
			runtime.KeepAlive(spin(ownerDelay))
			owner.Reset()
		})
		group.Go(func() {
			<-start
			runtime.KeepAlive(spin(otherDelay))
			noteHandOffString(other, handOffUntracedContainer, lent)
		})
		close(start)
		group.Wait()
		kept := slabBase(owner.byteSlab) == ownerSlab
		pending := foreignPendingFor(owner.slabOwner, ownerSlab)
		if kept {
			recycled[mode]++
		}
		if pending {
			late[mode]++
		}
		if kept && !pending {
			unhonoured[mode]++
		}
		relicence(owner)
		if foreignPendingFor(owner.slabOwner, ownerSlab) {
			unadopted[mode]++
		}
		dropForeignPending(ownerSlab)
		overwriteStrings(owner, len(text))
		if lent != text {
			changed[mode]++
		}
		owner.Reset()
		other.Reset()
		runtime.KeepAlive(lent)
	}
	for _, mode := range []string{"blind", "foreign"} {
		if unhonoured[mode] > 0 {
			observations.fire("hand-off-not-honoured", "%s: in %d of %d iterations the release recycled the byte slab although no revocation was left pending, so a hand-off that landed before Reset finished was missed",
				mode, unhonoured[mode], iterations/2)
		}
		if unadopted[mode] > 0 {
			observations.fire("pending-not-adopted", "%s: in %d of %d iterations a revocation left pending after the release was still pending after the owner's next acquisition",
				mode, unadopted[mode], iterations/2)
		}
		observations.note("%s: owner recycled its byte slab in %d of %d iterations; a late hand-off was left pending in %d; the lent string changed in %d",
			mode, recycled[mode], iterations/2, late[mode], changed[mode])
	}
	assertNoHazard(t, observations, "")
}

func spin(iterations int) int {
	sink := 0
	for i := range iterations {
		sink += i ^ (sink >> 3)
	}
	return sink
}

func TestReuseHazardReleaseIsIdempotent(t *testing.T) {
	observations := &hazardObservations{}
	const text = "revoked before the first release"
	arena := newArena(t)
	held := arenaString(arena, text)
	noteHandOffString(arena, handOffUntracedContainer, held)
	releases := licenceTotal().releases.Load()
	arena.releaseLicence()
	afterFirst := slabBase(arena.byteSlab)
	arena.releaseLicence()
	arena.Reset()
	if got := licenceTotal().releases.Load() - releases; got != 1 {
		observations.fire("release-not-idempotent", "three release calls counted %d releases", got)
	}
	if slabBase(arena.byteSlab) != afterFirst {
		observations.fire("release-not-idempotent", "a repeated release replaced the byte slab again")
	}
	if arena.licenceState.released {
		observations.fire("flag-not-rearmed", "Reset left the released flag set")
	}
	relicence(arena)
	if arena.licenceState.released {
		observations.fire("flag-not-rearmed", "a fresh acquisition kept the released flag")
	}
	overwriteStrings(arena, len(text))
	observations.compareString("string revoked before the release", text, held)
	runtime.KeepAlive(held)
	assertNoHazard(t, observations, "")

	late := &hazardObservations{}
	const lateText = "handed off after the release and before Reset"
	lateArena := newArena(t)
	handed := arenaString(lateArena, lateText)
	lateArena.releaseLicence()
	noteHandOffString(lateArena, handOffUntracedContainer, handed)
	lateArena.Reset()
	relicence(lateArena)
	overwriteStrings(lateArena, len(lateText))
	late.compareString("string handed off between release and Reset", lateText, handed)
	runtime.KeepAlive(handed)
	assertNoHazard(t, late, "")
}

func TestReuseHazardStaleForeignRevocationsStayWithTheirHolder(t *testing.T) {
	observations := &hazardObservations{}
	const rounds = 64
	leaked := 0
	for range rounds {
		owner := newArena(t)
		text := arenaString(owner, "string in an arena abandoned unadopted")
		ownerSlab := slabBase(owner.byteSlab)
		witness := newArena(t)
		noteHandOffString(witness, handOffUntracedContainer, text)
		recorded := foreignPendingFor(owner.slabOwner, ownerSlab)
		abandonUnpooledRegisterArena(owner)
		if foreignPendingFor(owner.slabOwner, ownerSlab) {
			leaked++
			dropForeignPending(ownerSlab)
		}
		if !recorded {
			observations.note("a witness's hand-off recorded no revocation against the abandoned arena")
		}
		witness.Reset()
	}
	observations.note("entries left behind by %d abandoned arenas: %d", rounds, leaked)
	if leaked > 0 {
		observations.fire("pending-entry-leaked", "%d of %d abandoned arenas left a revocation pending", leaked, rounds)
	}

	const text = "held by an arena whose slab address a dead arena's entry names"
	dead := arenaSlabOwners.Add(1)
	holder := newArena(t)
	held := arenaString(holder, text)
	cSlab := slabBase(holder.byteSlab)
	t.Cleanup(func() { dropForeignPending(cSlab) })
	injectForeignPending(dead, cSlab)
	witness := newArena(t)
	noteHandOffString(witness, handOffUntracedContainer, held)
	if !foreignPendingFor(holder.slabOwner, cSlab) {
		assertNoHazard(t, observations, "the witness's hand-off recorded no revocation beside the stale entry")
		return
	}
	holder.Reset()
	adopted, abandoned := !foreignPendingFor(holder.slabOwner, cSlab), slabBase(holder.byteSlab) != cSlab
	observations.note("own entry adopted=%v, slab abandoned=%v", adopted, abandoned)
	if !adopted || !abandoned {
		observations.fire("pending-not-adopted", "the arena did not adopt its own revocation at an address a stale entry also names")
	}
	staleKept := foreignPendingFor(dead, cSlab)
	forgetLicenceSlabOwner(dead)
	staleDropped := !foreignPendingFor(dead, cSlab)
	observations.note("stale entry left by the release=%v, removed by its own holder's drop=%v", staleKept, staleDropped)
	if !staleKept {
		observations.fire("foreign-entry-taken", "the release took a revocation recorded for another arena")
	}
	if !staleDropped {
		observations.fire("pending-entry-leaked", "the dead arena's drop left its entry pending")
	}
	relicence(holder)
	overwriteStrings(holder, len(text))
	observations.compareString("string handed off from a slab a stale entry also names", text, held)
	runtime.KeepAlive(held)
	witness.Reset()
	holder.Reset()
	assertNoHazard(t, observations, "")
}

func TestReleaseClosureSpanDisagreementIsAnInvariantFailure(t *testing.T) {
	if !licencePageCheck {
		t.Skip("the span table is checked in the reuse audit build")
	}
	arena := &RegisterArena{}
	arena.licenceState.closureSpans = []licenceDataSpan{{base: 0x1000, end: 0x2000}}

	requireInvariantPanic(t, func() { arena.closureDataSlabSpan(0x1800) })
}
