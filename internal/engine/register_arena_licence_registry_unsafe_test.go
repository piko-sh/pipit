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
	"fmt"
	"math/rand/v2"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
)

const shardTestBase = uintptr(0x7e00_0000_0000)

type shardModel struct{ entries []licenceSlabRange }

type publishTestOperation struct {
	name string
	run  func(t *testing.T, random *rand.Rand, vm *VM)
}

var publishTestOperations = []publishTestOperation{
	{name: "cells", run: func(_ *testing.T, random *rand.Rand, vm *VM) {
		for range 1 + random.IntN(200) {
			vm.Arena.newChunkCell()
		}
	}},
	{name: "a long run of cells and closures", run: func(_ *testing.T, random *rand.Rand, vm *VM) {
		for range 1000 + random.IntN(4000) {
			vm.Arena.newChunkCell()
			vm.Arena.newChunkClosure()
		}
	}},
	{name: "closures", run: func(_ *testing.T, random *rand.Rand, vm *VM) {
		for range 1 + random.IntN(200) {
			vm.Arena.newChunkClosure()
		}
	}},
	{name: "string bytes", run: func(_ *testing.T, random *rand.Rand, vm *VM) {
		arenaString(vm.Arena, strings.Repeat("s", 1+random.IntN(64<<10)))
	}},
	{name: "a retired mutable byte slab", run: func(t *testing.T, random *rand.Rand, vm *VM) {
		if vm.Arena.splitsMutableBytes() && cap(vm.Arena.mutByteSlab) < 4<<20 {
			retireMutBytes(t, vm.Arena, 1+random.IntN(256), byte(random.IntN(256)))
		}
	}},
	{name: "generic bytes", run: func(_ *testing.T, random *rand.Rand, vm *VM) {
		vm.Arena.growGenericBytesSlab(64 + random.IntN(8<<10))
	}},
	{name: "slice headers", run: func(_ *testing.T, _ *rand.Rand, vm *VM) {
		vm.Arena.growSliceHeaderSlab()
	}},
	{name: "an int backing", run: func(_ *testing.T, random *rand.Rand, vm *VM) {
		vm.Arena.GrowIntBackingSlab(1 + random.IntN(4096))
	}},
	{name: "KRef chunks", run: func(_ *testing.T, random *rand.Rand, vm *VM) {
		if vm.Arena.krefPlaces() {
			vm.Arena.krefTake(reflect.TypeFor[*int](), 1+random.IntN(64))
		}
	}},
	{name: "a collection", run: func(_ *testing.T, _ *rand.Rand, vm *VM) {
		vm.Arena.MinorGC(vm)
	}},
	{name: "a release and Reset", run: func(t *testing.T, _ *rand.Rand, vm *VM) {
		vm.Arena.Reset()
		relicence(vm.Arena)
		setArenaBudget(t, vm.Arena, 1<<30)
	}},
}

type publishTestOperationWindow struct {
	before []licenceSlabRange
	step   int
}

type publishTestObservation struct {
	entry licenceSlabRange
	step  int
	found bool
}

type fullSortPublication struct {
	next    []licenceSlabRange
	added   []licenceSlabRange
	removed []licenceSlabRange
	dropped []uintptr
	changed bool
}

func TestReuseHazardCollectedArenaCleanupSparesTheNewOwnersRevocation(t *testing.T) {
	observations := &hazardObservations{}
	const text = "lent bytes whose revocation a collected arena's cleanup must spare"
	buffer := make([]byte, InitialByteSlabSize)
	base := slabBase(buffer)
	t.Cleanup(func() { dropForeignPending(base) })

	witness := newArena(t)
	awaitQueuedCleanups(t)
	large := &licenceRegistryShards[licenceLargeShard]
	large.mu.Lock()
	locked := true
	unlock := func() {
		if locked {
			locked = false
			large.mu.Unlock()
		}
	}
	defer unlock()

	dead := publishAndDropArenaHolding(t, buffer)
	require.Eventually(t, func() bool {
		runtime.GC()
		return !registryHoldsOwnerAt(dead, base)
	}, 20*time.Second, time.Millisecond, "the collected arena's cleanup removes its entry for the slab")
	observations.note("cleanup of arena %d removed its entry at %#x and is paused before its drop", dead, base)

	owner, published := publishArenaHoldingWithin(buffer, 10*time.Second)
	if !published {
		unlock()
		assertNoHazard(t, observations, "the new owner's publication needed the held shard")
		return
	}
	if entry, found := licenceSlabContaining(base); !found || entry.owner != owner.slabOwner {
		unlock()
		assertNoHazard(t, observations, "the new owner's slab is not the registered one at the reused address")
		return
	}
	lent := arenaString(owner, text)
	if slabBase(owner.byteSlab) != base || uintptr(unsafe.Pointer(unsafe.StringData(lent))) != base {
		unlock()
		assertNoHazard(t, observations, "the lent string is not at the reused address")
		return
	}

	noteHandOffString(witness, handOffUntracedContainer, lent)
	if !foreignPendingFor(owner.slabOwner, base) {
		unlock()
		assertNoHazard(t, observations, "the witness's hand-off recorded no foreign revocation")
		return
	}
	observations.note("the witness's hand-off returned with a revocation pending at %#x for the new owner", base)
	injectForeignPending(dead, base)

	unlock()
	require.Eventually(t, func() bool { return !foreignPendingFor(dead, base) }, 20*time.Second, time.Millisecond,
		"the collected arena's cleanup resumes and drops its own revocation")
	kept := foreignPendingFor(owner.slabOwner, base)
	observations.note("after the cleanup dropped the collected arena's entry: the new owner's still pending=%v", kept)
	if !kept {
		observations.fire("foreign-entry-taken", "the collected arena's cleanup dropped the new owner's revocation at %#x", base)
	}

	owner.Reset()
	observations.note("owner's release, begun after the hand-off returned, recycled the slab=%v", slabBase(owner.byteSlab) == base)
	relicence(owner)
	overwriteStrings(owner, len(text))
	observations.compareString("string handed off before the owner's release", text, lent)
	runtime.KeepAlive(lent)
	runtime.KeepAlive(buffer)
	witness.Reset()
	owner.Reset()
	assertNoHazard(t, observations, "")
}

