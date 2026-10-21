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
	"maps"
	"reflect"
	"slices"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

type unboxedOutcome struct {
	Revoked         map[uintptr]HandOffCause
	Foreign         map[uintptr]HandOffCause
	EpochRevoked    bool
	BanksRevoked    bool
	EpochCause      HandOffCause
	EpochReason     licenceEpochReason
	WalkBudget      int
	Memo            licenceWalkMemo
	HandOffs        int64
	BlindHandOffs   int64
	SharedWalks     int64
	BlindByteWalks  int64
	BlindAll        uint64
	BlindBytes      uint64
	ForeignRevokes  int64
	EpochRevokesSum int64
	MarkLag         uint64
	ByteMarkLag     uint64
}

type unboxedScenario struct {
	name    string
	prepare func(a *RegisterArena)
}

func TestHandOffWalksAStoredValueOnce(t *testing.T) {
	counting := SetLicenceCountingForTest(true)
	t.Cleanup(func() { SetLicenceCountingForTest(counting) })
	arena := newArena(t)
	t.Cleanup(arena.Reset)
	backing := arena.AllocStringBacking(2)
	backing[0] = arenaString(arena, "stored once")
	other := arena.AllocStringBacking(2)
	value, otherValue := reflect.ValueOf(backing), reflect.ValueOf(other)

	pair := func(first HandOffCause, between func(), second reflect.Value, boxed bool) int64 {
		walkedOnce := walkedOnceStatistics()
		handOffs := licenceStatistics().HandOffs[handOffUntracedContainer.String()]
		before := arena.licenceNoteSeq()
		noteHandOff(arena, first, value)
		if between != nil {
			between()
		}
		noteHandOffAfter(arena, handOffUntracedContainer, second, boxed, before)
		require.Equal(t, handOffs+1, licenceStatistics().HandOffs[handOffUntracedContainer.String()], "the second note is counted by cause")
		return walkedOnceStatistics() - walkedOnce
	}
	require.EqualValues(t, 1, pair(handOffHeapPassthrough, nil, value, false), "the identical value is not walked again")
	require.Zero(t, pair(handOffHeapPassthrough, nil, otherValue, false), "a different value is walked")
	require.Zero(t, pair(handOffHeapPassthrough, nil, value, true), "a boxed note of it is walked")
	require.Zero(t, pair(handOffHeapPassthrough, func() { noteHandOff(arena, handOffHeapPassthrough, otherValue) }, value, false),
		"a note in between ends the pair")
	require.Zero(t, pair(handOffNativeArgBorrows, nil, value, false), "a first note that does not revoke walks nothing, so the second walks")
}

func TestHandOffAfterAPreviousNoteWalksAnotherValue(t *testing.T) {
	arena := newArena(t)
	t.Cleanup(arena.Reset)
	strs := arena.AllocStringBacking(2)
	ints := arena.AllocIntBacking(2)
	intBase, owned := licenceDataSlabOf(arena, uintptr(unsafe.Pointer(unsafe.SliceData(ints))))
	require.True(t, owned, "test harness: the int backing is the arena's own")
	before := arena.licenceNoteSeq()
	noteHandOff(arena, handOffHeapPassthrough, reflect.ValueOf(strs))
	require.False(t, arena.licenceRevokedBase(intBase), "test harness: the first note does not reach the int backing")
	noteHandOffAfter(arena, handOffUntracedContainer, reflect.ValueOf(ints), false, before)
	require.True(t, arena.licenceRevokedBase(intBase), "another value after a previous note is walked")
}

