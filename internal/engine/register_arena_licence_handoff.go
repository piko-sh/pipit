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
	"sync/atomic"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// HandOffCause names a point where a value may leave the arena's lifetime without a
// barrier that copies it out. The enumeration lives in the ISA package, whose spec rows
// declare the causes each operation hands off under.
type HandOffCause = isa.HandOffCause

const (
	// handOffNativeArgUnknown is isa.HandOffNativeArgUnknown.
	handOffNativeArgUnknown = isa.HandOffNativeArgUnknown

	// handOffNativeArgRetains is isa.HandOffNativeArgRetains.
	handOffNativeArgRetains = isa.HandOffNativeArgRetains

	// handOffNativeArgBorrows is isa.HandOffNativeArgBorrows.
	handOffNativeArgBorrows = isa.HandOffNativeArgBorrows

	// handOffNativeArgResultAliases is isa.HandOffNativeArgResultAliases.
	handOffNativeArgResultAliases = isa.HandOffNativeArgResultAliases

	// handOffWalkExhausted is isa.HandOffWalkExhausted.
	handOffWalkExhausted = isa.HandOffWalkExhausted

	// handOffNotSettable is isa.HandOffNotSettable.
	handOffNotSettable = isa.HandOffNotSettable

	// handOffIdentityKept is isa.HandOffIdentityKept.
	handOffIdentityKept = isa.HandOffIdentityKept

	// handOffHeapPassthrough is isa.HandOffHeapPassthrough.
	handOffHeapPassthrough = isa.HandOffHeapPassthrough

	// handOffUntracedContainer is isa.HandOffUntracedContainer.
	handOffUntracedContainer = isa.HandOffUntracedContainer

	// handOffSpawn is isa.HandOffSpawn.
	handOffSpawn = isa.HandOffSpawn

	// handOffSharedStore is isa.HandOffSharedStore.
	handOffSharedStore = isa.HandOffSharedStore

	// numHandOffCauses is the number of causes.
	numHandOffCauses = isa.NumHandOffCauses

	// revokedEpochStoreRouteBuilt is true in the builds whose assembly may make a pointer
	// store in a revoked, local-blind epoch itself (revokedLocalBlind). The store-census
	// build and the abort-injection build keep the Go handler's barrier for every such
	// store, so the census sees each store and an armed injection can reach each hand-off.
	revokedEpochStoreRouteBuilt = !storeCensusBuilt && !abortInjectBuilt
)

// licenceNoteKey identifies a noted value by its reflect.Value words and the note's
// boxing.
type licenceNoteKey struct {
	// typ, ptr and flag are the reflect.Value's words.
	typ, ptr, flag uintptr

	// boxed is the note's boxed argument.
	boxed bool
}

// storedValuesWalkedOnce counts the second notes settled without a walk, while the
// licence counters are on (SetLicenceCountingForTest).
var storedValuesWalkedOnce atomic.Int64

// revokedEpochNoteCounts holds hand-off counts by cause and blind hand-off counts not yet
// published by noteRevokedEpochHandOff.
type revokedEpochNoteCounts struct {
	// byCause counts hand-offs by cause.
	byCause [numHandOffCauses]int64

	// blind counts blind hand-offs.
	blind int64
}

// minorGCSiteHook is the installed collection-site observer, or nil. Production code pays
// one atomic load per collection.
var minorGCSiteHook atomic.Pointer[func()]

// pcGranted reports whether the escape pass granted the instruction at pc.
//
// Takes grants (map[int]bool) which is one of the function's escape-pass annotations
// (ArenaSafeAllocPCs, FieldStoreArenaSafePCs or InPlaceHeaderReusePCs).
// Takes pc (int) which is the instruction's program counter.
//
// Returns bool which is true when the instruction is granted.
func pcGranted(grants map[int]bool, pc int) bool {
	return grants[pc]
}

// localStoreGranted reports whether the licence checker granted the map store at pc.
//
// Takes function (*program.CompiledFunction) which carries the checker's grants.
// Takes pc (int) which is the store's program counter.
//
// Returns bool which is true when the store is granted.
func localStoreGranted(function *program.CompiledFunction, pc int) bool {
	return function.LocalContainerStorePCs.Has(pc)
}

