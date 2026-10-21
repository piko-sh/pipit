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

package program

import "math/bits"

// pcSetWordBits is the number of program counters one word of a PCSet covers.
const pcSetWordBits = 64

// PCSet is a set of program counters, with nil representing an empty set.
type PCSet []uint64

// PCSetOf returns the set holding the given program counters.
//
// Takes pcs (...int) which are the program counters. Negative ones are ignored.
//
// Returns PCSet which is nil when no program counter was kept.
func PCSetOf(pcs ...int) PCSet {
	var set PCSet
	for _, pc := range pcs {
		set = set.With(pc)
	}
	return set
}

// Has reports whether the set holds a program counter.
//
// Takes pc (int) which is the program counter.
//
// Returns bool which is false for one that is negative or beyond the set.
func (s PCSet) Has(pc int) bool {
	word := pc / pcSetWordBits
	return pc >= 0 && word < len(s) && s[word]&(1<<(pc%pcSetWordBits)) != 0
}

// With returns the set with a program counter added, growing it in place when it can.
//
// Takes pc (int) which is the program counter. A negative one is ignored.
//
// Returns PCSet which holds pc and everything the receiver held.
func (s PCSet) With(pc int) PCSet {
	if pc < 0 {
		return s
	}
	word := pc / pcSetWordBits
	if word >= len(s) {
		s = append(s, make(PCSet, word-len(s)+1)...)
	}
	s[word] |= 1 << (pc % pcSetWordBits)
	return s
}

// Count returns the number of program counters in the set.
//
// Returns int which is the count.
func (s PCSet) Count() int {
	count := 0
	for _, word := range s {
		count += bits.OnesCount64(word)
	}
	return count
}

// pcs lists the program counters in the set, ascending.
//
// Returns []int which is nil for an empty set.
func (s PCSet) pcs() []int {
	var pcs []int
	for index, word := range s {
		for word != 0 {
			pcs = append(pcs, index*pcSetWordBits+bits.TrailingZeros64(word))
			word &= word - 1
		}
	}
	return pcs
}
