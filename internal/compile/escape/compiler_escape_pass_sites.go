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

// siteScan holds the jump targets and jumps shared by escape walks over one body.
type siteScan struct {
	// compiledFunction is the function whose sites are annotated.
	compiledFunction *program.CompiledFunction

	// jumpTargets is program.JumpTargetsOf of the body.
	jumpTargets map[int]bool

	// jumps holds every jump of the body in program-counter order.
	jumps []siteScanJump
}

// siteScanJump is one jump of a body.
type siteScanJump struct {
	// from is the jump's program counter.
	from int

	// target is the program counter it jumps to.
	target int
}

// outputEscapesFromSite reports whether the value produced at sitePC can escape the
// frame.
//
// When the site sits in a loop, a second walk from the loop header to the site is seeded
// with every register the first walk saw the value copied to, because the previous
// iteration's value may still be live.
//
// Takes slot (uint8) which is the site's output general register.
// Takes sitePC (int) which is the PC of the allocation site.
// Takes afterSite (int) which is the first PC after the site and its extension words.
//
// Returns true when any use of the value may escape.
func (scan *siteScan) outputEscapesFromSite(slot uint8, sitePC, afterSite int) bool {
	compiledFunction := scan.compiledFunction
	seed := [isa.GeneralRegisterBankSize]bool{}
	seed[slot] = true
	escapes, ever := analyseGeneralRegisterEscapeWithTargets(compiledFunction, scan.jumpTargets, &seed, afterSite, len(compiledFunction.Body))
	if escapes {
		return true
	}
	head, inLoop := scan.enclosingLoopHead(sitePC)
	if !inLoop {
		return false
	}
	escapes, _ = analyseGeneralRegisterEscapeWithTargets(compiledFunction, scan.jumpTargets, &ever, head, sitePC)
	return escapes
}

// enclosingLoopHead finds the lowest target of any backward jump that spans sitePC.
//
// Takes sitePC (int) which is the PC being classified.
//
// Returns int which is the loop head PC.
// Returns bool which is true when sitePC lies inside a loop.
func (scan *siteScan) enclosingLoopHead(sitePC int) (int, bool) {
	head := -1
	for _, jump := range scan.jumps {
		if jump.from < sitePC || jump.target > sitePC {
			continue
		}
		if head < 0 || jump.target < head {
			head = jump.target
		}
	}
	return head, head >= 0
}

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
	case isa.SubOpTier2AllocateStructLiteral, isa.SubOpTier2MakeMap:
	default:
		return false
	}
	if pc+1 >= len(body) || body[pc+1].Op != isa.OpExtension {
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
	case isa.OpAllocateIndirect, isa.OpLoadGeneralConstant:
		return true
	case isa.OpAddress:
		return inst.C != engine.AddrSourceStable
	default:
		return false
	}
}

// newSiteScan computes the jump targets and jumps of compiledFunction's body.
//
// Takes compiledFunction (*CompiledFunction) whose body is scanned.
//
// Returns *siteScan which serves every site of the body.
func newSiteScan(compiledFunction *program.CompiledFunction) *siteScan {
	body := compiledFunction.Body
	scan := &siteScan{compiledFunction: compiledFunction, jumpTargets: make(map[int]bool, len(body)/4), jumps: nil}
	for pc := range body {
		if target, ok := program.JumpTargetAt(body, pc); ok {
			scan.jumpTargets[target] = true
			scan.jumps = append(scan.jumps, siteScanJump{from: pc, target: target})
		}
	}
	return scan
}

// structFieldOpEscapes classifies the tier-0 struct-field opcode family for escape
// analysis.
//
// Takes inst (instruction) which is the instruction to classify.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns escapes (bool) which is true when the field operation may escape the value.
// Returns classified (bool) which is true when inst belongs to the field opcode family.
func structFieldOpEscapes(inst isa.Instruction, tainted *[isa.GeneralRegisterBankSize]bool) (escapes, classified bool) {
	switch inst.Op {
	case isa.OpGetStructFieldInt64, isa.OpGetStructFieldUint64, isa.OpGetStructFieldFloat64, isa.OpGetStructFieldBool,
		isa.OpGetStructFieldGeneral, isa.OpGetStructFieldRawPointer, isa.OpGetStructFieldElementGeneral,
		isa.OpLengthStructField, isa.OpGetStructFieldElementField,
		isa.OpGetElementFieldInt64, isa.OpGetElementFieldUint64, isa.OpGetElementFieldFloat64,
		isa.OpGetElementFieldBool, isa.OpGetElementFieldString,
		isa.OpSetElementFieldInt64, isa.OpSetElementFieldFloat64,
		isa.OpSetStructFieldInt64, isa.OpSetStructFieldUint64, isa.OpSetStructFieldFloat64, isa.OpSetStructFieldBool,
		isa.OpCopyStructFieldGeneral, isa.OpSwapStructFieldsGeneral:
		return false, true
	case isa.OpSetStructFieldGeneral, isa.OpSetStructFieldElementGeneral:
		return tainted[inst.B], true
	default:
		return false, false
	}
}
