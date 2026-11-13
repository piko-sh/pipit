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
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

type markedReceiver struct{ text string }

type markOutcome struct {
	byteSlabs   []bool
	genericSlab []bool
	headerSlabs []bool
	liveBytes   int64
	liveGeneric int64
	budget      int
	failClosed  licenceFailClosedReason
}

type markedRecord struct {
	text  string
	count int
}

type mapInUnexportedField struct {
	lookup map[int]string
}

type sliceInUnexportedField struct {
	pairs []*gcTracePair
}

func markOf(t *testing.T, vm *VM, value reflect.Value) *gcMarkState {
	t.Helper()
	vm.CallStack = []CallFrame{{Registers: Registers{General: []reflect.Value{value}}}}
	vm.FramePointer = 0
	state := acquireGCMarkState(vm.Arena)
	vm.markPhase(state)
	return state
}

func TestLicenceMarkFailsClosedOnAFunctionValue(t *testing.T) {
	captured := strings.Repeat("x", 3)
	closure := func() string { return captured }
	madeFunc := reflect.MakeFunc(reflect.TypeFor[func() string](), func([]reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(captured)}
	})
	for _, testCase := range []struct {
		name       string
		value      reflect.Value
		failClosed bool
	}{
		{"top-level function", reflect.ValueOf(strings.ToUpper), false},
		{"closure", reflect.ValueOf(closure), true},
		{"reflect.MakeFunc", madeFunc, true},
	} {
		vm := newGCTestVM()
		vm.Arena.attachLicencePolicy(&VMLimits{})
		state := markOf(t, vm, testCase.value)
		if testCase.failClosed {
			require.Equal(t, failClosedFunction, state.failClosed, testCase.name)
		} else {
			require.Zero(t, state.failClosed, testCase.name)
		}
		releaseGCMarkState(state)
	}
}

func (r markedReceiver) Len() int { return len(r.text) }

func TestLicenceMarkTracesAMethodValuesReceiver(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena
	arena.attachLicencePolicy(&VMLimits{})
	setArenaBudget(t, arena, 1<<30)
	held := arenaString(arena, "held only by a method value's receiver")
	arena.GrowByteSlab(InitialByteSlabSize)
	method := reflect.ValueOf(markedReceiver{text: held}).MethodByName("Len")
	state := markOf(t, vm, method)
	defer releaseGCMarkState(state)
	require.Zero(t, state.failClosed)
	require.True(t, state.OldByteSlabLive[0], "the receiver's string keeps its slab live")
}

func TestLicenceMarkTracesMapValues(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena
	arena.attachLicencePolicy(&VMLimits{})
	setArenaBudget(t, arena, 1<<30)
	held := arenaString(arena, "held only by a map value")
	arena.GrowByteSlab(InitialByteSlabSize)
	vm.CallStack = []CallFrame{{Registers: Registers{General: []reflect.Value{reflect.ValueOf(map[int]string{1: held})}}}}
	vm.FramePointer = 0
	state := acquireGCMarkState(arena)
	defer releaseGCMarkState(state)
	vm.markPhase(state)
	require.Zero(t, state.failClosed)
	require.True(t, state.OldByteSlabLive[0], "the licensed mark must trace into the map")
}

func TestLicenceMarkWalksAMapWithoutCopyingItsEntries(t *testing.T) {
	if raceDetectorOn {
		t.Skip("the race detector changes allocation counts")
	}
	vm := newGCTestVM()
	arena := vm.Arena
	arena.attachLicencePolicy(&VMLimits{})
	setArenaBudget(t, arena, 1<<30)
	lookup := make(map[int]string, 64)
	for index := range 64 {
		lookup[index] = arenaString(arena, "held only by a map value")
	}
	arena.GrowByteSlab(InitialByteSlabSize)
	value := reflect.ValueOf(lookup)
	state := acquireGCMarkState(arena)
	defer releaseGCMarkState(state)

	allocations := testing.AllocsPerRun(20, func() {
		state.traceBudget = licenceMarkTraceBudget
		state.visited.reset(gcVisitedKeptSize)
		markReflectValue(arena, state, value)
	})
	require.True(t, state.OldByteSlabLive[0])
	require.Zero(t, allocations, "the mark state keeps its holders, and copies no entry")
}

