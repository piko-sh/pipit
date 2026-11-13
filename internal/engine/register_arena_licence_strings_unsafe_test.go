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
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestByteBackingsTakeTheMutableByteSlab(t *testing.T) {
	arena := newArena(t)
	grows := mutableByteStatistics()
	backing := arena.AllocByteBacking(24)
	require.True(t, arena.ownsMutBytes(bytesStart(backing)), "a licensed arena's []byte backing lies in the mutable byte slab")
	require.False(t, slabsOwnPointer(arena.byteSlab, arena.oldByteSlabs, bytesStart(backing)), "and not in the string byte slab")
	require.Equal(t, 24, cap(backing), "three-index form")
	require.True(t, arena.OwnsSliceBacking(unsafe.Pointer(unsafe.SliceData(backing))))
	require.True(t, arena.ownsSliceBackingOf(reflect.ValueOf(backing)))
	require.True(t, arena.holdsEscapableData())
	require.Greater(t, mutableByteStatistics(), grows)

	big := arena.AllocByteBacking(3 * initialMutByteSlabSize)
	require.True(t, arena.ownsMutBytes(bytesStart(big)), "a grown mutable slab is owned")
	require.True(t, arena.ownsMutBytes(bytesStart(backing)), "and so is the retired one")
	require.NotEmpty(t, arena.oldMutByteSlabs)

	text := arenaString(arena, "string bytes")
	require.True(t, arena.OwnsString(text))
	require.False(t, arena.stringOverMutBytes(text), "string bytes stay in the string byte slab")
	arena.Reset()
	require.Zero(t, arena.mutByteIndex)
	require.Empty(t, arena.oldMutByteSlabs)
}

func TestArenaWithoutALicenceKeepsOneByteSlab(t *testing.T) {
	plain := NewRegisterArena()
	c := plain.AllocByteBacking(8)
	require.True(t, slabsOwnPointer(plain.byteSlab, plain.oldByteSlabs, bytesStart(c)), "an arena without a licence keeps one slab")
	require.Nil(t, plain.mutByteSlab)
}

func TestMutableByteSlabIsPublishedLikeTheByteSlab(t *testing.T) {
	arena := newArena(t)
	backing := arena.AllocByteBacking(32)
	text := arenaString(arena, "published")
	mutEntry, found := licenceSlabContaining(bytesStart(backing))
	require.True(t, found, "the mutable slab is in the licence registry")
	byteEntry, found := licenceSlabContaining(uintptr(unsafe.Pointer(unsafe.StringData(text))))
	require.True(t, found)
	require.Equal(t, byteEntry.class, mutEntry.class, "a foreign walk names the mutable slab as it names the byte slab")
	require.Equal(t, licenceSlabData, mutEntry.class)
	require.Equal(t, arena.slabOwner, mutEntry.owner)
	require.True(t, slabPagesMayHold(bytesStart(backing)), "and on the page table")
	base, ok := licenceDataSlabOf(arena, bytesStart(backing))
	require.True(t, ok)
	require.Equal(t, uintptr(unsafe.Pointer(unsafe.SliceData(arena.mutByteSlab))), base)
	arena.Reset()
}

func TestRevokedMutableByteSlabIsReplacedAtRelease(t *testing.T) {
	arena := newArena(t)
	backing := arena.AllocByteBacking(12)
	copy(backing, "handed off!!")
	before := bytesStart(arena.mutByteSlab)
	noteHandOff(arena, handOffUntracedContainer, reflect.ValueOf(backing))
	require.Contains(t, arena.licenceState.revoked, before, "a []byte hand-off names the mutable slab")
	arena.Reset()
	require.NotEqual(t, before, bytesStart(arena.mutByteSlab), "release replaces the revoked mutable slab")
	require.Equal(t, "handed off!!", string(backing), "and leaves it uncleared")

	reused := newArena(t)
	reused.AllocByteBacking(12)
	kept := bytesStart(reused.mutByteSlab)
	reused.Reset()
	require.Equal(t, kept, bytesStart(reused.mutByteSlab), "a clean mutable slab is kept for reuse")
}

