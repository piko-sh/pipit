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
	"pipit.sh/pipit/internal/isa"
)

// joinEntries holds, for one sweep, the jumps that land on each word.
type joinEntries struct {
	// sources lists the jumping words by the word they land on, or is nil until asked for.
	sources map[int][]int
}

// of returns the jumps that land on each word of a body, working them out on the first
// call.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
//
// Returns map[int][]int which lists the jumping words by target.
func (entries *joinEntries) of(body []isa.Instruction) map[int][]int {
	if entries.sources == nil {
		entries.sources = jumpSourcesByTarget(body)
	}
	return entries.sources
}

// renameDefinitionsIntoJoinMove removes a move that jumps land on by renaming each
// incoming definition to write the move's destination.
//
// A way in is the word before the move, unless that word never falls through, and each
// jump that lands on the move. A jump has to be unconditional, since the other way out of
// a conditional one would be left without the source it was given. Each line is held to
// the rules of renameDefinitionIntoMove.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes sources (map[int][]int) which lists the jumping words by target.
// Takes move (coalesceCandidate) which is the move.
//
// Returns bool which is true when the move became redundant.
func renameDefinitionsIntoJoinMove(
	compiledFunction *program.CompiledFunction, jumpTargets map[int]bool, sources map[int][]int, move coalesceCandidate,
) bool {
	body := compiledFunction.Body
	ends, ok := joinLineEnds(body, sources, move.movePC)
	if !ok || !registerDeadFrom(compiledFunction, body, move.movePC+1, move.kind, move.source) {
		return false
	}
	var renamed []int
	for _, end := range ends {
		line, found := findRenameRangeBefore(body, jumpTargets, move, end)
		if !found {
			return false
		}
		definition := line[len(line)-1]
		if namesDebugVariable(compiledFunction, move.kind, move.source, min(definition, move.movePC), max(end, move.movePC)) {
			return false
		}
		renamed = append(renamed, line...)
	}
	for _, pc := range renamed {
		body[pc] = renameRegister(body[pc], move.kind, move.source, move.destination)
	}
	return true
}

// joinLineEnds lists the ends of the straight lines into a move.
//
// A jump onto the move may itself be jumped to. Every way into it has then to be
// accounted for in turn. These are the line that falls into it and each jump that lands
// on it, which must be unconditional too. A conditional jump or a table row that lands on
// the move, or on any jump leading to it, skips the definition a line ends with, so it
// rules the move out.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes sources (map[int][]int) which lists the jumping words by target.
// Takes movePC (int) which is the move.
//
// Returns []int which lists the ends, at least one.
// Returns bool which is false when some way to the move is not a straight line that ends
// at the move or at an unconditional jump.
func joinLineEnds(body []isa.Instruction, sources map[int][]int, movePC int) ([]int, bool) {
	var ends []int
	if fallsThroughInto(body, movePC) {
		ends = append(ends, movePC)
	}
	pending := slices.Clone(sources[movePC])
	followed := make(map[int]bool, len(pending))
	for len(pending) > 0 {
		jump := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if !isa.InstrIsTier1SubOp(body[jump], isa.SubOpJump) {
			return nil, false
		}
		if followed[jump] {
			continue
		}
		followed[jump] = true
		if fallsThroughInto(body, jump) {
			ends = append(ends, jump)
		}
		pending = append(pending, sources[jump]...)
	}
	return ends, len(ends) > 0
}

// fallsThroughInto reports whether control can reach a word from the instruction before
// it, which it cannot when that instruction always jumps or returns.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the word.
//
// Returns bool which is false for the first word and for a word after a terminator.
func fallsThroughInto(body []isa.Instruction, pc int) bool {
	previous := pc - 1
	for previous >= 0 && (body[previous].Op == isa.OpExtension || isCanonicalNoOperation(body[previous])) {
		previous--
	}
	if previous < 0 {
		return false
	}
	return isa.ShapeForInstruction(body[previous]).Flags&isa.ShapeFlagTerminator == 0
}
