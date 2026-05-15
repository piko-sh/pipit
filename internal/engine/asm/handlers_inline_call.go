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
		handlerCallInline(),
		handlerCallInlineScalar(),
		handlerCallMethodInline(),
		handlerCallInlineSetupGeneralBank(),
		handlerCallInlineClearGeneralBank(),
		handlerReturnInline(),
		handlerReturnVoidInline(),
		handlerTailCallInline(),
		handlerTailCallInlineSubroutine(),
	}
}

// handlerCallInlineSetupGeneralBank returns the handler definition for the general-bank
// setup trampoline invoked from handlerCallInline when isFastPath == 3.
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

// handlerCallInline returns the inline OpCall handler with ASM fast-path dispatch. Guard
// failures fall back to EXIT_CALL.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerCallInline() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerCallInline",
		Comment:   "handlerCallInline handles OpCall with ASM-inlined fast path.",
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

// handlerCallInlineScalar returns the isa.SubOpCallScalar handler, which shares the
// inline-call fast path with handlerCallInline via EmitCallInline.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerCallInlineScalar() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerCallInlineScalar",
		Comment:   "handlerCallInlineScalar handles opCallScalar; the compile-time gate guarantees scalar-only callees, so it shares handlerCallInline's inline-call body via EmitCallInline.",
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

// handlerCallMethodInline returns the inline method-call handler, using a per-site
// receiver-type cache and the ASM fast path. Cache misses exit with EXIT_TIER2.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerCallMethodInline() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerCallMethodInline",
		Comment:   "handlerCallMethodInline handles opCallMethod with a per-site receiver-type table and the ASM-inlined fast path.",
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

// handlerReturnInline returns the inline OpReturn handler. Guard failures fall back to
// EXIT_RETURN.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerReturnInline() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerReturnInline",
		Comment:   "handlerReturnInline handles OpReturn with ASM-inlined fast path.",
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

// handlerReturnVoidInline returns the inline OpReturnVoid handler. Guard failures fall
// back to EXIT_RETURN_VOID.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerReturnVoidInline() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerReturnVoidInline",
		Comment:   "handlerReturnVoidInline handles OpReturnVoid with ASM-inlined fast path.",
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

// handlerTailCallInline returns the handler definition for the tail-call inline handler,
// which CALLs handlerTailCallInlineSubroutine to perform the tail-call work in-loop.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerTailCallInline() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerTailCallInline",
		Comment:   "handlerTailCallInline handles OpTailCall in-loop via handlerTailCallInlineSubroutine (no exit-reason round-trip).",
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
		Comment:   "handlerTailCallInlineSubroutine performs the asmTailCallExecute round-trip and reloads dispatcher registers; CALLed from handlerTailCallInline.",
		FrameSize: "$24-0",
		Flags:     flagNoSplit,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InlineCallOperations().EmitTailCallInlineSubroutine(emitter)
		},
	}
}
