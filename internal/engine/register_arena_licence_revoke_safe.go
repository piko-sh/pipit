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

// walkHandOff is revokeValue's walk. This build has no walk: revokeWithoutWalk settles
// every revoking hand-off before one would start, and a hand-off that reaches here is
// settled the same way, by revoking the epoch and counting it blind.
//
// Takes cause (HandOffCause) which is the hand-off's cause.
func (a *RegisterArena) walkHandOff(_ reflect.Value, _ bool, cause HandOffCause, _ bool) {
	a.revokeEpoch(epochInexactProbe, cause)
	a.revokeBlind()
}

// revokeTrivially settles nothing in this build; a hand-off that reaches it goes on to
// walkHandOff, which revokes the epoch.
//
// Returns bool which is false.
func (*RegisterArena) revokeTrivially(_ reflect.Value, _ bool) bool {
	return false
}

// licenceStringNamesNothing reports whether a hand-off of s names no storage. This
// build's probes cannot tell on which page a string's bytes lie, so only the empty string
// names nothing.
//
// Takes s (string) which is the string handed off.
//
// Returns bool which is true when s is empty.
func licenceStringNamesNothing(_ *RegisterArena, s string) bool {
	return len(s) == 0
}

// licenceCoveredAddress reports whether address, the destination of a store, is covered
// storage of this arena: one of its data slabs that the licence revoked, or that a
// pending foreign revocation or a skipped walk since the last reading might have named.
//
// Takes arena (*RegisterArena) which is the arena.
// Takes address (uintptr) which is the destination's address.
//
// Returns bool which is true when the destination is covered storage of this arena.
func licenceCoveredAddress(arena *RegisterArena, address uintptr) bool {
	if !arena.licensed || address == 0 {
		return false
	}
	if arena.licenceCoverEmpty() || !licenceRegisteredPage(arena, address) {
		return false
	}
	base, found := licenceDataSlabOf(arena, address)
	if !found {
		return false
	}
	return arena.licenceCoveredSlab(base)
}

// licenceRevokedSlab is licenceRevokedBase for a slab value.
//
// Takes a (*RegisterArena) which is the arena whose revocation state is checked.
// Takes slab ([]T) which is the slab.
//
// Returns bool which is true for a revoked slab or a revoked epoch.
func licenceRevokedSlab[T any](a *RegisterArena, slab []T) bool {
	if cap(slab) == 0 {
		return false
	}
	return a.licenceRevokedBase(licenceSlabAddress(slab))
}

// adoptForeignRevocationsLocked has nothing to adopt in this build: without a walk, no
// arena records a revocation against another's storage.
func (*RegisterArena) adoptForeignRevocationsLocked() {}
