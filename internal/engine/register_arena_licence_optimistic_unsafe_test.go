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
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type optimisticRuleProbe struct {
	name  string
	fires func(t *testing.T, a *RegisterArena) bool
}

var optimisticRuleProbes = []optimisticRuleProbe{
	{name: "heap-flagged string make placed in the arena", fires: func(_ *testing.T, a *RegisterArena) bool {
		return optimisticMake(a, &program.CompiledFunction{}, reflect.String, unsafe.Sizeof(""), 1024)
	}},
	{name: "pointer-free pointee placed in the byte slab", fires: func(_ *testing.T, a *RegisterArena) bool {
		return optimisticPointee(a, nil, reflect.TypeFor[struct {
			A int

			B int
		}]())
	}},
	{name: "no defensive copy of a non-addressable composite", fires: func(_ *testing.T, arena *RegisterArena) bool {
		value := reflect.ValueOf(struct {
			P *int
			N int
		}{new(7), 1})
		return ReflectValuePtr(materialiseArenaCompositeFields(arena, value, false)) == ReflectValuePtr(value)
	}},
	{name: "no heap header for a slice written into a slice slot", fires: func(t *testing.T, arena *RegisterArena) bool {

		header := arena.allocSliceHeader()
		backing := arena.AllocIntBacking(4)
		*header = arenaSliceHeader{Data: unsafe.Pointer(unsafe.SliceData(backing)), Len: 4, Cap: 4}
		value := reflect.NewAt(reflect.TypeFor[[]int](), unsafe.Pointer(header)).Elem()
		require.True(t, arena.OwnsSliceHeaderPointer(ReflectValuePtr(value)))
		return arena.OwnsSliceHeaderPointer(ReflectValuePtr(materialiseArenaValueIntoSlot(arena, value, value.Type())))
	}},
	{name: "no barrier for a store into own uncovered storage", fires: func(_ *testing.T, a *RegisterArena) bool {
		return optimisticOwnStore(a, a.AllocBytes(16, 8))
	}},
	{name: "handed-off string kept by the clone budget", fires: func(_ *testing.T, arena *RegisterArena) bool {

		text := arenaString(arena, "kept once the epoch's clone budget is spent")
		for range optimisticCloneBudget(arena)/(len(text)+optimisticCloneOverhead) + 1 {
			if optimisticKeepsString(arena, text) {
				return true
			}
		}
		return false
	}},
	{name: "no end-of-run global sweep", fires: func(_ *testing.T, arena *RegisterArena) bool {
		previous := SetLicenceCountingForTest(true)
		defer SetLicenceCountingForTest(previous)
		before := optimisticStats()[OptimisticGlobalSweepSkipped]
		materialiseGlobalStrings(NewGlobalStore(), arena)
		return optimisticStats()[OptimisticGlobalSweepSkipped] > before
	}},
}

type typedStorePoint struct{ A, B, C int }

type typedStoreNamed struct {
	Name string
	N    int
}

func TestNoteKeptStringRevokesTheStringsOwnSlab(t *testing.T) {
	type outcome struct {
		Revoked    map[uintptr]HandOffCause
		Epoch      bool
		BlindMoved bool
		ByteMoved  bool
	}
	arena := newArena(t)
	kept := arenaString(arena, "kept in place by the clone budget")
	run := func(shared bool, note func()) outcome {
		arena.clearLicence()
		arena.licenceState.memo = licenceWalkMemo{}
		arena.licenceAlwaysShared = shared
		arena.licenceState.blindMarked = true
		arena.licenceState.blindMark, arena.licenceState.blindByteMark = arena.readBlindTotals()
		all, bytes := arena.blindTotals()
		note()
		afterAll, afterBytes := arena.blindTotals()
		return outcome{Revoked: maps.Clone(arena.licenceState.revoked), Epoch: arena.licenceState.epochRevoked,
			BlindMoved: afterAll != all, ByteMoved: afterBytes != bytes}
	}
	var scratch string
	production := func() {
		scratch = kept
		arena.recordHandOff(handOffIdentityKept, unsafeNewAt(stringTypeABIType, unsafe.Pointer(&scratch), reflect.String), false)
		scratch = ""
	}
	note := func() { noteKeptString(arena, kept) }
	base, found := licenceDataSlabOf(arena, uintptr(unsafe.Pointer(unsafe.StringData(kept))))
	require.True(t, found)

	privateProduction := run(false, production)
	require.Contains(t, privateProduction.Revoked, base, "unshared: the walk revokes the kept string's slab")
	shared, private := run(true, note), run(false, note)
	require.Equal(t, privateProduction, private, "unshared: the same outcome as the walk")
	require.Equal(t, private, shared, "shared: the string's own slab revoked, as unshared (census build %t)", storeCensusBuilt)
	require.False(t, shared.ByteMoved || shared.BlindMoved, "shared: no blind count")
}

