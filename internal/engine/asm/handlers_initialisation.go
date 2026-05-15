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

// initialisationHandlers returns the handler definitions for dispatch loop
// initialisation, jump table setup, and exit handlers.
//
// Returns []asmgen.HandlerDefinition[BytecodeArchitecturePort] covering initialisation
// and exit opcodes.
func initialisationHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		handlerInitJumpTable(),
		handlerInitJumpTableSSE41(),
		handlerInitSubOpJumpTables(),
		handlerDispatchLoop(),
		handlerTier2Fallback(),
		handlerCallExit(),
		handlerReturnExit(),
		handlerReturnVoidExit(),
		handlerTailCallExit(),
		handlerDispatchExit(),
	}
}

// handlerInitSubOpJumpTables returns the handler definition for initSubOpJumpTables,
// which patches .abi0 entry addresses into the sub-op tables. Using .abi0 rather than the
// ABIInternal wrapper avoids the per-dispatch frame that overflows NOSPLIT in tight
// loops.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerInitSubOpJumpTables() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "initSubOpJumpTables",
		Comment:   "initSubOpJumpTables installs .abi0 addresses of tier-1+ ASM handlers into the sub-op jump tables, bypassing the ABIInternal wrapper that adds a per-dispatch frame.",
		FrameSize: "$0-0", Flags: flagNoSplit,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InitialisationOperations().EmitInitSubOpJumpTables(emitter)
		},
	}
}

// handlerInitJumpTable returns the handler definition for initJumpTable, which populates
// the 256-entry dispatch table with pathBFallback as the default and patches tier-1
// opcode entries with their specific handler addresses.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerInitJumpTable() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "initJumpTable",
		Comment:   "initJumpTable fills the 256-entry dispatch table; tier-1 opcodes get their specific handlers, others get pathBFallback.",
		FrameSize: "$0-8", Flags: flagNoSplit,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InitialisationOperations().EmitInitJumpTable(emitter)
		},
	}
}

// handlerInitJumpTableSSE41 returns the handler definition for initJumpTableSSE41, which
// patches dispatch table entries for Floor, Ceil, and Trunc with ROUNDSD-based handlers
// on amd64 when the CPU supports SSE4.1.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerInitJumpTableSSE41() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:          "initJumpTableSSE41",
		Comment:       "initJumpTableSSE41 patches dispatch entries for ROUNDSD-based handlers (Floor, Ceil, Trunc); called only when the CPU supports SSE4.1.",
		Architectures: []asmgen.Architecture{asmgen.ArchitectureAMD64},
		FrameSize:     "$0-8", Flags: flagNoSplit,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InitialisationOperations().EmitInitJumpTableSSE41(emitter)
		},
	}
}

// handlerDispatchLoop returns the handler definition for dispatchLoop, the ASM dispatch
// entry point that loads the dispatchContext into pinned registers and performs the first
// DISPATCH_NEXT.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerDispatchLoop() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "dispatchLoop",
		Comment: "dispatchLoop is the ASM dispatch entry point; loads ctx into pinned registers and performs " +
			"the first dispatch (subsequent dispatches happen via DISPATCH_NEXT at each handler tail).",
		FrameSize: "$0-8", Flags: flagNoSplit,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InitialisationOperations().EmitDispatchLoop(emitter)
		},
	}
}

// handlerTier2Fallback returns the handler definition for pathBFallback, the default
// dispatch table target for non-tier-1 opcodes that un-advances the PC and returns to Go
// with EXIT_TIER2.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerTier2Fallback() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "pathBFallback", Comment: "pathBFallback is the default handler for non-tier-1 opcodes; un-advances pc and returns to Go with EXIT_TIER2.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InitialisationOperations().EmitTier2Fallback(emitter)
		},
	}
}

// handlerCallExit returns the EXIT_CALL exit handler, returning to Go for OpCall.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerCallExit() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerCallExit", Comment: "handlerCallExit exits to Go with EXIT_CALL for dedicated OpCall handling.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InitialisationOperations().EmitExitHandler(emitter, "EXIT_CALL")
		},
	}
}

// handlerReturnExit returns the EXIT_RETURN exit handler, returning to Go for OpReturn.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerReturnExit() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerReturnExit", Comment: "handlerReturnExit exits to Go with EXIT_RETURN for dedicated OpReturn handling.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InitialisationOperations().EmitExitHandler(emitter, "EXIT_RETURN")
		},
	}
}

// handlerReturnVoidExit returns the EXIT_RETURN_VOID exit handler, returning to Go for
// OpReturnVoid.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerReturnVoidExit() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerReturnVoidExit", Comment: "handlerReturnVoidExit exits to Go with EXIT_RETURN_VOID for dedicated OpReturnVoid handling.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InitialisationOperations().EmitExitHandler(emitter, "EXIT_RETURN_VOID")
		},
	}
}

// handlerTailCallExit returns the EXIT_TAIL_CALL exit handler, returning to Go for
// OpTailCall.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerTailCallExit() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerTailCallExit", Comment: "handlerTailCallExit exits to Go with EXIT_TAIL_CALL for dedicated OpTailCall handling.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.InitialisationOperations().EmitExitHandler(emitter, "EXIT_TAIL_CALL")
		},
	}
}

// handlerDispatchExit returns the arm64-only dispatchExit stub, a NOFRAME single-RET
// target. arm64 FRAMEd shims JMP here because a literal RET inside a FRAMEd function
// triggers an auto-epilogue that double-tears-down the manually cleaned frame.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerDispatchExit() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "dispatchExit",
		Comment: "dispatchExit is the arm64 JMP target for FRAMEd shims and DISPATCH_NEXT/DIV_BY_ZERO_EXIT paths; " +
			"its single RET pops the caller-restored R30 without the auto-epilogue that would double-tear-down a FRAMEd shim.",
		FrameSize:     frameSizeZero,
		Flags:         flagsNoSplitNoFrame,
		Architectures: []asmgen.Architecture{asmgen.ArchitectureARM64},
		Emit: func(emitter *asmgen.Emitter, _ BytecodeArchitecturePort) {
			emitter.Instruction("RET")
		},
	}
}
