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

import "unsafe"

// revokeByteSlabsFrom is revokeByteSlabs, leaving the current slab alone when current is
// false.
//
// Takes current (bool) which is false when the current slab need not be revoked.
func (a *RegisterArena) revokeByteSlabsFrom(current bool) {
	revoke := func(slab []byte) {
		if cap(slab) != 0 {
			a.revokeSlab(uintptr(unsafe.Pointer(unsafe.SliceData(slab))), noHandOffCause)
		}
	}
	if current {
		revoke(a.byteSlab)
	}
	for _, slab := range a.oldByteSlabs {
		revoke(slab)
	}
}

// foreignLocal reports whether the licence omits foreign revocations because the arena's
// VM serves no native call.
//
// Returns bool which is true when a foreign revocation is not recorded.
func (a *RegisterArena) foreignLocal() bool {
	if !a.licensed {
		return false
	}
	return a.servesNoNativeCall()
}

// revokeBlindBytes stands in for a walk that skipped only strings in memory another
// goroutine may be writing.
func (a *RegisterArena) revokeBlindBytes() {
	a.revokeByteSlabsFrom(a.byteIndex != 0)
	if a.blindLocal() {
		if licenceCounting {
			blindScopeCounters.localByteBlind.Add(1)
			a.stats().blindByteHandOffs.Add(1)
		}
		return
	}
	if !a.licenceState.foreignLocked {
		licenceSealMu.RLock()
		defer licenceSealMu.RUnlock()
	}
	step := uint64(0)
	if a.blindCounters().bytes.bump() {
		step++
	}
	global, chain := a.blindCallbackScope(a.servesNativeCaller())
	if global && licenceBlindCallbacks.bytes.bump() {
		step++
	}
	if len(chain) != 0 {
		bumpServedChain(chain, true)
	}
	a.advanceOwnBlindMark(step, true)
	if licenceCounting {
		a.stats().blindByteHandOffs.Add(1)
	}
}