func TestRevokedEpochBookkeepingIsTheBarriersHandOff(t *testing.T) {
	if storeCensusBuilt {
		t.Skip("the revoked-epoch routes stand aside in the census build")
	}
	node := &pointerStoreNode{value: 1}
	type outcome struct {
		noteSeqDelta, revisionDelta, revoked uint64
		epochRevoked, banksRevoked           bool
		lastNotedSame                        bool
		handOffs, blind, local               int64
	}
	run := func(handOff func(a *RegisterArena, v reflect.Value)) outcome {
		arena := revokedEpochGoroutineArena(t)
		require.True(t, revokedEpochLocalBlind(arena), "test harness: the revoked-epoch routes' state")
		value := reflect.ValueOf(node)
		seq, revision := arena.licenceState.noteSeq, arena.licenceState.revision
		handOffs := licenceTotal().handOffs[handOffUntracedContainer].Load()
		blind, local := licenceTotal().blindHandOffs.Load(), blindScopeStatistics().LocalBlindHandOffs
		handOff(arena, value)
		arena.flushRevokedEpochNotes()
		observed := outcome{
			noteSeqDelta:  arena.licenceState.noteSeq - seq,
			revisionDelta: uint64(arena.licenceState.revision - revision),
			revoked:       uint64(len(arena.licenceState.revoked)),
			epochRevoked:  arena.licenceState.epochRevoked,
			banksRevoked:  arena.licenceState.banksRevoked,
			lastNotedSame: arena.licenceState.lastNoted == licenceNoteKeyOf(value, false) && arena.licenceState.lastNotedSeq == arena.licenceState.noteSeq,
			handOffs:      licenceTotal().handOffs[handOffUntracedContainer].Load() - handOffs,
			blind:         licenceTotal().blindHandOffs.Load() - blind,
			local:         blindScopeStatistics().LocalBlindHandOffs - local,
		}
		arena.Reset()
		return observed
	}
	barrier := run(func(a *RegisterArena, v reflect.Value) {
		require.Equal(t, v.Pointer(), MaterialiseArenaValue(a, v).Pointer(), "the barrier keeps the pointer")
	})
	bookkeeping := run(func(a *RegisterArena, v reflect.Value) {
		a.noteRevokedEpochHandOff(handOffUntracedContainer, v)
	})
	require.Equal(t, barrier, bookkeeping)
	require.Equal(t, int64(1), barrier.handOffs, "the barrier counted one hand-off")
}

func TestRevokedEpochCountsReachTheSharedCountersAtReset(t *testing.T) {
	if storeCensusBuilt {
		t.Skip("the revoked-epoch routes stand aside in the census build")
	}
	arena := revokedEpochGoroutineArena(t)
	require.True(t, revokedEpochLocalBlind(arena), "test harness: the revoked-epoch routes' state")
	value := reflect.ValueOf(&pointerStoreNode{value: 2})
	handOffs := licenceTotal().handOffs[handOffUntracedContainer].Load()
	blind, local := licenceTotal().blindHandOffs.Load(), blindScopeStatistics().LocalBlindHandOffs
	for range 3 {
		arena.noteRevokedEpochHandOff(handOffUntracedContainer, value)
	}
	require.Equal(t, int64(3), arena.licenceState.revokedEpochNotes.blind, "the counts wait in the arena")
	require.Equal(t, handOffs, licenceTotal().handOffs[handOffUntracedContainer].Load(), "no shared count yet")
	arena.Reset()
	require.Zero(t, arena.licenceState.revokedEpochNotes.blind, "Reset flushed the arena's counts")
	require.Equal(t, int64(3), licenceTotal().handOffs[handOffUntracedContainer].Load()-handOffs, "hand-offs")
	require.Equal(t, int64(3), licenceTotal().blindHandOffs.Load()-blind, "blind hand-offs")
	require.Equal(t, int64(3), blindScopeStatistics().LocalBlindHandOffs-local, "local blind hand-offs")
}