func publishAndDropArenaHolding(t *testing.T, buffer []byte) uint64 {
	t.Helper()
	arena := NewRegisterArena()
	arena.replaceByteSlab(buffer, false)
	arena.attachLicencePolicy(&VMLimits{})
	entry, found := licenceSlabContaining(slabBase(buffer))
	require.True(t, found, "the arena's byte slab must be published")
	require.Equal(t, arena.slabOwner, entry.owner, "the arena's byte slab must be published under its owner")
	return arena.slabOwner
}

func publishArenaHoldingWithin(buffer []byte, limit time.Duration) (*RegisterArena, bool) {
	done := make(chan *RegisterArena, 1)
	go func() {
		a := NewRegisterArena()
		a.replaceByteSlab(buffer, false)
		a.attachLicencePolicy(&VMLimits{})
		done <- a
	}()
	select {
	case a := <-done:
		return a, true
	case <-time.After(limit):
		return nil, false
	}
}

func awaitQueuedCleanups(t *testing.T) {
	t.Helper()
	for range 3 {
		ran := make(chan struct{})
		runtime.AddCleanup(new([32]byte), func(done chan struct{}) { close(done) }, ran)
		require.Eventually(t, func() bool {
			runtime.GC()
			select {
			case <-ran:
				return true
			default:
				return false
			}
		}, 10*time.Second, time.Millisecond, "a queued cleanup runs")
	}
}

func registryHoldsOwnerAt(owner uint64, start uintptr) bool {
	for _, entry := range licenceRegistryRanges() {
		if entry.owner == owner && entry.start == start {
			return true
		}
	}
	return false
}

func TestReuseHazardReacquisitionSparesTheNewOwnersRevocation(t *testing.T) {
	observations := &hazardObservations{}
	const text = "lent bytes whose revocation another arena's acquisition must spare"
	buffer := make([]byte, InitialByteSlabSize)
	base := slabBase(buffer)
	t.Cleanup(func() { dropForeignPending(base) })

	previous := NewRegisterArena()
	previous.replaceByteSlab(buffer, false)
	previous.attachLicencePolicy(&VMLimits{})
	previous.GrowByteSlab(len(previous.byteSlab) + 1)
	if len(previous.oldByteSlabs) == 0 || slabBase(previous.oldByteSlabs[0]) != base || !registryHoldsOwnerAt(previous.slabOwner, base) {
		assertNoHazard(t, observations, "the retired slab is not published")
		return
	}
	injectForeignPending(previous.slabOwner, base)
	previous.Reset()
	observations.note("previous arena dropped its retired slab at %#x at Reset; still in the registry=%v", base, registryHoldsOwnerAt(previous.slabOwner, base))
	if !foreignPendingFor(previous.slabOwner, base) {
		observations.note("Reset's publication dropped the revocation recorded against the retired slab while the arena held it")
	} else {
		observations.fire("pending-entry-leaked", "the previous arena's publication at Reset left its own entry at %#x pending", base)
	}
	revokeForeignSlab(licenceForeignKey{start: base, owner: previous.slabOwner}, handOffNativeArgUnknown)
	if foreignPendingFor(previous.slabOwner, base) {
		observations.fire("pending-entry-leaked", "a revocation against the slab the previous arena gave up at Reset was recorded at %#x, and nothing would drop it", base)
	}

	owner := NewRegisterArena()
	owner.replaceByteSlab(buffer, false)
	owner.attachLicencePolicy(&VMLimits{})
	lent := arenaString(owner, text)
	if entry, found := licenceSlabContaining(base); !found || entry.owner != owner.slabOwner || uintptr(unsafe.Pointer(unsafe.StringData(lent))) != base {
		assertNoHazard(t, observations, "the new owner's slab is not the registered one at the reused address")
		return
	}
	witness := newArena(t)
	noteHandOffString(witness, handOffUntracedContainer, lent)
	if !foreignPendingFor(owner.slabOwner, base) {
		assertNoHazard(t, observations, "the witness's hand-off recorded no foreign revocation")
		return
	}
	observations.note("the witness's hand-off returned with a revocation pending at %#x for the new owner", base)

	relicence(previous)
	kept := foreignPendingFor(owner.slabOwner, base)
	observations.note("after the previous arena's next acquisition: the new owner's still pending=%v", kept)
	if !kept {
		observations.fire("foreign-entry-taken", "the previous arena's publication dropped the new owner's revocation at %#x", base)
	}

	owner.Reset()
	observations.note("owner's release recycled the slab=%v", slabBase(owner.byteSlab) == base)
	relicence(owner)
	overwriteStrings(owner, len(text))
	observations.compareString("string handed off before the owner's release", text, lent)
	runtime.KeepAlive(lent)
	runtime.KeepAlive(buffer)
	witness.Reset()
	previous.Reset()
	owner.Reset()
	assertNoHazard(t, observations, "")
}

