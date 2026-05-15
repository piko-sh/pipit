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
	// labelSkip is the branch-target label used by conditional jump handlers to bypass the
	// jump offset application.
	labelSkip = "skip"
)

// comparisonHandlers returns the handler definitions for comparison and control flow
// opcodes.
//
// Returns []asmgen.HandlerDefinition[BytecodeArchitecturePort] covering all comparison
// and control flow opcodes.
func comparisonHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		integerComparisonHandler("handlerEqInt", "handlerEqInt sets ints[A] = (ints[B] == ints[C]) ? 1 : 0.", "EQ"),
		integerComparisonHandler("handlerNeInt", "handlerNeInt sets ints[A] = (ints[B] != ints[C]) ? 1 : 0.", "NE"),
		integerComparisonHandler("handlerLtInt", "handlerLtInt sets ints[A] = (ints[B] < ints[C]) ? 1 : 0.", "LT"),
		integerComparisonHandler("handlerLeInt", "handlerLeInt sets ints[A] = (ints[B] <= ints[C]) ? 1 : 0.", "LE"),
		integerComparisonHandler("handlerGtInt", "handlerGtInt sets ints[A] = (ints[B] > ints[C]) ? 1 : 0.", "GT"),
		integerComparisonHandler("handlerGeInt", "handlerGeInt sets ints[A] = (ints[B] >= ints[C]) ? 1 : 0.", "GE"),
		uintComparisonHandler("handlerEqUint", "handlerEqUint sets ints[A] = (uints[B] == uints[C]) ? 1 : 0.", "EQ"),
		uintComparisonHandler("handlerNeUint", "handlerNeUint sets ints[A] = (uints[B] != uints[C]) ? 1 : 0.", "NE"),
		uintComparisonHandler("handlerLtUint", "handlerLtUint sets ints[A] = (uints[B] < uints[C]) ? 1 : 0.", "LT"),
		uintComparisonHandler("handlerLeUint", "handlerLeUint sets ints[A] = (uints[B] <= uints[C]) ? 1 : 0.", "LE"),
		uintComparisonHandler("handlerGtUint", "handlerGtUint sets ints[A] = (uints[B] > uints[C]) ? 1 : 0.", "GT"),
		uintComparisonHandler("handlerGeUint", "handlerGeUint sets ints[A] = (uints[B] >= uints[C]) ? 1 : 0.", "GE"),
		floatComparisonHandler("handlerEqFloat", "handlerEqFloat sets ints[A] = (floats[B] == floats[C]) ? 1 : 0.", "EQ"),
		floatComparisonHandler("handlerNeFloat", "handlerNeFloat sets ints[A] = (floats[B] != floats[C]) ? 1 : 0.", "NE"),
		floatComparisonHandler("handlerLtFloat", "handlerLtFloat sets ints[A] = (floats[B] < floats[C]) ? 1 : 0.", "LT"),
		floatComparisonHandler("handlerLeFloat", "handlerLeFloat sets ints[A] = (floats[B] <= floats[C]) ? 1 : 0.", "LE"),
		floatComparisonHandler("handlerGtFloat", "handlerGtFloat sets ints[A] = (floats[B] > floats[C]) ? 1 : 0.", "GT"),
		floatComparisonHandler("handlerGeFloat", "handlerGeFloat sets ints[A] = (floats[B] >= floats[C]) ? 1 : 0.", "GE"),
		handlerJump(),
		handlerJumpIfTrue(),
		handlerJumpIfFalse(),
	}
}

// integerComparisonHandler builds a handler for a signed integer relational comparison.
//
// Takes name (string) which is the Go symbol name for the TEXT directive.
// Takes comment (string) which is the godoc-style comment for the generated assembly.
// Takes condition (string) which is the relational operator (EQ, NE, LT, LE, GT, GE).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func integerComparisonHandler(name, comment, condition string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: name, Comment: comment,
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.IntegerCompareAndSet(emitter, condition, scratches[0], scratches[1], scratches[2])
			architecture.DispatchNext(emitter)
		},
	}
}

// uintComparisonHandler builds a handler for an unsigned integer relational comparison,
// writing the boolean result into the int bank.
//
// Takes name (string) which is the Go symbol name for the TEXT directive.
// Takes comment (string) which is the godoc-style comment for the generated assembly.
// Takes condition (string) which is the relational operator (EQ, NE, LT, LE, GT, GE).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func uintComparisonHandler(name, comment, condition string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: name, Comment: comment,
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.UintCompareAndSet(emitter, condition, scratches[0], scratches[1], scratches[2])
			architecture.DispatchNext(emitter)
		},
	}
}

// floatComparisonHandler builds a handler for a float relational comparison, writing the
// boolean result into the int bank.
//
// Takes name (string) which is the Go symbol name for the TEXT directive.
// Takes comment (string) which is the godoc-style comment for the generated assembly.
// Takes condition (string) which is the relational operator (EQ, NE, LT, LE, GT, GE).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func floatComparisonHandler(name, comment, condition string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: name, Comment: comment,
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.FloatCompareAndSet(emitter, condition, scratches[0], scratches[1], scratches[2])
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerJump returns the unconditional Jump handler, advancing PC by a signed 16-bit
// offset.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerJump() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerJump", Comment: "handlerJump unconditionally jumps by signed 16-bit offset B|(C<<8).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractSignedBC(emitter, scratches[0])
			architecture.AddToProgramCounter(emitter, scratches[0])
			architecture.EmitBackEdgePoll(emitter)
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerJumpIfTrue returns the JumpIfTrue handler, jumping by a signed offset when
// ints[A] is non-zero.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerJumpIfTrue() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerJumpIfTrue", Comment: "handlerJumpIfTrue jumps if ints[A] != 0.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.LoadFromBank(emitter, asmgen.RegisterBankInteger, scratches[0], scratches[1])
			architecture.TestAndBranch(emitter, scratches[1], "ZERO", labelSkip)
			architecture.ExtractSignedBC(emitter, scratches[0])
			architecture.AddToProgramCounter(emitter, scratches[0])
			architecture.EmitBackEdgePoll(emitter)
			emitter.Blank()
			emitter.Label(labelSkip)
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerJumpIfFalse returns the JumpIfFalse handler, jumping by a signed offset when
// ints[A] is zero.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerJumpIfFalse() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerJumpIfFalse", Comment: "handlerJumpIfFalse jumps if ints[A] == 0.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.LoadFromBank(emitter, asmgen.RegisterBankInteger, scratches[0], scratches[1])
			architecture.TestAndBranch(emitter, scratches[1], "NONZERO", labelSkip)
			architecture.ExtractSignedBC(emitter, scratches[0])
			architecture.AddToProgramCounter(emitter, scratches[0])
			architecture.EmitBackEdgePoll(emitter)
			emitter.Blank()
			emitter.Label(labelSkip)
			architecture.DispatchNext(emitter)
		},
	}
}