func TestRevokedLocalBlindHoldsOnlyWhileTheRevokedEpochRouteWould(t *testing.T) {
	if storeCensusBuilt {
		t.Skip("the revoked-epoch routes stand aside in the census build")
	}
	state := func(arena *RegisterArena) bool { return arena.licenceState.revokedLocalBlind }

	fresh := newGoroutineArenaInTree(t, &ResourceTracker{})
	defer fresh.Reset()
	require.False(t, revokedEpochLocalBlind(fresh), "an epoch not yet revoked")
	require.False(t, state(fresh), "the flag records the route's answer for an epoch not yet revoked")

	arena := revokedEpochGoroutineArena(t)
	require.False(t, state(arena), "nothing sets the flag before the route is asked")
	require.True(t, revokedEpochLocalBlind(arena))
	require.Equal(t, revokedEpochStoreRouteBuilt, state(arena), "the flag holds where the build lets the assembly read it")
	require.Equal(t, !abortInjectBuilt, revokedEpochStoreRouteBuilt, "the abort-injection build keeps the Go handler's barrier")

	empties := []struct {
		name  string
		write func(*RegisterArena)
		holds bool
	}{
		{"a licence VM serving a native call", func(a *RegisterArena) {
			a.setLicenceVM(&VM{reentrantInterpreterVM: true, debugKind: DebugThreadCallback})
		}, false},
		{"the same goroutine VM set again", func(a *RegisterArena) { a.setLicenceVM(&VM{}) }, true},
		{"an owner VM set", func(a *RegisterArena) { a.setOwnerVM(&VM{}) }, true},
		{"a new epoch", (*RegisterArena).clearLicence, false},
		{"a new licence", func(a *RegisterArena) { a.attachLicencePolicy(&VMLimits{}) }, false},
	}
	for _, write := range empties {
		require.True(t, revokedEpochLocalBlind(arena) || !write.holds, "%s: test harness", write.name)
		write.write(arena)
		require.False(t, state(arena), "%s empties the flag", write.name)
		require.Equal(t, write.holds, revokedEpochLocalBlind(arena), "%s: the route's own answer", write.name)
		require.Equal(t, write.holds && revokedEpochStoreRouteBuilt, state(arena), "%s: asking again records the answer", write.name)
		if !write.holds {
			arena.Reset()
			arena = revokedEpochGoroutineArena(t)
			require.True(t, revokedEpochLocalBlind(arena), "%s: test harness", write.name)
		}
	}

	arena.Reset()
	require.False(t, state(arena), "Reset empties the flag")
	require.False(t, revokedEpochLocalBlind(arena), "an arena Reset has no licence")
}

func TestRevokedEpochMapSliceRouteIsTheBarriers(t *testing.T) {
	if storeCensusBuilt {
		t.Skip("the revoked-epoch routes stand aside in the census build")
	}
	slot := reflect.TypeFor[[]string]()
	slice := []string{"a", "b"}
	type outcome struct {
		noteSeqDelta, revisionDelta, revoked uint64
		lastNotedSame, samePointer           bool
		handOffs, blind, local               int64
	}
	run := func(store func(a *RegisterArena, v reflect.Value) reflect.Value) outcome {
		arena := revokedEpochGoroutineArena(t)
		require.True(t, revokedEpochLocalBlind(arena))
		value := reflect.ValueOf(slice)
		seq, revision := arena.licenceState.noteSeq, arena.licenceState.revision
		handOffs := licenceTotal().handOffs[handOffUntracedContainer].Load()
		blind, local := licenceTotal().blindHandOffs.Load(), blindScopeStatistics().LocalBlindHandOffs
		stored := store(arena, value)
		arena.flushRevokedEpochNotes()
		observed := outcome{
			noteSeqDelta:  arena.licenceState.noteSeq - seq,
			revisionDelta: uint64(arena.licenceState.revision - revision),
			revoked:       uint64(len(arena.licenceState.revoked)),
			lastNotedSame: arena.licenceState.lastNoted == licenceNoteKeyOf(value, false) && arena.licenceState.lastNotedSeq == arena.licenceState.noteSeq,
			samePointer:   stored.Pointer() == value.Pointer() && stored.Len() == value.Len(),
			handOffs:      licenceTotal().handOffs[handOffUntracedContainer].Load() - handOffs,
			blind:         licenceTotal().blindHandOffs.Load() - blind,
			local:         blindScopeStatistics().LocalBlindHandOffs - local,
		}
		arena.Reset()
		return observed
	}
	barrier := run(func(a *RegisterArena, v reflect.Value) reflect.Value {
		value := materialiseArenaValueIntoSlot(a, v, slot)
		noteBoxedHandOff(a, handOffUntracedContainer, value, false)
		return value
	})
	route := run(func(a *RegisterArena, v reflect.Value) reflect.Value {
		require.True(t, mapSliceValueRevokedEpoch(a, v, slot))
		return v
	})
	require.Equal(t, barrier, route)
	require.Equal(t, int64(2), barrier.handOffs, "the barrier hands the slice off twice")
	require.True(t, barrier.samePointer, "the barrier keeps the slice")
}