func (m *shardModel) publish(owner uint64, next []licenceSlabRange) {
	var out []licenceSlabRange
	for _, entry := range m.entries {
		if entry.owner == owner || licenceOverlapsAny(next, entry) {
			continue
		}
		out = append(out, entry)
	}
	out = append(out, next...)
	slices.SortFunc(out, compareLicenceRanges)
	m.entries = out
}

func (m *shardModel) forget(owner uint64) {
	m.entries = slices.DeleteFunc(m.entries, func(entry licenceSlabRange) bool { return entry.owner == owner })
}

func (m *shardModel) find(address uintptr) (licenceSlabRange, bool) {
	for _, entry := range m.entries {
		if entry.start <= address && address < entry.end {
			return entry, true
		}
	}
	return licenceSlabRange{}, false
}

func shardTestRange(random *rand.Rand, owner uint64, span uintptr) licenceSlabRange {
	var size uintptr
	switch random.IntN(10) {
	case 0:
		size = uintptr(1<<20 + random.IntN(2<<20))
	case 1, 2:
		size = uintptr(16 * (1 + random.IntN(8192)))
	default:
		size = uintptr(16 * (1 + random.IntN(512)))
	}
	var start uintptr
	if random.IntN(3) == 0 {
		region := uintptr(random.IntN(int(span >> licenceShardRegionShift)))
		start = shardTestBase + region<<licenceShardRegionShift - uintptr(16*random.IntN(64))
	} else {
		start = shardTestBase + uintptr(16*random.IntN(int(span/16)))
	}
	return licenceSlabRange{start: start, end: start + size, owner: owner, class: licenceSlabData}
}

func shardTestCheck(t *testing.T, random *rand.Rand, model *shardModel, owners map[uint64]bool) {
	t.Helper()
	var got []licenceSlabRange
	for _, entry := range licenceRegistryRanges() {
		if owners[entry.owner] {
			got = append(got, entry)
		}
	}
	require.True(t, slices.Equal(got, model.entries), "registry holds %d test ranges, model %d:\n got %v\nwant %v", len(got), len(model.entries), got, model.entries)
	probe := func(address uintptr) {
		want, wantFound := model.find(address)
		entry, found := licenceSlabContaining(address)
		if found && !owners[entry.owner] {
			found = false
		}
		require.Equal(t, wantFound, found, "lookup %#x", address)
		if found {
			require.Equal(t, want, entry, "lookup %#x", address)
		}
	}
	for _, entry := range model.entries {
		probe(entry.start)
		probe(entry.end - 1)
		probe(entry.end)
		probe(entry.start + uintptr(random.IntN(int(entry.end-entry.start))))
	}
	for range 64 {
		probe(shardTestBase + uintptr(random.IntN(64<<20)))
	}
	pages := map[uintptr]int32{}
	for _, entry := range model.entries {
		for page := entry.start >> slabPageShift; page <= (entry.end-1)>>slabPageShift; page++ {
			pages[page]++
		}
	}
	for page, want := range pages {
		require.Equal(t, want, slabPageRegionOf(page).counter(page).Load(), "page %#x's range count", page<<slabPageShift)
	}
}

