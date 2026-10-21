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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// optimisticMinMakeBytes is the minimum size of a heap-flagged make backing placed in
	// the arena.
	optimisticMinMakeBytes = 1024

	// optimisticCloneOverhead approximates the per-allocation cost of a string clone in
	// bytes, so that many tiny clones count for more than their length.
	optimisticCloneOverhead = 16
)

// OptimisticRule names one of the optimistic rules; it indexes
// LicenceSnapshot.Optimistic.
type OptimisticRule int

const (
	// OptimisticMake counts heap-flagged makes placed in the arena.
	OptimisticMake OptimisticRule = iota

	// OptimisticPointee counts pointer-free pointees placed in the byte slab.
	OptimisticPointee

	// OptimisticKeptString counts handed-off strings kept in place.
	OptimisticKeptString

	// OptimisticClonedInPlace counts strings cloned inside a []string kept by identity.
	OptimisticClonedInPlace

	// OptimisticGlobalSweepSkipped counts skipped end-of-run sweeps of globals.
	OptimisticGlobalSweepSkipped

	// numOptimisticRules is the number of rules counted.
	numOptimisticRules
)

// optimisticCounts counts the rules' uses while licenceCounting is on.
var optimisticCounts [numOptimisticRules]atomic.Int64

// optimisticCloneBudget returns the per-epoch string-cloning budget, measured as the cost
// of replacing arena's current byte slab.
//
// Takes arena (*RegisterArena) which is the arena.
//
// Returns int which is the budget in bytes.
func optimisticCloneBudget(arena *RegisterArena) int {
	return max(cap(arena.byteSlab), InitialByteSlabSize)
}

// optimisticArena reports whether the optimistic rules apply to arena.
//
// It is licensed, in a build whose probes are exact.
//
// Takes arena (*RegisterArena) which may be nil.
//
// Returns bool which is true when the rules apply.
func optimisticArena(arena *RegisterArena) bool {
	return arenaUsesUnsafeSlabs && licenceExactProbes && arena != nil && arena.licensed
}

// optimisticMake reports whether a make the compiler placed on the heap may take arena
// storage instead.
//
// Takes arena (*RegisterArena) which may be nil.
// Takes root (*program.CompiledFunction) which is the running program's root.
// Takes elementKind (reflect.Kind) which is the element kind.
// Takes elementSize (uintptr) which is the element size in bytes.
// Takes capacity (int) which is the make's capacity.
//
// Returns bool which is true when the backing goes to the arena.
func optimisticMake(arena *RegisterArena, root *program.CompiledFunction, elementKind reflect.Kind, elementSize uintptr, capacity int) bool {
	if !optimisticArena(arena) || elementKind != reflect.String || capacity <= 0 ||
		uintptr(capacity)*elementSize < optimisticMinMakeBytes || root.OptimisticMakeVetoed() {
		return false
	}
	root.NoteOptimisticMakePlaced()
	countOptimistic(OptimisticMake)
	return true
}

// storeArenaTypedSlice is storeHeapTypedSlice's arena twin for a heap-flagged make placed
// in the arena.
//
// Takes vm (*VM) which provides the arena.
// Takes registers (*Registers) which holds the typed-slice banks.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes slot (uint8) which is the destination register.
// Takes length (int) which sizes the slice.
// Takes capacity (int) which sizes the backing.
//
// Returns bool which is false when kind is not a typed-slice bank.
func storeArenaTypedSlice(vm *VM, registers *Registers, kind isa.RegisterKind, slot uint8, length, capacity int) bool {
	arena := vm.Arena
	switch kind {
	case isa.RegisterSliceInt64:
		backing := arena.AllocIntBacking(capacity)
		registers.SlicesInt[slot] = backing[:length:capacity]
	case isa.RegisterSliceFloat64:
		backing := arena.AllocFloatBacking(capacity)
		registers.slicesFloat[slot] = backing[:length:capacity]
	case isa.RegisterSliceString:
		backing := arena.AllocStringBacking(capacity)
		registers.slicesString[slot] = backing[:length:capacity]
	case isa.RegisterSliceBool:
		backing := arena.AllocBoolBacking(capacity)
		registers.slicesBool[slot] = backing[:length:capacity]
	case isa.RegisterSliceUint64:
		backing := arena.AllocUintBacking(capacity)
		registers.slicesUint[slot] = backing[:length:capacity]
	case isa.RegisterSliceByte:
		backing := arena.AllocByteBacking(capacity)
		clear(backing)
		registers.slicesByte[slot] = backing[:length:capacity]
	default:
		return false
	}
	return true
}

