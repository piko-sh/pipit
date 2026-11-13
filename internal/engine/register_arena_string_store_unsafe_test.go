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
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestMaterialiseStringForBackingStoreWithoutLicenceDecidesAsTheFullRule(t *testing.T) {
	parent := GetRegisterArena()
	t.Cleanup(func() { PutRegisterArena(parent) })
	abandoning := NewRegisterArena()
	abandoned := arenaString(abandoning, "left-to-an-escaped-value")
	abandoning.abandonEscapedSlabsLazily()

	for _, cloneUnowned := range []bool{false, true} {
		arena := GetRegisterArena()
		arena.cloneUnownedStrings = cloneUnowned
		require.False(t, arena.licensed)
		require.Empty(t, arena.kref.sorted)
		backings := map[string]unsafe.Pointer{
			"own backing":  unsafe.Pointer(unsafe.SliceData(arena.AllocStringBacking(1))),
			"heap backing": unsafe.Pointer(unsafe.SliceData(make([]string, 1))),
		}
		values := map[string]string{
			"host":           strings.Clone("host-string"),
			"constant":       "constant-string",
			"own arena":      arenaString(arena, "own-string"),
			"another arena":  arenaString(parent, "parent-string"),
			"abandoned slab": abandoned,
			"empty":          "",
		}
		for backingName, backing := range backings {
			for valueName, value := range values {
				for _, inPlace := range []bool{false, true} {
					short := materialiseStringForBackingStoreWithoutLicence(arena, backing, value)
					full := materialiseStringForBackingStoreFull(arena, backing, value, inPlace)
					require.Equal(t, value, short)
					require.Equal(t, unsafe.StringData(full) == unsafe.StringData(value), unsafe.StringData(short) == unsafe.StringData(value),
						"cloneUnowned=%v %s, %s string, inPlace=%v: keeps as the full rule does", cloneUnowned, backingName, valueName, inPlace)
				}
			}
		}
		PutRegisterArena(arena)
	}
}

func TestStoresStringAsItIsAgreesWithTheLicensedRules(t *testing.T) {
	parent := newArena(t)
	t.Cleanup(parent.Reset)
	abandoning := NewRegisterArena()
	abandoned := arenaString(abandoning, "left-to-an-escaped-value")
	abandoning.abandonEscapedSlabsLazily()

	taken := map[string]bool{}
	for _, cloneUnowned := range []bool{false, true} {
		for _, revoked := range []bool{false, true} {
			arena := newArena(t)
			arena.cloneUnownedStrings = cloneUnowned
			ownBacking := arena.AllocStringBacking(1)
			if revoked {
				noteHandOff(arena, handOffIdentityKept, reflect.ValueOf(&ownBacking[0]))
			}
			backings := map[string]unsafe.Pointer{
				"own backing":  unsafe.Pointer(unsafe.SliceData(ownBacking)),
				"heap backing": unsafe.Pointer(unsafe.SliceData(make([]string, 1))),
			}
			values := map[string]string{
				"host":           strings.Clone("host-string"),
				"constant":       "constant-string",
				"own arena":      arenaString(arena, "own-string"),
				"another arena":  arenaString(parent, "parent-string"),
				"abandoned slab": abandoned,
				"empty":          "",
			}
			for valueName, value := range values {
				asItIs := storesStringAsItIs(arena, value)
				taken[valueName] = taken[valueName] || asItIs
				if !asItIs {
					continue
				}
				for backingName, backing := range backings {
					for _, inPlace := range []bool{false, true} {
						stored := materialiseStringForBackingStoreByRules(arena, backing, value, inPlace)
						require.Same(t, unsafe.StringData(value), unsafe.StringData(stored),
							"cloneUnowned=%v revoked=%v %s, %s string, inPlace=%v: every rule stores it as it is",
							cloneUnowned, revoked, backingName, valueName, inPlace)
					}
				}
			}
			arena.Reset()
		}
	}
	require.Equal(t, map[string]bool{
		"host": true, "constant": true, "own arena": false, "another arena": true, "abandoned slab": true, "empty": true,
	}, taken, "the early exit takes strings in no arena slab, and another arena's only when unowned strings are not cloned")
}

func TestStoresStringAsItIsStandsAsideForArenaStrings(t *testing.T) {
	parent := newArena(t)
	t.Cleanup(parent.Reset)
	arena := newArena(t)
	t.Cleanup(arena.Reset)
	arena.cloneUnownedStrings = true
	require.False(t, storesStringAsItIs(arena, arenaString(arena, "own")), "the arena's own string")
	require.False(t, storesStringAsItIs(arena, arenaString(parent, "foreign")), "another arena's string when unowned strings are cloned")
	require.True(t, storesStringAsItIs(arena, strings.Clone("host")), "a host string")
	require.True(t, storesStringAsItIs(arena, ""), "the empty string")
}

