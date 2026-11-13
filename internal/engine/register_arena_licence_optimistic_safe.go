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

//go:build safe || (js && wasm)

package engine

import "reflect"

// noteKeptString notes a handed-off string kept by identity. This build's probes cannot
// name the string's byte slab, so the note revokes the epoch (revokeWithoutWalk).
//
// Takes arena (*RegisterArena) which is licensed and owns the string.
// Takes text (string) which is the string kept.
func noteKeptString(arena *RegisterArena, text string) {
	const cause = handOffIdentityKept
	if abortInjectBuilt {
		injectHandOffAbort(arena)
	}
	if storeCensusBuilt {
		censusNoteHandOff(arena, cause, reflect.ValueOf(text))
	}
	if !arena.licensed || !licenceRevokes(cause) {
		return
	}
	arena.licenceState.noteSeq++
	if licenceCounting {
		arena.stats().handOffs[cause].Add(1)
	}
	arena.revokeWithoutWalk(cause)
}

// optimisticOwnValueStore reports whether a store of v into destination needs no barrier;
// in this build every store takes its barrier.
//
// Returns bool which is false.
func optimisticOwnValueStore(_ *RegisterArena, _, _ reflect.Value) bool {
	return false
}

// optimisticCloneSliceStrings does nothing in this build, which has no optimistic rules.
func optimisticCloneSliceStrings(_ *RegisterArena, _ reflect.Value) {}

// optimisticByteSlabRevoked reports no revoked byte slab in this build, whose probes
// cannot name the slab a string lies in.
//
// Returns bool which is false.
func optimisticByteSlabRevoked(_ *RegisterArena, _ string) bool {
	return false
}

// optimisticNew is reflect.New in this build, which places nothing in the arena's byte
// slab for it.
//
// Takes reflectType (reflect.Type) which is the pointee's type.
//
// Returns reflect.Value which is a pointer to a zeroed heap value.
func optimisticNew(_ *RegisterArena, reflectType reflect.Type) reflect.Value {
	return reflect.New(reflectType)
}

// optimisticStringSlotStore makes no store in this build, which has no optimistic rules;
// the caller takes its barrier.
//
// Returns bool which is false.
func optimisticStringSlotStore(_ *RegisterArena, _, _ reflect.Value) bool {
	return false
}