// optimisticPointee reports whether a pointee the escape pass did not grant may take the
// arena's byte slab.
//
// A program that calls runtime.SetFinalizer places none, because the finaliser intrinsic
// drops a registration on arena storage, so such a pointee would never be finalised.
//
// Takes arena (*RegisterArena) which may be nil.
// Takes root (*program.CompiledFunction) which is the running program's root, or nil.
// Takes reflectType (reflect.Type) which is the pointee type.
//
// Returns bool which is true when the pointee goes to the arena.
func optimisticPointee(arena *RegisterArena, root *program.CompiledFunction, reflectType reflect.Type) bool {
	if !optimisticArena(arena) || reflectType.Kind() == reflect.Slice || !typeIsPointerFree(reflectType) ||
		uintptr(reflectType.Align()) > arenaMaxAlignment || programSetsFinalisers(root) {
		return false
	}
	countOptimistic(OptimisticPointee)
	return true
}

// materialiseArenaValueIntoSlot is MaterialiseArenaValue for a value about to be written
// into an addressable slot of type slot.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes value (reflect.Value) which is the value about to be stored.
// Takes slot (reflect.Type) which is the destination's type.
//
// Returns reflect.Value which is the value to store.
func materialiseArenaValueIntoSlot(arena *RegisterArena, value reflect.Value, slot reflect.Type) reflect.Value {
	if storesPointerFreeByValue(value, slot) {
		return value
	}
	if !optimisticArena(arena) || !value.IsValid() || slot == nil {
		return MaterialiseArenaValue(arena, value)
	}
	switch {
	case value.Kind() == reflect.Slice && slot.Kind() == reflect.Slice:
		optimisticCloneSliceStrings(arena, value)
		return revokeRemainder(arena, handOffUntracedContainer, value, false)
	case value.Kind() == reflect.String && slot.Kind() == reflect.String:

		if kept, ok := optimisticStringValue(arena, value); ok {
			return kept
		}
	default:
	}
	return MaterialiseArenaValue(arena, value)
}

// materialiseArenaValueUnconditionalIntoSlot is materialiseArenaValueUnconditional, the
// escape copy, for a value about to be written into an addressable slot of type slot.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes value (reflect.Value) which is the value about to be stored.
// Takes slot (reflect.Type) which is the destination's type.
//
// Returns reflect.Value which is the value to store.
func materialiseArenaValueUnconditionalIntoSlot(arena *RegisterArena, value reflect.Value, slot reflect.Type) reflect.Value {
	if storesPointerFreeByValue(value, slot) {
		return value
	}
	if !optimisticArena(arena) || !value.IsValid() || slot == nil {
		return materialiseArenaValueUnconditional(arena, value)
	}
	switch {
	case value.Kind() == reflect.Slice && slot.Kind() == reflect.Slice:
		if !arena.ownsSliceBackingOf(value) {
			noteHandOff(arena, handOffHeapPassthrough, value)
			return value
		}
		optimisticCloneSliceStrings(arena, value)
		noteHandOff(arena, handOffIdentityKept, value)
		return value
	case value.Kind() == reflect.String && slot.Kind() == reflect.String:
		if kept, ok := optimisticStringValue(arena, value); ok {
			return kept
		}
	default:
	}
	return materialiseArenaValueUnconditional(arena, value)
}

// optimisticKeepsString reports whether an own arena string should remain in place
// instead of being cloned during a hand-off or outside store.
//
// Takes arena (*RegisterArena) which may be nil.
// Takes text (string) which is the string.
//
// Returns bool which is true when text is kept.
func optimisticKeepsString(arena *RegisterArena, text string) bool {
	if !optimisticArena(arena) || !arena.OwnsString(text) {
		return false
	}
	if arena.licenceState.epochRevoked || optimisticByteSlabRevoked(arena, text) {
		return true
	}
	cost := len(text) + optimisticCloneOverhead
	if arena.optimisticClonedBytes+cost <= optimisticCloneBudget(arena) {
		arena.optimisticClonedBytes += cost
		return false
	}
	return true
}

