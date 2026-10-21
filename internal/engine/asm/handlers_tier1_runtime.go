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
	// goSymbolCapacityGeneral is the Plan-9 ASM symbol for the Cap trampoline.
	goSymbolCapacityGeneral = "·asmCallCapacityGeneral(SB)"

	// goSymbolConvertBytesToString is the Plan-9 ASM symbol for the BytesToString trampoline
	// (arena-backed string materialisation).
	goSymbolConvertBytesToString = "·asmCallConvertBytesToString(SB)"

	// goSymbolBoxSliceInt64 is the Plan-9 ASM symbol for the BoxSliceInt64 trampoline
	// (reflect.ValueOf on an int slice via vm).
	goSymbolBoxSliceInt64 = "·asmCallBoxSliceInt64(SB)"
)

// tier1RuntimeHandlers returns the tier-1 umbrella sub-op handler definitions for Go
// runtime intrinsics that need vm access.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler definitions
// for this group.
func tier1RuntimeHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		handlerCapacityGeneral(),
		handlerConvertBytesToString(),
		handlerBoxSliceInt64(),
		handlerAllocateStructLiteral(),
	}
}

// handlerAllocateStructLiteral returns the AllocateStructLiteral handler, performing a
// pure-assembly arena bump allocation of a zeroed pointer-free struct into general[C].
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerAllocateStructLiteral() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerAllocateStructLiteral",
		Comment:   "handlerAllocateStructLiteral carves a zeroed pointer-free struct literal from the arena generic-bytes slab into general[C]. It falls back to Go via EXIT_TIER2.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitAllocateStructLiteral(emitter)
		},
	}
}

// handlerCapacityGeneral returns the Cap sub-op handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerCapacityGeneral() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoTwoOperandShim("handlerCapacityGeneral",
		"handlerCapacityGeneral sets ints[B] = cap(value at C) via asmCallCapacityGeneral (collectionLengthOrCap).",
		goSymbolCapacityGeneral)
}

// handlerConvertBytesToString returns the BytesToString sub-op handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerConvertBytesToString() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoTwoOperandShim("handlerConvertBytesToString",
		"handlerConvertBytesToString sets strings[B] = string(bytes at C) via asmCallConvertBytesToString (arena-backed).",
		goSymbolConvertBytesToString)
}

// handlerBoxSliceInt64 returns the BoxSliceInt64 sub-op handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerBoxSliceInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoTwoOperandShim("handlerBoxSliceInt64",
		"handlerBoxSliceInt64 boxes []int64 at C into general[B] via asmCallBoxSliceInt64 (reflect.ValueOf).",
		goSymbolBoxSliceInt64)
}
