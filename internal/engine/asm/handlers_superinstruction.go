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

const (
	// labelTaken is the branch-target label used when the comparison condition holds and the
	// jump should be skipped.
	labelTaken = "taken"

	// labelDispatch is the convergence label where all paths rejoin before calling
	// DispatchNext.
	labelDispatch = "dispatch"

	// labelJump is the label of the path a fused loop latch takes when it branches back.
	labelJump = "jump"
)

// superinstructionHandlers returns the handler set for fused superinstruction opcodes
// that combine arithmetic and comparison operations with conditional jumps.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler definitions
// for this group.
func superinstructionHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		constantArithmeticHandler("handlerSubtractInt64Constant", "handlerSubtractInt64Constant sets ints[A] = ints[B] - intConstants[C].", "SUB"),
		constantArithmeticHandler("handlerAddInt64Constant", "handlerAddInt64Constant sets ints[A] = ints[B] + intConstants[C].", "ADD"),
		constantArithmeticHandler("handlerMultiplyInt64Constant", "handlerMultiplyInt64Constant sets ints[A] = ints[B] * intConstants[C].", "MUL"),
		handlerAddInt64ConstantThenJump(),
		handlerMultiplyInt64ConstantThenAdd(),
		handlerIncrementInt64ThenJumpIfLessThan(),
		handlerIncrementInt64ThenJumpIfLessOrEqual(),
		handlerIncrementInt64ThenJumpIfLessThanConstant(),
		handlerRangeNextSliceByte(),
	}
}

// constantArithmeticHandler builds a handler for ints[A] = ints[B] op intConstants[C].
//
// Takes name (string) which is the handler function name.
// Takes comment (string) which is the handler's doc comment.
// Takes operation (string) which is the arithmetic operation mnemonic.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func constantArithmeticHandler(name, comment, operation string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: name, Comment: comment,
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.IntegerBinaryOperationConstant(emitter, operation, scratches[0], scratches[1], scratches[2])
			architecture.DispatchNext(emitter)
		},
	}
}

// compareConstantJumpFalseHandler builds a fused compare-against-constant-and-jump
// superinstruction that jumps when the condition does not hold.
//
// The not-taken path polls the back-edge budget so fused loops remain cancellable.
//
// Takes name (string) which is the handler function name.
// Takes comment (string) which is the handler's doc comment.
// Takes condition (string) which is the comparison condition mnemonic.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func compareConstantJumpFalseHandler(name, comment, condition string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: name, Comment: comment,
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])

			architecture.IntegerCompareConstantAndBranch(emitter, condition, scratches[0], scratches[1], labelTaken)
			jumpByExtensionOrSkipIt(emitter, architecture, scratches[0])
		},
	}
}

// compareConstantJumpTrueHandler returns the EqIntConstJumpTrue handler, which jumps when
// ints[B] == intConstants[C].
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func compareConstantJumpTrueHandler() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerEqualInt64ConstantThenJumpIfTrue", Comment: "handlerEqualInt64ConstantThenJumpIfTrue compares ints[B] == intConstants[C] and jumps if true.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])

			architecture.IntegerCompareConstantAndBranch(emitter, "NE", scratches[0], scratches[1], labelTaken)
			jumpByExtensionOrSkipIt(emitter, architecture, scratches[0])
		},
	}
}