func TestLicenceMarkStopsOnceFailedClosed(t *testing.T) {
	arena := NewRegisterArena()
	arena.attachLicencePolicy(&VMLimits{})
	setArenaBudget(t, arena, 1<<30)
	lookup := map[int]string{1: arenaString(arena, "held only by a map value")}
	arena.GrowByteSlab(InitialByteSlabSize)
	value := reflect.ValueOf(lookup)

	state := acquireGCMarkState(arena)
	defer releaseGCMarkState(state)
	state.failClose(failClosedFunction)
	budget := state.traceBudget
	markReflectValue(arena, state, value)
	require.False(t, state.OldByteSlabLive[0], "a failed-closed licensed mark visits nothing more")
	require.False(t, state.spendTraceBudget())
	require.Equal(t, budget, state.traceBudget, "a settled mark spends no budget")
	require.Equal(t, failClosedFunction, state.failClosed, "the first reason stands")
	reclaimed, recycled := arena.compactPhase(state)
	require.Zero(t, reclaimed, "a failed-closed licensed cycle compacts nothing")
	require.Zero(t, recycled)

	withoutLicence := acquireGCMarkState(arena)
	defer releaseGCMarkState(withoutLicence)
	withoutLicence.licensed = false
	withoutLicence.failClose(failClosedFunction)
	require.False(t, withoutLicence.markSettled(), "an arena without a licence compacts by its marks")
	markReflectValue(arena, withoutLicence, reflect.ValueOf([]string{lookup[1]}))
	require.True(t, withoutLicence.OldByteSlabLive[0], "so its mark keeps marking")
}

func TestLicenceMarkMapHoldersAreZeroAndFreeAfterAWalk(t *testing.T) {
	arena := NewRegisterArena()
	arena.attachLicencePolicy(&VMLimits{})
	setArenaBudget(t, arena, 1<<30)
	inner := map[string]any{arenaString(arena, "inner key"): arenaString(arena, "inner element")}
	outer := map[string]any{arenaString(arena, "outer key"): inner, "heap key": arenaString(arena, "outer element")}
	arena.GrowByteSlab(InitialByteSlabSize)
	state := acquireGCMarkState(arena)
	defer releaseGCMarkState(state)

	state.mapHolders = state.mapHolders[:0]

	markMapWalk(arena, state, reflect.ValueOf(outer))
	require.True(t, state.OldByteSlabLive[0], "the nested walk marks through its own holders")
	require.Len(t, state.mapHolders, 4, "the nested map of the same type takes a second key and element holder")
	for index, holder := range state.mapHolders {
		require.False(t, holder.inUse, "holder %d is free", index)
		require.True(t, holder.value.IsZero(), "holder %d keeps nothing reachable", index)
	}
	markMapWalk(arena, state, reflect.ValueOf(outer))
	require.Len(t, state.mapHolders, 4, "a later walk reuses the kept holders")

	large := map[int][512]byte{1: {}}
	keyHolder := state.takeMapHolder(reflect.TypeFor[[512]byte]())
	require.False(t, keyHolder.kept, "a holder larger than the bound is not kept")
	keyHolder.release()
	markMapWalk(arena, state, reflect.ValueOf(large))
	require.Len(t, state.mapHolders, 4)
}

func markOutcomeOf(state *gcMarkState) markOutcome {
	if state.failClosed != 0 {
		return markOutcome{budget: state.traceBudget, failClosed: state.failClosed}
	}
	return markOutcome{
		byteSlabs:   append([]bool(nil), state.OldByteSlabLive...),
		genericSlab: append([]bool(nil), state.OldGenericBytesLive...),
		headerSlabs: append([]bool(nil), state.OldSliceHeaderLive...),
		liveBytes:   state.CurrentLiveBytes,
		liveGeneric: state.CurrentLiveGenericBytes,
		budget:      state.traceBudget,
		failClosed:  state.failClosed,
	}
}

func markMapWalk(arena *RegisterArena, state *gcMarkState, value reflect.Value) {
	base := len(state.work)
	markMapEntries(state, value, 0)
	drainMark(arena, state, base)
}