// localContainerStore reports whether a map store can skip its value hand-off.
//
// The licence checker granted the store in LocalContainerStorePCs. Its map was made by
// the running function and nothing has handed it off, so only the running VM can reach
// it, and what is stored in it leaves the arena's lifetime only if the map later does.
// That later hand-off walks the map's entries and revokes what they reference.
//
// The grant is honoured only where that walk exists and reads the whole map. The arena
// must hold a licence with exact probes. Its VM must share memory with no goroutine,
// since a walk in a shared VM stops at Go heap storage. And it must not be a re-entrant
// VM's arena, whose strings may belong to another VM's arena and are cloned by the
// barrier this store skips.
//
// Takes arena (*RegisterArena) which ran the store. It may be nil.
// Takes frame (*CallFrame) which is the running frame, its program counter past the
// store.
// Takes container (reflect.Value) which is the map stored into.
//
// Returns bool which is true when the value is stored as it is.
func localContainerStore(arena *RegisterArena, frame *CallFrame, container reflect.Value) bool {
	if !localStoreGranted(frame.Function, frame.ProgramCounter-1) {
		return false
	}
	local := arenaUsesUnsafeSlabs && licenceExactProbes && arena != nil && arena.licensed && !arena.heapData &&
		!arena.cloneUnownedStrings && !arena.licenceShared(0)
	if storeCensusBuilt && local {
		censusNoteLocalContainerStore(arena, container)
	}
	return local
}

// licenceNoteSeq returns the arena's note sequence number, for a second-note site to pass
// to noteHandOffAfter; zero in an arena without a licence.
//
// Returns uint64 which is the number.
func (a *RegisterArena) licenceNoteSeq() uint64 {
	if a == nil || !a.licensed {
		return 0
	}
	return a.licenceState.noteSeq
}

// licenceRecordNote records a revoking note of value, after its walk, so a second note of
// the same value can skip its walk (noteHandOffAfter).
//
// Takes value (reflect.Value) which is the noted value.
// Takes boxed (bool) which is the note's boxed argument.
func (a *RegisterArena) licenceRecordNote(value reflect.Value, boxed bool) {
	if !value.IsValid() {
		return
	}
	state := &a.licenceState
	state.lastNoted, state.lastNotedSeq = licenceNoteKeyOf(value, boxed), state.noteSeq
}

// noteHandOffAfter is noteBoxedHandOff for the second note of a value the first note,
// made after before was read, may already have walked.
//
// Takes arena (*RegisterArena) which is the releasing VM's arena; may be nil.
// Takes cause (HandOffCause) which names the site.
// Takes value (reflect.Value) which is the value handed off.
// Takes boxed (bool) which is true when value is about to be converted to an interface.
// Takes before (uint64) which is licenceNoteSeq read before the first note.
func noteHandOffAfter(arena *RegisterArena, cause HandOffCause, value reflect.Value, boxed bool, before uint64) {
	if arena == nil || !arena.licensed {
		return
	}
	if licenceRevokes(cause) {
		state := &arena.licenceState
		if state.lastNotedSeq > before && state.noteSeq == state.lastNotedSeq {
			if value.IsValid() && licenceNoteKeyOf(value, boxed) == state.lastNoted {
				arena.countHandOffWalkedOnce(cause, value)
				return
			}
		}
	}
	arena.recordHandOff(cause, value, boxed)
}

// countHandOffWalkedOnce performs recordHandOffShared's bookkeeping without repeating the
// walk already performed for the previous note's value.
//
// Takes cause (HandOffCause) which names the site.
// Takes value (reflect.Value) which is the value handed off.
func (a *RegisterArena) countHandOffWalkedOnce(cause HandOffCause, value reflect.Value) {
	if abortInjectBuilt {
		injectHandOffAbort(a)
	}
	if storeCensusBuilt {
		censusNoteHandOff(a, cause, value)
	}
	a.licenceState.noteSeq++
	if licenceCounting {
		a.stats().handOffs[cause].Add(1)
		storedValuesWalkedOnce.Add(1)
	}
}

