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

import "piko.sh/safeconv"

// gcVisitSet is the licensed mark's set of traced referents (firstVisit).
type gcVisitSet struct {
	// slots holds the keys, a power of two of them, or none before the first insert. A slot
	// whose key has a nil pointer is empty, since the mark never visits nil.
	slots []gcVisitKey

	// count is the number of keys held.
	count int
}

// insert adds key to the set.
//
// Takes key (gcVisitKey) which is the referent, whose pointer is not nil.
//
// Returns bool which is true when key was not already held.
func (set *gcVisitSet) insert(key gcVisitKey) bool {
	if (set.count+1)*gcVisitSetLoadDenominator > len(set.slots)*gcVisitSetLoadNumerator {
		set.grow()
	}
	mask := len(set.slots) - 1
	for index := gcVisitHash(key) & mask; ; index = (index + 1) & mask {
		slot := &set.slots[index]
		if slot.pointer == nil {
			*slot = key
			set.count++
			return true
		}
		if *slot == key {
			return false
		}
	}
}

// contains reports whether the set holds key.
//
// Takes key (gcVisitKey) which is the referent.
//
// Returns bool which is true when it does.
func (set *gcVisitSet) contains(key gcVisitKey) bool {
	if set.count == 0 {
		return false
	}
	mask := len(set.slots) - 1
	for index := gcVisitHash(key) & mask; ; index = (index + 1) & mask {
		slot := &set.slots[index]
		if slot.pointer == nil {
			return false
		}
		if *slot == key {
			return true
		}
	}
}

// grow doubles the table, or makes it at gcVisitedInitialSize slots, and re-inserts every
// key.
func (set *gcVisitSet) grow() {
	previous := set.slots
	set.slots = make([]gcVisitKey, max(2*len(previous), gcVisitedInitialSize))
	set.count = 0
	for index := range previous {
		if previous[index].pointer != nil {
			set.insert(previous[index])
		}
	}
}

// reset forgets every key, so the set keeps no referent reachable, and drops the table
// once it has grown past keptSlots slots.
//
// Takes keptSlots (int) which is the largest table kept.
func (set *gcVisitSet) reset(keptSlots int) {
	if len(set.slots) > keptSlots {
		set.slots = nil
	} else if set.count != 0 {
		clear(set.slots)
	}
	set.count = 0
}

// gcVisitHash spreads a key's address and extent over the word, by Fibonacci hashing.
// Keys that differ only in their type share a hash and are told apart by probing.
//
// Takes key (gcVisitKey) which is the referent.
//
// Returns int which is the hash, non-negative.
func gcVisitHash(key gcVisitKey) int {
	hash := (uint64(uintptr(key.pointer)) ^ safeconv.Int64ToUint64Reinterpret(int64(key.extent))<<gcVisitExtentShift) * fibonacciHashMultiplier
	return int(hash >> gcVisitHashDropBits)
}