func markCapacityWalk(arena *RegisterArena, state *gcMarkState, value reflect.Value) {
	base := len(state.work)
	state.work = append(state.work, gcMarkWork{value: capacityElements(value), kind: gcMarkCapacityElements})
	drainMark(arena, state, base)
}

func markMapEntriesByCopying(arena *RegisterArena, state *gcMarkState, value reflect.Value) {
	iterator := value.MapRange()
	for iterator.Next() {
		if !state.spendTraceBudget() {
			return
		}
		markReflectValue(arena, state, iterator.Key())
		markReflectValue(arena, state, iterator.Value())
	}
}

func markWith(arena *RegisterArena, budget int, mark func(*gcMarkState)) markOutcome {
	state := acquireGCMarkState(arena)
	defer releaseGCMarkState(state)
	state.traceBudget = budget
	mark(state)
	return markOutcomeOf(state)
}

func TestLicenceMarkMapWalkDecidesAsTheCopyingWalk(t *testing.T) {
	arena := NewRegisterArena()
	arena.attachLicencePolicy(&VMLimits{})
	setArenaBudget(t, arena, 1<<30)
	texts := make([]string, 6)
	for index := range texts {
		texts[index] = arenaString(arena, strings.Repeat("k", index+1))
	}
	arena.GrowByteSlab(InitialByteSlabSize)
	pairType := reflect.TypeFor[gcTracePair]()
	pointer, _ := retireGenericBytesAllocation(t, arena, pairType.Size(), uintptr(pairType.Align()))
	pair := (*gcTracePair)(pointer)
	heapText := strings.Repeat("h", 4)

	cases := map[string]any{
		"arena string elements":       map[int]string{1: texts[0], 2: texts[1], 3: heapText, 4: texts[2], 5: texts[3]},
		"arena string keys":           map[string]int{texts[0]: 1, texts[1]: 2, heapText: 3, texts[4]: 4, texts[5]: 5},
		"strings on both sides":       map[string]string{texts[0]: texts[1], heapText: texts[2], texts[3]: heapText, texts[4]: texts[5], "": ""},
		"pointers into a chunk":       map[int]*gcTracePair{1: pair, 2: nil, 3: {A: 1}, 4: pair, 5: nil},
		"pointer-free sides":          map[gcTracePair]gcTracePair{{A: 1}: {B: 2}, {A: 2}: {}, {A: 3}: {}, {A: 4}: {}, {A: 5}: {}},
		"interface elements":          map[int]any{1: texts[0], 2: pair, 3: gcTracePair{A: 1}, 4: nil, 5: []string{texts[1]}},
		"interface keys":              map[any]markedRecord{texts[0]: {text: texts[1]}, 2: {count: 2}, heapText: {}, pair: {text: texts[2]}, 5.0: {}},
		"slices of pointers":          map[string][]*gcTracePair{texts[0]: {pair, nil}, heapText: nil, texts[1]: {}, texts[2]: {pair}, texts[3]: {nil}},
		"nested maps":                 map[string]map[int]string{texts[0]: {1: texts[1]}, heapText: nil, texts[2]: {}, texts[3]: {2: heapText}, texts[4]: {3: texts[5]}},
		"nested maps of one type":     map[string]any{texts[0]: map[string]any{texts[1]: texts[2]}, heapText: map[string]any{heapText: pair}, texts[3]: texts[4], texts[5]: nil},
		"empty map":                   map[string]string{},
		"pointer-free key and record": map[int64]markedRecord{1: {text: texts[0]}, 2: {}, 3: {text: heapText}, 4: {text: texts[1]}, 5: {}},
	}
	unexported := reflect.ValueOf(mapInUnexportedField{lookup: map[int]string{1: texts[0], 2: heapText, 3: texts[1], 4: texts[2], 5: texts[3]}}).Field(0)
	require.False(t, unexported.CanInterface())

	for name, lookup := range cases {
		requireSameMapMarks(t, arena, name, reflect.ValueOf(lookup))
	}
	requireSameMapMarks(t, arena, "map read through an unexported field", unexported)

	marked := markWith(arena, licenceMarkTraceBudget, func(state *gcMarkState) {
		markMapWalk(arena, state, reflect.ValueOf(cases["arena string elements"]))
	})
	require.True(t, marked.byteSlabs[0], "the comparison covers a map whose entries keep a slab")
	marked = markWith(arena, licenceMarkTraceBudget, func(state *gcMarkState) {
		markMapWalk(arena, state, reflect.ValueOf(cases["pointers into a chunk"]))
	})
	require.True(t, marked.genericSlab[0], "the comparison covers a map whose entries keep a chunk")
}

