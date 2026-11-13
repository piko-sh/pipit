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
	"slices"
	"strings"
	"unsafe"
)

var (
	// byteSliceType is the reflect type of []byte.
	byteSliceType = reflect.TypeFor[[]byte]()
)

// ownUncoveredStringSlot reports whether pointer is in the arena's own data storage,
// uncovered, and the arena keeps foreign strings there (keepsForeignStrings), so a
// foreign string stored there needs no clone.
//
// Takes pointer (unsafe.Pointer) which is the destination.
//
// Returns bool which is true when the clone is skipped.
func (a *RegisterArena) ownUncoveredStringSlot(pointer unsafe.Pointer) bool {
	if !a.keepsForeignStrings() || !ownUncoveredStorage(a, pointer) {
		return false
	}
	if licenceCounting {
		foreignStringsKept.Add(1)
	}
	if a.callbackRoot != nil {
		a.callbackRoot.NoteCallbackForeignClone()
	}
	return true
}

// noteBytePlacement records that the site the frame is executing placed a result whose
// storage starts at pointer.
//
// Takes arena (*RegisterArena) which placed it.
// Takes frame (*CallFrame) which is executing the site.
// Takes pointer (unsafe.Pointer) which is the result's backing.
func noteBytePlacement(arena *RegisterArena, frame *CallFrame, pointer unsafe.Pointer) {
	base, found := licenceDataSlabOf(arena, uintptr(pointer))
	if !found {
		return
	}
	site := bytePlacementSite{function: frame.Function, programCounter: frame.ProgramCounter - 1, base: base}
	if slices.Contains(arena.bytePlacementSites, site) {
		return
	}
	arena.bytePlacementSites = append(arena.bytePlacementSites, site)
}

// placeStringToBytes builds []byte(text) in the arena's mutable byte slab, its header in
// the slice-header slab. Callers use it only when the arena splits its bytes
// (splitsMutableBytes).
//
// Takes arena (*RegisterArena) which provides the slabs.
// Takes frame (*CallFrame) which is executing the site.
// Takes text (string) which is converted.
//
// Returns reflect.Value which is the []byte, of length and capacity len(text).
func placeStringToBytes(arena *RegisterArena, frame *CallFrame, text string) reflect.Value {
	backing := arena.AllocByteBacking(len(text))
	copy(backing, text)
	if len(text) != 0 {
		noteBytePlacement(arena, frame, unsafe.Pointer(unsafe.SliceData(backing)))
	}
	return arenaWrapMakeBacking(arena, backing, len(text), len(text), byteSliceType)
}

// placeSplit builds strings.Split(text, sep) in the arena's string backing slab, its
// header in the slice-header slab, for a non-empty separator.
//
// Takes arena (*RegisterArena) which provides the slabs.
// Takes frame (*CallFrame) which is executing the site.
// Takes text (string) which is split.
// Takes sep (string) which is the separator, not empty.
//
// Returns reflect.Value which is the []string, of length and capacity the number of
// parts.
func placeSplit(arena *RegisterArena, frame *CallFrame, text, sep string) reflect.Value {
	count := strings.Count(text, sep) + 1
	backing := arena.AllocStringBacking(count)
	i := 0
	for i < count-1 {
		index := strings.Index(text, sep)
		if index < 0 {
			break
		}
		backing[i] = text[:index]
		text = text[index+len(sep):]
		i++
	}
	backing[i] = text
	noteBytePlacement(arena, frame, unsafe.Pointer(unsafe.SliceData(backing)))
	return arenaWrapMakeBacking(arena, backing, i+1, count, stringSliceType)
}

// stringOverMutBytes reports whether s's bytes lie in the arena's mutable byte slabs,
// which must never happen. The census and tests use it.
//
// Takes s (string) which is the string.
//
// Returns bool which is true for a string over mutable bytes.
func (a *RegisterArena) stringOverMutBytes(s string) bool {
	return len(s) != 0 && a.ownsMutBytes(uintptr(unsafe.Pointer(unsafe.StringData(s))))
}

// copiesNativeString reports whether text can be copied as a native argument.
//
// Takes text (string) which is the argument.
//
// Returns bool which is true when the caller passes a heap copy of text.
func (a *RegisterArena) copiesNativeString(text string) bool {
	if len(text) == 0 || !a.OwnsString(text) {
		return false
	}
	state := &a.licenceState
	if len(text) > natCopyMaxBytes || state.epochRevoked {
		countNativeCopyDeclined()
		return false
	}
	if base, _, ok := licenceListSpan(a.byteSlab, a.oldByteSlabs, uintptr(unsafe.Pointer(unsafe.StringData(text)))); ok {
		if _, revoked := state.revoked[base]; revoked {
			countNativeCopyDeclined()
			return false
		}
	}
	cost := len(text) + optimisticCloneOverhead
	if a.optimisticClonedBytes+cost > optimisticCloneBudget(a) {
		countNativeCopyDeclined()
		return false
	}
	a.optimisticClonedBytes += cost
	if licenceCounting {
		nativeCopyCounters.copied.Add(1)
		nativeCopyCounters.copiedBytes.Add(int64(len(text)))
	}
	return true
}

// probeStringInRevokedEpoch probes a string handed off in a revoked epoch.
//
// Takes text (string) which is the string handed off.
// Takes cause (HandOffCause) which is the hand-off's cause.
func (a *RegisterArena) probeStringInRevokedEpoch(text string, cause HandOffCause) {
	if licenceStringNamesNothing(a, text) {
		return
	}
	pointer := uintptr(unsafe.Pointer(unsafe.StringData(text)))
	if _, own := licenceDataSlabOf(a, pointer); own {
		return
	}
	a.revokeNonDataAddress(pointer, cause)
}

// keepsForeignStrings reports whether a foreign string can remain uncloned in own
// uncovered storage.
//
// Returns bool which is true when the clone is skipped.
func (a *RegisterArena) keepsForeignStrings() bool {
	return arenaUsesUnsafeSlabs && licenceExactProbes && a != nil && a.licensed && a.cloneUnownedStrings
}

// countNativeCopyDeclined counts a string left to the revoking path while the licence
// counters are on, so the process-wide counter costs nothing otherwise.
func countNativeCopyDeclined() {
	if licenceCounting {
		nativeCopyCounters.declined.Add(1)
	}
}