func TestShardedLicenceRegistryMatchesTheModel(t *testing.T) {
	random := rand.New(rand.NewPCG(20261101, 1))
	ownerBase := uint64(1) << 60
	owners := map[uint64]bool{}

	live := map[uint64][]licenceSlabRange{}
	var abandoned []uint64
	next := func() uint64 { ownerBase++; owners[ownerBase] = true; return ownerBase }
	for range 12 {
		live[next()] = nil
	}
	model := &shardModel{}
	span := uintptr(48 << 20)
	heldByOthers := func(owner uint64, entry licenceSlabRange) bool {
		for other, held := range live {
			if other != owner && licenceOverlapsAnyUnsorted(held, entry) {
				return true
			}
		}
		return false
	}
	for step := range 6000 {
		var owner uint64
		for candidate := range live {
			owner = candidate
			if random.IntN(3) == 0 {
				break
			}
		}
		switch {
		case random.IntN(40) == 0:
			delete(live, owner)
			abandoned = append(abandoned, owner)
			live[next()] = nil
		case random.IntN(30) == 0 && len(abandoned) > 0:
			gone := abandoned[0]
			abandoned = abandoned[1:]
			forgetLicenceSlabOwner(gone)
			model.forget(gone)
		default:
			var held []licenceSlabRange
			for _, entry := range live[owner] {
				if random.IntN(3) != 0 {
					held = append(held, entry)
				}
			}
			for range random.IntN(5) {
				entry := shardTestRange(random, owner, span)
				if !licenceOverlapsAnyUnsorted(held, entry) && !heldByOthers(owner, entry) {
					held = append(held, entry)
				}
			}
			slices.SortFunc(held, compareLicenceRanges)
			added := licenceRangesNotIn(nil, held, live[owner])
			removed := licenceRangesNotIn(nil, live[owner], held)
			applyLicenceRanges(owner, added, removed)
			model.publish(owner, held)
			live[owner] = held
		}
		if step%50 == 0 {
			shardTestCheck(t, random, model, owners)
		}
	}
	shardTestCheck(t, random, model, owners)
	for owner := range owners {
		forgetLicenceSlabOwner(owner)
		model.forget(owner)
	}
	shardTestCheck(t, random, model, owners)
}

func licenceOverlapsAnyUnsorted(list []licenceSlabRange, entry licenceSlabRange) bool {
	for _, other := range list {
		if other.start < entry.end && entry.start < other.end {
			return true
		}
	}
	return false
}

func BenchmarkLicenceRegistryLargeShardPublish(b *testing.B) {
	const (
		slabBytes = uintptr(4) << licenceShardRegionShift
		base      = uintptr(1) << 56
		owner     = uint64(1) << 62
	)
	for _, live := range []int{10, 100, 1_000, 10_000} {
		b.Run(fmt.Sprintf("live=%d", live), func(b *testing.B) {
			entries := make([]licenceSlabRange, live)
			for index := range entries {
				start := base + uintptr(index+1)*2*slabBytes
				entries[index] = licenceSlabRange{start: start, end: start + slabBytes, owner: owner + uint64(index+1), class: licenceSlabData}
				applyLicenceRanges(entries[index].owner, entries[index:index+1], nil)
			}
			b.Cleanup(func() {
				for index := range entries {
					applyLicenceRanges(entries[index].owner, nil, entries[index:index+1])
				}
			})
			own := []licenceSlabRange{{start: base, end: base + slabBytes, owner: owner, class: licenceSlabData}}
			for b.Loop() {
				applyLicenceRanges(owner, own, nil)
				applyLicenceRanges(owner, nil, own)
			}
		})
	}
}

func crowdedShardRanges(index, count int) []licenceSlabRange {
	ranges := make([]licenceSlabRange, count)
	for position := range ranges {
		region := uintptr(1)<<(slabAddressBits-licenceShardRegionShift) + uintptr(position*licenceSmallShards+index)
		start := region << licenceShardRegionShift
		ranges[position] = licenceSlabRange{start: start, end: start + 4096, owner: uint64(position + 1)}
	}
	return ranges
}

func TestAPublicationIntoACrowdedShardAllocatesNothing(t *testing.T) {
	const index = 3
	ranges := crowdedShardRanges(index, 4*licencePublishWindowLimit)
	var shard licenceRegistryShard
	shard.mu.Lock()
	defer shard.mu.Unlock()
	shard.applyLocked(index, 0, ranges, nil)
	held := ranges[len(ranges)-2 : len(ranges)-1]
	flipped := ranges[len(ranges)-1:]
	turn := 0
	publish := func() {
		turn ^= 1
		if turn == 1 {
			shard.applyLocked(index, 0, nil, flipped)
		} else {
			shard.applyLocked(index, 0, flipped, nil)
		}
	}
	publish()
	publish()
	require.Len(t, shard.current.Load().ranges, len(ranges), "test harness: the flip leaves the table whole")
	require.Contains(t, shard.current.Load().ranges, held[0])

	requireNoAllocations(t, publish)
}

func TestTheDropSetMarksEveryPositionOfALargeTable(t *testing.T) {
	var shard licenceRegistryShard
	drops := shard.dropSet(1000)
	for _, position := range []int{0, 63, 64, 255, 256, 999} {
		require.False(t, drops.has(position))
		drops.add(position)
		require.True(t, drops.has(position))
	}
	require.Equal(t, 6, drops.count)
	again := shard.dropSet(1000)
	require.False(t, again.has(999), "the kept words are cleared for the next publication")
	require.Zero(t, again.count)
}

func pickPublishTestOperation(random *rand.Rand) publishTestOperation {
	operation := publishTestOperations[random.IntN(len(publishTestOperations))]
	if operation.name == "a release and Reset" && random.IntN(4) != 0 {
		return publishTestOperations[0]
	}
	return operation
}