func TestCallbackKeepsForeignStringsOnlyInOwnUncoveredStorage(t *testing.T) {
	owner := newArena(t)
	foreign := arenaString(owner, "the parent's string")
	callback := newArena(t)
	callback.cloneUnownedStrings = true
	require.True(t, callback.mayHoldForeignString(foreign))
	backing := callback.AllocStringBacking(2)
	pending := licenceForeignRevokedCount.Load()
	stored := MaterialiseStringForTypedSliceStore(callback, backing, foreign)
	require.Equal(t, unsafe.StringData(foreign), unsafe.StringData(stored), "no clone into own uncovered storage")
	require.Equal(t, pending, licenceForeignRevokedCount.Load(), "and no foreign revocation until the storage is handed off")

	heap := make([]string, 2)
	require.False(t, callback.ownUncoveredStringSlot(unsafe.Pointer(unsafe.SliceData(heap))), "a heap destination is not own storage")
	require.True(t, callback.ownUncoveredStringSlot(unsafe.Pointer(unsafe.SliceData(backing))))
	noteHandOff(callback, handOffUntracedContainer, reflect.ValueOf(backing))
	require.False(t, callback.ownUncoveredStringSlot(unsafe.Pointer(unsafe.SliceData(backing))), "covered (handed-off) storage is not own uncovered storage")
	plain := newArena(t)
	require.False(t, plain.keepsForeignStrings(), "an arena that does not clone unowned strings keeps no foreign string uncloned")
	plain.Reset()
	callback.Reset()
	owner.Reset()
}

func foreignStringInCallbackStorage(t *testing.T, shared bool) bool {
	t.Helper()
	owner := newArena(t)
	text := arenaString(owner, "the owner's bytes")
	ownerSlab := bytesStart(owner.byteSlab)
	callback := newArena(t)
	callback.cloneUnownedStrings = true
	callback.licenceAlwaysShared = shared
	backing := callback.AllocStringBacking(2)
	backing[0] = MaterialiseStringForTypedSliceStore(callback, backing, text)
	require.Equal(t, unsafe.StringData(text), unsafe.StringData(backing[0]), "the callback keeps the owner's string uncloned")
	noteHandOff(callback, handOffNativeArgUnknown, reflect.ValueOf(backing))
	callback.Reset()
	owner.Reset()
	return ownerSlab != bytesStart(owner.byteSlab)
}

func TestForeignStringInCallbackStorageIsCoveredWhenHandedOff(t *testing.T) {
	require.True(t, foreignStringInCallbackStorage(t, false), "an unshared walk revokes the owner's slab as foreign")
	require.True(t, foreignStringInCallbackStorage(t, true), "a shared walk counts the skipped strings byte-blind, so the owner revokes its byte slabs")
}

func TestRevokedEpochCallbackProbesAForeignString(t *testing.T) {
	tree := &ResourceTracker{}
	caller := newArenaInTree(t, &ResourceTracker{})
	callback := newCallbackArenaInTree(t, tree)
	require.True(t, callback.servesNativeCaller(), "test harness: a callback VM")
	noteHandOff(callback, handOffIdentityKept, reflect.ValueOf(strings.ToLower))
	require.True(t, callback.licenceState.epochRevoked, "test harness: the callback's epoch is revoked")

	treeBlind, blind := tree.licenceBlind.all.Load(), licenceTotal().blindHandOffs.Load()
	borrowed := arenaString(caller, "the caller's string, handed off by the callback")
	noteHandOffString(callback, handOffUntracedContainer, borrowed)
	require.True(t, foreignPendingAt(t, uintptr(unsafe.Pointer(unsafe.StringData(borrowed)))),
		"the probe records a foreign revocation against the caller's slab")
	require.Equal(t, blind, licenceTotal().blindHandOffs.Load(), "the hand-off is not blind")
	require.Equal(t, treeBlind, tree.licenceBlind.all.Load(), "the tree's blind counter does not move")
	noteHandOffString(callback, handOffUntracedContainer, arenaString(callback, "the callback's own"))
	noteHandOffString(callback, handOffUntracedContainer, "")
	require.Equal(t, blind, licenceTotal().blindHandOffs.Load(), "own and empty strings name nothing")
	callback.Reset()
	caller.Reset()
}

func TestCopiesNativeStringAcceptsOnlySmallOwnUnrevokedStrings(t *testing.T) {
	arena := newArena(t)
	defer arena.Reset()
	require.False(t, arena.copiesNativeString(""))
	require.False(t, arena.copiesNativeString(strings.Repeat("h", 8)), "a heap string")
	require.True(t, arena.copiesNativeString(arenaString(arena, "small")))

	declined := nativeCopyCounters.declined.Load()
	require.False(t, arena.copiesNativeString(arenaString(arena, strings.Repeat("l", natCopyMaxBytes+1))), "past the per-string cap")
	arena.optimisticClonedBytes = optimisticCloneBudget(arena)
	require.False(t, arena.copiesNativeString(arenaString(arena, "over budget")), "past the epoch's copy budget")
	arena.optimisticClonedBytes = 0
	handedOff := arenaString(arena, "handed off")
	arena.revokeHandOff(handOffNativeArgRetains, reflect.ValueOf(handedOff), false, false)
	require.False(t, arena.licenceState.epochRevoked, "the hand-off revokes the slab, not the epoch")
	require.False(t, arena.copiesNativeString(arenaString(arena, "revoked slab")), "a string in a revoked slab")
	arena.licenceState.epochRevoked = true
	require.False(t, arena.copiesNativeString(arenaString(arena, "revoked epoch")))
	require.Equal(t, declined+4, nativeCopyCounters.declined.Load())
}

