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
	"reflect"
	"strings"
	"sync/atomic"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// bytePlacementSite is one conversion or split site that placed a result in an epoch.
type bytePlacementSite struct {
	// function holds the site.
	function *program.CompiledFunction

	// programCounter is the site's program counter.
	programCounter int

	// base is the start of the slab the result went to.
	base uintptr
}

// bytePlacementMaxSites bounds the sites an epoch records; a site beyond it builds on the
// heap.
const bytePlacementMaxSites = 64

const (
	// initialMutByteSlabSize is the size of an arena's first mutable byte slab. It is a
	// quarter of the string byte slab's, so a hand-off that revokes it gives up less.
	initialMutByteSlabSize = InitialByteSlabSize / 4
)

const (
	// natCopyMaxBytes is the largest string copied for a revoking native.
	natCopyMaxBytes = 4096
)

var (
	// foreignStringsKept counts foreign strings kept uncloned in own storage, for tests.
	foreignStringsKept atomic.Int64
)

var (
	// mutableByteSlabGrows counts mutable byte slabs installed, for tests and diagnostics.
	mutableByteSlabGrows atomic.Int64

	// mutableByteSlabsDropped counts retired mutable byte slabs collections dropped
	// (compactOldMutByteSlabs), while licenceCounting is on.
	mutableByteSlabsDropped atomic.Int64

	// mutableByteBytesDropped is their capacity in bytes.
	mutableByteBytesDropped atomic.Int64
)

var (
	// nativeCopyCounters counts native string copies, for tests and diagnostics.
	nativeCopyCounters struct {
		copied, copiedBytes, declined atomic.Int64
	}
)

// NativeCopyStats counts string arguments copied for revoking natives since the process
// started.
type NativeCopyStats struct {
	// Copied counts string arguments copied for a revoking native.
	Copied int64

	// CopiedBytes counts their bytes.
	CopiedBytes int64

	// Declined counts own-slab strings left to revocation because they exceed the size cap
	// or epoch budget, or their slab is already revoked.
	Declined int64
}

// foreignStringStatistics returns the number of foreign strings kept uncloned in a
// callback's own storage since the process started.
//
// Returns int64 which is the count.
func foreignStringStatistics() int64 { return foreignStringsKept.Load() }

// placesBytes reports whether the current conversion or split can place its result in the
// arena.
//
// Takes vm (*VM) which provides the arena.
// Takes frame (*CallFrame) which is the running frame; nil answers false.
//
// Returns bool which is true when the result goes to the arena.
func placesBytes(vm *VM, frame *CallFrame) bool {
	if !arenaUsesUnsafeSlabs || !licenceExactProbes || frame == nil || frame.Function == nil {
		return false
	}
	arena := vm.Arena
	if arena == nil || !arena.licensed || arena.heapData {
		return false
	}
	return len(arena.bytePlacementSites) < bytePlacementMaxSites && !frame.Function.ConversionArenaVetoed(frame.ProgramCounter-1)
}

// settleBytePlacements vetoes, at a licensed release, every site whose result went to a
// slab a hand-off revoked in the ending epoch, and forgets the epoch's sites.
//
// An epoch revocation vetoes nothing. It abandons every slab whatever was placed.
func (a *RegisterArena) settleBytePlacements() {
	if len(a.bytePlacementSites) == 0 {
		return
	}
	for _, site := range a.bytePlacementSites {
		if _, revoked := a.licenceState.revoked[site.base]; revoked && site.function != nil {
			site.function.VetoConversionArena(site.programCounter)
		}
	}
	clear(a.bytePlacementSites)
	a.bytePlacementSites = a.bytePlacementSites[:0]
}

// vetoRevokedBytePlacements is settleBytePlacements for a minor collection within the
// epoch. It vetoes every site whose result went to a slab a hand-off has revoked so far,
// forgets those sites and keeps the rest for the release.
func (a *RegisterArena) vetoRevokedBytePlacements() {
	kept := a.bytePlacementSites[:0]
	for _, site := range a.bytePlacementSites {
		if _, revoked := a.licenceState.revoked[site.base]; revoked && site.function != nil {
			site.function.VetoConversionArena(site.programCounter)
			continue
		}
		kept = append(kept, site)
	}
	clear(a.bytePlacementSites[len(kept):])
	a.bytePlacementSites = kept
}

// mutableByteStatistics returns the number of mutable byte slabs installed since the
// process started (each arena's first one included).
//
// Returns int64 which is the count.
func mutableByteStatistics() int64 { return mutableByteSlabGrows.Load() }

// splitsMutableBytes reports whether the arena allocates []byte backings from a separate
// mutable byte slab.
//
// It requires a licensed arena with exact probes that allocates from slabs.
//
// Returns bool which is true when []byte backings go to mutByteSlab.
func (a *RegisterArena) splitsMutableBytes() bool {
	return arenaUsesUnsafeSlabs && licenceExactProbes && a.licensed && !a.heapData
}