func newPublishTestVM(t *testing.T) *VM {
	t.Helper()
	vm := newLicensedMutBytesVM(t)
	t.Cleanup(func() {
		vm.Arena.Reset()
		abandonUnpooledRegisterArena(vm.Arena)
	})
	return vm
}

func sortedPublishTestRanges(ranges []licenceSlabRange) []licenceSlabRange {
	out := slices.Clone(ranges)
	slices.SortFunc(out, compareLicenceRanges)
	return out
}

func requirePublicationIsTheFullSort(t *testing.T, arena *RegisterArena, step string) {
	t.Helper()
	state := &arena.licenceState
	owner := arena.licenceOwnerID()
	for index := 1; index < len(state.published); index++ {
		require.Less(t, state.published[index-1].start, state.published[index].start, "%s: published is strictly increasing by start", step)
	}
	require.Equal(t, sortedPublishTestRanges(state.gathered), state.published, "%s: published is the full sort of the last gathering", step)
	held := sortedPublishTestRanges(arena.appendLicenceRanges(nil, owner))
	require.Equal(t, held, state.published, "%s: published names every slab the arena holds, and no other", step)
	var registered []licenceSlabRange
	for _, entry := range licenceRegistryRanges() {
		if entry.owner == owner {
			registered = append(registered, entry)
		}
	}
	require.Equal(t, state.published, registered, "%s: the registry holds exactly the publication", step)
}

func TestLicencePublicationStaysTheFullSortThroughInterleavedArenaOperations(t *testing.T) {
	for seed := range uint64(6) {
		random := rand.New(rand.NewPCG(20261110, 1000+seed))
		vm := newPublishTestVM(t)
		requirePublicationIsTheFullSort(t, vm.Arena, "a fresh arena")
		for range 160 {
			operation := pickPublishTestOperation(random)
			operation.run(t, random, vm)
			requirePublicationIsTheFullSort(t, vm.Arena, operation.name)
		}
		state := &vm.Arena.licenceState
		require.NotZero(t, state.publishMerged, "seed %d: some publications merge their window", seed)
		require.NotZero(t, state.publishSorted, "seed %d: some publications fall back to the full sort", seed)
	}
}

func TestPublishingAClosureChunkMergesRatherThanSortingTheArena(t *testing.T) {
	patterns := []struct {
		name string

		cells int

		closures int
	}{
		{name: "cells only", cells: 1},
		{name: "closures only", closures: 1},
		{name: "one of each", cells: 1, closures: 1},
		{name: "two cells to a closure", cells: 2, closures: 1},
		{name: "three cells to a closure", cells: 3, closures: 1},
		{name: "a cell to three closures", cells: 1, closures: 3},
		{name: "five cells to seven closures", cells: 5, closures: 7},
	}
	for _, pattern := range patterns {
		t.Run(pattern.name, func(t *testing.T) {
			vm := newPublishTestVM(t)
			arena := vm.Arena
			state := &arena.licenceState
			sorted := state.publishSorted
			for arena.closureChunkCount() < 1500 {
				for range pattern.cells {
					arena.newChunkCell()
				}
				for range pattern.closures {
					arena.newChunkClosure()
				}
			}
			require.Equal(t, sorted, state.publishSorted, "a long epoch of chunks never sorts the arena's ranges")
			requirePublicationIsTheFullSort(t, arena, "a long epoch of chunks")
			for _, take := range []func(){func() { arena.newChunkCell() }, func() { arena.newChunkClosure() }} {
				chunks := arena.closureChunkCount()
				merged := state.publishMerged
				for arena.closureChunkCount() == chunks {
					take()
				}
				require.Equal(t, sorted, state.publishSorted, "a new chunk does not sort the arena's %d ranges", len(state.published))
				require.Equal(t, merged+1, state.publishMerged, "it merges its window instead")
				requirePublicationIsTheFullSort(t, arena, "a new chunk")
			}
		})
	}
}

