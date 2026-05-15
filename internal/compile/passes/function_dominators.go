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

import "pipit.sh/pipit/internal/isa"

const (
	// bitsPerWord is the number of bits in a uint64 dominator bitset word.
	bitsPerWord = 64

	// maxDominatorIterations caps the iterative dataflow loop used by
	// ComputeFunctionDominators. The bitset lattice converges quickly for real CFGs; the cap
	// protects against pathological non-convergence.
	maxDominatorIterations = 64
)

// functionDominators holds per-PC dominator bitsets packed into []uint64. Unreachable PCs
// have a nil bitset, so Dominates returns false for dead code.
type functionDominators struct {
	// bits holds the per-PC dominator bitsets, one []uint64 row per PC.
	bits [][]uint64

	// wordsPerRow is the number of uint64 words needed to represent all PCs.
	wordsPerRow int

	// pcCount is the total number of PCs in the function body.
	pcCount int
}

// Dominates reports whether definePC dominates usePC, meaning every path from the
// function entry to usePC passes through definePC.
//
// Takes definePC (int) which is the program counter of the candidate dominator
// definition.
// Takes usePC (int) which is the program counter of the use under test.
//
// Returns false for unreachable PCs (nil bitset) and for PCs outside the function's body
// length.
func (dom *functionDominators) Dominates(definePC, usePC int) bool {
	if dom == nil {
		return false
	}
	if usePC < 0 || usePC >= len(dom.bits) {
		return false
	}
	row := dom.bits[usePC]
	if row == nil {
		return false
	}
	if definePC < 0 || definePC >= dom.pcCount {
		return false
	}
	word := definePC / bitsPerWord
	bit := uint(definePC % bitsPerWord)
	return row[word]&(1<<bit) != 0
}

// ComputeFunctionDominators returns the dominator table for body using iterative
// dataflow. Returns nil when body is empty or the dataflow does not converge within
// maxDominatorIterations, so callers fall back to performing no rewrites rather than
// risking an unsound dominance answer.
//
// Takes body ([]instruction) which is the instruction stream.
//
// Returns the populated dominator table, or nil.
//
//revive:disable-next-line:unexported-return // package-internal type stays unexported.
func ComputeFunctionDominators(body []isa.Instruction) *functionDominators {
	if len(body) == 0 {
		return nil
	}
	return computeFunctionDominatorsFrom(body, buildAliasPredecessors(body), computeReachable(body, len(body)))
}

// computeFunctionDominatorsFrom runs the dominator dataflow over body using predecessor
// lists and a reachability mask the caller already holds, so FunctionAnalysis can reuse
// its eager members rather than rebuilding them.
//
// Takes body ([]instruction) which is the linear instruction stream of the function under
// analysis.
// Takes predecessors ([][]int) which is the per-PC predecessor list from
// buildAliasPredecessors.
// Takes reachable ([]bool) which is the per-PC reachability mask from computeReachable.
//
// Returns the populated dominator table, or nil when body is empty or the iterative
// dataflow did not converge within maxDominatorIterations.
func computeFunctionDominatorsFrom(body []isa.Instruction, predecessors [][]int, reachable []bool) *functionDominators {
	if len(body) == 0 {
		return nil
	}
	n := len(body)
	dom := initialiseDominatorTable(n)
	seedReachableDominators(dom, reachable)
	scratch := make([]uint64, dom.wordsPerRow)
	converged := false
	for range maxDominatorIterations {
		if !relaxDominatorsOnce(dom, predecessors, reachable, scratch) {
			converged = true
			break
		}
	}
	if !converged {
		return nil
	}
	return dom
}

// initialiseDominatorTable allocates the dominator structure and seeds the entry PC's
// bitset to {0}.
//
// Takes n (int) which is the number of PCs in the function body.
//
// Returns a dominator table with the entry row populated and all other rows nil.
func initialiseDominatorTable(n int) *functionDominators {
	wordsPerRow := (n + bitsPerWord - 1) / bitsPerWord
	dom := new(functionDominators{
		bits:        make([][]uint64, n),
		wordsPerRow: wordsPerRow,
		pcCount:     n,
	})
	dom.bits[0] = make([]uint64, wordsPerRow)
	dom.bits[0][0] = 1
	return dom
}