func requireSameMapMarks(t *testing.T, arena *RegisterArena, name string, value reflect.Value) {
	t.Helper()
	for _, budget := range []int{licenceMarkTraceBudget, value.Len(), value.Len() - 1, 2, 0} {
		want := markWith(arena, budget, func(state *gcMarkState) { markMapEntriesByCopying(arena, state, value) })
		got := markWith(arena, budget, func(state *gcMarkState) { markMapWalk(arena, state, value) })
		require.Equal(t, want, got, "%s with a budget of %d", name, budget)
	}
}

func TestLicenceMarkWalksASlicesCapacityWithoutReslicing(t *testing.T) {
	if raceDetectorOn {
		t.Skip("the race detector changes allocation counts")
	}
	arena := NewRegisterArena()
	arena.attachLicencePolicy(&VMLimits{})
	setArenaBudget(t, arena, 1<<30)
	pairType := reflect.TypeFor[gcTracePair]()
	pointer, index := retireGenericBytesAllocation(t, arena, pairType.Size(), uintptr(pairType.Align()))
	backing := make([]*gcTracePair, 8)
	backing[6] = (*gcTracePair)(pointer)
	value := reflect.ValueOf(backing[:2])
	state := acquireGCMarkState(arena)
	defer releaseGCMarkState(state)

	allocations := testing.AllocsPerRun(20, func() {
		state.traceBudget = licenceMarkTraceBudget
		state.visited.reset(gcVisitedKeptSize)
		markSliceHeaderAt(arena, state, value, 0)
		drainMark(arena, state, 0)
	})
	require.True(t, state.OldGenericBytesLive[index], "a pointer in spare capacity keeps its chunk")
	require.Zero(t, allocations, "the walk builds no slice header")
}

func markSliceCapacityByReslicing(arena *RegisterArena, state *gcMarkState, value reflect.Value) {
	full := value.Slice(0, value.Cap())
	for index := range full.Len() {
		if !state.spendTraceBudget() {
			return
		}
		markReflectValue(arena, state, full.Index(index))
	}
}

func TestLicenceMarkCapacityWalkDecidesAsTheReslicingWalk(t *testing.T) {
	arena := NewRegisterArena()
	arena.attachLicencePolicy(&VMLimits{})
	setArenaBudget(t, arena, 1<<30)
	texts := make([]string, 4)
	for index := range texts {
		texts[index] = arenaString(arena, strings.Repeat("s", index+1))
	}
	arena.GrowByteSlab(InitialByteSlabSize)
	pairType := reflect.TypeFor[gcTracePair]()
	pointer, _ := retireGenericBytesAllocation(t, arena, pairType.Size(), uintptr(pairType.Align()))
	pair := (*gcTracePair)(pointer)
	header, _ := retireSliceHeader(t, arena)

	pointers := make([]*gcTracePair, 6)
	pointers[1], pointers[4] = pair, pair
	anys := []any{texts[0], pair, gcTracePair{A: 1}, nil, map[int]string{1: texts[1]}, texts[2]}
	records := []markedRecord{{text: texts[0]}, {count: 1}, {text: strings.Repeat("h", 3)}, {text: texts[3]}, {}}
	nested := [][]*gcTracePair{nil, pointers[:2], {pair}, pointers}
	headers := []*arenaSliceHeader{nil, header, nil}
	cases := map[string]reflect.Value{
		"pointers past the length":               reflect.ValueOf(pointers[:2]),
		"interfaces":                             reflect.ValueOf(anys[:3]),
		"records past the length":                reflect.ValueOf(records[:1]),
		"slices of slices":                       reflect.ValueOf(nested[:2]),
		"pointers to slice headers":              reflect.ValueOf(headers[:1]),
		"full slice":                             reflect.ValueOf(pointers),
		"slice read through an unexported field": reflect.ValueOf(sliceInUnexportedField{pairs: pointers[:3]}).Field(0),
	}
	require.False(t, cases["slice read through an unexported field"].CanInterface())

	for name, value := range cases {
		for _, budget := range []int{licenceMarkTraceBudget, value.Cap(), value.Cap() - 1, 2, 0} {
			want := markWith(arena, budget, func(state *gcMarkState) { markSliceCapacityByReslicing(arena, state, value) })
			got := markWith(arena, budget, func(state *gcMarkState) { markCapacityWalk(arena, state, value) })
			require.Equal(t, want, got, "%s with a budget of %d", name, budget)
		}
	}

	marked := markWith(arena, licenceMarkTraceBudget, func(state *gcMarkState) {
		markCapacityWalk(arena, state, cases["pointers past the length"])
	})
	require.True(t, marked.genericSlab[0], "the comparison covers a chunk kept only by spare capacity")
	marked = markWith(arena, licenceMarkTraceBudget, func(state *gcMarkState) {
		markCapacityWalk(arena, state, cases["records past the length"])
	})
	require.True(t, marked.byteSlabs[0], "the comparison covers a slab kept only by spare capacity")
}

