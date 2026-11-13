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

	"pipit.sh/pipit/internal/engine/program"
)

// enteredInline is the number of entry points a liveness walk keeps without allocating.
const enteredInline = 8

// reachedWords is the set of words a walk has reached, counted as it grows.
type reachedWords struct {
	// set holds the words.
	set program.PCSet

	// count is the number of words in set.
	count int
}

// has reports whether the walk has reached pc. A nil set holds nothing.
//
// Takes pc (int) which is the word.
//
// Returns bool which is true when pc was added.
func (reached *reachedWords) has(pc int) bool {
	return reached != nil && reached.set.Has(pc)
}

// add records that the walk has reached pc.
//
// Takes pc (int) which is the word.
func (reached *reachedWords) add(pc int) {
	if pc < 0 || reached.set.Has(pc) {
		return
	}
	reached.set = reached.set.With(pc)
	reached.count++
}

// enteredSet is the set of entry points a liveness walk has taken.
type enteredSet struct {
	// more holds the entries past the inline ones.
	more program.PCSet

	// few holds the first entries.
	few [enteredInline]int

	// count is the number of entries in few.
	count int
}

// enter records pc as entered.
//
// Takes pc (int) which is the entry point.
//
// Returns bool which is false when pc had been entered before.
func (entered *enteredSet) enter(pc int) bool {
	if slices.Contains(entered.few[:entered.count], pc) {
		return false
	}
	if entered.count < enteredInline {
		entered.few[entered.count] = pc
		entered.count++
		return true
	}
	if entered.more.Has(pc) {
		return false
	}
	entered.more = entered.more.With(pc)
	return true
}