func TestContainerStoreIsWalkedInASharedArena(t *testing.T) {
	arena := newGoroutineArenaInTree(t, &ResourceTracker{})
	t.Cleanup(arena.Reset)
	host := make([]string, 2)
	noteHandOff(arena, handOffNativeArgUnknown, reflect.ValueOf(host))
	require.True(t, arena.licenceState.memo.peek(unsafe.Pointer(unsafe.SliceData(host)), reflectValueABIType(reflect.TypeFor[string]()), 2),
		"test harness: the private walk memoises the host backing")
	arena.licenceAlwaysShared = true
	text := arenaString(arena, "copied into a memoised host backing while goroutines run")
	copy(host, []string{text, text})
	noteStringCopy(arena, host)

	requireByteSlabRevoked(t, arena, text)
}

func TestContainerStoreIntoAPrivatelyMemoisedBackingRevokesTheStrings(t *testing.T) {
	arena := newGoroutineArenaInTree(t, &ResourceTracker{})
	t.Cleanup(arena.Reset)
	host := make([]string, 2)
	noteHandOff(arena, handOffNativeArgUnknown, reflect.ValueOf(host))
	require.True(t, arena.licenceState.memo.peek(unsafe.Pointer(unsafe.SliceData(host)), reflectValueABIType(reflect.TypeFor[string]()), 2),
		"test harness: the private walk memoises the host backing")
	text := arenaString(arena, "copied into a privately memoised host backing")
	copy(host, []string{text, text})
	noteStringCopy(arena, host)

	requireByteSlabRevoked(t, arena, text)
}

func TestContainerStoreIsWalkedInAPrivateArena(t *testing.T) {
	arena := newGoroutineArenaInTree(t, &ResourceTracker{})
	t.Cleanup(arena.Reset)
	host := make([]string, 2)
	noteHandOff(arena, handOffNativeArgUnknown, reflect.ValueOf(host))
	text := arenaString(arena, "copied into a memoised host backing")
	copy(host, []string{text, text})
	noteStringCopy(arena, host)

	requireByteSlabRevoked(t, arena, text)
	require.False(t, arena.licenceState.containerStore, "the container-store mark is cleared after the note")
}

func TestStoreWalksRunOutLikeAnyOther(t *testing.T) {
	stores := map[string]func(a *RegisterArena, wide []string){
		"copy-builtin": func(a *RegisterArena, wide []string) {
			noteReflectCopy(a, reflect.ValueOf(wide), len(wide))
		},
		"append-host-backing": func(a *RegisterArena, wide []string) {
			noteAppendIntoBacking(a, reflect.ValueOf(make([]any, 0, 1)), reflect.ValueOf(wide), 1, false)
		},
		"shared-store": func(a *RegisterArena, wide []string) {
			noteHandOff(a, handOffSharedStore, reflect.ValueOf(wide))
		},
	}
	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			arena := newArena(t)
			blind := arena.blindCounters().all.Load()
			store(arena, make([]string, licenceWalkLeafBudget+1))
			require.True(t, arena.licenceState.epochRevoked)
			require.Equal(t, epochProbeExhausted, arena.licenceState.epochReason)
			require.Greater(t, arena.blindCounters().all.Load(), blind, "an exhausted store walk must count a blind hand-off")
		})
	}
}