type markGraphNode struct {
	Next   *markGraphNode
	Other  *markGraphNode
	Leaf   *gcTracePair
	Any    any
	Run    func() int
	Items  []*markGraphNode
	Lookup map[int]*markGraphNode
}

func markChain(length int, leaf *gcTracePair) *markGraphNode {
	head := &markGraphNode{Leaf: leaf}
	for range length - 1 {
		head = &markGraphNode{Next: head}
	}
	return head
}

func TestLicenceMarkTracesAReferenceCycleOnce(t *testing.T) {
	vm := newLicensedMarkVM(t)
	pair, index := retiredPair(t, vm.Arena)
	nodes := make([]*markGraphNode, 1000)
	for i := range nodes {
		nodes[i] = &markGraphNode{}
	}
	for i, node := range nodes {
		node.Next = nodes[(i+1)%len(nodes)]
		node.Other = nodes[(i+len(nodes)-1)%len(nodes)]
	}
	nodes[len(nodes)/2].Leaf = pair

	state := markOf(t, vm, reflect.ValueOf(nodes[0]))
	defer releaseGCMarkState(state)
	require.Zero(t, state.failClosed, "a doubly linked ring of 1,000 nodes is traced without failing closed")
	require.True(t, state.OldGenericBytesLive[index], "a value 500 hops from the root keeps its chunk live")
}

func TestLicenceMarkTracesALinkedListOfAnyLength(t *testing.T) {
	vm := newLicensedMarkVM(t)
	pair, index := retiredPair(t, vm.Arena)

	state := markOf(t, vm, reflect.ValueOf(markChain(1<<16, pair)))
	defer releaseGCMarkState(state)
	require.Zero(t, state.failClosed, "a list of 65,536 nodes is traced without failing closed")
	require.True(t, state.OldGenericBytesLive[index], "the leaf at its end keeps its chunk live")
}

func TestMinorGCCompactsAroundALinkedListOfAnyLength(t *testing.T) {
	vm := newLicensedMarkVM(t)
	arena := vm.Arena
	pair, _ := retiredPair(t, arena)
	retiredPair(t, arena)
	slabs := len(arena.oldGenericByteSlabs)
	chain := markChain(1<<16, pair)
	vm.CallStack = []CallFrame{{Registers: Registers{General: []reflect.Value{reflect.ValueOf(chain)}}}}
	vm.FramePointer = 0

	arena.MinorGC(vm)

	require.Len(t, arena.oldGenericByteSlabs, slabs-1, "the chunk no root reaches is reclaimed")
	require.True(t, retainsGenericSlabHolding(arena, unsafe.Pointer(pair)), "the chunk the list's last node reaches is kept")
}

