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

// inlineCallHandlers returns the handler definitions for inline call and return handlers.
//
// Returns []asmgen.HandlerDefinition[BytecodeArchitecturePort] covering inline call and
// return opcodes.
func inlineCallHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		handlerCall(),
		handlerCallScalar(),
		handlerCallMethod(),
		handlerCallInlineSetupGeneralBank(),
		handlerCallInlineClearGeneralBank(),
		handlerReturn(),
		handlerReturnVoid(),
		handlerTailCall(),
		handlerTailCallInlineSubroutine(),
	}
}

// handlerCallInlineSetupGeneralBank returns the handler definition for the general-bank
// setup trampoline invoked from handlerCall when isFastPath == 3.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerCallInlineSetupGeneralBank() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerCallInlineSetupGeneralBank",
		Comment:   "handlerCallInlineSetupGeneralBank: trampoline for isFastPath==3 general-bank setup.",
		FrameSize: "$32-0",
		Flags:     flagNoSplit,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InlineCallOperations().EmitCallInlineSetupGeneralBank(emitter)
		},
	}
}

// handlerCallInlineClearGeneralBank returns the handler definition for the general-bank
// return-side trampoline that clears the GC-visible general slab on frame pop.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerCallInlineClearGeneralBank() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerCallInlineClearGeneralBank",
		Comment:   "handlerCallInlineClearGeneralBank: trampoline that clears the GC-visible general slab on return.",
		FrameSize: "$24-0",
		Flags:     flagNoSplit,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InlineCallOperations().EmitCallInlineClearGeneralBank(emitter)
		},
	}
}

// handlerCall returns the inline isa.SubOpCall handler with ASM fast-path dispatch. Guard
// failures fall back to EXIT_CALL.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerCall() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerCall",
		Comment:   "handlerCall handles OpCall with ASM-inlined fast path.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		ArchFlags: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: flagNoSplit,
		},
		ArchFrameSize: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: frameSizeShim2ArgARM64,
		},
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InlineCallOperations().EmitCallInline(emitter)
		},
	}
}

// handlerCallScalar returns the isa.SubOpCallScalar handler, which shares the inline-call
// fast path with handlerCall via EmitCallInline.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerCallScalar() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerCallScalar",
		Comment:   "handlerCallScalar handles isa.SubOpCallScalar. The compile-time gate guarantees scalar-only callees, so it shares handlerCall's inline-call body via EmitCallInline.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		ArchFlags: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: flagNoSplit,
		},
		ArchFrameSize: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: frameSizeShim2ArgARM64,
		},
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InlineCallOperations().EmitCallInline(emitter)
		},
	}
}

// handlerCallMethod returns the inline method-call handler, using a per-site
// receiver-type cache and the ASM fast path. Cache misses exit with EXIT_TIER2.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerCallMethod() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerCallMethod",
		Comment:   "handlerCallMethod handles isa.SubOpCallMethod with a per-site receiver-type table and the ASM-inlined fast path.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		ArchFlags: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: flagNoSplit,
		},
		ArchFrameSize: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: frameSizeShim2ArgARM64,
		},
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitCallMethodInline(emitter)
		},
	}
}

// handlerReturn returns the inline isa.SubOpTier2Return handler. Guard failures fall back
// to EXIT_RETURN.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerReturn() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerReturn",
		Comment:   "handlerReturn handles OpReturn with ASM-inlined fast path.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		ArchFlags: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: flagNoSplit,
		},
		ArchFrameSize: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: frameSizeShim2ArgARM64,
		},
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InlineCallOperations().EmitReturnInline(emitter)
		},
	}
}

// handlerReturnVoid returns the inline isa.SubOpTier3ReturnVoid handler. Guard failures
// fall back to EXIT_RETURN_VOID.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerReturnVoid() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerReturnVoid",
		Comment:   "handlerReturnVoid handles OpReturnVoid with ASM-inlined fast path.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		ArchFlags: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: flagNoSplit,
		},
		ArchFrameSize: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: frameSizeShim2ArgARM64,
		},
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InlineCallOperations().EmitReturnVoidInline(emitter)
		},
	}
}

// handlerTailCall returns the handler definition for the tail-call inline handler, which
// CALLs handlerTailCallInlineSubroutine to perform the tail-call work in-loop.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerTailCall() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerTailCall",
		Comment:   "handlerTailCall handles isa.SubOpTailCall in-loop via handlerTailCallInlineSubroutine (no exit-reason round-trip).",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		ArchFlags: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: flagNoSplit,
		},
		ArchFrameSize: map[asmgen.Architecture]string{
			asmgen.ArchitectureARM64: frameSizeShim2ArgARM64,
		},
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InlineCallOperations().EmitTailCallInline(emitter)
		},
	}
}

// handlerTailCallInlineSubroutine returns the handler definition for the sub-routine that
// performs the asmTailCallExecute round-trip and reloads dispatcher registers.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerTailCallInlineSubroutine() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerTailCallInlineSubroutine",
		Comment:   "handlerTailCallInlineSubroutine performs the asmTailCallExecute round-trip and reloads dispatcher registers. Called from handlerTailCall.",
		FrameSize: "$24-0",
		Flags:     flagNoSplit,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InlineCallOperations().EmitTailCallInlineSubroutine(emitter)
		},
	}
}