// walkedOnceStatistics returns how many second notes were settled without a walk
// (noteHandOffAfter).
//
// Returns int64 which is the count.
func walkedOnceStatistics() int64 {
	return storedValuesWalkedOnce.Load()
}

// revokedEpochLocalBlind reports whether a hand-off settles locally with nothing left to
// revoke.
//
// Takes arena (*RegisterArena) which may be nil.
//
// Returns bool which is true when noteRevokedEpochHandOff stands for the barrier's chain.
func revokedEpochLocalBlind(arena *RegisterArena) bool {
	if storeCensusBuilt || arena == nil || !arena.licensed {
		return false
	}
	state := &arena.licenceState
	local := state.epochRevoked && state.banksRevoked && arena.blindLocal()
	state.revokedLocalBlind = local && revokedEpochStoreRouteBuilt
	return local
}

// noteRevokedEpochHandOff performs the bookkeeping left after revokeRemainder hands off
// value when revokedEpochLocalBlind holds.
//
// Takes cause (HandOffCause) which is the hand-off's cause.
// Takes value (reflect.Value) which is the pointer handed off.
func (a *RegisterArena) noteRevokedEpochHandOff(cause HandOffCause, value reflect.Value) {
	a.licenceState.noteSeq++
	if !licenceRevokes(cause) {
		return
	}
	if licenceCounting {
		notes := &a.licenceState.revokedEpochNotes
		notes.byCause[cause]++
		notes.blind++
	}
	a.licenceRecordNote(value, false)
}

// flushRevokedEpochNotes adds the counts noteRevokedEpochHandOff has made to the shared
// counters and clears them.
func (a *RegisterArena) flushRevokedEpochNotes() {
	notes := &a.licenceState.revokedEpochNotes
	if notes.blind == 0 {
		return
	}
	stats := a.stats()
	for cause, count := range notes.byCause {
		if count != 0 {
			stats.handOffs[cause].Add(count)
		}
	}
	stats.blindHandOffs.Add(notes.blind)
	if licenceCounting {
		blindScopeCounters.localBlind.Add(notes.blind)
	}
	*notes = revokedEpochNoteCounts{}
}

// mapSliceValueRevokedEpoch is handleSetMapStringGeneral's barrier for a slice stored
// into a map of slices on the revoked-epoch fast path (revokedEpochLocalBlind), in an
// arena where optimistic placement applies (optimisticArena).
//
// Takes arena (*RegisterArena) which ran the store. It may be nil.
// Takes value (reflect.Value) which is the value stored.
// Takes slot (reflect.Type) which is the map's element type.
//
// Returns bool which is true when the barrier was settled and value is stored as it is.
func mapSliceValueRevokedEpoch(arena *RegisterArena, value reflect.Value, slot reflect.Type) bool {
	if abortInjectArmed() || !revokedEpochLocalBlind(arena) || !optimisticArena(arena) ||
		!value.IsValid() || value.Kind() != reflect.Slice || slot.Kind() != reflect.Slice {
		return false
	}
	arena.noteRevokedEpochHandOff(handOffUntracedContainer, value)
	arena.noteRevokedEpochHandOff(handOffUntracedContainer, value)
	return true
}

// SetMinorGCSiteHook installs hook as the process-wide observer of MinorGC entries, for
// tests that check where collections run.
//
// Takes hook (func()) which is the observer, or nil.
//
// Returns func() which restores the previous hook.
func SetMinorGCSiteHook(hook func()) func() {
	var next *func()
	if hook != nil {
		next = &hook
	}
	previous := minorGCSiteHook.Swap(next)
	return func() { minorGCSiteHook.Store(previous) }
}

// noteHandOff revokes what v references at a hand-off site of cause when the arena is
// licensed. The disabled path is one nil check and a flag test, and inlines.
//
// Takes a (*RegisterArena) which is the releasing VM's arena; may be nil.
// Takes cause (HandOffCause) which names the site.
// Takes v (reflect.Value) which is the value handed off.
func noteHandOff(a *RegisterArena, cause HandOffCause, v reflect.Value) {
	if a == nil || !a.licensed {
		return
	}
	a.recordHandOff(cause, v, false)
}