func TestKeptStringInASharedVMRevokesItsSlab(t *testing.T) {
	arena := newArena(t)
	t.Cleanup(arena.Reset)
	kept := arenaString(arena, "kept in place by the clone budget")
	base, found := licenceDataSlabOf(arena, uintptr(unsafe.Pointer(unsafe.StringData(kept))))
	require.True(t, found)
	arena.licenceAlwaysShared = true
	noteKeptString(arena, kept)
	require.Contains(t, arena.licenceState.revoked, base, "the kept string's slab is revoked")
}

func TestOptimisticRulesFireWithALicence(t *testing.T) {
	for _, probe := range optimisticRuleProbes {
		t.Run(probe.name, func(t *testing.T) {
			a := newArena(t)
			defer a.Reset()
			require.True(t, probe.fires(t, a))
		})
	}
}

func TestOptimisticPointeeIsDeclinedForAProgramThatSetsFinalisers(t *testing.T) {
	a := newArena(t)
	defer a.Reset()
	pointee := reflect.TypeFor[int32]()
	plain := &program.CompiledFunction{GeneralConstants: []reflect.Value{reflect.ValueOf(runtime.GC)}}
	require.True(t, optimisticPointee(a, plain, pointee), "a program without finalisers places its pointees")
	sets, known := plain.FinaliserUse(0)
	require.True(t, known, "the answer is recorded on the root")
	require.False(t, sets)

	callee := &program.CompiledFunction{GeneralConstants: []reflect.Value{reflect.ValueOf(runtime.SetFinalizer)}}
	finalising := &program.CompiledFunction{Functions: []*program.CompiledFunction{callee}}
	require.False(t, optimisticPointee(a, finalising, pointee), "SetFinalizer drops registrations on arena storage, so its pointees take the heap")
	sets, known = finalising.FinaliserUse(1)
	require.True(t, known)
	require.True(t, sets)

	callee.GeneralConstants = nil
	require.False(t, optimisticPointee(a, finalising, pointee), "the recorded answer stands")

	plain.Functions = append(plain.Functions, callee, &program.CompiledFunction{GeneralConstants: []reflect.Value{reflect.ValueOf(runtime.SetFinalizer)}})
	require.False(t, optimisticPointee(a, plain, pointee), "a function a session added to the table is scanned")
}

func TestOptimisticRulesNeedALicence(t *testing.T) {
	a := NewRegisterArena()
	require.False(t, a.licensed)
	require.False(t, optimisticArena(a), "the rules apply only to a licensed arena")
	require.False(t, optimisticArena(nil))
}

