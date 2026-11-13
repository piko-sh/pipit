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
	// goSymbolMathSin is the Plan-9 ASM symbol of the Go trampoline forwarding to math.Sin.
	goSymbolMathSin = "·asmCallMathSin(SB)"

	// goSymbolMathCos is the Plan-9 ASM symbol of the Go trampoline forwarding to math.Cos.
	goSymbolMathCos = "·asmCallMathCos(SB)"

	// goSymbolMathExp is the Plan-9 ASM symbol of the Go trampoline forwarding to math.Exp.
	goSymbolMathExp = "·asmCallMathExp(SB)"

	// goSymbolMathTan is the Plan-9 ASM symbol of the Go trampoline forwarding to math.Tan.
	goSymbolMathTan = "·asmCallMathTan(SB)"

	// goSymbolMathMod is the Plan-9 ASM symbol of the Go trampoline forwarding to math.Mod
	// (3-operand).
	goSymbolMathMod = "·asmCallMathMod(SB)"
)

// tier1MathHandlers returns the handler definitions for the tier-1 umbrella sub-ops that
// delegate to Go math intrinsics.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler definitions
// for this group.
func tier1MathHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		handlerMathSin(),
		handlerMathCos(),
		handlerMathExp(),
		handlerMathTan(),
		handlerMathMod(),
	}
}

// handlerMathSin builds the SubOpMathSin handler via a single-level inline Go call shim.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerMathSin() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoTwoOperandShim(
		"handlerMathSin",
		"handlerMathSin sets floats[B] = math.Sin(floats[C]) via asmCallMathSin.",
		goSymbolMathSin,
	)
}

// handlerMathCos builds the SubOpMathCos handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerMathCos() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoTwoOperandShim(
		"handlerMathCos",
		"handlerMathCos sets floats[B] = math.Cos(floats[C]) via asmCallMathCos.",
		goSymbolMathCos,
	)
}

// handlerMathExp builds the SubOpMathExp handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerMathExp() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoTwoOperandShim(
		"handlerMathExp",
		"handlerMathExp sets floats[B] = math.Exp(floats[C]) via asmCallMathExp.",
		goSymbolMathExp,
	)
}

// handlerMathTan builds the SubOpMathTan handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerMathTan() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoTwoOperandShim(
		"handlerMathTan",
		"handlerMathTan sets floats[B] = math.Tan(floats[C]) via asmCallMathTan.",
		goSymbolMathTan,
	)
}

// handlerMathMod builds the SubOpMathMod handler (3-operand, computing B = math.Mod(C,
// ext.A)).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerMathMod() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim(
		"handlerMathMod",
		"handlerMathMod sets floats[B] = math.Mod(floats[C], floats[ext.A]) via asmCallMathMod.",
		goSymbolMathMod,
	)
}

// inlineGoTwoOperandShim builds a single-level NOSPLIT handler for a tier-1 sub-op of the
// shape "B = goFn(C)".
//
// Takes name (string) which is the handler function name.
// Takes comment (string) which is the handler's doc comment.
// Takes goSymbol (string) which is the Go symbol to CALL.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func inlineGoTwoOperandShim(name, comment, goSymbol string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   comment,
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		ArchFlags: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: flagNoSplit,
		},
		ArchFrameSize: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: frameSizeShim3ArgARM64,
		},
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitInlineGoCallTwoOperandShim(emitter, goSymbol)
		},
	}
}

// inlineGoThreeOperandShim builds the single-level NOSPLIT handler for a 3-operand sub-op
// (B, C, ext.A). Uses ADJSP $40 to fit the 4-argument abi0 frame.
//
// Takes name (string) which is the handler function name.
// Takes comment (string) which is the handler's doc comment.
// Takes goSymbol (string) which is the Go symbol to CALL.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func inlineGoThreeOperandShim(name, comment, goSymbol string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   comment,
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		ArchFlags: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: flagNoSplit,
		},
		ArchFrameSize: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: frameSizeShim4ArgARM64,
		},
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitInlineGoCallThreeOperandShim(emitter, goSymbol)
		},
	}
}
