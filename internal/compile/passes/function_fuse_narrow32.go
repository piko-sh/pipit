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
	"context"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// narrow32Width is the truncation width the fused 32-bit operations perform.
const narrow32Width = 32

// fuseNarrow32Pass folds a 32-bit truncation into the integer operation whose result it
// narrows. It runs at the end of the post-purity stage, after every pass that reasons
// about the plain operations.
type fuseNarrow32Pass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (fuseNarrow32Pass) Name() string { return "fuse-narrow32" }

// Run applies fuseNarrow32().
//
// Takes state (*PassContext) which carries the shared analysis, invalidated on change.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error (always nil).
func (fuseNarrow32Pass) Run(_ context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	if fuseNarrow32(compiledFunction.Body, state.Analysis.JumpTargets()) {
		state.Analysis.invalidate()
	}
	return nil
}

// fuseNarrow32 rewrites every operation-plus-truncation pair in body.
//
// Takes body ([]isa.Instruction) which is rewritten in place.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
//
// Returns true when a pair was fused.
func fuseNarrow32(body []isa.Instruction, jumpTargets map[int]bool) bool {
	changed := false
	for pc := range body {
		fused, kind, dest, ok := narrow32Form(body[pc])
		if !ok {
			continue
		}
		truncation, ok := followingTruncation(body, jumpTargets, pc, kind, dest)
		if !ok || (kind == isa.RegisterUint && feedsMaskedReturn(body, truncation)) {
			continue
		}
		body[pc] = fused
		body[truncation] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
		changed = true
	}
	return changed
}

// narrow32Form maps an operation to its fused 32-bit form.
//
// Takes inst (isa.Instruction) which is the candidate operation.
//
// Returns the fused instruction, the destination bank and register, and whether inst has
// a fused form.
func narrow32Form(inst isa.Instruction) (isa.Instruction, isa.RegisterKind, uint8, bool) {
	fused := inst
	switch inst.Op {
	case isa.OpAddInt:
		fused.Op = isa.OpAddInt32
	case isa.OpSubInt:
		fused.Op = isa.OpSubInt32
	case isa.OpMulInt:
		fused.Op = isa.OpMulInt32
	case isa.OpAddIntConst:
		fused.Op = isa.OpAddInt32Const
	case isa.OpSubIntConst:
		fused.Op = isa.OpSubInt32Const
	case isa.OpAddUint:
		fused.Op = isa.OpAddUint32
		return fused, isa.RegisterUint, inst.A, true
	case isa.OpDrillTier1:
		return narrow32Tier2Form(inst)
	default:
		return inst, isa.RegisterInt, 0, false
	}
	return fused, isa.RegisterInt, inst.A, true
}

// narrow32Tier2Form maps the tier-2 int increment and decrement to their 32-bit forms.
//
// Takes inst (isa.Instruction) which is the candidate operation.
//
// Returns the fused instruction, the destination bank and register, and whether inst has
// a fused form.
func narrow32Tier2Form(inst isa.Instruction) (isa.Instruction, isa.RegisterKind, uint8, bool) {
	switch {
	case isa.InstrIsTier2SubOp(inst, isa.SubOpTier2IncInt):
		return isa.NewTier2Instruction(isa.SubOpTier2IncInt32, inst.C), isa.RegisterInt, inst.C, true
	case isa.InstrIsTier2SubOp(inst, isa.SubOpTier2DecInt):
		return isa.NewTier2Instruction(isa.SubOpTier2DecInt32, inst.C), isa.RegisterInt, inst.C, true
	default:
		return inst, isa.RegisterInt, 0, false
	}
}

// followingTruncation finds a 32-bit truncation of reg right after pc, past any NOPs,
// with no branch landing between them.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes pc (int) which is the operation.
// Takes kind (isa.RegisterKind) which is the destination bank.
// Takes reg (uint8) which is the destination register.
//
// Returns the truncation's PC and whether it was found.
func followingTruncation(body []isa.Instruction, jumpTargets map[int]bool, pc int, kind isa.RegisterKind, reg uint8) (int, bool) {
	for next := pc + 1; next < len(body); next++ {
		if jumpTargets[next] {
			return 0, false
		}
		inst := body[next]
		if isCanonicalNop(inst) {
			continue
		}
		return next, inst.Op == isa.OpTruncateNarrow && inst.A == reg && inst.B == narrow32Width && isa.RegisterKind(inst.C) == kind
	}
	return 0, false
}

// feedsMaskedReturn reports whether the truncation at pc leads straight into a return,
// optionally through one uint move, which is the tail the engine's fused-eval matchers
// recognise as a masked uint operation.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the truncation.
//
// Returns true when the truncation must stay for those matchers.
func feedsMaskedReturn(body []isa.Instruction, pc int) bool {
	next := nextNonNop(body, pc+1)
	if next < len(body) && isa.InstrIsTier1SubOp(body[next], isa.SubOpMoveUint) {
		next = nextNonNop(body, next+1)
	}
	return next < len(body) && program.IsReturnInstruction(body[next])
}

// nextNonNop returns the first PC at or after pc that is not a NOP, or the body length.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the starting PC.
//
// Returns the PC.
func nextNonNop(body []isa.Instruction, pc int) int {
	for pc < len(body) && isCanonicalNop(body[pc]) {
		pc++
	}
	return pc
}