func TestLicenceRegistryReadersSeeConsistentSnapshotsWhileAnArenaPublishes(t *testing.T) {
	random := rand.New(rand.NewPCG(20261110, 77))
	vm := newPublishTestVM(t)
	arena := vm.Arena
	owner := arena.licenceOwnerID()
	const readers = 4
	const steps = 240
	var window atomic.Pointer[publishTestOperationWindow]
	var stop atomic.Bool
	window.Store(&publishTestOperationWindow{before: slices.Clone(arena.licenceState.published), step: -1})
	observations := make([][]publishTestObservation, readers)
	failures := make([][]string, readers)
	var group sync.WaitGroup
	for reader := range readers {
		group.Go(func() {
			local := rand.New(rand.NewPCG(20261110, uint64(reader)))
			for !stop.Load() {
				current := window.Load()
				if len(current.before) != 0 {
					entry := current.before[local.IntN(len(current.before))]
					found := licencePublishes(owner, entry.start)
					if window.Load() == current {
						observations[reader] = append(observations[reader], publishTestObservation{entry: entry, step: current.step, found: found})
					}
				}
				index := local.IntN(len(licenceRegistryShards))
				buffer := licenceRegistryShards[index].acquire()
				if buffer != nil {
					for position := 1; position < len(buffer.ranges); position++ {
						if buffer.ranges[position-1].start >= buffer.ranges[position].start {
							failures[reader] = append(failures[reader], "a shard snapshot is not strictly increasing by start")
						}
						if buffer.ranges[position-1].end > buffer.ranges[position].start {
							failures[reader] = append(failures[reader], "a shard snapshot holds overlapping ranges")
						}
					}
				}
				buffer.release()
			}
		})
	}
	after := make([][]licenceSlabRange, steps)
	for step := range steps {
		window.Store(&publishTestOperationWindow{before: slices.Clone(arena.licenceState.published), step: step})
		operation := pickPublishTestOperation(random)
		operation.run(t, random, vm)
		after[step] = slices.Clone(arena.licenceState.published)
	}
	stop.Store(true)
	group.Wait()
	for reader := range readers {
		require.Empty(t, failures[reader], "reader %d", reader)
	}
	checked := 0
	for reader := range readers {
		for _, observation := range observations[reader] {
			if observation.step < 0 || !slices.Contains(after[observation.step], observation.entry) {
				continue
			}
			checked++
			require.True(t, observation.found, "a range held before and after operation %d was missing from the registry while it ran", observation.step)
		}
	}
	require.NotZero(t, checked, "test harness: the readers looked up ranges held throughout an operation")
	requirePublicationIsTheFullSort(t, arena, "after the readers")
}

func fullSortReference(published, gathered []licenceSlabRange) fullSortPublication {
	next := slices.Clone(gathered)
	slices.SortFunc(next, compareLicenceRanges)
	if slices.Equal(next, published) {
		return fullSortPublication{next: next}
	}
	return fullSortPublication{
		next:    next,
		added:   licenceRangesNotIn(nil, next, published),
		removed: licenceRangesNotIn(nil, published, next),
		dropped: licenceRangesMissing(nil, published, next),
		changed: true,
	}
}

func requireMatchesFullSort(t *testing.T, state *arenaLicenceExactState, gathered []licenceSlabRange, step string) {
	t.Helper()
	want := fullSortReference(slices.Clone(state.published), gathered)
	changed := state.advancePublication(slices.Clone(gathered))
	require.Equal(t, want.changed, changed, "%s: whether the publication changed", step)
	require.True(t, slices.Equal(want.next, state.published), "%s: published equals the full sort\n got %v\nwant %v", step, state.published, want.next)
	require.True(t, slices.Equal(gathered, state.gathered), "%s: gathered is the new gathering", step)
	if !changed {
		return
	}
	require.True(t, slices.Equal(want.added, state.addedScratch), "%s: added\n got %v\nwant %v", step, state.addedScratch, want.added)
	require.True(t, slices.Equal(want.removed, state.removedScratch), "%s: removed\n got %v\nwant %v", step, state.removedScratch, want.removed)
	require.True(t, slices.Equal(want.dropped, state.droppedScratch), "%s: dropped\n got %v\nwant %v", step, state.droppedScratch, want.dropped)
}

func publishTestRanges(random *rand.Rand, used map[uintptr]bool, count int) []licenceSlabRange {
	out := make([]licenceSlabRange, 0, count)
	for len(out) < count {
		start := shardTestBase + uintptr(random.IntN(1<<20))<<12
		if used[start] {
			continue
		}
		used[start] = true
		out = append(out, licenceSlabRange{start: start, end: start + uintptr(16*(1+random.IntN(255))), owner: 7, class: licenceSlabClass(random.IntN(3))})
	}
	return out
}

func mutatePublishTestGathering(random *rand.Rand, used map[uintptr]bool, gathered []licenceSlabRange) ([]licenceSlabRange, string) {
	out := slices.Clone(gathered)
	at := func() int { return random.IntN(len(out) + 1) }
	switch random.IntN(14) {
	case 0, 1, 2:
		position := at()
		return slices.Insert(out, position, publishTestRanges(random, used, 1+random.IntN(4))...), "insert a few"
	case 3:
		if len(out) > 2*licencePublishWindowLimit {
			position := random.IntN(len(out) - licencePublishWindowLimit)
			return slices.Delete(out, position, position+licencePublishWindowLimit+1), "delete past the window limit"
		}
		position := at()
		return slices.Insert(out, position, publishTestRanges(random, used, licencePublishWindowLimit+random.IntN(64))...), "insert past the window limit"
	case 4, 5:
		if len(out) == 0 {
			return out, "no change"
		}
		position := random.IntN(len(out))
		return slices.Delete(out, position, min(len(out), position+1+random.IntN(3))), "delete a few"
	case 6:
		if len(out) < 2 {
			return out, "no change"
		}
		from := random.IntN(len(out))
		to := random.IntN(len(out))
		entry := out[from]
		out = slices.Delete(out, from, from+1)
		return slices.Insert(out, min(to, len(out)), entry), "move one"
	case 7:
		if len(out) == 0 {
			return out, "no change"
		}
		position := random.IntN(len(out))
		out[position].end += 16
		return out, "grow one in place"
	case 8:
		if len(out) == 0 {
			return out, "no change"
		}
		position := random.IntN(len(out))
		out[position].class = (out[position].class + 1) % 3
		return out, "reclass one"
	case 9:
		if len(out) == 0 {
			return out, "no change"
		}
		return slices.Insert(out, at(), out[random.IntN(len(out))]), "duplicate one"
	case 10:
		if len(out) == 0 {
			return out, "no change"
		}
		twin := out[random.IntN(len(out))]
		twin.end += 32
		return slices.Insert(out, at(), twin), "a second range at one start"
	case 11:
		position := at()
		return slices.Insert(out, position, publishTestRanges(random, used, 1)...), "insert one"
	case 12:
		if random.IntN(8) != 0 {
			return out, "no change"
		}
		return nil, "clear"
	default:
		return out, "no change"
	}
}