func TestPointerFreeValueSkipsTheIntermediateCopy(t *testing.T) {
	arena := newArena(t)
	cell := (*typedStorePoint)(arena.AllocBytes(unsafe.Sizeof(typedStorePoint{}), 8))
	*cell = typedStorePoint{1, 2, 3}
	value := reflect.NewAt(reflect.TypeFor[typedStorePoint](), unsafe.Pointer(cell)).Elem()
	require.True(t, arena.ownsBytePointer(ReflectValuePtr(value)))

	require.True(t, storesPointerFreeByValue(value, value.Type()))
	got := materialiseArenaValueIntoSlot(arena, value, value.Type())
	require.Equal(t, ReflectValuePtr(value), ReflectValuePtr(got), "no intermediate copy")
	var slot typedStorePoint
	reflect.ValueOf(&slot).Elem().Set(got)
	*cell = typedStorePoint{9, 9, 9}
	require.Equal(t, typedStorePoint{1, 2, 3}, slot, "the slot's copy detaches the value")

	require.False(t, storesPointerFreeByValue(value, reflect.TypeFor[any]()), "an interface slot keeps the value by reference")
	require.False(t, storesPointerFreeByValue(reflect.ValueOf(typedStoreNamed{}), reflect.TypeFor[typedStoreNamed]()), "pointer-bearing")
	arena.Reset()
}

func TestOwnCellStoreByValueNeedsOwnUncoveredStorage(t *testing.T) {
	arena := newArena(t)
	cell := arena.AllocBytes(unsafe.Sizeof(typedStoreNamed{}), 8)
	value := reflect.ValueOf(typedStoreNamed{Name: arenaString(arena, "own"), N: 1})
	require.True(t, ownCellStoreByValue(arena, cell, value), "own uncovered storage")

	var heap typedStoreNamed
	require.False(t, ownCellStoreByValue(arena, unsafe.Pointer(&heap), value), "heap pointee")

	noteHandOffString(arena, handOffUntracedContainer, arenaString(arena, "revokes the byte slab"))
	revokedCell := arena.AllocStringBytes(16)
	require.True(t, arena.licenceCoveredAt(unsafe.Pointer(&revokedCell[0])))
	require.False(t, ownCellStoreByValue(arena, unsafe.Pointer(&revokedCell[0]), value), "covered storage stays a hand-off")
	arena.Reset()

	plain := NewRegisterArena()
	plainCell := plain.AllocBytes(unsafe.Sizeof(typedStoreNamed{}), 8)
	require.False(t, ownCellStoreByValue(plain, plainCell, value), "an arena without a licence")
	require.False(t, ownCellStoreByValue(nil, plainCell, value))
}

func TestOwnCellStoreByValueRefusesForeignValuesInSharedArenas(t *testing.T) {
	arena := newArena(t)
	arena.cloneUnownedStrings = true
	cell := arena.AllocBytes(unsafe.Sizeof(typedStoreNamed{}), 8)
	require.False(t, ownCellStoreByValue(arena, cell, reflect.ValueOf(typedStoreNamed{Name: "x"})),
		"a pointer-bearing composite in an arena that clones foreign strings takes the barrier")
	require.True(t, ownCellStoreByValue(arena, cell, reflect.ValueOf(typedStorePoint{})), "pointer-free values carry no foreign reference")
	arena.Reset()
}

func TestStoresPointerFreeByValueKindPrecheckKeepsTheAnswer(t *testing.T) {
	type flat struct{ a, b int }
	type withPointer struct{ p *int }
	full := func(v reflect.Value, slot reflect.Type) bool {
		return v.IsValid() && slot != nil && slot.Kind() != reflect.Interface && v.Type() == slot && typeIsPointerFree(slot)
	}
	values := []any{
		1, uint8(2), uintptr(3), 1.5, complex(1, 2), true, "s", []int{1}, map[int]int{}, new(int),
		flat{}, withPointer{}, [2]int{}, [0]*int{}, [1]*int{}, func() {}, make(chan int), unsafe.Pointer(nil),
	}
	for _, v := range values {
		value := reflect.ValueOf(v)
		for _, slot := range []reflect.Type{value.Type(), reflect.TypeFor[any](), reflect.TypeFor[int](), nil} {
			require.Equal(t, full(value, slot), storesPointerFreeByValue(value, slot), "%T into %v", v, slot)
		}
	}
	require.False(t, storesPointerFreeByValue(reflect.Value{}, reflect.TypeFor[int]()))
}