func TestLicenceMarkFailsClosedPastItsStepBudget(t *testing.T) {
	vm := newLicensedMarkVM(t)
	pair, _ := retiredPair(t, vm.Arena)
	chain := reflect.ValueOf(markChain(64, pair))
	unlimited := acquireGCMarkState(vm.Arena)
	markReflectValue(vm.Arena, unlimited, chain)
	steps := licenceMarkStepBudget - unlimited.stepBudget
	releaseGCMarkState(unlimited)
	for _, testCase := range []struct {
		budget     int
		failClosed licenceFailClosedReason
	}{{budget: steps, failClosed: 0}, {budget: steps - 1, failClosed: failClosedBudget}} {
		state := acquireGCMarkState(vm.Arena)
		state.stepBudget = testCase.budget
		markReflectValue(vm.Arena, state, chain)
		require.Equal(t, testCase.failClosed, state.failClosed, "64 nodes and a leaf with %d steps", testCase.budget)
		require.Empty(t, state.work, "a mark that stops drops every queued step")
		releaseGCMarkState(state)
	}
}

func TestLicenceMarkEndsACycleOfReflectValues(t *testing.T) {
	vm := newLicensedMarkVM(t)
	slot := reflect.New(reflect.TypeFor[reflect.Value]()).Elem()
	slot.Set(reflect.ValueOf(slot))
	state := acquireGCMarkState(vm.Arena)
	defer releaseGCMarkState(state)
	state.stepBudget = 1 << 12

	markReflectValue(vm.Arena, state, slot)

	require.Equal(t, failClosedBudget, state.failClosed, "a reflect.Value that holds itself ends at the step budget")
}

func TestLicenceMarkFailsClosedPastItsVisitBudget(t *testing.T) {
	vm := newLicensedMarkVM(t)
	pair, _ := retiredPair(t, vm.Arena)
	chain := reflect.ValueOf(markChain(64, pair))
	for _, testCase := range []struct {
		budget     int
		failClosed licenceFailClosedReason
	}{{budget: 65, failClosed: 0}, {budget: 64, failClosed: failClosedBudget}} {
		state := acquireGCMarkState(vm.Arena)
		state.visitBudget = testCase.budget
		markReflectValue(vm.Arena, state, chain)
		require.Equal(t, testCase.failClosed, state.failClosed, "64 nodes and a leaf with a budget of %d", testCase.budget)
		releaseGCMarkState(state)
	}
}

func TestLicenceMarkFailsClosedAtOnceOnABackingItsAllowanceCannotCover(t *testing.T) {
	vm := newLicensedMarkVM(t)
	elements := make([]any, 256)
	for index := range elements {
		elements[index] = new(int)
	}
	backing := reflect.ValueOf(elements)
	unlimited := acquireGCMarkState(vm.Arena)
	markReflectValue(vm.Arena, unlimited, backing)
	require.Zero(t, unlimited.failClosed)
	spent := licenceMarkTraceBudget - unlimited.traceBudget
	traced := unlimited.visited.count
	releaseGCMarkState(unlimited)
	require.GreaterOrEqual(t, spent, len(elements), "every element costs a visit of the allowance")
	require.Greater(t, traced, len(elements), "and a full walk visits what each element points at")

	for _, testCase := range []struct {
		name       string
		budget     int
		failClosed licenceFailClosedReason
		visited    int
	}{
		{name: "an allowance that covers the walk", budget: spent, failClosed: 0, visited: traced},
		{name: "one short of the element count", budget: len(elements) - 1, failClosed: failClosedBudget, visited: 1},
		{name: "no allowance left", budget: 0, failClosed: failClosedBudget, visited: 1},
	} {
		state := acquireGCMarkState(vm.Arena)
		state.traceBudget = testCase.budget
		markReflectValue(vm.Arena, state, backing)
		require.Equal(t, testCase.failClosed, state.failClosed, testCase.name)
		require.Equal(t, testCase.visited, state.visited.count, "%s: only the backing itself is visited when the walk cannot finish", testCase.name)
		require.Empty(t, state.work, testCase.name)
		releaseGCMarkState(state)
	}
}

func TestLicenceMarkStateForgetsItsReferentsOnRelease(t *testing.T) {
	vm := newLicensedMarkVM(t)
	pair, _ := retiredPair(t, vm.Arena)
	state := markOf(t, vm, reflect.ValueOf(markChain(8, pair)))
	require.NotZero(t, state.visited.count)
	releaseGCMarkState(state)
	require.Zero(t, state.visited.count, "a pooled mark state keeps no referent reachable")
	for _, slot := range state.visited.slots {
		require.Nil(t, slot.pointer, "and no slot holds one")
	}
	require.Nil(t, state.program)
}