func TestLicencePublicationMatchesTheFullSort(t *testing.T) {
	for seed := range uint64(40) {
		random := rand.New(rand.NewPCG(20261110, seed))
		used := map[uintptr]bool{}
		state := &arenaLicenceExactState{}
		gathered := publishTestRanges(random, used, random.IntN(64))
		requireMatchesFullSort(t, state, gathered, "first publication")
		for range 300 {
			var how string
			gathered, how = mutatePublishTestGathering(random, used, gathered)
			requireMatchesFullSort(t, state, gathered, how)
		}
		require.NotZero(t, state.publishMerged, "seed %d: some publications merge", seed)
		require.NotZero(t, state.publishSorted, "seed %d: some publications fall back to the full sort", seed)
	}
}

func TestLicencePublicationFallsBackToTheFullSort(t *testing.T) {
	random := rand.New(rand.NewPCG(20261110, 99))
	base := func() (*arenaLicenceExactState, []licenceSlabRange) {
		t.Helper()
		state := &arenaLicenceExactState{}
		gathered := publishTestRanges(random, map[uintptr]bool{}, 40)
		require.True(t, state.advancePublication(slices.Clone(gathered)))
		return state, gathered
	}
	tests := []struct {
		name string

		gather func(gathered []licenceSlabRange) []licenceSlabRange

		tamper func(state *arenaLicenceExactState)
	}{
		{name: "a window past the limit", gather: func(gathered []licenceSlabRange) []licenceSlabRange {
			return append(slices.Clone(gathered), publishTestRanges(random, map[uintptr]bool{}, licencePublishWindowLimit+1)...)
		}},
		{name: "a new window that is not strictly increasing", gather: func(gathered []licenceSlabRange) []licenceSlabRange {
			fresh := publishTestRanges(random, map[uintptr]bool{gathered[0].start: true}, 1)[0]
			return append(slices.Clone(gathered), fresh, fresh)
		}},
		{name: "a merged list that is not strictly increasing", gather: func(gathered []licenceSlabRange) []licenceSlabRange {
			twin := gathered[5]
			twin.end += 16
			return append(slices.Clone(gathered), twin)
		}},
		{name: "a removed range published lacks", gather: func(gathered []licenceSlabRange) []licenceSlabRange {
			return slices.Delete(slices.Clone(gathered), 7, 8)
		}, tamper: func(state *arenaLicenceExactState) {
			state.published = slices.DeleteFunc(slices.Clone(state.published), func(entry licenceSlabRange) bool { return entry == state.gathered[7] })
		}},
		{name: "a removed range published holds differently", gather: func(gathered []licenceSlabRange) []licenceSlabRange {
			return slices.Delete(slices.Clone(gathered), 9, 10)
		}, tamper: func(state *arenaLicenceExactState) {
			state.published = slices.Clone(state.published)
			for index := range state.published {
				if state.published[index] == state.gathered[9] {
					state.published[index].end += 16
				}
			}
		}},
		{name: "a published list that is not strictly increasing", gather: func(gathered []licenceSlabRange) []licenceSlabRange {
			return slices.Delete(slices.Clone(gathered), 11, 12)
		}, tamper: func(state *arenaLicenceExactState) {
			state.published = append(slices.Clone(state.published), state.published[len(state.published)-1])
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, gathered := base()
			if tt.tamper != nil {
				tt.tamper(state)
			}
			merged := state.publishMerged
			sorted := state.publishSorted
			requireMatchesFullSort(t, state, tt.gather(gathered), tt.name)
			require.Equal(t, merged, state.publishMerged, "the publication does not merge")
			require.Equal(t, sorted+1, state.publishSorted, "the publication falls back to the full sort")
		})
	}
}

