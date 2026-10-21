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
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// fuseMapEntryAppend rewrites `GET_MAP_STRING_GENERAL t, m, k`, `APPEND_STRING u, t, v`
// and `SET_MAP_STRING_GENERAL m, k, u` into one isa.OpAppendStringToMapString, the shape
// of m[k] = append(m[k], v).
//
// The lookup and the store must name one map register and one key register, the append
// must build a slice of its own and not grow its operand in place, and neither temporary
// may be the map. Only no-operation words may lie between the three, no jump may land
// past the lookup, and both temporaries must be dead after the store, since the fused
// operation writes neither.
//
// Takes compiledFunction (*program.CompiledFunction) whose call sites and result kinds
// the liveness guard reads.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate lookup.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns true when the three were fused.
func fuseMapEntryAppend(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	lookup := body[i]
	if lookup.Op != isa.OpGetMapStringGeneral {
		return false
	}
	grow, found := wordPastNoOperations(body, i+1, jumpTargets)
	if !found || body[grow].Op != isa.OpAppendString || body[grow].B != lookup.A || body[grow].A == body[grow].B {
		return false
	}
	store, found := wordPastNoOperations(body, grow+1, jumpTargets)
	if !found || body[store].Op != isa.OpSetMapStringGeneral {
		return false
	}
	held, grown, receiver, key := lookup.A, body[grow].A, lookup.B, lookup.C
	if body[store].A != receiver || body[store].B != key || body[store].C != grown || held == receiver || grown == receiver {
		return false
	}
	if !registerDeadFrom(compiledFunction, body, store+1, isa.RegisterGeneral, held) ||
		!registerDeadFrom(compiledFunction, body, store+1, isa.RegisterGeneral, grown) {
		return false
	}
	body[i] = isa.NewInstruction(isa.OpAppendStringToMapString, receiver, key, body[grow].C)
	body[grow] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	body[store] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// wordPastNoOperations returns the first word at or after pc that is not a no-operation,
// when no jump lands on it or on a no-operation passed on the way.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is where the search starts.
// Takes jumpTargets (map[int]bool) which marks the jump destinations.
//
// Returns int which is the word found.
// Returns bool which is false when the body ends first or a jump lands in the way.
func wordPastNoOperations(body []isa.Instruction, pc int, jumpTargets map[int]bool) (int, bool) {
	for pc < len(body) && isCanonicalNoOperation(body[pc]) && !jumpTargets[pc] {
		pc++
	}
	return pc, pc < len(body) && !jumpTargets[pc]
}