// noteBoxedHandOff is noteHandOff for a value about to be converted to an interface,
// which shares the value's own storage when it is indirect and not addressable.
//
// Takes a (*RegisterArena) which is the releasing VM's arena; may be nil.
// Takes cause (HandOffCause) which names the site.
// Takes v (reflect.Value) which is the value handed off.
// Takes boxed (bool) which is true when the destination is interface-typed.
func noteBoxedHandOff(a *RegisterArena, cause HandOffCause, v reflect.Value, boxed bool) {
	if a == nil || !a.licensed {
		return
	}
	a.recordHandOff(cause, v, boxed)
}

// noteHandOffString is noteHandOff for a string held outside a reflect.Value, so the
// disabled path does not box it.
//
// Takes arena (*RegisterArena) which is the releasing VM's arena; may be nil.
// Takes cause (HandOffCause) which names the site.
// Takes text (string) which is the string handed off.
func noteHandOffString(arena *RegisterArena, cause HandOffCause, text string) {
	if arena == nil || !arena.licensed {
		return
	}
	if arena.notesUnboxed() {
		arena.recordHandOffString(cause, text, false)
		return
	}
	arena.recordHandOff(cause, reflect.ValueOf(text), false)
}

// noteHandOffSliceRegister is noteHandOff for the typed slice in a slice-bank register,
// boxed only if the licence must walk it.
//
// Takes arena (*RegisterArena) which is the releasing VM's arena; may be nil.
// Takes cause (HandOffCause) which names the site.
// Takes registers (*Registers) which holds the register.
// Takes kind (isa.RegisterKind) which selects the slice bank.
// Takes register (uint8) which is the register.
func noteHandOffSliceRegister(arena *RegisterArena, cause HandOffCause, registers *Registers, kind isa.RegisterKind, register uint8) {
	if arena == nil || !arena.licensed || !isTypedSliceRegisterKind(kind) {
		return
	}
	if arena.notesUnboxed() {
		arena.recordHandOffSliceRegister(cause, registers, kind, register, false)
		return
	}
	if value, ok := sliceBankToReflect(registers, kind, register); ok {
		arena.recordHandOff(cause, value, false)
	}
}

// isTypedSliceRegisterKind reports whether kind is one of the typed slice banks, whose
// registers registerToReflectValue boxes in a fresh reflect.Value when given no arena.
//
// Takes kind (isa.RegisterKind) which is the register kind.
//
// Returns bool which is true for the int, float, string, bool, uint and byte slice banks.
func isTypedSliceRegisterKind(kind isa.RegisterKind) bool {
	switch kind {
	case isa.RegisterSliceInt64, isa.RegisterSliceFloat64, isa.RegisterSliceString, isa.RegisterSliceBool,
		isa.RegisterSliceUint64, isa.RegisterSliceByte:
		return true
	default:
		return false
	}
}

// notesUnboxed reports whether a string or typed-slice hand-off may be noted before, or
// without, boxing the value in a reflect.Value.
//
// Returns bool which is true when recordHandOffString and recordHandOffSliceRegister
// apply.
func (a *RegisterArena) notesUnboxed() bool {
	return !storeCensusBuilt && a.licensed
}

// recordHandOffString is recordHandOff for a string, under notesUnboxed.
//
// Takes cause (HandOffCause) which names the site.
// Takes text (string) which is the string handed off.
// Takes boxed (bool) which is true when text is about to be converted to an interface.
func (a *RegisterArena) recordHandOffString(cause HandOffCause, text string, boxed bool) {
	a.licenceState.noteSeq++
	shared := a.licenceShared(0)
	if abortInjectBuilt {
		injectHandOffAbort(a)
	}
	if !licenceRevokes(cause) {
		return
	}
	if licenceCounting {
		a.stats().handOffs[cause].Add(1)
	}
	if a.revokedEpochProbes(boxed) {
		a.probeStringInRevokedEpoch(text, cause)
		return
	}
	if a.revokeWithoutWalk(cause) || !boxed && licenceStringNamesNothing(a, text) {
		return
	}
	a.walkHandOff(BoxStringToGeneral(nil, text), boxed, cause, shared)
}

