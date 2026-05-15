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

package asm

import (
	"piko.sh/asmgen"
)

// handlerSubOpRangeCheckUintJumpFalse builds the fused range-check handler for
// SubOpTier2RangeCheckUintJumpFalse.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpRangeCheckUintJumpFalse() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSubOpRangeCheckUintJumpFalse",
		Comment:   "handlerSubOpRangeCheckUintJumpFalse jumps if uints[C] < ext1.a or uints[C] > ext1.b; offset packed in ext2.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitRangeCheckUintJumpFalse(emitter)
		},
	}
}

// handlerSubOpEqUintConstJumpFalse builds the fused uint-immediate compare-and-branch
// handler for SubOpEqUintConstJumpFalse.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpEqUintConstJumpFalse() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSubOpEqUintConstJumpFalse",
		Comment:   "handlerSubOpEqUintConstJumpFalse jumps by the ext offset when uints[B] != C, stepping past the ext word and the trailing no-op either way.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitEqUintConstJumpFalse(emitter)
		},
	}
}

// tier1SuperRangeCheckHandlers returns the handler definitions for the super-instruction
// fusions that operate on uint values with constant range bounds.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler definitions
// for this group.
func tier1SuperRangeCheckHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		handlerSubOpRangeCheckUintJumpFalse(),
		handlerSubOpEqUintConstJumpFalse(),
		compareConstantJumpFalseHandler("handlerLeIntConstJumpFalse", "handlerLeIntConstJumpFalse compares ints[B] <= intConstants[C] and jumps if false.", "LE"),
		compareConstantJumpFalseHandler("handlerLtIntConstJumpFalse", "handlerLtIntConstJumpFalse compares ints[B] < intConstants[C] and jumps if false.", "LT"),
		compareConstantJumpFalseHandler("handlerEqIntConstJumpFalse", "handlerEqIntConstJumpFalse compares ints[B] == intConstants[C] and jumps if false.", "EQ"),
		compareConstantJumpTrueHandler(),
		compareConstantJumpFalseHandler("handlerGeIntConstJumpFalse", "handlerGeIntConstJumpFalse compares ints[B] >= intConstants[C] and jumps if false.", "GE"),
		compareConstantJumpFalseHandler("handlerGtIntConstJumpFalse", "handlerGtIntConstJumpFalse compares ints[B] > intConstants[C] and jumps if false.", "GT"),
		compareRegisterJumpFalseHandler("handlerLtIntJumpFalse", "handlerLtIntJumpFalse compares ints[B] < ints[C] and jumps if false.", "LT"),
		compareRegisterJumpFalseHandler("handlerLeIntJumpFalse", "handlerLeIntJumpFalse compares ints[B] <= ints[C] and jumps if false.", "LE"),
		compareRegisterJumpFalseHandler("handlerGtIntJumpFalse", "handlerGtIntJumpFalse compares ints[B] > ints[C] and jumps if false.", "GT"),
		compareRegisterJumpFalseHandler("handlerGeIntJumpFalse", "handlerGeIntJumpFalse compares ints[B] >= ints[C] and jumps if false.", "GE"),
		compareRegisterJumpFalseHandler("handlerEqIntJumpFalse", "handlerEqIntJumpFalse compares ints[B] == ints[C] and jumps if false.", "EQ"),
		compareRegisterJumpFalseHandler("handlerNeIntJumpFalse", "handlerNeIntJumpFalse compares ints[B] != ints[C] and jumps if false.", "NE"),
	}
}

// compareRegisterJumpFalseHandler produces the handler for a fused register-to-register
// compare-and-jump-if-false sub-op.
//
// Takes name (string) which is the handler function name.
// Takes comment (string) which is the handler's doc comment.
// Takes condition (string) which is the comparison condition mnemonic.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func compareRegisterJumpFalseHandler(name, comment, condition string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: name, Comment: comment,
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			architecture.IntegerCompareAndBranch(emitter, condition, scratches[0], scratches[1], labelTaken)
			architecture.LoadNextInstructionWord(emitter, scratches[0])
			architecture.AddToProgramCounter(emitter, scratches[0])
			architecture.EmitBackEdgePoll(emitter)
			architecture.UnconditionalBranch(emitter, labelDispatch)
			emitter.Blank()
			emitter.Label(labelTaken)
			architecture.IncrementProgramCounter(emitter)
			emitter.Blank()
			emitter.Label(labelDispatch)
			architecture.DispatchNext(emitter)
		},
	}
}