func TestCopyNativeStringArgumentsCopiesForARevokingCallee(t *testing.T) {
	arena := newArena(t)
	defer arena.Reset()
	vm := newTestVM(t)
	original := arenaString(arena, "argument")
	registers := &Registers{Strings: []string{original, "heap"}}
	site := &program.CallSite{Arguments: []program.VarLocation{
		{Register: 0, Kind: isa.RegisterString},
		{Register: 1, Kind: isa.RegisterString},
		{Register: 7, Kind: isa.RegisterString},
		{Register: 0, Kind: isa.RegisterGeneral},
	}}

	require.Nil(t, copyNativeStringArguments(arena, registers, site, BorrowBorrows, vm), "a borrowing callee is lent the string")
	restore := copyNativeStringArguments(arena, registers, site, BorrowRetains, vm)
	require.NotNil(t, restore)
	require.Equal(t, 1, restore.nStrings)
	requireClone(t, original, registers.Strings[0], "a callee that may keep the string gets a heap copy")
	require.Equal(t, "heap", registers.Strings[1])
	restore.undo(registers)
	require.Same(t, unsafe.StringData(original), unsafe.StringData(registers.Strings[0]))
}

func TestCopyNativeMethodStringArgumentsCopiesForARevokingMethod(t *testing.T) {
	type label string
	arena := newArena(t)
	defer arena.Reset()
	text := arenaString(arena, "method")
	arguments := []reflect.Value{reflect.ValueOf(label(text)), reflect.ValueOf(3), {}}
	copyNativeMethodStringArguments(arena, reflect.ValueOf(strings.Contains), arguments)
	require.Same(t, unsafe.StringData(text), unsafe.StringData(arguments[0].String()), "a borrowing method is lent the string")

	copyNativeMethodStringArguments(arena, reflect.ValueOf(func(label, int) {}), arguments)
	requireClone(t, text, arguments[0].String(), "a method that may keep the string gets a copy")
	require.Equal(t, reflect.TypeFor[label](), arguments[0].Type())
}

func TestPlacesBytesNeedsALicenceAndAnUnvetoedSite(t *testing.T) {
	vm := newTestVM(t)
	function := &program.CompiledFunction{}
	frame := &CallFrame{Function: function, ProgramCounter: 1}
	require.False(t, placesBytes(vm, nil))
	vm.Arena = NewRegisterArena()
	require.False(t, placesBytes(vm, frame), "an arena without a licence")
	vm.Arena = newArena(t)
	require.True(t, placesBytes(vm, frame))
	function.VetoConversionArena(0)
	require.False(t, placesBytes(vm, frame), "an escape vetoed the site")
}

func TestNativeCopyMaxBytesIsTheCap(t *testing.T) {
	require.Equal(t, natCopyMaxBytes, NativeCopyMaxBytes())
}

func TestMinorCollectionVetoesASiteWhoseResultWasRevoked(t *testing.T) {
	vm := newLicensedMarkVM(t)
	arena := vm.Arena
	function := &program.CompiledFunction{}
	leaving := &CallFrame{Function: function, ProgramCounter: 1}
	staying := &CallFrame{Function: function, ProgramCounter: 3}

	handedOff := placeStringToBytes(arena, leaving, "stored in a global")
	noteHandOff(arena, handOffUntracedContainer, handedOff)
	arena.growMutByteSlab(initialMutByteSlabSize)
	placeStringToBytes(arena, staying, "stays in the frame")
	require.Len(t, arena.bytePlacementSites, 2)

	arena.MinorGC(vm)
	require.True(t, function.ConversionArenaVetoed(0), "a collection within the epoch vetoes the site whose result was revoked")
	require.False(t, placesBytes(vm, leaving), "so its later results take the heap")
	require.False(t, function.ConversionArenaVetoed(2), "a site whose slab is clean is not vetoed")
	require.True(t, placesBytes(vm, staying))
	require.Len(t, arena.bytePlacementSites, 1, "the clean site is kept for the release to settle")
	require.Equal(t, 2, arena.bytePlacementSites[0].programCounter)
}