// recordHandOffSliceRegister applies recordHandOffString's logic to a typed-slice bank
// register.
//
// Takes cause (HandOffCause) which names the site.
// Takes registers (*Registers) which holds the register.
// Takes kind (isa.RegisterKind) which is a typed slice bank.
// Takes register (uint8) which is the register.
// Takes boxed (bool) which is true when the slice is about to be converted to an
// interface.
func (a *RegisterArena) recordHandOffSliceRegister(cause HandOffCause, registers *Registers, kind isa.RegisterKind, register uint8, boxed bool) {
	a.licenceState.noteSeq++
	shared := a.licenceShared(0)
	if abortInjectBuilt {
		injectHandOffAbort(a)
	}
	if !licenceRevokes(cause) {
		return
	}
	if licenceCounting {
		a.stats().handOffs[cause].Add(1)
	}
	if a.revokeWithoutWalk(cause) {
		return
	}
	value := registerToReflectValue(nil, registers, kind, register)
	if !boxed && a.revokeTrivially(value, shared) {
		return
	}
	a.walkHandOff(value, boxed, cause, shared)
}

// countsHandOffs reports whether the arena is licensed, for sites that must do work to
// build the value they note.
//
// Returns bool which is true when hand-offs are noted.
func (a *RegisterArena) countsHandOffs() bool {
	return a != nil && a.licensed
}

// recordHandOff is noteHandOff's slow path.
//
// Takes cause (HandOffCause) which names the site.
// Takes v (reflect.Value) which is the value handed off.
// Takes boxed (bool) which is true when v is about to be converted to an interface.
//
//go:noinline
func (a *RegisterArena) recordHandOff(cause HandOffCause, v reflect.Value, boxed bool) {
	a.recordHandOffShared(cause, v, boxed, a.licenceShared(0))
}

// recordHandOffShared is recordHandOff with the sharing already decided.
//
// Takes cause (HandOffCause) which names the site.
// Takes value (reflect.Value) which is the value handed off.
// Takes boxed (bool) which is true when value is about to be converted to an interface.
// Takes shared (bool) which is true when another goroutine may be writing what value
// reaches, so neither the probe nor the walk may read through it.
func (a *RegisterArena) recordHandOffShared(cause HandOffCause, value reflect.Value, boxed bool, shared bool) {
	if abortInjectBuilt {
		injectHandOffAbort(a)
	}
	if storeCensusBuilt {
		censusNoteHandOff(a, cause, value)
	}
	if a.licensed {
		a.revokeHandOff(cause, value, boxed, shared)
	}
}

// noteNativeCallArguments notes the reference-carrying arguments of a native call, read
// straight from their registers so every dispatch path (typed fast path, linked call,
// reflect call) is noted once and without side effects on the arena.
//
// Takes arena (*RegisterArena) which notes the hand-offs.
// Takes registers (*Registers) which holds the argument registers.
// Takes site (*program.CallSite) which locates the arguments.
// Takes function (reflect.Value) which is the callee; interpreted closures are skipped.
func noteNativeCallArguments(arena *RegisterArena, registers *Registers, site *program.CallSite, function reflect.Value) {
	if !function.IsValid() || function.Kind() != reflect.Func {
		return
	}
	noteNativeCallArgumentsClassed(arena, registers, site, function, arena.nativeBorrowClass(function))
}

// nativeCallClass classes a native callee once for a call, so the native copies (small
// strings copied for natives that would retain them) and the hand-off notes share one
// classification.
//
// Takes arena (*RegisterArena) which classes the callee.
// Takes function (reflect.Value) which is the callee.
//
// Returns BorrowClass which is its class.
// Returns bool which is false when function is not a func (nothing is copied or noted
// then).
func nativeCallClass(arena *RegisterArena, function reflect.Value) (BorrowClass, bool) {
	if !function.IsValid() || function.Kind() != reflect.Func {
		return BorrowUnknown, false
	}
	return arena.nativeBorrowClass(function), true
}