// seedReachableDominators fills every reachable non-entry PC's bitset with the universal
// "every PC dominates" starting state.
//
// Takes dom (*FunctionDominators) which is the dominator table whose rows are being
// seeded.
// Takes reachable ([]bool) which is the per-PC reachability mask from the entry.
func seedReachableDominators(dom *functionDominators, reachable []bool) {
	universe := make([]uint64, dom.wordsPerRow)
	for index := range universe {
		universe[index] = ^uint64(0)
	}
	if tailBits := dom.pcCount % bitsPerWord; tailBits != 0 {
		universe[len(universe)-1] = (1 << tailBits) - 1
	}
	for pc := 1; pc < dom.pcCount; pc++ {
		if !reachable[pc] {
			continue
		}
		row := make([]uint64, dom.wordsPerRow)
		copy(row, universe)
		dom.bits[pc] = row
	}
}

// relaxDominatorsOnce performs one sweep of the dataflow relaxation.
//
// Takes dom (*FunctionDominators) which is the dominator table mutated in place.
// Takes predecessors ([][]int) which is the per-PC predecessor lists from the alias CFG.
// Takes reachable ([]bool) which is the per-PC reachability mask from the entry.
// Takes scratch ([]uint64) which is a pre-allocated buffer for the intersection result,
// reused across iterations to avoid per-call allocation.
//
// Returns true when any PC's bitset changed, prompting another sweep.
func relaxDominatorsOnce(dom *functionDominators, predecessors [][]int, reachable []bool, scratch []uint64) bool {
	changed := false
	for pc := 1; pc < dom.pcCount; pc++ {
		if !reachable[pc] {
			continue
		}
		preds := predecessors[pc]
		if len(preds) == 0 {
			continue
		}
		computeDominatorIntersection(dom.bits, preds, scratch)
		word := pc / bitsPerWord
		bit := uint(pc % bitsPerWord)
		scratch[word] |= 1 << bit
		if !uint64SliceEqual(dom.bits[pc], scratch) {
			copy(dom.bits[pc], scratch)
			changed = true
		}
	}
	return changed
}

// computeReachable returns a slice indexed by PC marking every PC reachable from the
// function entry via the CFG built by buildAliasSuccessors. Unreachable PCs are skipped
// during dominator iteration so they remain nil in the table, which GVN treats as dead
// code that cannot serve as a candidate definition.
//
// Takes body ([]instruction) which is the linear instruction stream of the function under
// analysis.
// Takes n (int) which is the number of PCs in the function body.
//
// Returns the per-PC reachability mask from the entry.
func computeReachable(body []isa.Instruction, n int) []bool {
	reachable := make([]bool, n)
	if n == 0 {
		return reachable
	}
	reachable[0] = true
	successors := buildAliasSuccessors(body)
	worklist := []int{0}
	for len(worklist) > 0 {
		pc := worklist[len(worklist)-1]
		worklist = worklist[:len(worklist)-1]
		for _, successor := range successors[pc] {
			if successor < 0 || successor >= n {
				continue
			}
			if reachable[successor] {
				continue
			}
			reachable[successor] = true
			worklist = append(worklist, successor)
		}
	}
	return reachable
}

// computeDominatorIntersection writes the bitwise AND of the dominator bitsets of every
// reachable predecessor into result. The result slice is zeroed when no predecessor was
// reachable.
//
// Takes bits ([][]uint64) which is the per-PC dominator bitsets from the dominator table.
// Takes preds ([]int) which is the predecessor PCs of the PC under relaxation.
// Takes result ([]uint64) which is the caller-provided scratch buffer written in place.
func computeDominatorIntersection(bits [][]uint64, preds []int, result []uint64) {
	seeded := false
	for _, pred := range preds {
		predBits := lookupReachableDomBits(bits, pred)
		if predBits == nil {
			continue
		}
		if !seeded {
			copy(result, predBits)
			seeded = true
			continue
		}
		for index := range result {
			result[index] &= predBits[index]
		}
	}
	if !seeded {
		clear(result)
	}
}

// lookupReachableDomBits returns bits[pred] when the predecessor is in range and has been
// visited (its bitset allocated).
//
// Takes bits ([][]uint64) which is the per-PC dominator bitsets from the dominator table.
// Takes pred (int) which is the predecessor PC to look up.
//
// Returns the predecessor's bitset, or nil when out of range or unreachable.
func lookupReachableDomBits(bits [][]uint64, pred int) []uint64 {
	if pred < 0 || pred >= len(bits) {
		return nil
	}
	return bits[pred]
}

// uint64SliceEqual reports whether two slices of uint64 hold identical values in every
// position.
//
// Takes a ([]uint64) which is the first slice to compare.
// Takes b ([]uint64) which is the second slice to compare.
//
// Returns true when lengths match and every position is equal, otherwise false.
func uint64SliceEqual(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