// allocMutByteBacking bump-allocates size bytes from the mutable byte slab in three-index
// form (len == cap == size), as AllocByteBacking does from the string byte slab.
//
// Takes size (int) which is the number of bytes, greater than zero.
//
// Returns a []byte of length size and capacity size backed by the mutable byte slab.
func (a *RegisterArena) allocMutByteBacking(size int) []byte {
	if a.mutByteIndex+size > len(a.mutByteSlab) {
		a.growMutByteSlab(size)
	}
	index := a.mutByteIndex
	a.mutByteIndex += size
	a.noteAlloc(int64(size))
	return a.mutByteSlab[index : index+size : index+size]
}

// growMutByteSlab installs a new mutable byte slab, retiring the current one to
// oldMutByteSlabs so live []byte values over it stay owned, and publishes it to the
// licence registry before any value can reference it.
//
// Takes minExtra (int) which is the minimum number of bytes the new slab must hold.
//
//go:noinline
func (a *RegisterArena) growMutByteSlab(minExtra int) {
	oldCap := a.retiredSlabCharge(uint64(cap(a.mutByteSlab)))
	newSize := a.dataSlabCapacity(max(len(a.mutByteSlab)*2, minExtra, initialMutByteSlabSize, a.takeLazyFloor(len(a.mutByteSlab), &a.licenceState.lazyFloor.mutBytes)), minExtra)
	newSize = a.fitGrow(newSize, minExtra, 1, len(a.mutByteSlab), oldCap)
	a.admitGrow(safeconv.IntToUint64(newSize), oldCap)
	a.slabRetiredSinceReset.mutBytes += a.mutByteIndex
	if len(a.mutByteSlab) > 0 {
		a.oldMutByteSlabs = append(a.oldMutByteSlabs, a.mutByteSlab)
		a.noteChargeHandedOn(slabChargeKey(a.mutByteSlab), oldCap)
		trimRetainedCharged(a, &a.oldMutByteSlabs, 1)
	}
	a.mutByteSlab = make([]byte, newSize)
	a.mutByteIndex = 0
	a.publishLicenceSlabs()
	a.ChargeArenaAllocation(uint64(cap(a.mutByteSlab)), oldCap)
	if licenceCounting {
		mutableByteSlabGrows.Add(1)
	}
}

// ownsMutBytes reports whether pointer lies in the arena's current or retired mutable
// byte slabs.
//
// Takes pointer (uintptr) which is the address.
//
// Returns bool which is true for an address in a mutable byte slab.
func (a *RegisterArena) ownsMutBytes(pointer uintptr) bool {
	if a.mutByteSlab == nil && len(a.oldMutByteSlabs) == 0 {
		return false
	}
	return slabsOwnPointer(a.mutByteSlab, a.oldMutByteSlabs, pointer)
}

// dropMutByteSlabs forgets the mutable byte slabs, current and retired, leaving them to
// the collector; the next []byte allocates a fresh one.
func (a *RegisterArena) dropMutByteSlabs() {
	a.mutByteSlab, a.oldMutByteSlabs, a.mutByteIndex = nil, nil, 0
}

// nativeCopyStatistics returns the native copy counters, which move only while the
// licence counters are on (SetLicenceCountingForTest).
//
// Returns NativeCopyStats which is a snapshot.
func nativeCopyStatistics() NativeCopyStats {
	return NativeCopyStats{
		Copied:      nativeCopyCounters.copied.Load(),
		CopiedBytes: nativeCopyCounters.copiedBytes.Load(),
		Declined:    nativeCopyCounters.declined.Load(),
	}
}

// NativeCopyMaxBytes returns the per-string cap, natCopyMaxBytes.
//
// Returns int which is the cap in bytes.
func NativeCopyMaxBytes() int { return natCopyMaxBytes }

// nativeCopyApplies reports whether a licensed arena with exact probes may copy string
// arguments passed to revoking native functions.
//
// Takes arena (*RegisterArena) which may be nil.
//
// Returns bool which is true when string arguments of revoking natives may be copied.
func nativeCopyApplies(arena *RegisterArena) bool {
	return arenaUsesUnsafeSlabs && licenceExactProbes && arena != nil && arena.licensed
}

// copiesNativeStrings reports whether nativeCopyApplies permits copying and the arena's
// epoch remains unrevoked.
//
// Takes arena (*RegisterArena) which may be nil.
//
// Returns bool which is true when the call sites run the copy pass.
func copiesNativeStrings(arena *RegisterArena) bool {
	return nativeCopyApplies(arena) && (!arena.licenceState.epochRevoked || licenceCounting)
}

// nativeRevokes reports whether a call of function hands its arguments off by a revoking
// cause, so its string arguments may be copied.
//
// Takes arena (*RegisterArena) which classes the callee.
// Takes function (reflect.Value) which is the native callee.
//
// Returns bool which is true for a native function whose class revokes.
func nativeRevokes(arena *RegisterArena, function reflect.Value) bool {
	return function.IsValid() && function.Kind() == reflect.Func &&
		licenceRevokes(arena.nativeBorrowClass(function).handOffCause())
}