// noteNativeCallArgumentsClassed is noteNativeCallArguments with the callee already
// classed.
//
// Takes arena (*RegisterArena) which notes the hand-offs.
// Takes registers (*Registers) which holds the argument registers.
// Takes site (*program.CallSite) which locates the arguments.
// Takes function (reflect.Value) which is the callee, a func.
// Takes class (BorrowClass) which is its class.
func noteNativeCallArgumentsClassed(arena *RegisterArena, registers *Registers, site *program.CallSite, function reflect.Value, class BorrowClass) {
	cause := class.handOffCause()
	functionType := function.Type()
	for index, location := range site.Arguments {
		if isScalarRegisterKind(location.Kind) {
			continue
		}
		boxed := nativeParameterIsInterface(functionType, index+len(site.LinkedTypeArgs), site.IsEllipsisSpread)
		if arena.notesUnboxed() {
			switch {
			case location.Kind == isa.RegisterString:
				arena.recordHandOffString(cause, registers.Strings[location.Register], boxed)
				continue
			case isTypedSliceRegisterKind(location.Kind):
				arena.recordHandOffSliceRegister(cause, registers, location.Kind, location.Register, boxed)
				continue
			}
		}
		value := registerToReflectValue(nil, registers, location.Kind, location.Register)
		arena.recordHandOff(cause, value, boxed)
	}
}

// noteNativeScalarArguments notes the scalar arguments the reflect call path built, which
// registerToReflectValue may have boxed in the arena.
//
// Takes arena (*RegisterArena) which notes the hand-offs.
// Takes site (*program.CallSite) which locates the arguments.
// Takes function (reflect.Value) which is the native callee.
// Takes arguments ([]reflect.Value) which are the built arguments, indexed like
// site.Arguments.
func noteNativeScalarArguments(arena *RegisterArena, site *program.CallSite, function reflect.Value, arguments []reflect.Value) {
	if !function.IsValid() || function.Kind() != reflect.Func {
		return
	}
	cause := arena.nativeBorrowClass(function).handOffCause()
	functionType := function.Type()
	for index, location := range site.Arguments {
		if !isScalarRegisterKind(location.Kind) || index >= len(arguments) {
			continue
		}
		boxed := nativeParameterIsInterface(functionType, index+len(site.LinkedTypeArgs), site.IsEllipsisSpread)
		arena.recordHandOff(cause, arguments[index], boxed)
	}
}

// isScalarRegisterKind reports whether kind's registers hold scalars that the reflect
// path may box in the arena.
//
// Takes kind (isa.RegisterKind) which is the argument's register kind.
//
// Returns bool which is true for the int, float, uint, complex and bool banks.
func isScalarRegisterKind(kind isa.RegisterKind) bool {
	switch kind {
	case isa.RegisterInt64, isa.RegisterFloat64, isa.RegisterUint64, isa.RegisterComplex128, isa.RegisterBool:
		return true
	default:
		return false
	}
}

// nativeParameterIsInterface reports whether argument index of a call to a function of
// type functionType lands in an interface-typed parameter.
//
// Takes functionType (reflect.Type) which is the callee's type.
// Takes index (int) which is the argument position.
// Takes spread (bool) which is true when the call spreads a slice into the variadic.
//
// Returns bool which is true for an interface parameter or variadic element.
func nativeParameterIsInterface(functionType reflect.Type, index int, spread bool) bool {
	parameterCount := functionType.NumIn()
	if parameterCount == 0 {
		return false
	}
	if functionType.IsVariadic() && index >= parameterCount-1 {
		variadic := functionType.In(parameterCount - 1)
		if spread {
			return false
		}
		return variadic.Elem().Kind() == reflect.Interface
	}
	if index >= parameterCount {
		return false
	}
	return functionType.In(index).Kind() == reflect.Interface
}

