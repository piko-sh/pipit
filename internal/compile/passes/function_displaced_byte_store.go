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
	"math"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// fuseDisplacedByteStore rewrites `ADD_INT64_CONSTANT t, i, k` followed by
// `SET_SLICE_BYTE_ELEMENT s, t, v` into the store alone, indexed by i with k added to its
// displacement byte.
//
// The fused form no longer writes t, so t must be dead after the store, and the sum of k
// and any displacement the store already has must fit the byte. Only no-operation words
// may lie between the two, and none of those nor the store may be a jump target, so the
// add is the only way to reach the store.
//
// Takes compiledFunction (*program.CompiledFunction) whose constant pool and call sites
// the checks read.
// Takes body ([]isa.Instruction) which is the instruction sequence.
// Takes i (int) which is the candidate add's index.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns true when the pattern matched and was rewritten in place.
func fuseDisplacedByteStore(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	add := body[i]
	if add.Op != isa.OpAddInt64Constant || int(add.C) >= len(compiledFunction.IntConstants) {
		return false
	}
	store := i + 1
	for store < len(body) && isCanonicalNoOperation(body[store]) && !jumpTargets[store] {
		store++
	}
	if store+1 >= len(body) || jumpTargets[store] ||
		!isa.InstrIsTier1SubOp(body[store], isa.SubOpSetSliceByteElement) ||
		body[store].C != add.A || body[store+1].Op != isa.OpExtension {
		return false
	}
	extension := body[store+1]
	constant := compiledFunction.IntConstants[add.C]
	if constant < 0 || constant > math.MaxUint8-int64(extension.B) {
		return false
	}
	if !registerDeadFrom(compiledFunction, body, store+2, isa.RegisterInt64, add.A) {
		return false
	}
	body[i] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	body[store] = isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, body[store].B, add.B)
	body[store+1] = isa.NewInstruction(isa.OpExtension, extension.A, extension.B+safeconv.MustIntToUint8(int(constant)), extension.C)
	return true
}