func TestReuseHazardStringStoredWithoutABarrierSurvivesReuse(t *testing.T) {
	observations := &hazardObservations{}
	const hostText, foreignText = "a host string stored as it is", "another arena's string stored into the host"
	parent := newArena(t)
	arena := newArena(t)
	arena.cloneUnownedStrings = true
	names := make([]string, 2)
	backing := unsafe.Pointer(unsafe.SliceData(names))
	host := strings.Clone(hostText)
	names[0] = materialiseStringForSliceStore(arena, backing, host)
	names[1] = materialiseStringForSliceStore(arena, backing, arenaString(parent, foreignText))
	observations.note("host string kept as it is=%v; foreign string cloned=%v",
		unsafe.StringData(names[0]) == unsafe.StringData(host), !parent.OwnsString(names[1]))
	keptHost, keptForeign := names[0], names[1]
	for range 3 {
		arena.Reset()
		relicence(arena)
		overwriteStrings(arena, len(hostText)+len(foreignText))
		parent.Reset()
		relicence(parent)
		overwriteStrings(parent, len(foreignText))
	}
	observations.compareString("host string stored without a barrier", hostText, keptHost)
	observations.compareString("foreign string stored through the barrier", foreignText, keptForeign)
	runtime.KeepAlive(names)
	arena.Reset()
	parent.Reset()
	assertNoHazard(t, observations, "")
}

func TestReuseHazardStoreIntoPendingStorageIsNotAHandOff(t *testing.T) {
	observations := &hazardObservations{}
	const text = "stored into a backing a callback left pending"
	caller := newArena(t)
	callback := newArena(t)
	names := caller.AllocStringBacking(2)
	backing := slabBase(caller.stringBackingSlab)
	t.Cleanup(func() { dropForeignPending(backing) })
	noteHandOff(callback, handOffUntracedContainer, reflect.ValueOf(names))
	pending := foreignPending(backing)
	_, revoked := caller.licenceState.revoked[backing]
	require.True(t, pending, "test harness: the callback's walk must leave a revocation pending against the backing")
	require.False(t, revoked, "test harness: the caller's licence must not yet revoke the backing")
	callback.Reset()
	names[0] = materialiseStringForSliceStore(caller, unsafe.Pointer(unsafe.SliceData(names)), arenaString(caller, text))
	_, inArena := licenceDataSlabOf(caller, uintptr(unsafe.Pointer(unsafe.StringData(names[0]))))
	observations.note("stored string left in the caller's arena by the barrier=%v", inArena)
	kept := names[0]
	names[0] = "constant"
	caller.Reset()
	relicence(caller)
	overwriteStrings(caller, len(text))
	observations.compareString("host's copy of the stored string", text, kept)
	runtime.KeepAlive(names)
	assertNoHazard(t, observations, "")
}

func TestReuseHazardStoreIntoARevokedBackingIsNotAHandOff(t *testing.T) {
	observations := &hazardObservations{}
	const text = "stored into a revoked backing after the hand-off"
	arena := newArena(t)
	names := arena.AllocStringBacking(2)
	noteHandOff(arena, handOffIdentityKept, reflect.ValueOf(&names[0]))
	backingRevoked := arena.licenceRevokedBase(slabBase(arena.stringBackingSlab))
	stored := materialiseStringForSliceStore(arena, unsafe.Pointer(unsafe.SliceData(names)), arenaString(arena, text))
	names[0] = stored
	_, inArena := licenceDataSlabOf(arena, uintptr(unsafe.Pointer(unsafe.StringData(stored))))
	byteRevoked := arena.licenceRevokedBase(slabBase(arena.byteSlab))
	observations.note("backing revoked=%v; stored string left in the arena by the barrier=%v; byte slab revoked=%v",
		backingRevoked, inArena, byteRevoked)
	arena.Reset()
	relicence(arena)
	overwriteStrings(arena, len(text))
	observations.compareString("element stored after the hand-off", text, names[0])
	runtime.KeepAlive(names)
	assertNoHazard(t, observations, "")
}

func TestTypedSliceFieldBarrierHandsOffAnArenaBacking(t *testing.T) {
	runWithLicence(t, func() any { return &generalFields{} }, func(t *testing.T, vm *VM, _ *CallFrame, _ *Registers) {
		backing := vm.Arena.AllocIntBacking(4)[:2]
		backing[0], backing[1] = 41, 82
		field := reflect.New(reflect.TypeFor[[]int64]()).Elem()
		stored := storeSliceFieldBarrier(vm.Arena, field, backing)
		require.Equal(t, []int64{41, 82}, stored)
		requirePointeeHandedOff(t, vm.Arena, unsafe.Pointer(unsafe.SliceData(stored)))
	})
}
