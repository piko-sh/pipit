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

//go:build pipit_store_census && !safe && !(js && wasm)

package engine

import (
	"maps"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func censusRowByName(t *testing.T, op string) isa.OpSpec {
	t.Helper()
	for _, row := range isa.AllSpecs() {
		if row.Name == op {
			return row
		}
	}
	require.FailNowf(t, "no ISA row", "%s", op)
	return isa.OpSpec{}
}

func censusCoveredStoreDelta(store func()) int64 {
	count := func() int64 {
		total := int64(0)
		for key, n := range StoreCensusViolations() {
			if strings.HasPrefix(key, "uncovered-outside-store\t") && strings.Contains(key, "\tarena-covered\t") {
				total += n
			}
		}
		return total
	}
	before := count()
	store()
	return count() - before
}

func TestCensusFlagsAStoreIntoCoveredOwnStorage(t *testing.T) {
	arena := newArena(t)
	t.Cleanup(arena.Reset)
	arena.licenceState.blindMarked = true
	arena.licenceState.blindMark, arena.licenceState.blindByteMark = arena.readBlindTotals()
	backing := arena.AllocStringBacking(4)
	base, owned := licenceDataSlabOf(arena, uintptr(unsafe.Pointer(unsafe.SliceData(backing))))
	require.True(t, owned, "test harness: the backing is the arena's own data slab")
	arena.revokeSlab(base, handOffNativeArgUnknown)
	require.False(t, censusArenaCoversAll(arena), "test harness: the arena has uncovered storage to find")
	row := censusRowByName(t, "SET_SLICE_STRING_ELEMENT")
	store := func(text string) {
		containers := []censusContainer{}
		if container, ok := censusClassify(arena, reflect.ValueOf(backing)); ok {
			require.Equal(t, "arena-covered", container.class)
			containers = append(containers, container)
		}
		backing[0] = text
		censusSettle(arena, row, containers, false)
	}

	require.Positive(t, censusCoveredStoreDelta(func() {
		store(arenaString(arena, "stored into covered storage with no hand-off"))
	}), "a covered own slab now references an unrevoked byte slab")

	backing[0] = ""
	text := arenaString(arena, "stored into covered storage after its hand-off")
	require.Zero(t, censusCoveredStoreDelta(func() {
		noteHandOffString(arena, handOffNativeArgUnknown, text)
		store(text)
	}), "the string's hand-off revoked its byte slab, so the store leaves nothing uncovered")
}

func isolateCensusViolations(t *testing.T) {
	t.Helper()
	censusMu.Lock()
	saved := maps.Clone(censusViolations)
	censusMu.Unlock()
	t.Cleanup(func() {
		censusMu.Lock()
		censusViolations = saved
		censusMu.Unlock()
	})
}

func censusCount(kind, op, class, detail string) int64 {
	censusMu.Lock()
	defer censusMu.Unlock()
	return censusViolations[censusViolationKey{kind: kind, op: op, class: class, detail: detail}]
}

func TestCensusRecordTransitionClassifiesTheStore(t *testing.T) {
	isolateCensusViolations(t)
	move, mapSet := censusRowByName(t, "MOVE_GENERAL"), censusRowByName(t, "SET_MAP")
	censusMu.Lock()
	censusRecordTransition(move, "heap-local", false, true, "detail")
	censusRecordTransition(mapSet, "heap-local", true, true, "detail")
	censusRecordTransition(move, censusClassHeapPublished, false, true, "detail")
	censusRecordTransition(mapSet, censusClassGlobal, false, false, "detail")
	censusMu.Unlock()

	require.EqualValues(t, 1, censusCount("undeclared-local-store", "MOVE_GENERAL", "heap-local", "detail"))
	require.EqualValues(t, 1, censusCount("unrevoked-local-store", "MOVE_GENERAL", "heap-local", "detail"))
	require.Zero(t, censusCount("unrevoked-local-store", "SET_MAP", "heap-local", "detail"), "a revoked store")
	require.EqualValues(t, 1, censusCount("undeclared-outside-store", "MOVE_GENERAL", censusClassHeapPublished, "detail"))
	require.EqualValues(t, 1, censusCount("unrevoked-outside-store", "MOVE_GENERAL", censusClassHeapPublished, "detail"))
	require.EqualValues(t, 1, censusCount("unrevoked-outside-store-without-licence", "SET_MAP", censusClassGlobal, "detail"))
	require.True(t, censusArenaLicensed(newArena(t)))
	require.False(t, censusArenaLicensed(nil))
}

func TestCensusSettleGlobalsDrainsTheInstructionsStores(t *testing.T) {
	isolateCensusViolations(t)
	arena := newArena(t)
	t.Cleanup(arena.Reset)
	globals := NewGlobalStore()
	log := censusGlobalLogFor(globals)
	censusNoteGlobalStore(globals, reflect.ValueOf("before the instruction"))
	generation := log.generation.Load()
	censusNoteGlobalStore(globals, reflect.ValueOf(arenaString(arena, "stored by the instruction")))
	row := censusRowByName(t, "MOVE_GENERAL")

	censusSettleGlobals(arena, row, log, generation, false)
	require.Len(t, log.recent, 1, "only the instruction's own store is drained")
	require.EqualValues(t, 1, censusCount("unrevoked-outside-store", "MOVE_GENERAL", censusClassGlobal, "string"))
	require.EqualValues(t, 1, censusCount("undeclared-outside-store", "MOVE_GENERAL", censusClassGlobal, "string"))
}

func TestCensusNoteHandOffChecksTheInstructionsRow(t *testing.T) {
	isolateCensusViolations(t)
	arena := newArena(t)
	t.Cleanup(arena.Reset)
	state := &censusVMState{row: censusRowByName(t, "MOVE_GENERAL"), inInstruction: true}
	censusArenaStates.store(arena, state)
	t.Cleanup(func() { censusArenaStates.delete(arena) })

	entered, was := censusEnterBoundary(arena)
	require.Same(t, state, entered)
	require.True(t, was)
	require.False(t, state.inInstruction, "a boundary suspends instruction attribution")
	censusLeaveBoundary(entered, was)
	require.True(t, state.inInstruction)
	_, was = censusEnterBoundary(nil)
	require.False(t, was)

	published := &[]int{1}
	before := StoreCensusViolations()
	censusNoteHandOff(arena, handOffNativeArgRetains, reflect.ValueOf(published))
	require.True(t, state.revoked, "a revoking cause marks the instruction revoked")
	after := StoreCensusViolations()
	undeclared := int64(0)
	for key, count := range after {
		if strings.HasPrefix(key, "undeclared-cause\tMOVE_GENERAL\t") {
			undeclared += count - before[key]
		}
	}
	require.EqualValues(t, 1, undeclared, "MOVE_GENERAL declares no hand-off")
	_, isPublished := censusPublished.load(uintptr(unsafe.Pointer(published)))
	require.True(t, isPublished, "what is handed off is published")
}

func TestCensusForgetsAPublishedAddressWhenAMapIsMadeThere(t *testing.T) {
	made := reflect.ValueOf(map[string][]string{})
	censusPublish(made)
	_, published := censusPublished.load(uintptr(made.UnsafePointer()))
	require.True(t, published, "the address is published, as it would be had the host once held storage there")

	censusNoteMadeMap(made)
	_, published = censusPublished.load(uintptr(made.UnsafePointer()))
	require.False(t, published, "a map the guest has just made was never published")

	censusPublish(made)
	_, published = censusPublished.load(uintptr(made.UnsafePointer()))
	require.True(t, published, "handing it off later publishes it again")
	censusNoteMadeMap(reflect.ValueOf(map[string]int(nil)))
	censusNoteMadeMap(reflect.ValueOf(3))
}

func censusKindCount(kind string) int64 {
	total := int64(0)
	for key, n := range StoreCensusViolations() {
		if strings.HasPrefix(key, kind+"\t") {
			total += n
		}
	}
	return total
}

func TestCensusChecksAGrantedStoresMapIsNeitherMemoisedNorPublished(t *testing.T) {
	arena := newArena(t)
	memoisedBefore := censusKindCount("local-store-memoised-container")
	publishedBefore := censusKindCount("local-store-published-container")

	local := reflect.ValueOf(map[string][]string{})
	censusNoteLocalContainerStore(arena, local)
	censusNoteLocalContainerStore(arena, reflect.ValueOf(map[string][]string(nil)))
	censusNoteLocalContainerStore(arena, reflect.ValueOf(7))
	require.Equal(t, memoisedBefore, censusKindCount("local-store-memoised-container"), "a map nothing walked is absent from the memo")
	require.Equal(t, publishedBefore, censusKindCount("local-store-published-container"))

	walked := reflect.ValueOf(map[string][]string{})
	arena.licenceState.memo.note(walked.UnsafePointer(), reflectValueABIType(walked.Type()), 0)
	censusNoteLocalContainerStore(arena, walked)
	require.Equal(t, memoisedBefore+1, censusKindCount("local-store-memoised-container"),
		"a later hand-off of a memoised map would be settled without a walk")

	handedOff := reflect.ValueOf(map[string][]string{})
	censusPublish(handedOff)
	censusNoteLocalContainerStore(arena, handedOff)
	require.Equal(t, publishedBefore+1, censusKindCount("local-store-published-container"))
	require.True(t, StoreCensusGates("local-store-memoised-container"))
	require.True(t, StoreCensusGates("local-store-published-container"))
}

func TestStoreCensusGatesOnlyTheGatingKinds(t *testing.T) {
	require.True(t, StoreCensusGates("uncovered-outside-store"))
	require.False(t, StoreCensusGates("undeclared-local-store"))
}

func TestCensusNoteBuiltinCopyFlagsAnUnnotedCopyOfArenaReferences(t *testing.T) {
	isolateCensusViolations(t)
	arena := NewRegisterArena()
	destination := []string{arenaString(arena, "copied out of the arena")}
	censusNoteBuiltinCopy(arena, reflect.ValueOf(destination), 1)
	require.EqualValues(t, 1, censusCount("copy-builtin-unnoted", "COPY_GENERAL", "outside", "[]string"))

	censusNoteBuiltinCopy(arena, reflect.ValueOf(destination), 0)
	censusNoteBuiltinCopy(arena, reflect.ValueOf([]int{1}), 1)
	censusNoteBuiltinCopy(arena, reflect.ValueOf([]string{"heap"}), 1)
	censusNoteBuiltinCopy(nil, reflect.ValueOf(destination), 1)
	require.EqualValues(t, 1, censusCount("copy-builtin-unnoted", "COPY_GENERAL", "outside", "[]string"), "nothing else is flagged")
}

func TestCensusFlagsStringsOverMutableBytes(t *testing.T) {
	isolateCensusViolations(t)
	arena := newArena(t)
	t.Cleanup(arena.Reset)
	row := censusRowByName(t, "MOVE_GENERAL")
	registers := &Registers{Strings: []string{"heap"}}
	censusNoteStringsOverMutBytes(arena, registers, row)
	require.Zero(t, censusCount("string-over-mutable-bytes", "MOVE_GENERAL", "register", "string"), "the arena has no mutable bytes yet")

	mutable := arena.AllocByteBacking(8)
	over := unsafe.String(unsafe.SliceData(mutable), len(mutable))
	registers = &Registers{Strings: []string{over}, General: []reflect.Value{reflect.ValueOf(over)}, slicesString: [][]string{{over, "heap"}}}
	censusNoteStringsOverMutBytes(arena, registers, row)
	require.EqualValues(t, 3, censusCount("string-over-mutable-bytes", "MOVE_GENERAL", "register", "string"))
}

func TestCensusNoteServedStoreChecksTheRegistry(t *testing.T) {
	isolateCensusViolations(t)
	served := newArena(t)
	t.Cleanup(served.Reset)
	text := arenaString(served, "served")
	target := unsafe.Pointer(unsafe.SliceData(served.AllocStringBacking(1)))
	count := func(kind string) int64 {
		total := int64(0)
		for key, n := range StoreCensusViolations() {
			if strings.HasPrefix(key, kind+"\t") {
				total += n
			}
		}
		return total
	}
	foreignValue, foreignTarget := count("served-store-foreign-value"), count("served-store-foreign-target")
	censusNoteServedStore(served, served, target, strings.Repeat("h", 4))
	require.Equal(t, foreignValue+1, count("served-store-foreign-value"), "a heap string is not the served arena's")
	censusNoteServedStore(served, served, unsafe.Pointer(new(string)), text)
	require.Equal(t, foreignTarget+1, count("served-store-foreign-target"), "a heap destination is not the served arena's")
}

func TestKRefMayBeCoveredReadsTheLicenceWithoutChangingIt(t *testing.T) {
	arena := newArena(t)
	t.Cleanup(arena.Reset)
	require.False(t, arena.krefMayBeCovered(unsafe.Pointer(new(int))), "not a KRef chunk")
	slot := krefTestSlot(t, arena, reflect.TypeFor[krefTestNode]())
	pointer := slot.Addr().UnsafePointer()
	arena.licenceState.blindMarked = false
	require.True(t, arena.krefMayBeCovered(pointer), "a skipped walk might have named the chunk")
	arena.licenceState.blindMarked = true
	arena.licenceState.blindMark, arena.licenceState.blindByteMark = arena.readBlindTotals()
	require.False(t, arena.krefMayBeCovered(pointer))
	chunk, _ := arena.krefFind(uintptr(pointer))
	arena.revokeSlab(chunk.base, handOffNativeArgRetains)
	require.True(t, arena.krefMayBeCovered(pointer), "a revoked chunk")
}

func TestCensusClassifiesContainers(t *testing.T) {
	arena := newArena(t)
	t.Cleanup(arena.Reset)
	_, known := censusClassify(arena, reflect.ValueOf(3))
	require.False(t, known, "a scalar is no container")
	var nilInterface any
	pointer, _ := censusStorage(reflect.ValueOf(&nilInterface).Elem())
	require.Nil(t, pointer)
	pointer, _ = censusStorage(reflect.ValueOf(struct{ p *int }{}))
	require.Nil(t, pointer, "a struct with no address writes nowhere")
	pointer, _ = censusStorage(reflect.ValueOf(&struct{ p *int }{}).Elem())
	require.NotNil(t, pointer)

	local, known := censusClassify(arena, reflect.ValueOf([]string{"heap"}))
	require.True(t, known)
	require.Equal(t, "heap-local", local.class)
	published := []string{"published"}
	censusPublish(reflect.ValueOf(published))
	container, _ := censusClassify(arena, reflect.ValueOf(published))
	require.Equal(t, censusClassHeapPublished, container.class)
	own, _ := censusClassify(arena, reflect.ValueOf(arena.AllocStringBacking(2)))
	require.Equal(t, "arena", own.class)

	references, probed := censusProbe(nil, reflect.ValueOf([]string{}))
	require.False(t, references)
	require.False(t, probed, "no arena to probe")
	require.False(t, censusOwnStorageCovered(NewRegisterArena(), unsafe.Pointer(new(int))), "an arena without a licence covers nothing")
}

func TestCensusContainersReadsTheGeneralOperands(t *testing.T) {
	arena := newArena(t)
	t.Cleanup(arena.Reset)
	row := censusRowByName(t, "SET_MAP")
	registers := &Registers{General: []reflect.Value{reflect.ValueOf(map[string]int{}), reflect.ValueOf("key"), reflect.ValueOf(1)}}
	containers := censusContainers(arena, registers, isa.NewInstruction(isa.OpSetMap, 0, 1, 2), row)
	require.Len(t, containers, 1, "the map is the only container")
	require.Empty(t, censusContainers(arena, registers, isa.NewInstruction(isa.OpSetMap, 9, 9, 9), row), "registers out of range")
}

func TestCensusPublishesWhatANativeCallReturned(t *testing.T) {
	registers := &Registers{General: []reflect.Value{reflect.ValueOf([]int{1})}, slicesString: [][]string{{"a"}}}
	before := censusGeneralStorage(registers)
	returned, names := []int{2}, []string{"b"}
	registers.General[0] = reflect.ValueOf(returned)
	registers.slicesString[0] = names
	censusPublishCallResults(registers, before)
	_, published := censusPublished.load(uintptr(unsafe.Pointer(unsafe.SliceData(returned))))
	require.True(t, published)
	_, published = censusPublished.load(uintptr(unsafe.Pointer(unsafe.SliceData(names))))
	require.True(t, published)
	censusPublishCallResults(registers, nil)
}