func TestUnboxedHandOffNotesMatchTheBoxedOnes(t *testing.T) {
	arena := newArena(t)
	foreign := newArena(t)
	own := arenaString(arena, "own byte slab")
	other := arenaString(foreign, "foreign byte slab")
	heap := string([]byte("go heap"))
	ownSlice := arena.AllocStringBacking(3)
	ownSlice[0] = own
	registers := NewRegisters([isa.NumRegisterKinds]uint32{isa.RegisterString: 1, isa.RegisterSliceString: 2})
	registers.slicesString[0] = ownSlice
	registers.slicesString[1] = []string{heap, own}

	type handOff struct {
		name   string
		boxed  func(cause HandOffCause, boxed bool)
		direct func(cause HandOffCause, boxed bool)
	}
	stringHandOff := func(name, s string) handOff {
		return handOff{name: name,
			boxed:  func(cause HandOffCause, boxed bool) { arena.recordHandOff(cause, BoxStringToGeneral(nil, s), boxed) },
			direct: func(cause HandOffCause, boxed bool) { arena.recordHandOffString(cause, s, boxed) },
		}
	}
	sliceHandOff := func(name string, register uint8) handOff {
		return handOff{name: name,
			boxed: func(cause HandOffCause, boxed bool) {
				arena.recordHandOff(cause, registerToReflectValue(nil, &registers, isa.RegisterSliceString, register), boxed)
			},
			direct: func(cause HandOffCause, boxed bool) {
				arena.recordHandOffSliceRegister(cause, &registers, isa.RegisterSliceString, register, boxed)
			},
		}
	}
	handOffs := []handOff{
		stringHandOff("own string", own), stringHandOff("foreign string", other),
		stringHandOff("heap string", heap), stringHandOff("empty string", ""),
		sliceHandOff("own []string", 0), sliceHandOff("heap []string", 1),
	}
	scenarios := []unboxedScenario{
		{"nothing revoked", func(*RegisterArena) {}},
		{"epoch revoked", func(a *RegisterArena) { a.revokeEpoch(epochProbeExhausted, handOffNativeArgUnknown) }},
		{"shared", func(a *RegisterArena) { a.licenceAlwaysShared = true }},
	}
	run := func(scenario unboxedScenario, note func()) unboxedOutcome {
		foreign.adoptForeignRevocations()
		foreign.clearLicence()
		arena.clearLicence()
		arena.licenceState.memo = licenceWalkMemo{}
		arena.licenceAlwaysShared = false
		arena.licenceState.blindMarked = true
		arena.licenceState.blindMark, arena.licenceState.blindByteMark = arena.readBlindTotals()
		scenario.prepare(arena)
		before := licenceTotal()
		blindAll, blindBytes := licenceBlindProcess.all.Load(), licenceBlindProcess.bytes.Load()
		note()
		after := licenceTotal()
		foreign.adoptForeignRevocations()
		sum := func(c *licenceCounters) (total int64) {
			for index := range c.epochRevokes {
				total += c.epochRevokes[index].Load()
			}
			return total
		}
		handOffs := func(c *licenceCounters) (total int64) {
			for index := range c.handOffs {
				total += c.handOffs[index].Load()
			}
			return total
		}
		state := &arena.licenceState
		total, byteTotal := arena.blindTotals()
		return unboxedOutcome{
			MarkLag: total - state.blindMark, ByteMarkLag: byteTotal - state.blindByteMark,
			Revoked: maps.Clone(state.revoked), Foreign: maps.Clone(foreign.licenceState.revoked),
			EpochRevoked: state.epochRevoked, BanksRevoked: state.banksRevoked,
			EpochCause: state.epochCause, EpochReason: state.epochReason, WalkBudget: state.walkBudget,
			Memo:            licenceWalkMemo{entries: slices.Clone(state.memo.entries), spare: slices.Clone(state.memo.spare), count: state.memo.count, epoch: state.memo.epoch, used: state.memo.used},
			HandOffs:        handOffs(after) - handOffs(before),
			BlindHandOffs:   after.blindHandOffs.Load() - before.blindHandOffs.Load(),
			SharedWalks:     after.sharedWalks.Load() - before.sharedWalks.Load(),
			BlindByteWalks:  after.blindByteHandOffs.Load() - before.blindByteHandOffs.Load(),
			BlindAll:        licenceBlindProcess.all.Load() - blindAll,
			BlindBytes:      licenceBlindProcess.bytes.Load() - blindBytes,
			ForeignRevokes:  after.foreignRevokes.Load() - before.foreignRevokes.Load(),
			EpochRevokesSum: sum(after) - sum(before),
		}
	}
	walked := 0
	for _, scenario := range scenarios {
		for _, route := range handOffs {
			for cause := range HandOffCause(numHandOffCauses) {
				for _, boxed := range []bool{false, true} {
					want := run(scenario, func() { route.boxed(cause, boxed) })
					got := run(scenario, func() { route.direct(cause, boxed) })
					require.True(t, reflect.DeepEqual(want, got), "%s, %s, cause %s, boxed %v:\nboxed  %+v\ndirect %+v",
						scenario.name, route.name, cause, boxed, want, got)
					if len(want.Revoked) != 0 || len(want.Foreign) != 0 {
						walked++
					}
				}
			}
		}
	}
	require.NotZero(t, walked, "some hand-offs must revoke a slab, or the comparison is vacuous")
	require.Equal(t, !storeCensusBuilt, arena.notesUnboxed(),
		"call sites take the unboxed entries outside the census build, which observes the boxed value")
}

