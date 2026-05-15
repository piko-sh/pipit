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

//go:build pipit_arena_paranoid

package engine

import (
	"math"
)

const (
	// ArenaPoisonInt is arenaPoisonWord as a signed int64 value.
	ArenaPoisonInt int64 = -2459565876494606883

	// ArenaPoisonByte is the canary byte for released generic-byte slab storage.
	ArenaPoisonByte byte = 0xDD

	// arenaParanoidEnabled gates the arena poisoning diagnostics.
	arenaParanoidEnabled = true

	// arenaPoisonWord is the canary bit pattern for released slabs.
	arenaPoisonWord uint64 = 0xDDDDDDDDDDDDDDDD
)

// poisonReleasedGenericBytes overwrites the generic byte slab and every retired
// generation with canary bytes. Must run before Reset zeroes genericBytesIndex, because
// Reset only zeroes the index without clearing the slab, so stale reads would silently
// return plausible data.
func (a *RegisterArena) poisonReleasedGenericBytes() {
	if a.genericBytesIndex > 0 {
		poisonBytes(a.genericBytesSlab[:a.genericBytesIndex])
	}
	for _, old := range a.oldGenericByteSlabs {
		poisonBytes(old)
	}
}

// poisonReleasedPrimitives overwrites released primitive-slab ranges with canary values.
//
// Must run before the allocation indices roll back, while both the save point and the
// current indices are visible. The pointer-bearing slabs are untouched because Restore()
// already zeroes those for GC correctness; the generic byte slab is covered separately by
// poisonReleasedGenericBytes at Reset, because it is not rolled back per frame.
//
// Takes sp (*ArenaSavePoint) which is the save point the arena is about to roll back to.
func (a *RegisterArena) poisonReleasedPrimitives(sp *ArenaSavePoint) {
	for i := sp.IntIndex; i < a.IntIndex; i++ {
		a.intSlab[i] = ArenaPoisonInt
	}
	for i := sp.FloatIndex; i < a.FloatIndex; i++ {
		a.floatSlab[i] = math.Float64frombits(arenaPoisonWord)
	}
	for i := sp.BoolIndex; i < a.BoolIndex; i++ {
		a.boolSlab[i] = true
	}
	for i := sp.UintIndex; i < a.UintIndex; i++ {
		a.uintSlab[i] = arenaPoisonWord
	}
	for i := sp.ComplexIndex; i < a.ComplexIndex; i++ {
		a.complexSlab[i] = complex(math.Float64frombits(arenaPoisonWord), math.Float64frombits(arenaPoisonWord))
	}

}

// poisonBytes fills b with the canary byte.
//
// Takes b ([]byte) which is the range to poison.
func poisonBytes(b []byte) {
	for i := range b {
		b[i] = ArenaPoisonByte
	}
}
