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
	// goSymbolCap is the Plan-9 ASM symbol for the Cap trampoline.
	goSymbolCap = "·asmCallCap(SB)"

	// goSymbolBytesToString is the Plan-9 ASM symbol for the BytesToString trampoline
	// (arena-backed string materialisation).
	goSymbolBytesToString = "·asmCallBytesToString(SB)"

	// goSymbolBoxSliceInt is the Plan-9 ASM symbol for the BoxSliceInt trampoline
	// (reflect.ValueOf on an int slice via vm).
	goSymbolBoxSliceInt = "·asmCallBoxSliceInt(SB)"
)

// tier1RuntimeHandlers returns the tier-1 umbrella sub-op handler definitions for Go
// runtime intrinsics that need vm access.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler definitions
// for this group.
func tier1RuntimeHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		handlerSubOpCap(),
		handlerSubOpBytesToString(),
		handlerSubOpBoxSliceInt(),
		handlerSubOpAllocStructLiteral(),
	}
}

// handlerSubOpAllocStructLiteral returns the AllocStructLiteral handler, performing a
// pure-assembly arena bump allocation of a zeroed pointer-free struct into general[C].
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpAllocStructLiteral() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSubOpAllocStructLiteral",
		Comment:   "handlerSubOpAllocStructLiteral carves a zeroed pointer-free struct literal from the arena generic-bytes slab into general[C]; falls back to Go via EXIT_TIER2.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitAllocStructLiteral(emitter)
		},
	}
}

// 2-operand sub-ops (Cap, BytesToString, BoxSliceInt).

// handlerSubOpCap returns the Cap sub-op handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpCap() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoTwoOperandShim("handlerSubOpCap",
		"handlerSubOpCap sets ints[B] = cap(value at C) via asmCallCap (collectionLengthOrCap).",
		goSymbolCap)
}

// handlerSubOpBytesToString returns the BytesToString sub-op handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpBytesToString() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoTwoOperandShim("handlerSubOpBytesToString",
		"handlerSubOpBytesToString sets strings[B] = string(bytes at C) via asmCallBytesToString (arena-backed).",
		goSymbolBytesToString)
}

// handlerSubOpBoxSliceInt returns the BoxSliceInt sub-op handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpBoxSliceInt() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoTwoOperandShim("handlerSubOpBoxSliceInt",
		"handlerSubOpBoxSliceInt boxes []int64 at C into general[B] via asmCallBoxSliceInt (reflect.ValueOf).",
		goSymbolBoxSliceInt)
}
