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

package engine

import (
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// accumulateArenaBytecodeHints walks a function body, counting the opcodes that write to
// the byte slab or allocate from a typed backing slab. Each count contributes to its
// hint's per-opcode budget, summed across the program to give sizeArenaFromFunctions a
// tight pre-sizing target.
//
// Takes hints (*arenaBytecodeHints) which is the accumulator.
// Takes body ([]isa.Instruction) which is the function's bytecode.
// Takes allFuncs ([]*CompiledFunction) which is the file-set function table for resolving
// closure indices.
func accumulateArenaBytecodeHints(hints *arenaBytecodeHints, body []isa.Instruction, allFuncs []*program.CompiledFunction) {
	for i := range body {
		switch body[i].Op {
		case isa.OpConcatenateString:
			hints.Bytes += arenaConcatenateStringAvgBytes
		case isa.OpConcatenateStringRune:
			hints.Bytes += arenaConcatenateStringRuneAvgBytes
		case isa.OpDrillTier1:
			if isa.SubOpcode(body[i].A) == isa.SubOpDrillTier2 {
				accumulateTier2Hints(hints, body[i].B)
				continue
			}
			accumulateTier1Hints(hints, body[i].A)
		case isa.OpBoxAsInterface, isa.OpBoxAsType:
			accumulatePackHint(hints, isa.RegisterKind(body[i].C))
		case isa.OpMakeClosure:
			accumulateClosureHint(hints, body[i], allFuncs)
		case isa.OpAppend, isa.OpAppendSpread, isa.OpAppendInPlace, isa.OpAppendSpreadInPlace, isa.OpAppendStruct:
			hints.sliceHeaders++
			hints.appendOccurrences++
		case isa.OpAppendByte, isa.OpAppendByteInPlace, isa.OpAppendInt64, isa.OpAppendFloat64, isa.OpAppendString:
			hints.sliceHeaders++
		case isa.OpMakeSlice:
			hints.sliceHeaders++
			hints.makeSliceGeneric++
			hints.genericBytes += arenaMakeSliceGenericAvgBytes
		case isa.OpAllocateIndirect:
			hints.allocateIndirectOccurrences++
			hints.sliceHeaders++
			hints.genericBytes += arenaAllocateIndirectAvgBytes
		default:
		}
	}
}

// accumulateClosureHint charges the callee's upvalue descriptor count against the upvalue
// hints. Falls through silently for out-of-range indices.
//
// Takes hints (*arenaBytecodeHints) which is the accumulator.
// Takes instr (isa.Instruction) which is the isa.OpMakeClosure instruction.
// Takes allFuncs ([]*CompiledFunction) which is the file-set function slice.
func accumulateClosureHint(hints *arenaBytecodeHints, instr isa.Instruction, allFuncs []*program.CompiledFunction) {
	index := int(instr.WideIndex())
	if index < 0 || index >= len(allFuncs) {
		return
	}
	callee := allFuncs[index]
	if callee == nil {
		return
	}
	descriptors := len(callee.UpvalueDescriptors)
	hints.upvalueCells += descriptors
	hints.upvalueReferences += descriptors
}

// accumulatePackHint bumps the appropriate per-kind box or slice-header hint for a pack
// instruction.
//
// Takes hints (*arenaBytecodeHints) which is the accumulator.
// Takes sourceKind (isa.RegisterKind) which selects the kind branch.
func accumulatePackHint(hints *arenaBytecodeHints, sourceKind isa.RegisterKind) {
	switch sourceKind {
	case isa.RegisterInt64:
		hints.boxInts++
	case isa.RegisterUint64:
		hints.boxUints++
	case isa.RegisterFloat64:
		hints.boxFloats++
	case isa.RegisterString:
		hints.boxStrings++
	case isa.RegisterComplex128:
		hints.boxComplexes++
	case isa.RegisterSliceInt64, isa.RegisterSliceFloat64, isa.RegisterSliceString,
		isa.RegisterSliceBool, isa.RegisterSliceUint64, isa.RegisterSliceByte:
		hints.sliceHeaders++
	default:
	}
}

// accumulateTier1Hints handles a single isa.OpDrillTier1 instruction, charging the
// appropriate budget to the right hint field based on the umbrella sub-op encoded in
// operand A.
//
// Takes hints (*arenaBytecodeHints) which is the accumulator.
// Takes subOp (uint8) which is the sub-opcode discriminator from the umbrella
// instruction's A operand.
func accumulateTier1Hints(hints *arenaBytecodeHints, subOp uint8) {
	switch isa.SubOpcode(subOp) {
	case isa.SubOpConvertBytesToString:
		hints.Bytes += arenaBytesToStringAvgBytes
	case isa.SubOpStrconvItoa:
		hints.Bytes += arenaItoaMaxBytes
	case isa.SubOpStrconvFormatInt:
		hints.Bytes += arenaFormatIntMaxBytes
	case isa.SubOpConvertRuneToString:
		hints.Bytes += arenaRuneToStringMaxBytes
	case isa.SubOpMakeSliceInt64:
		hints.MakeSliceInt64++
	case isa.SubOpMakeSliceFloat64:
		hints.MakeSliceFloat64++
	case isa.SubOpMakeSliceString:
		hints.MakeSliceString++
	case isa.SubOpMakeSliceBool:
		hints.MakeSliceBool++
	case isa.SubOpMakeSliceUint64:
		hints.MakeSliceUint64++
	default:
	}
}

// accumulateTier2Hints handles a single tier-2 instruction, charging the arena hint its
// allocation implies.
//
// The map and struct-literal allocations sit at tier 2 because each takes a single
// destination register, so they are told apart by operand B rather than operand A.
//
// Takes hints (*arenaBytecodeHints) which accumulates the estimate.
// Takes subOp (uint8) which is the tier-2 sub-opcode byte.
func accumulateTier2Hints(hints *arenaBytecodeHints, subOp uint8) {
	switch isa.SubOpcodeTier2(subOp) {
	case isa.SubOpTier2MakeMap, isa.SubOpTier2AllocateStructLiteral:
		hints.makeMapOccurrences++
		hints.genericBytes += arenaMakeMapAvgBytes
	default:
	}
}