type markOuterPair struct {
	Inner markInnerPair
	Leaf  *gcTracePair
}

type markInnerPair struct {
	Leaf *gcTracePair
}

func TestLicenceMarkTracesOneAddressOnceForEachTypeAndExtent(t *testing.T) {
	vm := newLicensedMarkVM(t)
	innerLeaf, innerIndex := retiredPair(t, vm.Arena)
	outerLeaf, outerIndex := retiredPair(t, vm.Arena)
	outer := &markOuterPair{Inner: markInnerPair{Leaf: innerLeaf}, Leaf: outerLeaf}
	backing := []*gcTracePair{innerLeaf, nil, nil, outerLeaf}
	for name, roots := range map[string][]reflect.Value{
		"a field's address before its struct's": {reflect.ValueOf(&outer.Inner), reflect.ValueOf(outer)},
		"a short capacity before a longer one":  {reflect.ValueOf(backing[:1:1]), reflect.ValueOf(backing[:1:4])},
	} {
		vm.CallStack = []CallFrame{{Registers: Registers{General: roots}}}
		vm.FramePointer = 0
		state := acquireGCMarkState(vm.Arena)
		vm.markPhase(state)
		require.Zero(t, state.failClosed, name)
		require.True(t, state.OldGenericBytesLive[innerIndex], name)
		require.True(t, state.OldGenericBytesLive[outerIndex], "%s: the second view reaches more and is traced", name)
		releaseGCMarkState(state)
	}
}

type markGraph struct {
	nodes    []*markGraphNode
	closures []*RuntimeClosure
	wrappers []reflect.Value
	leaves   []*gcTracePair
	chunks   []int
	edges    map[any][]any
	roots    []any
}

func (g *markGraph) edge(from, to any) {
	g.edges[from] = append(g.edges[from], to)
}

func newMarkGraph(t *testing.T, arena *RegisterArena, random *rand.Rand) *markGraph {
	t.Helper()
	g := &markGraph{edges: map[any][]any{}}
	for range 1 + random.IntN(12) {
		leaf, index := retiredPair(t, arena)
		g.leaves = append(g.leaves, leaf)
		g.chunks = append(g.chunks, index)
	}
	for range 1 + random.IntN(40) {
		g.nodes = append(g.nodes, &markGraphNode{})
	}
	for range random.IntN(6) {
		closure := &RuntimeClosure{}
		g.closures = append(g.closures, closure)
		g.wrappers = append(g.wrappers, registeredWrapper(closure))
	}
	pick := func() any {
		switch choice := random.IntN(4); {
		case choice == 0 && len(g.closures) > 0:
			return g.closures[random.IntN(len(g.closures))]
		case choice == 1:
			return g.leaves[random.IntN(len(g.leaves))]
		default:
			return g.nodes[random.IntN(len(g.nodes))]
		}
	}
	node := func() *markGraphNode { return g.nodes[random.IntN(len(g.nodes))] }
	for _, n := range g.nodes {
		if random.IntN(2) == 0 {
			n.Next = node()
			g.edge(n, n.Next)
		}
		if random.IntN(3) == 0 {
			n.Other = node()
			g.edge(n, n.Other)
		}
		if random.IntN(3) == 0 {
			n.Leaf = g.leaves[random.IntN(len(g.leaves))]
			g.edge(n, n.Leaf)
		}
		if random.IntN(3) == 0 {
			target := pick()
			n.Any = target
			g.edge(n, target)
		}
		if random.IntN(4) == 0 && len(g.closures) > 0 {
			which := random.IntN(len(g.closures))
			n.Run = requireAs[func() int](t, g.wrappers[which].Interface())
			g.edge(n, g.closures[which])
		}
		if random.IntN(4) == 0 {
			items := make([]*markGraphNode, 1+random.IntN(4), 6)
			for i := range cap(items) {
				if random.IntN(2) == 0 {
					items[:cap(items)][i] = node()
					g.edge(n, items[:cap(items)][i])
				}
			}
			n.Items = items
		}
		if random.IntN(5) == 0 {
			n.Lookup = map[int]*markGraphNode{}
			for key := range 1 + random.IntN(3) {
				n.Lookup[key] = node()
				g.edge(n, n.Lookup[key])
			}
		}
	}
	for _, closure := range g.closures {
		for range random.IntN(4) {
			target := pick()
			closure.upvalues = append(closure.upvalues, generalCell(target))
			g.edge(closure, target)
		}
	}
	for range 1 + random.IntN(4) {
		g.roots = append(g.roots, pick())
	}
	return g
}