// materialiseStringOptimistic is materialiseStringUnconditional with the string clone
// budget.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes text (string) which is the string about to leave the arena.
//
// Returns string which is text kept, or a clone.
func materialiseStringOptimistic(arena *RegisterArena, text string) string {
	if optimisticKeepsString(arena, text) {
		countOptimistic(OptimisticKeptString)
		noteKeptString(arena, text)
		return text
	}
	return cloneArenaString(arena, text)
}

// optimisticStringValue applies the string clone budget to a string-kinded reflect.Value
// at an escape copy.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes value (reflect.Value) which is string-kinded.
//
// Returns reflect.Value which is value when kept.
// Returns bool which is true then.
func optimisticStringValue(arena *RegisterArena, value reflect.Value) (reflect.Value, bool) {
	text := value.String()
	if !optimisticKeepsString(arena, text) {
		return reflect.Value{}, false
	}
	countOptimistic(OptimisticKeptString)
	noteKeptString(arena, text)
	return value, true
}

// rebox returns v, a handed-off string kept in place, re-boxed on the heap when its own
// storage is a slot of the arena's string box slab, which Reset clears (the bytes stay
// where they are).
//
// Takes arena (*RegisterArena) which owns the box slab.
// Takes v (reflect.Value) which is string-kinded.
// Takes s (string) which is v's string.
//
// Returns reflect.Value which does not live in the box slab.
func rebox(arena *RegisterArena, v reflect.Value, s string) reflect.Value {
	if arena.OwnsStringBox(ReflectValuePtr(v)) {
		return reflect.ValueOf(s).Convert(v.Type())
	}
	return v
}

// countOptimistic counts one use of rule when counting is on.
//
// Takes rule (OptimisticRule) which is the rule used.
func countOptimistic(rule OptimisticRule) {
	if licenceCounting {
		optimisticCounts[rule].Add(1)
	}
}

// optimisticStats returns the rules' use counts since the process started, or since the
// last ResetOptimisticStats; all zero unless SetLicenceCountingForTest turned counting
// on.
//
// Returns [numOptimisticRules]int64 which is indexed by OptimisticRule.
func optimisticStats() [numOptimisticRules]int64 {
	var counts [numOptimisticRules]int64
	for rule := range numOptimisticRules {
		counts[rule] = optimisticCounts[rule].Load()
	}
	return counts
}

// ResetOptimisticStats zeroes the counts.
func ResetOptimisticStats() {
	for rule := range numOptimisticRules {
		optimisticCounts[rule].Store(0)
	}
}

// optimisticFieldString applies the string clone budget to a string field of a composite
// being escape-copied.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes field (reflect.Value) which is string-kinded.
//
// Returns string which is the clone.
// Returns bool which is true when the field must change.
func optimisticFieldString(arena *RegisterArena, field reflect.Value) (string, bool) {
	text := field.String()
	if !arena.OwnsString(text) {
		return "", false
	}
	if optimisticKeepsString(arena, text) {
		countOptimistic(OptimisticKeptString)
		noteKeptString(arena, text)
		return "", false
	}
	return strings.Clone(text), true
}

// setReboundString writes s into an addressable string field or element, rebinding
// through unsafeNewAt so an unexported field's flagRO bit does not block the write.
//
// Takes destination (reflect.Value) which is the field or element.
// Takes s (string) which is the replacement.
func setReboundString(destination reflect.Value, s string) {
	if destination.CanAddr() {
		destination = unsafeNewAt(reflectValueABIType(destination.Type()), ReflectValuePtr(destination), destination.Kind())
	}
	destination.SetString(s)
}

// storesPointerFreeByValue reports whether value can be written into a slot of type slot
// as it is, with no escape copy.
//
// Takes value (reflect.Value) which is the value about to be stored.
// Takes slot (reflect.Type) which is the destination's type; nil answers false.
//
// Returns bool which is true when the barrier's copy would be an intermediate.
func storesPointerFreeByValue(value reflect.Value, slot reflect.Type) bool {
	if !value.IsValid() || slot == nil {
		return false
	}

	switch k := value.Kind(); {
	case k >= reflect.Bool && k <= reflect.Complex128, k == reflect.Array, k == reflect.Struct:
	default:
		return false
	}
	return slot.Kind() != reflect.Interface && value.Type() == slot && typeIsPointerFree(slot)
}
