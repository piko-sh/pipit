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

// narrow32Handlers returns the handlers that fuse integer arithmetic with the 32-bit
// truncation of its result.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler definitions
// for this group.
func narrow32Handlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		integer32BinaryHandler("handlerAddInt32", "handlerAddInt32 sets ints[A] = int64(int32(ints[B] + ints[C])).", "ADD"),
		integer32BinaryHandler("handlerSubtractInt32", "handlerSubtractInt32 sets ints[A] = int64(int32(ints[B] - ints[C])).", "SUB"),
		integer32BinaryHandler("handlerMultiplyInt32", "handlerMultiplyInt32 sets ints[A] = int64(int32(ints[B] * ints[C])).", "MUL"),
		integer32ConstantHandler("handlerAddInt32Constant", "handlerAddInt32Constant sets ints[A] = int64(int32(ints[B] + intConstants[C])).", "ADD"),
		integer32ConstantHandler("handlerSubtractInt32Constant", "handlerSubtractInt32Constant sets ints[A] = int64(int32(ints[B] - intConstants[C])).", "SUB"),
		uint32BinaryHandler("handlerAddUint32", "handlerAddUint32 sets uints[A] = uint64(uint32(uints[B] + uints[C])).", "ADD"),
		uint32BinaryHandler("handlerSubtractUint32", "handlerSubtractUint32 sets uints[A] = uint64(uint32(uints[B] - uints[C])).", "SUB"),
		uint32BinaryHandler("handlerMultiplyUint32", "handlerMultiplyUint32 sets uints[A] = uint64(uint32(uints[B] * uints[C])).", "MUL"),
		uint32ConstantHandler("handlerAddUint32Constant",
			"handlerAddUint32Constant sets uints[B] = uint64(uint32(uints[C] + uintConstants[ext wide index])).", "add32"),
		uint32ConstantHandler("handlerSubtractUint32Constant",
			"handlerSubtractUint32Constant sets uints[B] = uint64(uint32(uints[C] - uintConstants[ext wide index])).", "sub32"),
	}
}

// uint32ConstantHandler builds a tier-1 handler for uints[B] = uint64(uint32(uints[C] op
// uintConstants[ext])), consuming the extension word.
//
// Takes name (string) which is the handler function name.
// Takes comment (string) which is the handler's doc comment.
// Takes operation (string) which is the narrowing selector EmitUintConstArith accepts.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func uint32ConstantHandler(name, comment, operation string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: name, Comment: comment,
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitUintConstArith(emitter, operation)
		},
	}
}

// integer32BinaryHandler builds a handler for ints[A] = int64(int32(ints[B] op ints[C])).
//
// Takes name (string) which is the handler function name.
// Takes comment (string) which is the handler's doc comment.
// Takes operation (string) which is the arithmetic operation mnemonic.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func integer32BinaryHandler(name, comment, operation string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: name, Comment: comment,
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.IntegerBinaryOperation(emitter, operation, scratches[0], scratches[1], scratches[2])
			architecture.IntegerNarrow32(emitter, scratches[0])
			architecture.DispatchNext(emitter)
		},
	}
}

// integer32ConstantHandler builds a handler for ints[A] = int64(int32(ints[B] op
// intConstants[C])).
//
// Takes name (string) which is the handler function name.
// Takes comment (string) which is the handler's doc comment.
// Takes operation (string) which is the arithmetic operation mnemonic.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func integer32ConstantHandler(name, comment, operation string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: name, Comment: comment,
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.IntegerBinaryOperationConstant(emitter, operation, scratches[0], scratches[1], scratches[2])
			architecture.IntegerNarrow32(emitter, scratches[0])
			architecture.DispatchNext(emitter)
		},
	}
}

// uint32BinaryHandler builds a handler for uints[A] = uint64(uint32(uints[B] op
// uints[C])).
//
// Takes name (string) which is the handler function name.
// Takes comment (string) which is the handler's doc comment.
// Takes operation (string) which is the arithmetic operation mnemonic.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func uint32BinaryHandler(name, comment, operation string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: name, Comment: comment,
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.UintBinaryOperation(emitter, operation, scratches[0], scratches[1], scratches[2])
			architecture.UintNarrow32(emitter, scratches[0], scratches[1])
			architecture.DispatchNext(emitter)
		},
	}
}
