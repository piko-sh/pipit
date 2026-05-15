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

package escape

import (
	"reflect"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// arenaCandidateSiteAt classifies the instruction at pc as an arena allocation site and
// names the general register it writes: the pointer-producing tier-0 sites
// isArenaCandidateSite() recognises (output A), and the tier-2 struct-literal sites
// isStructLiteralSiteAt() recognises (output C).
//
// Takes compiledFunction (*CompiledFunction) whose body and type table are read.
// Takes pc (int) which is the instruction's program counter.
//
// Returns outputSlot (uint8) which is the site's output general register.
// Returns ok (bool) which is false when the instruction is not an allocation site.
func arenaCandidateSiteAt(compiledFunction *program.CompiledFunction, pc int) (outputSlot uint8, ok bool) {
	inst := compiledFunction.Body[pc]
	if isArenaCandidateSite(inst) {
		return inst.A, true
	}
	if isStructLiteralSiteAt(compiledFunction, pc) {
		return inst.C, true
	}
	return 0, false
}

// isStructLiteralSiteAt reports whether the tier-2 instruction at pc allocates a struct
// literal whose storage may stay in the register arena.
//
// Takes compiledFunction (*CompiledFunction) whose body and type table are read.
// Takes pc (int) which is the instruction's program counter.
//
// Returns true for a struct-literal site with a resolvable struct type.
func isStructLiteralSiteAt(compiledFunction *program.CompiledFunction, pc int) bool {
	body := compiledFunction.Body
	inst := body[pc]
	if inst.Op != isa.OpDrillTier1 || isa.SubOpcode(inst.A) != isa.SubOpDrillTier2 {
		return false
	}
	switch isa.SubOpcodeTier2(inst.B) {
	case isa.SubOpTier2AllocStructLiteral, isa.SubOpTier2MakeMap:
	default:
		return false
	}
	if pc+1 >= len(body) || body[pc+1].Op != isa.OpExt {
		return false
	}
	extension := body[pc+1]
	typeIndex := int(extension.A) | int(extension.B)<<isa.WideBitShift
	if typeIndex >= len(compiledFunction.TypeTable) {
		return false
	}
	entry := compiledFunction.TypeTable[typeIndex]
	return entry != nil && entry.Kind() == reflect.Struct
}

// isArenaCandidateSite reports whether inst allocates storage eligible for the arena when
// the resulting pointer does not escape.
//
// Takes inst (instruction) which is the instruction to classify.
//
// Returns true for the pointer-producing allocation opcodes.
func isArenaCandidateSite(inst isa.Instruction) bool {
	switch inst.Op {
	case isa.OpAllocIndirect, isa.OpLoadGeneralConst:
		return true
	case isa.OpAddr:
		return inst.C != engine.AddrSourceStable
	default:
		return false
	}
}

// outputEscapesFromSite reports whether the value produced at sitePC can escape the
// frame.
//
// When the site sits in a loop, a second walk from the loop header to the site is seeded
// with every register the first walk saw the value copied to, because the previous
// iteration's value may still be live.
//
// Takes compiledFunction (*CompiledFunction) whose body is walked.
// Takes slot (uint8) which is the site's output general register.
// Takes sitePC (int) which is the PC of the allocation site.
// Takes afterSite (int) which is the first PC after the site and its extension words.
//
// Returns true when any use of the value may escape.
func outputEscapesFromSite(compiledFunction *program.CompiledFunction, slot uint8, sitePC, afterSite int) bool {
	seed := [isa.GeneralRegisterBankSize]bool{}
	seed[slot] = true
	escapes, ever := analyseGeneralRegisterEscapeSeeded(compiledFunction, &seed, afterSite, len(compiledFunction.Body))
	if escapes {
		return true
	}
	head, inLoop := enclosingLoopHead(compiledFunction.Body, sitePC)
	if !inLoop {
		return false
	}
	escapes, _ = analyseGeneralRegisterEscapeSeeded(compiledFunction, &ever, head, sitePC)
	return escapes
}

// enclosingLoopHead finds the lowest target of any backward jump that spans sitePC.
//
// Takes body ([]instruction) which is the function body.
// Takes sitePC (int) which is the PC being classified.
//
// Returns the loop head PC and true when sitePC lies inside a loop.
func enclosingLoopHead(body []isa.Instruction, sitePC int) (int, bool) {
	head := -1
	for pc := sitePC; pc < len(body); pc++ {
		target, ok := program.JumpTargetAt(body, pc)
		if !ok || target > sitePC {
			continue
		}
		if head < 0 || target < head {
			head = target
		}
	}
	return head, head >= 0
}

// structFieldOpEscapes classifies the tier-0 struct-field opcode family for escape
// analysis.
//
// Takes inst (instruction) which is the instruction to classify.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns the escape verdict and true when inst belongs to the family; false, false for
// every other opcode.
func structFieldOpEscapes(inst isa.Instruction, tainted *[isa.GeneralRegisterBankSize]bool) (escapes, classified bool) {
	switch inst.Op {
	case isa.OpGetStructFieldIntT0, isa.OpGetStructFieldUint, isa.OpGetStructFieldFloat, isa.OpGetStructFieldBool,
		isa.OpGetStructFieldGeneral, isa.OpGetStructFieldRawPointerT0, isa.OpGetStructFieldIndexGeneral,
		isa.OpGetStructFieldSliceLen, isa.OpGetStructFieldSliceIndexScalar,
		isa.OpSliceIndexStructFieldInt, isa.OpSliceIndexStructFieldUint, isa.OpSliceIndexStructFieldFloat,
		isa.OpSliceIndexStructFieldBool, isa.OpSliceIndexStructFieldString,
		isa.OpSetStructFieldIntT0, isa.OpSetStructFieldUint, isa.OpSetStructFieldFloat, isa.OpSetStructFieldBool,
		isa.OpCopyStructFieldGeneralT0, isa.OpSwapStructFieldsGeneralT0:
		return false, true
	case isa.OpSetStructFieldGeneral, isa.OpSetStructFieldIndexGeneral:
		return tainted[inst.B], true
	default:
		return false, false
	}
}