func (g *markGraph) reachedChunks() map[int]bool {
	seen := map[any]bool{}
	pending := append([]any(nil), g.roots...)
	for len(pending) > 0 {
		next := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[next] {
			continue
		}
		seen[next] = true
		pending = append(pending, g.edges[next]...)
	}
	reached := map[int]bool{}
	for i, leaf := range g.leaves {
		if seen[leaf] {
			reached[g.chunks[i]] = true
		}
	}
	return reached
}

func TestLicenceMarkMarksExactlyTheChunksItsRootsReach(t *testing.T) {
	for seed := range uint64(300) {
		random := rand.New(rand.NewPCG(seed, 7))
		vm := newLicensedMarkVM(t)
		g := newMarkGraph(t, vm.Arena, random)
		general := make([]reflect.Value, len(g.roots))
		for i, root := range g.roots {
			general[i] = reflect.ValueOf(root)
		}
		vm.CallStack = []CallFrame{{Registers: Registers{General: general}}}
		vm.FramePointer = 0
		state := acquireGCMarkState(vm.Arena)
		vm.markPhase(state)
		require.Zero(t, state.failClosed, "seed %d", seed)
		reached := g.reachedChunks()
		for index, live := range state.OldGenericBytesLive {
			require.Equal(t, reached[index], live, "seed %d: chunk %d", seed, index)
		}
		releaseGCMarkState(state)
	}
}

func TestLicenceMarkAllocatesNothingPerCycleOnceWarm(t *testing.T) {
	if raceDetectorOn {
		t.Skip("the race detector changes allocation counts")
	}
	for _, length := range []int{1 << 10, 20000} {
		vm := newLicensedMarkVM(t)
		arena := vm.Arena
		pair, index := retiredPair(t, arena)
		lookup := map[int]*markGraphNode{1: markChain(length, nil), 2: markChain(8, pair)}
		items := []*markGraphNode{markChain(length, nil), nil, markChain(8, nil)}
		vm.CallStack = []CallFrame{{Registers: Registers{General: []reflect.Value{
			reflect.ValueOf(markChain(length, pair)), reflect.ValueOf(lookup), reflect.ValueOf(items[:1]),
		}}}}
		vm.FramePointer = 0
		state := acquireGCMarkState(arena)
		cycle := func() {
			state.prepare(arena)
			vm.markPhase(state)
		}

		requireNoAllocations(t, cycle)

		require.Zero(t, state.failClosed)
		require.True(t, state.OldGenericBytesLive[index], "a mark over lists of %d nodes", length)
		releaseGCMarkState(state)
	}
}

type markArrayHolder struct {
	Pairs [6]*gcTracePair
	Texts [3]string
}

func TestLicenceMarkWalksEveryArrayElement(t *testing.T) {
	vm := newLicensedMarkVM(t)
	arena := vm.Arena
	first, firstIndex := retiredPair(t, arena)
	last, lastIndex := retiredPair(t, arena)
	held := arenaString(arena, "held only by an array's last element")
	arena.GrowByteSlab(InitialByteSlabSize)
	holder := &markArrayHolder{}
	holder.Pairs[0], holder.Pairs[5] = first, last
	holder.Texts[2] = held

	state := markOf(t, vm, reflect.ValueOf(holder))
	defer releaseGCMarkState(state)

	require.Zero(t, state.failClosed)
	require.True(t, state.OldGenericBytesLive[firstIndex], "the first element keeps its chunk")
	require.True(t, state.OldGenericBytesLive[lastIndex], "the last element keeps its chunk")
	require.True(t, state.OldByteSlabLive[0], "a string in an array's last element keeps its slab")
}