func TestOwnSlotTestsAnswerFalseInARevokedEpoch(t *testing.T) {
	arena := revokedEpochGoroutineArena(t)
	defer arena.Reset()
	backing := arena.AllocIntBacking(1)
	slot := unsafe.Pointer(&backing[0])
	require.True(t, arena.OwnsSliceBacking(slot), "test harness: an own data slot")
	require.False(t, ownUncoveredStorage(arena, slot), "own storage is covered")
	require.False(t, optimisticOwnSlotStore(arena, slot, reflect.ValueOf(int64(1))))
	require.False(t, krefMemberSlotStore(arena, slot, reflect.ValueOf(int64(1))))
	require.False(t, arena.licenceCoverEmpty())
	require.True(t, arena.licenceRevokedBase(uintptr(slot)))
}

func TestStoreArenaTypedSliceTakesEveryTypedBank(t *testing.T) {
	arena := newArena(t)
	defer arena.Reset()
	vm := newTestVM(t)
	vm.Arena = arena
	registers := &Registers{
		SlicesInt:    make([][]int64, 1),
		slicesFloat:  make([][]float64, 1),
		slicesString: make([][]string, 1),
		slicesBool:   make([][]bool, 1),
		slicesUint:   make([][]uint64, 1),
		slicesByte:   make([][]byte, 1),
	}
	for _, kind := range []isa.RegisterKind{isa.RegisterSliceInt64, isa.RegisterSliceFloat64, isa.RegisterSliceString,
		isa.RegisterSliceBool, isa.RegisterSliceUint64, isa.RegisterSliceByte} {
		require.True(t, storeArenaTypedSlice(vm, registers, kind, 0, 2, 4), "kind %d", kind)
	}
	require.Len(t, registers.SlicesInt[0], 2)
	require.Equal(t, 4, cap(registers.slicesByte[0]))
	require.True(t, arena.ownsAnyStorage(unsafe.Pointer(unsafe.SliceData(registers.SlicesInt[0]))), "the backing is arena storage")
	require.False(t, storeArenaTypedSlice(vm, registers, isa.RegisterGeneral, 0, 1, 1), "not a typed bank")
}

func TestMaterialiseIntoSlotKeepsWhatTheSlotCopies(t *testing.T) {
	arena := newArena(t)
	defer arena.Reset()
	heapSlice := reflect.ValueOf([]int{1, 2})
	sliceType := reflect.TypeFor[[]int]()
	require.Equal(t, heapSlice.Pointer(), materialiseArenaValueUnconditionalIntoSlot(arena, heapSlice, sliceType).Pointer(),
		"a heap slice passes through")

	ownBacking := arena.AllocIntBacking(2)
	own := reflect.ValueOf(ownBacking)
	require.Equal(t, own.Pointer(), materialiseArenaValueUnconditionalIntoSlot(arena, own, reflect.TypeFor[[]int64]()).Pointer(),
		"an own slice keeps its identity in a slice slot")

	text := arenaString(arena, "kept in place")
	arena.optimisticClonedBytes = optimisticCloneBudget(arena)
	kept := materialiseArenaValueUnconditionalIntoSlot(arena, reflect.ValueOf(text), reflect.TypeFor[string]())
	require.Same(t, unsafe.StringData(text), unsafe.StringData(kept.String()), "past the clone budget the string stays where it is")

	scalar := reflect.ValueOf(7)
	require.Equal(t, scalar, materialiseArenaValueUnconditionalIntoSlot(arena, scalar, reflect.TypeFor[int]()))
	withoutLicence := NewRegisterArena()
	require.Equal(t, "heap", materialiseArenaValueUnconditionalIntoSlot(withoutLicence, reflect.ValueOf("heap"), reflect.TypeFor[any]()).Interface())
}

