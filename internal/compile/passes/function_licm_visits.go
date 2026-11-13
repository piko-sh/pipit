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

package passes

import (
	"slices"

	"pipit.sh/pipit/internal/isa"
)

// maxHoistedLoopWords caps the number of words in a loop searched for hoists.
const maxHoistedLoopWords = 4096

// loopVisits records the loops a hoisting family has finished searching.
//
// A loop is named by its latch, the one word of a loop that no hoist out of it moves.
type loopVisits struct {
	// latches holds the latch of each finished loop.
	latches []int
}

// finished reports whether the family has already searched loop and found nothing left.
//
// Takes loop (loopRange) which is the loop in question.
//
// Returns bool which is true when the loop needs no further search.
func (visits *loopVisits) finished(loop loopRange) bool {
	return slices.Contains(visits.latches, loop.latch)
}

// finish records that a search of loop found nothing to lift or rename.
//
// Takes loop (loopRange) which is the loop searched.
func (visits *loopVisits) finish(loop loopRange) {
	visits.latches = append(visits.latches, loop.latch)
}

// lifted moves the recorded latches as a hoist of width words from readPC to header moves
// them, which is up by width for a latch between the two.
//
// Takes header (int) which is the PC the words were lifted to.
// Takes readPC (int) which is the PC they were lifted from.
// Takes width (int) which is the number of words lifted.
func (visits *loopVisits) lifted(header, readPC, width int) {
	for index, latch := range visits.latches {
		if latch >= header && latch < readPC {
			visits.latches[index] = latch + width
		}
	}
}

// loopOpenToHoisting reports whether a hoisting family searches loop, which it does when
// the loop is entered by falling into its header and is no longer than
// maxHoistedLoopWords.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes loop (loopRange) which is the loop.
//
// Returns bool which is true when the loop is searched.
func loopOpenToHoisting(body []isa.Instruction, loop loopRange) bool {
	return loop.latch-loop.header <= maxHoistedLoopWords && loopHeaderReachableByFallThrough(body, loop)
}
