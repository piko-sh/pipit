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

// VerifyInPlaceAppendSafety demotes in-place append opcodes to fresh-slot siblings when
// alias analysis cannot confirm the source register is unaliased.
//
// Requires AliasInfo from RunPointerAliasAnalysis. When AliasInfo is nil the pass leaves
// every site untouched, relying on the AST pre-pass's conservative emission.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
func VerifyInPlaceAppendSafety(compiledFunction *program.CompiledFunction) {
	if !inPlaceAppendPassApplies(compiledFunction) {
		return
	}
	verifyInPlaceAppendSafety(compiledFunction, newFunctionAnalysis(compiledFunction))
}

// inPlaceAppendPassApplies reports whether the pass has anything to check.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function under
// inspection.
//
// Returns true when the body is non-empty and alias information is attached.
func inPlaceAppendPassApplies(compiledFunction *program.CompiledFunction) bool {
	return compiledFunction != nil && len(compiledFunction.Body) != 0 && compiledFunction.AliasInfo != nil
}

// verifyInPlaceAppendSafety is VerifyInPlaceAppendSafety with the jump-target set
// supplied by the caller, so the per-site liveness scans share one decode.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes analysis (*FunctionAnalysis) which supplies the jump-target set on demand.
func verifyInPlaceAppendSafety(compiledFunction *program.CompiledFunction, analysis *functionAnalysis) {
	if !inPlaceAppendPassApplies(compiledFunction) {
		return
	}
	jumpTargets := analysis.JumpTargets()
	for index := range compiledFunction.Body {
		instruction := compiledFunction.Body[index]
		switch instruction.Op {
		case isa.OpAppendByteFastInPlace:
			if !inPlaceAppendSiteIsSafe(compiledFunction, index, instruction.B, jumpTargets) {
				compiledFunction.Body[index] = isa.NewInstruction(isa.OpAppendByteFast, instruction.A, instruction.B, instruction.C)
			}
		case isa.OpAppendInPlace:
			if !inPlaceAppendSiteIsSafe(compiledFunction, index, instruction.B, jumpTargets) {
				compiledFunction.Body[index] = isa.NewInstruction(isa.OpAppend, instruction.A, instruction.B, instruction.C)
			}
		case isa.OpAppendSpreadInPlace:
			if !inPlaceAppendSiteIsSafe(compiledFunction, index, instruction.B, jumpTargets) {
				compiledFunction.Body[index] = isa.NewInstruction(isa.OpAppendSpread, instruction.A, instruction.B, instruction.C)
			}
		case isa.OpDrillTier1:
			maybeDemoteTier1InPlaceAppend(compiledFunction, index, instruction, jumpTargets)
		default:
		}
	}
}

// maybeDemoteTier1InPlaceAppend demotes unsafe tier-1 in-place sub-ops whose fresh-slot
// sibling shares the same operand encoding.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes index (int) which is the PC of the candidate instruction.
// Takes instr (isa.Instruction) which is the instruction at that PC.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
func maybeDemoteTier1InPlaceAppend(compiledFunction *program.CompiledFunction, index int, instr isa.Instruction, jumpTargets map[int]bool) {
	if isa.SubOpcode(instr.A) == isa.SubOpAppendUintInPlace && !inPlaceAppendSiteIsSafe(compiledFunction, index, instr.C, jumpTargets) {
		compiledFunction.Body[index] = isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpAppendUint), instr.B, instr.C)
	}
}

// inPlaceAppendSiteIsSafe reports whether the source register is unaliased.
//
// Uses compiledFunction.AliasInfo to query per-PC alias classes and
// generalRegisterIsDeadAfter to skip dead registers (their stale alias class no longer
// matters). Returns false on the first observed aliasing register - the source is
// potentially aliased and in-place mutation would break Go slice semantics.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes pc (int) which is the PC of the candidate in-place instruction.
// Takes sourceRegister (uint8) which is the register whose alias class the predicate
// checks.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
//
// Returns bool which is true when no live register shares the source's alias class.
func inPlaceAppendSiteIsSafe(compiledFunction *program.CompiledFunction, pc int, sourceRegister uint8, jumpTargets map[int]bool) bool {
	if compiledFunction.AliasInfo == nil {
		return false
	}
	peak := compiledFunction.NumRegisters[isa.RegisterGeneral]
	if peak == 0 {
		return true
	}
	for register := range peak {
		other := uint8(register)
		if other == sourceRegister {
			continue
		}
		if generalRegisterIsDeadAfter(compiledFunction, compiledFunction.Body, len(compiledFunction.Body), pc, other, jumpTargets) {
			continue
		}
		if compiledFunction.AliasInfo.MayAlias(pc, sourceRegister, other) {
			return false
		}
	}
	return true
}