// noteStringCopy notes a copy() into a []string whose backing is not arena-owned.
//
// Takes arena (*RegisterArena) which notes the hand-off.
// Takes copied ([]string) which is the destination's copied prefix.
func noteStringCopy(arena *RegisterArena, copied []string) {
	if len(copied) == 0 || arena.ownsStringBacking(reflect.ValueOf(copied).UnsafePointer()) {
		return
	}
	arena.recordContainerStore(handOffUntracedContainer, reflect.ValueOf(copied))
}

// noteReflectCopy notes a copy() into a reference-carrying slice whose backing is not
// arena-owned.
//
// Takes arena (*RegisterArena) which notes the hand-off.
// Takes destination (reflect.Value) which is the copy's destination slice.
// Takes count (int) which is the number of elements copied.
func noteReflectCopy(arena *RegisterArena, destination reflect.Value, count int) {
	if count == 0 || typeIsPointerFree(destination.Type().Elem()) || arena.ownsSliceBackingOf(destination) {
		return
	}
	arena.recordContainerStore(handOffUntracedContainer, destination.Slice(0, count))
}

// noteGoroutineSpawn notes each materialised argument passed to a new goroutine.
//
// Takes arena (*RegisterArena) which notes the hand-offs.
// Takes closure (reflect.Value) which is the goroutine's function.
// Takes arguments ([]reflect.Value) which are the materialised arguments.
// Takes shared (bool) which is licenceShared as the go statement found it before counting
// the goroutine it starts, which has not yet run.
func noteGoroutineSpawn(arena *RegisterArena, closure reflect.Value, arguments []reflect.Value, shared bool) {
	for _, argument := range arguments {
		arena.recordHandOffShared(handOffSpawn, argument, false, shared)
	}
	compiled, ok := reflect.TypeAssert[*RuntimeClosure](closure)
	if !ok || compiled == nil {
		return
	}
	noteSpawnedClosureObject(arena, compiled)
	for _, cell := range compiled.upvalues {
		if cell != nil {
			arena.recordHandOffShared(handOffSpawn, reflect.ValueOf(cell), false, shared)
		}
	}
}

// noteNativeMethodArguments notes the arguments of a native method call as the reflect
// path built them (receiver excluded).
//
// Takes arena (*RegisterArena) which notes the hand-offs.
// Takes method (reflect.Value) which is the bound native method.
// Takes arguments ([]reflect.Value) which are the built arguments.
// Takes spread (bool) which is true when the call spreads a slice into the variadic.
func noteNativeMethodArguments(arena *RegisterArena, method reflect.Value, arguments []reflect.Value, spread bool) {
	if !method.IsValid() || method.Kind() != reflect.Func {
		return
	}
	cause := arena.nativeBorrowClass(method).handOffCause()
	methodType := method.Type()
	for index, argument := range arguments {
		boxed := nativeParameterIsInterface(methodType, index, spread)
		arena.recordHandOff(cause, argument, boxed)
	}
}

// noteAppendIntoBacking notes references appended to an existing heap or host backing the
// arena does not own.
//
// Takes arena (*RegisterArena) which notes the hand-offs.
// Takes destination (reflect.Value) which is the slice appended to.
// Takes appended (reflect.Value) which is the element, or the spread slice.
// Takes count (int) which is the number of elements appended.
// Takes spread (bool) which is true when appended is a slice whose elements are appended.
func noteAppendIntoBacking(arena *RegisterArena, destination reflect.Value, appended reflect.Value, count int, spread bool) {
	if count == 0 || !appended.IsValid() || !destination.IsValid() || destination.Kind() != reflect.Slice ||
		destination.Len()+count > destination.Cap() {
		return
	}
	elementType := destination.Type().Elem()
	if elementType.Kind() != reflect.String && typeIsPointerFree(elementType) {
		return
	}
	if arena.ownsSliceBackingOf(destination) {
		return
	}
	boxed := elementType.Kind() == reflect.Interface
	if !spread {
		arena.recordHandOff(handOffUntracedContainer, appended, boxed)
		return
	}
	for index := range min(count, appended.Len()) {
		arena.recordHandOff(handOffUntracedContainer, appended.Index(index), boxed)
	}
}
