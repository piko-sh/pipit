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

import "pipit.sh/pipit/internal/engine/program"

// putRegisterArenaAbandoningEscapes returns a callback VM's arena to the pool after
// replacing every slab whose values may have escaped into the parent VM, leaving replaced
// slabs to the collector.
//
// Takes a (*RegisterArena) which may be nil.
func putRegisterArenaAbandoningEscapes(a *RegisterArena) {
	if a == nil {
		return
	}
	if a.holdsEscapableData() {
		a.abandonEscapedSlabs()
	}
	PutRegisterArena(a)
}

// abandonEscapedSlabs replaces every slab that may be referenced by an escaped value with
// a fresh one, so Reset never touches memory a live value still reads.
func (a *RegisterArena) abandonEscapedSlabs() {
	a.byteSlab = make([]byte, InitialByteSlabSize)
	a.oldByteSlabs = nil
	a.byteIndex = 0
	a.genericBytesSlab = nil
	a.oldGenericByteSlabs = nil
	a.genericBytesIndex = 0
	a.freeGenericByteChunks = nil
	a.sliceHeaderSlab = make([]arenaSliceHeader, initialSliceHeaderCapacity)
	a.oldSliceHeaderSlabs = nil
	a.sliceHeaderIndex = 0
	a.freeSliceHeaderChunks = nil
	a.abandonBackingSlabs()
	a.abandonBoxSlabs()
	a.upvalueCellSlab = make([]program.UpvalueCell, initialUpvalueCellSlabs)
	a.UpvalueCellIndex = 0
	a.upvalueReferenceSlab = make([]upvalue, initialUpvalueRefSlabs)
	a.UpvalueReferenceIndex = 0
	a.slabRetiredSinceReset = noSlabUsage
	a.slabGeneration++
}

// abandonBackingSlabs replaces the typed-slice backing slabs, current and retired, with
// fresh ones at their initial sizes.
func (a *RegisterArena) abandonBackingSlabs() {
	a.intBackingSlab = make([]int64, InitialIntBackingSize)
	a.oldIntBackings = nil
	a.intBackingIndex = 0
	a.floatBackingSlab = make([]float64, initialFloatBackingSize)
	a.oldFloatBackings = nil
	a.floatBackingIndex = 0
	a.stringBackingSlab = make([]string, initialStringBackingSize)
	a.oldStringBackings = nil
	a.stringBackingIndex = 0
	a.boolBackingSlab = make([]bool, initialBoolBackingSize)
	a.oldBoolBackings = nil
	a.boolBackingIndex = 0
	a.uintBackingSlab = make([]uint64, initialUintBackingSize)
	a.oldUintBackings = nil
	a.uintBackingIndex = 0
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
