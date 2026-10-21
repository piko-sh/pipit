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
	"reflect"
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
)

type seededLiteralNode struct {
	next  *seededLiteralNode
	value int
}

func TestWalkMemoIsSizedFromTheLastRun(t *testing.T) {
	arena := newArena(t)
	t.Cleanup(arena.Reset)
	sizing := program.TypedSlabCounts{Ints: 8, Generals: 8}
	state := &arena.licenceState
	state.regrow.sized = sizing
	state.memo.entries = make([]licenceMemoEntry, 4*licenceMemoKeepEntries)
	dropped := state.memo.release()
	require.Equal(t, 4*licenceMemoKeepEntries, dropped)
	state.memoHint, state.memoHintFor = dropped, state.regrow.sized

	arena.ensureCapacity(program.TypedSlabCounts{Ints: 9, Generals: 8})
	require.Zero(t, state.memo.hint, "another program's sizing takes no hint")
	require.Zero(t, state.memoHint, "the hint is one-shot")

	state.memoHint, state.memoHintFor = dropped, sizing
	arena.ensureCapacity(sizing)
	require.Equal(t, dropped, state.memo.hint)
	state.memo.grow()
	require.Len(t, state.memo.entries, dropped, "the first table is the size the last run needed")
	require.Zero(t, state.memo.hint)
}

func TestFreshBoundarySlotIsSeededInTheWalkMemo(t *testing.T) {
	nodeType := reflect.TypeFor[seededLiteralNode]()
	seeded := func(vm *VM, slot reflect.Value) bool {
		return vm.Arena.licenceState.memo.peek(ReflectValuePtr(slot), reflectValueABIType(nodeType), 0)
	}
	vmWithLicence := func() *VM {
		vm := newGCTestVM()
		vm.Arena.attachLicencePolicy(&VMLimits{})
		return vm
	}

	boundaryLiteral := func(vm *VM) reflect.Value {
		slot := vm.acquireBoundaryChunkSlot(nodeType)
		vm.seedFreshObject(slot)
		return slot
	}

	vm, slot := seedableStructLiteral(t, vmWithLicence, boundaryLiteral)
	require.True(t, slot.IsZero(), "the slot is zeroed")
	require.True(t, vm.ownsBoundarySnapshot(ReflectValuePtr(slot)), "test harness: a Go-heap boundary slot")
	require.True(t, seeded(vm, slot), "the slot is seeded")
	require.True(t, vm.Arena.revokeTrivially(slot.Addr(), false), "its first hand-off is a memo hit")

	vm = vmWithLicence()
	vm.Arena.revokeEpoch(epochSharedContainer, noHandOffCause)
	require.False(t, seeded(vm, boundaryLiteral(vm)), "a revoked epoch")

	vm = newGCTestVM()
	require.False(t, vm.Arena.licensed)
	require.False(t, seeded(vm, boundaryLiteral(vm)), "an arena without a licence")

	vm = vmWithLicence()
	slot = allocateStructLiteralValue(vm, nodeType)
	if vm.Arena.krefContains(ReflectValuePtr(slot)) {
		require.False(t, seeded(vm, slot), "a KRef slot is arena storage and is never seeded")
	}
}

func seedableStructLiteral(t *testing.T, newVM func() *VM, literal func(*VM) reflect.Value) (*VM, reflect.Value) {
	t.Helper()
	for attempt := range 64 {
		if attempt > 0 && attempt%16 == 0 {
			runtime.GC()
		}
		vm := newVM()
		slot := literal(vm)
		if licenceOutsideEveryArena(vm.Arena, uintptr(ReflectValuePtr(slot))) {
			return vm, slot
		}
	}
	require.FailNow(t, "64 boundary slots in a row lay in recorded slab ranges")
	return nil, reflect.Value{}
}

func fillMemo(memo *licenceWalkMemo, keep *[][]byte, count int) {
	for range count {
		container := make([]byte, 8)
		*keep = append(*keep, container)
		memo.note(unsafe.Pointer(&container[0]), nil, 0)
	}
}

func TestLicenceWalkMemoReleasePoolsALargeTable(t *testing.T) {
	var memo licenceWalkMemo
	var keep [][]byte
	fillMemo(&memo, &keep, 3000)
	size := len(memo.entries)
	require.Greater(t, size, licenceMemoKeepEntries, "test harness: the table must outgrow licenceMemoKeepEntries")
	memo.release()
	require.Nil(t, memo.entries, "the released arena keeps no large table")
	require.Zero(t, memo.count)
	table := takeMemoTable(size)
	require.Len(t, table, size)
	for _, entry := range table {
		require.Nil(t, entry.pointer, "a pooled table is empty and pins no container")
	}
	donateMemoTable(table)
}

func TestLicenceWalkMemoKeepsASmallTable(t *testing.T) {
	var memo licenceWalkMemo
	var keep [][]byte
	fillMemo(&memo, &keep, 100)
	size := len(memo.entries)
	require.LessOrEqual(t, size, licenceMemoKeepEntries)
	memo.release()
	require.Len(t, memo.entries, size, "a table within licenceMemoKeepEntries stays with its arena, entries and all")
	require.Equal(t, 100, memo.count)
}

func TestLicenceWalkMemoGrowthReusesPooledTables(t *testing.T) {
	containers := make([]uint64, 3000)
	run := func() {
		var memo licenceWalkMemo
		for index := range containers {
			memo.note(unsafe.Pointer(&containers[index]), nil, 0)
		}
		memo.release()
	}
	run()
	large := uint64(2*licenceMemoKeepEntries) * uint64(unsafe.Sizeof(licenceMemoEntry{}))
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	const runs = 8
	for range runs {
		run()
	}
	runtime.ReadMemStats(&after)
	perRun := (after.TotalAlloc - before.TotalAlloc) / runs
	t.Logf("memo bytes allocated per run: %d (an 8192-entry table is %d)", perRun, large)

	require.Less(t, perRun, large, "the large table is taken from the pool, not allocated each run")
}