func TestLicencePublicationMergesAShortWindow(t *testing.T) {
	random := rand.New(rand.NewPCG(20261110, 7))
	used := map[uintptr]bool{}
	state := &arenaLicenceExactState{}
	gathered := publishTestRanges(random, used, 2000)
	requireMatchesFullSort(t, state, gathered, "first publication")
	merged := state.publishMerged
	sorted := state.publishSorted
	gathered = slices.Insert(gathered, 1200, publishTestRanges(random, used, 4)...)
	requireMatchesFullSort(t, state, gathered, "a new chunk with its spares")
	gathered = slices.Delete(gathered, 300, 302)
	requireMatchesFullSort(t, state, gathered, "two slabs dropped")
	gathered[1500].end += 4096
	requireMatchesFullSort(t, state, gathered, "a slab regrown in place")
	require.Equal(t, merged+3, state.publishMerged, "each short window merges")
	require.Equal(t, sorted, state.publishSorted, "and none sorts the whole gathering")
}

func TestMergePublishedRangesRefusesWhatTheFullSortWouldNotProduce(t *testing.T) {
	ranges := func(starts ...uintptr) []licenceSlabRange {
		out := make([]licenceSlabRange, 0, len(starts))
		for _, start := range starts {
			out = append(out, licenceSlabRange{start: start, end: start + 8, owner: 1, class: licenceSlabData})
		}
		return out
	}
	tests := []struct {
		name string

		published []licenceSlabRange

		added []licenceSlabRange

		removed []licenceSlabRange

		want []licenceSlabRange

		ok bool
	}{
		{name: "adds and removes", published: ranges(16, 48, 80), added: ranges(32, 96), removed: ranges(48), want: ranges(16, 32, 80, 96), ok: true},
		{name: "adds before everything", published: ranges(48), added: ranges(16), want: ranges(16, 48), ok: true},
		{name: "replaces a range at its start", published: ranges(16, 48), added: []licenceSlabRange{{start: 48, end: 64, owner: 1}}, removed: ranges(48),
			want: []licenceSlabRange{ranges(16)[0], {start: 48, end: 64, owner: 1}}, ok: true},
		{name: "a removal published lacks", published: ranges(16, 48), removed: ranges(32)},
		{name: "a removal past the end", published: ranges(16, 48), removed: ranges(64)},
		{name: "a removal that differs", published: ranges(16, 48), removed: []licenceSlabRange{{start: 48, end: 80, owner: 1}}},
		{name: "an addition at a kept start", published: ranges(16, 48), added: []licenceSlabRange{{start: 48, end: 64, owner: 1}}},
		{name: "a published duplicate", published: ranges(16, 16, 48), removed: ranges(16)},
		{name: "a published list out of order", published: ranges(48, 16)},
		{name: "added ranges out of order", published: ranges(16), added: ranges(96, 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := mergePublishedRanges(nil, tt.published, tt.added, tt.removed)
			require.Equal(t, tt.ok, ok)
			if ok {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestLicenceWindowDifferenceRefusesWindowsThatAreNotStrictlyIncreasing(t *testing.T) {
	one := licenceSlabRange{start: 16, end: 32}
	two := licenceSlabRange{start: 16, end: 48}
	_, _, ok := licenceWindowDifference(nil, nil, []licenceSlabRange{one, two}, nil)
	require.False(t, ok, "two ranges at one start before")
	_, _, ok = licenceWindowDifference(nil, nil, nil, []licenceSlabRange{one, one})
	require.False(t, ok, "a duplicate after")
	added, removed, ok := licenceWindowDifference(nil, nil, []licenceSlabRange{one}, []licenceSlabRange{two})
	require.True(t, ok)
	require.Equal(t, []licenceSlabRange{two}, added, "a range regrown at its start is added")
	require.Equal(t, []licenceSlabRange{one}, removed, "and its old form removed")
}

func TestLicenceGatherWindowNeverOverlapsItsPrefixAndSuffix(t *testing.T) {
	a := licenceSlabRange{start: 16}
	b := licenceSlabRange{start: 32}
	c := licenceSlabRange{start: 48}
	tests := []struct {
		name string

		previous []licenceSlabRange

		gathered []licenceSlabRange

		prefix int

		suffix int
	}{
		{name: "equal", previous: []licenceSlabRange{a, b}, gathered: []licenceSlabRange{a, b}, prefix: 2},
		{name: "appended", previous: []licenceSlabRange{a, b}, gathered: []licenceSlabRange{a, b, c}, prefix: 2},
		{name: "inserted in the middle", previous: []licenceSlabRange{a, c}, gathered: []licenceSlabRange{a, b, c}, prefix: 1, suffix: 1},
		{name: "a repeated range inserted", previous: []licenceSlabRange{a, a}, gathered: []licenceSlabRange{a, a, a}, prefix: 2},
		{name: "removed at the front", previous: []licenceSlabRange{a, b, c}, gathered: []licenceSlabRange{b, c}, suffix: 2},
		{name: "empty", previous: nil, gathered: []licenceSlabRange{a}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, suffix := licenceGatherWindow(tt.previous, tt.gathered)
			require.Equal(t, tt.prefix, prefix, "prefix")
			require.Equal(t, tt.suffix, suffix, "suffix")
			require.LessOrEqual(t, prefix+suffix, min(len(tt.previous), len(tt.gathered)))
		})
	}
}