func TestReferencesArenaStorageProbesEveryKind(t *testing.T) {
	arena := newArena(t)
	defer arena.Reset()
	text := arenaString(arena, "arena text")
	box := arena.allocIntBox(3)
	type pair struct {
		label string
		count int
	}
	type scalars struct{ a, b int }
	var nilPointer *pair
	var nilMap map[string]int
	var nilInterface any
	cases := []struct {
		value reflect.Value
		name  string
		boxed bool
		want  bool
	}{
		{name: "no value", value: reflect.Value{}, want: false},
		{name: "an arena string", value: reflect.ValueOf(text), want: true},
		{name: "a heap string", value: reflect.ValueOf("heap"), want: false},
		{name: "an arena backing", value: reflect.ValueOf(arena.AllocIntBacking(2)), want: true},
		{name: "heap strings holding an arena string", value: reflect.ValueOf([]string{"heap", text}), want: true},
		{name: "heap integers", value: reflect.ValueOf([]int{1}), want: false},
		{name: "an array holding an arena string", value: reflect.ValueOf([2]string{text}), want: true},
		{name: "a struct holding an arena string", value: reflect.ValueOf(pair{label: text}), want: true},
		{name: "a pointer-free struct", value: reflect.ValueOf(scalars{1, 2}), want: false},
		{name: "a nil pointer", value: reflect.ValueOf(nilPointer), want: false},
		{name: "a pointer to an arena box", value: reflect.ValueOf(box), want: true},
		{name: "a heap pointer to an arena string", value: reflect.ValueOf(&pair{label: text}), want: true},
		{name: "a nil interface", value: reflect.ValueOf(&nilInterface).Elem(), want: false},
		{name: "an interface holding an arena string", value: reflect.ValueOf(&[]any{text}[0]).Elem(), want: true},
		{name: "a nil map", value: reflect.ValueOf(nilMap), want: false},
		{name: "a map keyed by an arena string", value: reflect.ValueOf(map[string]int{text: 1}), want: true},
		{name: "a map holding an arena string", value: reflect.ValueOf(map[int]string{1: text}), want: true},
		{name: "a heap map", value: reflect.ValueOf(map[string]int{"heap": 1}), want: false},
		{name: "an unsafe pointer into the arena", value: reflect.ValueOf(unsafe.Pointer(box)), want: true},
		{name: "an integer", value: reflect.ValueOf(4), want: false},
		{name: "an arena box shared with an interface", value: readOnlyBox(reflect.ValueOf(box).Elem()), boxed: true, want: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			references, truncated := arena.referencesArenaStorage(testCase.value, testCase.boxed, 4, new(64), false)
			require.Equal(t, testCase.want, references)
			require.False(t, truncated)
		})
	}
}

func TestReferencesArenaStorageAnswersYesWhenItCannotFinish(t *testing.T) {
	arena := newArena(t)
	defer arena.Reset()
	references, truncated := arena.referencesArenaStorage(reflect.ValueOf("heap"), false, 0, new(64), false)
	require.True(t, references, "out of depth")
	require.True(t, truncated)
	references, truncated = arena.referencesArenaStorage(reflect.ValueOf("heap"), false, 4, new(0), false)
	require.True(t, references, "out of budget")
	require.True(t, truncated)
	references, truncated = arena.referencesArenaStorage(reflect.ValueOf(map[string]int{"heap": 1}), false, 4, new(64), true)
	require.True(t, references, "a map another goroutine may be writing is not read")
	require.True(t, truncated)
	require.False(t, arena.ownsAnyStorage(nil))
	require.False(t, arena.referencesArenaString(""))
}
