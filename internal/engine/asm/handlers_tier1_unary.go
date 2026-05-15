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

// tier1UnaryHandlers returns the tier-1 ASM unary register handlers covering integer NEG,
// bitwise NOT, and float NEG.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler definitions
// for this group.
func tier1UnaryHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		tier1IntegerUnaryHandler("handlerSubOpNegInt", "NEG",
			"handlerSubOpNegInt computes ints[B] = -ints[C] for the tier-1 integer-negate sub-op."),
		tier1IntegerUnaryHandler("handlerSubOpBitNot", "NOT",
			"handlerSubOpBitNot computes ints[B] = ^ints[C] for the tier-1 bitwise-NOT sub-op."),
		tier1FloatUnaryHandler("handlerSubOpNegFloat", "NEG",
			"handlerSubOpNegFloat computes floats[B] = -floats[C] for the tier-1 float-negate sub-op."),
	}
}

// tier1IntegerUnaryHandler builds a tier-1 integer unary handler.
//
// Takes name (string) which is the handler function name.
// Takes operation (string) which is the unary operation mnemonic.
// Takes comment (string) which is the handler's doc comment.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func tier1IntegerUnaryHandler(name, operation, comment string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   comment,
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			architecture.IntegerUnaryOperation(emitter, operation, scratches[0], scratches[1])
			architecture.DispatchNext(emitter)
		},
	}
}

// tier1FloatUnaryHandler builds a tier-1 float unary handler with the same operand layout
// as tier1IntegerUnaryHandler.
//
// Takes name (string) which is the handler function name.
// Takes operation (string) which is the unary operation mnemonic.
// Takes comment (string) which is the handler's doc comment.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func tier1FloatUnaryHandler(name, operation, comment string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   comment,
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			architecture.FloatUnaryOperation(emitter, operation, scratches[0], scratches[1])
			architecture.DispatchNext(emitter)
		},
	}
}
