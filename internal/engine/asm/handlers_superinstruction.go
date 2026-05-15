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
)

// superinstructionHandlers returns the handler set for fused superinstruction opcodes
// that combine arithmetic and comparison operations with conditional jumps.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler definitions
// for this group.
func superinstructionHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		constantArithmeticHandler("handlerSubIntConst", "handlerSubIntConst sets ints[A] = ints[B] - intConstants[C].", "SUB"),
		constantArithmeticHandler("handlerAddIntConst", "handlerAddIntConst sets ints[A] = ints[B] + intConstants[C].", "ADD"),
		constantArithmeticHandler("handlerMulIntConst", "handlerMulIntConst sets ints[A] = ints[B] * intConstants[C].", "MUL"),
		handlerAddIntJump(),
		handlerIncIntJumpLt(),
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

// compareConstantJumpTrueHandler returns the EqIntConstJumpTrue handler, which jumps when
// ints[B] == intConstants[C]. Polarity is inverted relative to the JumpFalse factory.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func compareConstantJumpTrueHandler() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerEqIntConstJumpTrue", Comment: "handlerEqIntConstJumpTrue compares ints[B] == intConstants[C] and jumps if true.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])

			architecture.IntegerCompareConstantAndBranch(emitter, "NE", scratches[0], scratches[1], labelTaken)

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

// handlerAddIntJump returns the AddIntJump handler, fusing ints[A] = ints[B] +
// intConstants[C] with an unconditional jump.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerAddIntJump() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerAddIntJump", Comment: "handlerAddIntJump sets ints[A] = ints[B] + intConstants[C] and unconditionally jumps.",
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

// handlerIncIntJumpLt returns the IncIntJumpLt handler, fusing an in-place increment of
// ints[B] with a less-than comparison against ints[C] and a conditional backward jump.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerIncIntJumpLt() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerIncIntJumpLt", Comment: "handlerIncIntJumpLt increments ints[B] and jumps if ints[B] < ints[C] in tier-1 form (subOpIncIntJumpLt).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			architecture.IntegerInPlace(emitter, "INC", scratches[0])

			architecture.IntegerCompareAndBranch(emitter, "LT", scratches[0], scratches[1], "jump")
			architecture.IncrementProgramCounter(emitter)
			architecture.UnconditionalBranch(emitter, labelDispatch)
			emitter.Blank()
			emitter.Label("jump")
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
