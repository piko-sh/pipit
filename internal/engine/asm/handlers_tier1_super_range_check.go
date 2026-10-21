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

// handlerInRangeUint64ThenJumpIfFalse builds the fused range-check handler for
// SubOpTier2InRangeUint64ThenJumpIfFalse.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerInRangeUint64ThenJumpIfFalse() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerInRangeUint64ThenJumpIfFalse",
		Comment:   "handlerInRangeUint64ThenJumpIfFalse jumps if uints[C] < ext1.a or uints[C] > ext1.b. The offset is packed in ext2.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitInRangeUint64ThenJumpIfFalse(emitter)
		},
	}
}

// handlerEqualUint64ImmediateThenJumpIfFalse builds the fused uint-immediate
// compare-and-branch handler for SubOpEqualUint64ImmediateThenJumpIfFalse.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerEqualUint64ImmediateThenJumpIfFalse() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerEqualUint64ImmediateThenJumpIfFalse",
		Comment:   "handlerEqualUint64ImmediateThenJumpIfFalse jumps by the ext offset when uints[B] != C, stepping past the ext word and the trailing no-op either way.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitEqualUint64ImmediateThenJumpIfFalse(emitter)
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
		handlerInRangeUint64ThenJumpIfFalse(),
		handlerEqualUint64ImmediateThenJumpIfFalse(),
		compareConstantJumpFalseHandler("handlerLessOrEqualInt64ConstantThenJumpIfFalse",
			"handlerLessOrEqualInt64ConstantThenJumpIfFalse compares ints[B] <= intConstants[C] and jumps if false.",
			"LE"),
		compareConstantJumpFalseHandler("handlerLessThanInt64ConstantThenJumpIfFalse", "handlerLessThanInt64ConstantThenJumpIfFalse compares ints[B] < intConstants[C] and jumps if false.", "LT"),
		compareConstantJumpFalseHandler("handlerEqualInt64ConstantThenJumpIfFalse", "handlerEqualInt64ConstantThenJumpIfFalse compares ints[B] == intConstants[C] and jumps if false.", "EQ"),
		compareConstantJumpTrueHandler(),
		compareConstantJumpFalseHandler("handlerGreaterOrEqualInt64ConstantThenJumpIfFalse",
			"handlerGreaterOrEqualInt64ConstantThenJumpIfFalse compares ints[B] >= intConstants[C] and jumps if false.",
			"GE"),
		compareConstantJumpFalseHandler("handlerGreaterThanInt64ConstantThenJumpIfFalse",
			"handlerGreaterThanInt64ConstantThenJumpIfFalse compares ints[B] > intConstants[C] and jumps if false.",
			"GT"),
		compareRegisterJumpFalseHandler("handlerLessThanInt64ThenJumpIfFalse", "handlerLessThanInt64ThenJumpIfFalse compares ints[B] < ints[C] and jumps if false.", "LT"),
		compareRegisterJumpFalseHandler("handlerLessOrEqualInt64ThenJumpIfFalse", "handlerLessOrEqualInt64ThenJumpIfFalse compares ints[B] <= ints[C] and jumps if false.", "LE"),
		compareRegisterJumpFalseHandler("handlerGreaterThanInt64ThenJumpIfFalse", "handlerGreaterThanInt64ThenJumpIfFalse compares ints[B] > ints[C] and jumps if false.", "GT"),
		compareRegisterJumpFalseHandler("handlerGreaterOrEqualInt64ThenJumpIfFalse", "handlerGreaterOrEqualInt64ThenJumpIfFalse compares ints[B] >= ints[C] and jumps if false.", "GE"),
		compareRegisterJumpFalseHandler("handlerEqualInt64ThenJumpIfFalse", "handlerEqualInt64ThenJumpIfFalse compares ints[B] == ints[C] and jumps if false.", "EQ"),
		compareRegisterJumpFalseHandler("handlerNotEqualInt64ThenJumpIfFalse", "handlerNotEqualInt64ThenJumpIfFalse compares ints[B] != ints[C] and jumps if false.", "NE"),
		compareUintRegisterJumpFalseHandler("handlerEqualUint64ThenJumpIfFalse",
			"handlerEqualUint64ThenJumpIfFalse compares uints[B] == uints[C] and jumps if false.", "EQ"),
		compareUintRegisterJumpFalseHandler("handlerNotEqualUint64ThenJumpIfFalse",
			"handlerNotEqualUint64ThenJumpIfFalse compares uints[B] != uints[C] and jumps if false.", "NE"),
	}
}

// compareUintRegisterJumpFalseHandler produces the handler for a fused compare-and-jump
// of two uint registers.
//
// Takes name (string) which is the handler function name.
// Takes comment (string) which is the handler's doc comment.
// Takes condition (string) which is the condition, EQ or NE.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func compareUintRegisterJumpFalseHandler(name, comment, condition string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: name, Comment: comment,
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			architecture.UintCompareAndBranch(emitter, condition, scratches[0], scratches[1], labelTaken)
			jumpByExtensionOrSkipIt(emitter, architecture, scratches[0])
		},
	}
}

// jumpByExtensionOrSkipIt emits the tail of a fused conditional jump.
//
// Falling into it takes the jump the extension word encodes, and reaching labelTaken
// steps over the extension word instead.
//
// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
// Takes architecture (BytecodeArchitecturePort) which emits the instructions.
// Takes scratch (string) which is a scratch register for the offset.
func jumpByExtensionOrSkipIt(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort, scratch string) {
	architecture.LoadNextInstructionWord(emitter, scratch)
	architecture.AddToProgramCounter(emitter, scratch)
	architecture.EmitBackEdgePoll(emitter)
	architecture.UnconditionalBranch(emitter, labelDispatch)
	emitter.Blank()
	emitter.Label(labelTaken)
	architecture.IncrementProgramCounter(emitter)
	emitter.Blank()
	emitter.Label(labelDispatch)
	architecture.DispatchNext(emitter)
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
			jumpByExtensionOrSkipIt(emitter, architecture, scratches[0])
		},
	}
}