func TestOptimisticStringsAreClonedWithinTheBudget(t *testing.T) {
	arena := newArena(t)
	defer arena.Reset()
	require.Equal(t, "heap", materialiseStringOptimistic(arena, "heap"))

	within := arenaString(arena, "within the budget")
	requireClone(t, within, materialiseStringOptimistic(arena, within), "a string within the budget is cloned")
	_, kept := optimisticStringValue(arena, reflect.ValueOf(arenaString(arena, "also within")))
	require.False(t, kept)
	cloned, ok := optimisticFieldString(arena, reflect.ValueOf(arenaString(arena, "a field")))
	require.True(t, ok, "a field's string is cloned for the copy")
	require.Equal(t, "a field", cloned)
	_, ok = optimisticFieldString(arena, reflect.ValueOf("heap field"))
	require.False(t, ok, "a heap field is copied as it is")

	arena.optimisticClonedBytes = optimisticCloneBudget(arena)
	past := arenaString(arena, "past the budget")
	require.Same(t, unsafe.StringData(past), unsafe.StringData(materialiseStringOptimistic(arena, past)), "past the budget the string is kept")
	value, kept := optimisticStringValue(arena, reflect.ValueOf(past))
	require.True(t, kept)
	require.Equal(t, past, value.String())
	_, ok = optimisticFieldString(arena, reflect.ValueOf(past))
	require.False(t, ok, "a kept field is left as it is")
}

func TestReboxMovesAStringBoxOffTheArena(t *testing.T) {
	type label string
	arena := newArena(t)
	defer arena.Reset()
	text := arenaString(arena, "boxed")
	box := reflect.ValueOf(arena.allocStringBox(text)).Elem().Convert(reflect.TypeFor[label]())
	boxed := reflect.ValueOf(arena.allocStringBox(text)).Elem()
	reboxed := rebox(arena, boxed, text)
	require.False(t, arena.OwnsStringBox(ReflectValuePtr(reboxed)), "the box itself moves to the heap")
	require.Equal(t, text, reboxed.String())
	require.Equal(t, box, rebox(arena, box, text), "a value that is not an arena box stays")
}

func TestResetOptimisticStatsZeroesTheCounts(t *testing.T) {
	countOptimistic(OptimisticKeptString)
	ResetOptimisticStats()
	require.Zero(t, optimisticStats()[OptimisticKeptString])
}

func TestSetReboundStringWritesAnUnexportedField(t *testing.T) {
	holder := reflect.ValueOf(&struct{ name string }{}).Elem()
	setReboundString(holder.Field(0), "written")
	require.Equal(t, "written", holder.Field(0).String())
	plain := reflect.ValueOf(new(string)).Elem()
	setReboundString(plain, "plain")
	require.Equal(t, "plain", plain.String())
}

func TestOptimisticOwnValueStoreNeedsOwnUncoveredStorage(t *testing.T) {
	arena := newArena(t)
	defer arena.Reset()
	own := reflect.NewAt(reflect.TypeFor[string](), arena.AllocBytes(16, 8)).Elem()
	heap := reflect.ValueOf(new(string)).Elem()
	require.True(t, optimisticOwnValueStore(arena, own, reflect.ValueOf(arenaString(arena, "own"))), "an own string into own storage")
	require.False(t, optimisticOwnValueStore(arena, heap, reflect.ValueOf("heap")), "a heap destination")
	require.False(t, optimisticOwnValueStore(arena, reflect.ValueOf("not addressable"), reflect.ValueOf("x")))
	require.Nil(t, addressOf(reflect.Value{}))

	withoutLicence := NewRegisterArena()
	require.False(t, optimisticOwnValueStore(withoutLicence, heap, reflect.ValueOf("x")), "outside the optimistic rules")
	require.False(t, optimisticOwnValueStore(nil, heap, reflect.ValueOf("x")))
}

func TestNoteKeptStringRevokesTheEpochOnceItsBudgetIsSpent(t *testing.T) {
	arena := newArena(t)
	defer arena.Reset()
	arena.licenceState.walkBudget = 0
	noteKeptString(arena, arenaString(arena, "kept"))
	require.True(t, arena.licenceState.epochRevoked, "a spent walk budget revokes the epoch")

	withoutLicence := NewRegisterArena()
	noteKeptString(withoutLicence, arenaString(withoutLicence, "kept"))
	require.False(t, withoutLicence.licenceState.epochRevoked, "an arena without a licence notes nothing")
}