// copyNativeStringArguments copies small string arguments of a native function call.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes registers (*Registers) which holds the arguments.
// Takes site (*program.CallSite) which locates them.
// Takes class (BorrowClass) which is the callee's class, computed once for the call and
// shared with the noting.
// Takes vm (*VM) whose scratch record (restoreRecord) takes the rewrites, made at the
// first rewrite.
//
// Returns *nativeArgumentRestore which holds the rewrites to undo, or nil when there are
// none.
func copyNativeStringArguments(arena *RegisterArena, registers *Registers, site *program.CallSite, class BorrowClass, vm *VM) *nativeArgumentRestore {
	var restore *nativeArgumentRestore
	if !licenceRevokes(class.handOffCause()) {
		return nil
	}
	for _, location := range site.Arguments {
		if location.Kind != isa.RegisterString || int(location.Register) >= len(registers.Strings) {
			continue
		}
		original := registers.Strings[location.Register]
		if !arena.copiesNativeString(original) {
			continue
		}
		clone := strings.Clone(original)
		registers.Strings[location.Register] = clone
		if restore == nil {
			restore = vm.restoreRecord(true)
		}
		if restore.nStrings < nativeArgumentRestoreSlots {
			restore.strings[restore.nStrings] = nativeStringRestore{original: original, clone: clone, register: location.Register}
			restore.nStrings++
		}
	}
	return restore
}

// copyNativeMethodStringArguments copies small string arguments of a bound native method.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes method (reflect.Value) which is the bound native method.
// Takes arguments ([]reflect.Value) which are the built arguments, rewritten in place.
func copyNativeMethodStringArguments(arena *RegisterArena, method reflect.Value, arguments []reflect.Value) {
	if !nativeRevokes(arena, method) {
		return
	}
	for index, argument := range arguments {
		if !argument.IsValid() || argument.Kind() != reflect.String {
			continue
		}
		if s := argument.String(); arena.copiesNativeString(s) {
			arguments[index] = reflect.ValueOf(strings.Clone(s)).Convert(argument.Type())
		}
	}
}

// revokedEpochProbes reports whether to probe an unboxed string hand-off from a licensed
// arena whose epoch is revoked.
//
// Takes boxed (bool) which is true for a string reached through an interface.
//
// Returns bool which is true when probeStringInRevokedEpoch settles the hand-off.
func (a *RegisterArena) revokedEpochProbes(boxed bool) bool {
	return licenceExactProbes && !boxed && a.licensed && a.licenceState.epochRevoked &&
		a.servesNativeCaller()
}

// prepareNativeCallHandOffs copies and notes a native call's arguments as the arena's
// licence requires, and marks the call as borrowing when its callee keeps nothing it is
// handed (beginBorrowingNativeCall).
//
// Takes vm (*VM) which runs the call.
// Takes registers (*Registers) which hold the callee and arguments.
// Takes site (*program.CallSite) which locates them.
//
// Returns *nativeArgumentRestore which holds the string rewrites the caller undoes after
// the call, or nil when there are none.
// Returns bool which is true when the caller must end the borrowing call
// (endBorrowingNativeCall) after it.
func prepareNativeCallHandOffs(vm *VM, registers *Registers, site *program.CallSite) (*nativeArgumentRestore, bool) {
	var restore *nativeArgumentRestore
	function, class, classed := classedNativeCall(vm.Arena, registers, site)
	if classed {
		restore = copyNativeStringArguments(vm.Arena, registers, site, class, vm)
		noteNativeCallArgumentsClassed(vm.Arena, registers, site, function, class)
	} else if !copiesNativeStrings(vm.Arena) && vm.Arena.countsHandOffs() {
		function = registers.General[site.NativeRegister]
		noteNativeCallArguments(vm.Arena, registers, site, function)
	}
	return restore, vm.beginBorrowingNativeCall(function, class, classed)
}

// classedNativeCall returns a native call's callee and its borrow class when the arena
// copies native strings and the callee has a class.
//
// Takes arena (*RegisterArena) which runs the call.
// Takes registers (*Registers) which hold the callee.
// Takes site (*program.CallSite) which locates it.
//
// Returns reflect.Value which is the callee.
// Returns BorrowClass which is its class.
// Returns bool which is false when the arena copies no strings or the callee has no
// class.
func classedNativeCall(arena *RegisterArena, registers *Registers, site *program.CallSite) (reflect.Value, BorrowClass, bool) {
	if !copiesNativeStrings(arena) {
		return reflect.Value{}, BorrowUnknown, false
	}
	function := registers.General[site.NativeRegister]
	class, ok := nativeCallClass(arena, function)
	return function, class, ok
}
