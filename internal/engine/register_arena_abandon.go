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

// putRegisterArenaAbandoningEscapes returns a callback or bound-method VM's arena to the
// pool.
//
// Takes a (*RegisterArena) which may be nil.
func putRegisterArenaAbandoningEscapes(a *RegisterArena) {
	if a == nil {
		return
	}
	if a.holdsEscapableData() && !a.reentrantHandedOff {
		a.revokeEpoch(epochReentrant, noHandOffCause)
	}
	PutRegisterArena(a)
}

// abandonBoxSlabs drops the scalar box slabs. A fresh arena has none, so they are left
// nil and allocated again on first use.
func (a *RegisterArena) abandonBoxSlabs() {
	a.stringBoxSlab = nil
	a.stringBoxIndex = 0
	a.intBoxSlab = nil
	a.intBoxIndex = 0
	a.floatBoxSlab = nil
	a.floatBoxIndex = 0
	a.uintBoxSlab = nil
	a.uintBoxIndex = 0
	a.complexBoxSlab = nil
	a.complexBoxIndex = 0
}

// holdsEscapableBytes reports whether the arena used any byte storage since the last
// reset: string bytes, mutable bytes, generic bytes or slice headers, current or retired.
//
// Returns bool which is true when it did.
func (a *RegisterArena) holdsEscapableBytes() bool {
	return a.byteIndex != 0 || len(a.oldByteSlabs) != 0 ||
		a.mutByteIndex != 0 || len(a.oldMutByteSlabs) != 0 ||
		a.slabRetiredSinceReset != noSlabUsage ||
		a.genericBytesIndex != 0 || a.sliceHeaderIndex != 0 || len(a.oldSliceHeaderSlabs) != 0
}

// holdsEscapableBackings reports whether the arena used any typed backing array, scalar
// box or upvalue cell since the last reset.
//
// Returns bool which is true when it did.
func (a *RegisterArena) holdsEscapableBackings() bool {
	return a.intBackingIndex != 0 || a.floatBackingIndex != 0 || a.stringBackingIndex != 0 ||
		a.boolBackingIndex != 0 || a.uintBackingIndex != 0 ||
		a.stringBoxIndex != 0 || a.intBoxIndex != 0 || a.floatBoxIndex != 0 ||
		a.uintBoxIndex != 0 || a.complexBoxIndex != 0 ||
		a.UpvalueCellIndex != 0 || a.UpvalueReferenceIndex != 0
}
