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
	// contextOffsetComplexBase is the symbolic CTX_COMPLEX_BASE define. Each complex slot is
	// 16 bytes (real float64 then imag float64).
	contextOffsetComplexBase = "CTX_COMPLEX_BASE"

	// complexHalfOffsetReal is the offset of the real half within a complex128 slot,
	// expressed as a string for the asmgen primitive.
	complexHalfOffsetReal = "0"

	// complexHalfOffsetImag is the offset of the imaginary half within a complex128 slot,
	// expressed as a string for the asmgen primitive.
	complexHalfOffsetImag = "8"
)

// tier1ComplexHandlers returns the handler definitions for the tier-1 umbrella sub-ops on
// the complex register bank.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler definitions
// for this group.
func tier1ComplexHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		handlerRealPartComplex128(),
		handlerImaginaryPartComplex128(),
		handlerMoveComplex128(),
		handlerNegateComplex128(),
	}
}

// handlerRealPartComplex128 builds the SubOpRealPartComplex128 handler, setting floats[B]
// = real(complex[C]).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerRealPartComplex128() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return complexHalfHandler(
		"handlerRealPartComplex128",
		"handlerRealPartComplex128 sets floats[B] = real(complex[C]).",
		complexHalfOffsetReal,
	)
}

// handlerImaginaryPartComplex128 builds the SubOpImaginaryPartComplex128 handler, setting
// floats[B] = imag(complex[C]).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerImaginaryPartComplex128() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return complexHalfHandler(
		"handlerImaginaryPartComplex128",
		"handlerImaginaryPartComplex128 sets floats[B] = imag(complex[C]).",
		complexHalfOffsetImag,
	)
}

// handlerMoveComplex128 builds the SubOpMoveComplex128 handler, setting complex[B] =
// complex[C] (16-byte copy).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerMoveComplex128() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerMoveComplex128",
		Comment:   "handlerMoveComplex128 sets complex[B] = complex[C] (16-byte copy).",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitComplexCopy(emitter, contextOffsetComplexBase)
		},
	}
}

// handlerNegateComplex128 builds the SubOpNegateComplex128 handler, setting complex[B] =
// -complex[C] via IEEE 754 sign-bit XOR on each float64 half.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerNegateComplex128() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerNegateComplex128",
		Comment:   "handlerNegateComplex128 sets complex[B] = -complex[C] via sign-bit XOR.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitComplexNegate(emitter, contextOffsetComplexBase)
		},
	}
}

// complexHalfHandler builds a tier-1 complex-half extraction handler.
//
// Takes name (string) which is the handler function name.
// Takes comment (string) which is the handler's doc comment.
// Takes halfOffset (string) which is the byte offset of the target half within a
// complex128 slot.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func complexHalfHandler(name, comment, halfOffset string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   comment,
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			architecture.LoadComplexHalfToFloatBank(emitter, contextOffsetComplexBase, scratches[1], halfOffset, scratches[0])
			architecture.DispatchNext(emitter)
		},
	}
}