// handlerAddInt64ConstantThenJump returns the AddIntJump handler, fusing ints[A] =
// ints[B] + intConstants[C] with an unconditional jump.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerAddInt64ConstantThenJump() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerAddInt64ConstantThenJump", Comment: "handlerAddInt64ConstantThenJump sets ints[A] = ints[B] + intConstants[C] and unconditionally jumps.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.IntegerBinaryOperationConstant(emitter, "ADD", scratches[0], scratches[1], scratches[2])
			architecture.LoadNextInstructionWord(emitter, scratches[0])
			architecture.AddToProgramCounter(emitter, scratches[0])
			architecture.EmitBackEdgePoll(emitter)
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerMultiplyInt64ConstantThenAdd returns the handler fusing a multiplication by a
// constant with the addition of a second register, named by the extension word.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerMultiplyInt64ConstantThenAdd() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerMultiplyInt64ConstantThenAdd",
		Comment:   "handlerMultiplyInt64ConstantThenAdd sets ints[A] = ints[extension.A] + ints[B] * intConstants[C].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitMultiplyConstantThenAdd(emitter)
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerIncrementInt64ThenJumpIfLessThan returns the IncIntJumpLt handler, fusing an
// in-place increment of ints[B] with a less-than comparison against ints[C] and a
// conditional backward jump.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerIncrementInt64ThenJumpIfLessThan() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return incrementThenJumpHandler("handlerIncrementInt64ThenJumpIfLessThan",
		"handlerIncrementInt64ThenJumpIfLessThan increments ints[B] and jumps if ints[B] < ints[C] "+
			"in tier-1 form (subOpIncrementInt64ThenJumpIfLessThan).", "LT")
}

// handlerIncrementInt64ThenJumpIfLessOrEqual returns the handler that fuses an in-place
// increment of ints[B] with a less-or-equal comparison against ints[C] and a conditional
// backward jump.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerIncrementInt64ThenJumpIfLessOrEqual() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return incrementThenJumpHandler("handlerIncrementInt64ThenJumpIfLessOrEqual",
		"handlerIncrementInt64ThenJumpIfLessOrEqual increments ints[B] and jumps if ints[B] <= ints[C].", "LE")
}

// incrementThenJumpHandler builds a handler that increments ints[B] in place and jumps by
// the extension word's offset while the counter compares with ints[C] as the condition
// says.
//
// Takes name (string) which is the handler function name.
// Takes comment (string) which is the handler's doc comment.
// Takes condition (string) which is the comparison that keeps the loop going.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func incrementThenJumpHandler(name, comment, condition string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: name, Comment: comment,
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			architecture.IntegerInPlace(emitter, "INC", scratches[0])

			architecture.IntegerCompareAndBranch(emitter, condition, scratches[0], scratches[1], labelJump)
			architecture.IncrementProgramCounter(emitter)
			architecture.UnconditionalBranch(emitter, labelDispatch)
			emitter.Blank()
			emitter.Label(labelJump)
			architecture.LoadNextInstructionWord(emitter, scratches[0])
			architecture.AddToProgramCounter(emitter, scratches[0])
			architecture.EmitBackEdgePoll(emitter)
			emitter.Blank()
			emitter.Label(labelDispatch)
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerIncrementInt64ThenJumpIfLessThanConstant returns the handler fusing an in-place
// increment of ints[B] with a less-than comparison against intConstants[C] and a
// conditional backward jump.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerIncrementInt64ThenJumpIfLessThanConstant() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerIncrementInt64ThenJumpIfLessThanConstant",
		Comment: "handlerIncrementInt64ThenJumpIfLessThanConstant increments ints[B] and jumps if ints[B] < intConstants[C] " +
			"in tier-1 form (subOpIncrementInt64ThenJumpIfLessThanConstant).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			architecture.IntegerInPlace(emitter, "INC", scratches[0])

			architecture.IntegerCompareConstantAndBranch(emitter, "LT", scratches[0], scratches[1], labelJump)
			architecture.IncrementProgramCounter(emitter)
			architecture.UnconditionalBranch(emitter, labelDispatch)
			emitter.Blank()
			emitter.Label(labelJump)
			architecture.LoadNextInstructionWord(emitter, scratches[0])
			architecture.AddToProgramCounter(emitter, scratches[0])
			architecture.EmitBackEdgePoll(emitter)
			emitter.Blank()
			emitter.Label(labelDispatch)
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerRangeNextSliceByte builds the OpRangeNextSliceByte handler, a typed range-next
// step over slicesByte that applies the ext-word jump offset on end-of-range.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerRangeNextSliceByte() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerRangeNextSliceByte",
		Comment:   "handlerRangeNextSliceByte advances ints[A]; on end-of-range jumps by ext word; else uints[C] = byte at slicesByte[B][ints[A]].",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedRangeNextByte(emitter, contextOffsetSlicesByteBase)
		},
	}
}
