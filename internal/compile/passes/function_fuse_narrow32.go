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

const (
	// narrow32Width is the truncation width the fused 32-bit operations perform.
	narrow32Width = 32

	// instructionWithExtensionWords counts the words of an instruction that carries one
	// extension word.
	instructionWithExtensionWords = 2
)

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
		if !ok || (kind == isa.RegisterUint64 && feedsMaskedReturn(body, truncation)) {
			continue
		}
		body[pc] = fused
		body[truncation] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
		changed = true
	}
	return changed
}

// narrow32Form maps an operation to its fused 32-bit form.
//
// Takes inst (isa.Instruction) which is the candidate operation.
//
// Returns isa.Instruction which is the fused instruction.
// Returns isa.RegisterKind which is the destination bank.
// Returns uint8 which is the destination register.
// Returns bool which is true when the operation has a fused form.
func narrow32Form(inst isa.Instruction) (isa.Instruction, isa.RegisterKind, uint8, bool) {
	fused := inst
	switch inst.Op {
	case isa.OpAddInt64:
		fused.Op = isa.OpAddInt32
	case isa.OpSubtractInt64:
		fused.Op = isa.OpSubtractInt32
	case isa.OpMultiplyInt64:
		fused.Op = isa.OpMultiplyInt32
	case isa.OpAddInt64Constant:
		fused.Op = isa.OpAddInt32Constant
	case isa.OpSubtractInt64Constant:
		fused.Op = isa.OpSubtractInt32Constant
	case isa.OpAddUint64:
		fused.Op = isa.OpAddUint32
		return fused, isa.RegisterUint64, inst.A, true
	case isa.OpSubtractUint64:
		fused.Op = isa.OpSubtractUint32
		return fused, isa.RegisterUint64, inst.A, true
	case isa.OpMultiplyUint64:
		fused.Op = isa.OpMultiplyUint32
		return fused, isa.RegisterUint64, inst.A, true
	case isa.OpDrillTier1:
		return narrow32SubTierForm(inst)
	default:
		return inst, isa.RegisterInt64, 0, false
	}
	return fused, isa.RegisterInt64, inst.A, true
}

// narrow32SubTierForm maps the tier-1 uint constant arithmetic and the tier-2 int
// increment and decrement to their 32-bit forms.
//
// Takes inst (isa.Instruction) which is the candidate operation.
//
// Returns isa.Instruction which is the fused instruction.
// Returns isa.RegisterKind which is the destination bank.
// Returns uint8 which is the destination register.
// Returns bool which is true when the operation has a fused form.
func narrow32SubTierForm(inst isa.Instruction) (isa.Instruction, isa.RegisterKind, uint8, bool) {
	switch {
	case isa.InstrIsTier1SubOp(inst, isa.SubOpAddUint64Constant):
		return isa.NewTier1Instruction(isa.SubOpAddUint32Constant, inst.B, inst.C), isa.RegisterUint64, inst.B, true
	case isa.InstrIsTier1SubOp(inst, isa.SubOpSubtractUint64Constant):
		return isa.NewTier1Instruction(isa.SubOpSubtractUint32Constant, inst.B, inst.C), isa.RegisterUint64, inst.B, true
	case isa.InstrIsTier2SubOp(inst, isa.SubOpTier2IncrementInt64):
		return isa.NewTier2Instruction(isa.SubOpTier2IncrementInt32, inst.C), isa.RegisterInt64, inst.C, true
	case isa.InstrIsTier2SubOp(inst, isa.SubOpTier2DecrementInt64):
		return isa.NewTier2Instruction(isa.SubOpTier2DecrementInt32, inst.C), isa.RegisterInt64, inst.C, true
	default:
		return inst, isa.RegisterInt64, 0, false
	}
}

// narrow32UintResult recognises the uint operations narrow32Form() and
// narrow32SubTierForm() produce, each of which leaves its result truncated to 32 bits.
//
// Takes inst (isa.Instruction) which is the instruction.
//
// Returns register (uint8) which is the uint register written.
// Returns words (int) which is how many words the instruction takes, its extension word
// included.
// Returns ok (bool) which is false for any other instruction.
func narrow32UintResult(inst isa.Instruction) (register uint8, words int, ok bool) {
	switch inst.Op {
	case isa.OpAddUint32, isa.OpSubtractUint32, isa.OpMultiplyUint32:
		return inst.A, 1, true
	case isa.OpDrillTier1:
		if sub := isa.SubOpcode(inst.A); sub == isa.SubOpAddUint32Constant || sub == isa.SubOpSubtractUint32Constant {
			return inst.B, instructionWithExtensionWords, true
		}
	}
	return 0, 0, false
}

// followingTruncation finds a 32-bit truncation of reg right after pc, past the
// operation's own extension words and any NO_OPERATION words, with no branch landing
// between them.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes pc (int) which is the operation.
// Takes kind (isa.RegisterKind) which is the destination bank.
// Takes reg (uint8) which is the destination register.
//
// Returns int which is the truncation's PC.
// Returns bool which is true when a matching truncation was found.
func followingTruncation(body []isa.Instruction, jumpTargets map[int]bool, pc int, kind isa.RegisterKind, reg uint8) (int, bool) {
	for next := pc + 1; next < len(body); next++ {
		if jumpTargets[next] {
			return 0, false
		}
		inst := body[next]
		if isCanonicalNoOperation(inst) || inst.Op == isa.OpExtension {
			continue
		}
		return next, inst.Op == isa.OpTruncateToWidth && inst.A == reg && inst.B == narrow32Width && isa.RegisterKind(inst.C) == kind
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
	next := nextNonNoOperation(body, pc+1)
	if next < len(body) && isa.InstrIsTier1SubOp(body[next], isa.SubOpMoveUint64) {
		next = nextNonNoOperation(body, next+1)
	}
	return next < len(body) && program.IsReturnInstruction(body[next])
}

// nextNonNoOperation returns the first PC at or after pc that is not a NO_OPERATION, or
// the body length.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the starting PC.
//
// Returns the PC.
func nextNonNoOperation(body []isa.Instruction, pc int) int {
	for pc < len(body) && isCanonicalNoOperation(body[pc]) {
		pc++
	}
	return pc
}
