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

// fuseJoinedReadConversion folds a uint-to-int conversion that jumps land on into the
// definitions of its operand, when every way in ends with a small literal or a byte read.
//
// Each way in must end with the definition itself, so nothing between it and the
// conversion can read the uint register, which the int forms no longer write, and that
// register must be dead after the conversion.
//
// Takes compiledFunction (*program.CompiledFunction) whose call sites and result kinds
// the liveness guard reads.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate conversion.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
// Takes joins (*joinEntries) which lists the jumps by target, built when first needed.
//
// Returns bool which is true when the conversion was folded away.
func fuseJoinedReadConversion(
	compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool, joins *joinEntries,
) bool {
	if !jumpTargets[i] || !isa.InstrIsTier1SubOp(body[i], isa.SubOpConvertUint64ToInt64) {
		return false
	}
	signed, unsigned := body[i].B, body[i].C
	ends, ok := joinLineEnds(body, joins.of(body), i)
	if !ok || !registerDeadFrom(compiledFunction, body, i+1, isa.RegisterUint64, unsigned) {
		return false
	}
	definitions := make([]int, 0, len(ends))
	for _, end := range ends {
		definition, found := uintDefinitionEndingLine(body, jumpTargets, end, unsigned)
		if !found {
			return false
		}
		definitions = append(definitions, definition)
	}
	for _, definition := range definitions {
		body[definition] = intFormOfUintDefinition(body[definition], signed)
	}
	body[i] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// uintDefinitionEndingLine finds the instruction that ends the straight line before a
// word, when it defines a uint register in a way that has an int form.
//
// Only padding may lie between the instruction and the word, and no jump may land on that
// padding or on the instruction's extension word.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes end (int) which is the word the line runs up to.
// Takes unsigned (uint8) which is the uint register.
//
// Returns int which is the defining instruction.
// Returns bool which is false when the line ends with anything else.
func uintDefinitionEndingLine(body []isa.Instruction, jumpTargets map[int]bool, end int, unsigned uint8) (int, bool) {
	last := end - 1
	for last >= 0 && isCanonicalNoOperation(body[last]) {
		if jumpTargets[last] {
			return 0, false
		}
		last--
	}
	if last < 0 {
		return 0, false
	}
	definition := body[last]
	switch {
	case isa.InstrIsTier1SubOp(definition, isa.SubOpLoadUint64Immediate):
		return last, definition.B == unsigned
	case definition.Op == isa.OpGetElementUint64:
		return last, definition.A == unsigned
	case definition.Op == isa.OpExtension && last > 0 && !jumpTargets[last] && isa.InstrIsTier1SubOp(body[last-1], isa.SubOpGetSliceByteElement):
		return last - 1, body[last-1].B == unsigned
	default:
		return 0, false
	}
}

// intFormOfUintDefinition returns the instruction that puts into an int register what a
// definition uintDefinitionEndingLine accepted puts into a uint register.
//
// A literal of either bank is the operand byte itself. A byte or an unsigned element read
// into the int bank is the same number.
//
// Takes definition (isa.Instruction) which is the uint definition.
// Takes signed (uint8) which is the int register to write.
//
// Returns isa.Instruction which is the int form.
func intFormOfUintDefinition(definition isa.Instruction, signed uint8) isa.Instruction {
	switch {
	case isa.InstrIsTier1SubOp(definition, isa.SubOpLoadUint64Immediate):
		return isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, signed, definition.C)
	case isa.InstrIsTier1SubOp(definition, isa.SubOpGetSliceByteElement):
		return isa.NewTier1Instruction(isa.SubOpGetSliceByteElementInt64, signed, definition.C)
	default:
		return isa.NewInstruction(isa.OpGetElementInt64, signed, definition.B, definition.C)
	}
}
