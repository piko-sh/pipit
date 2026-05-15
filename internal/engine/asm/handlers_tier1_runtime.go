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

	// goSymbolMakeSliceInt is the Plan-9 ASM symbol for the MakeSliceInt trampoline
	// (runtime.makeslice for []int64).
	goSymbolMakeSliceInt = "·asmCallMakeSliceInt(SB)"

	// goSymbolMakeSliceFloat is the Plan-9 ASM symbol for the MakeSliceFloat trampoline
	// (runtime.makeslice for []float64).
	goSymbolMakeSliceFloat = "·asmCallMakeSliceFloat(SB)"

	// goSymbolMakeSliceString is the Plan-9 ASM symbol for the MakeSliceString trampoline
	// (runtime.makeslice for []string).
	goSymbolMakeSliceString = "·asmCallMakeSliceString(SB)"

	// goSymbolMakeSliceBool is the Plan-9 ASM symbol for the MakeSliceBool trampoline
	// (runtime.makeslice for []bool).
	goSymbolMakeSliceBool = "·asmCallMakeSliceBool(SB)"

	// goSymbolMakeSliceUint is the Plan-9 ASM symbol for the MakeSliceUint trampoline
	// (runtime.makeslice for []uint64).
	goSymbolMakeSliceUint = "·asmCallMakeSliceUint(SB)"

	// goSymbolMakeSliceByte is the Plan-9 ASM symbol for the MakeSliceByte trampoline
	// (runtime.makeslice for []byte).
	goSymbolMakeSliceByte = "·asmCallMakeSliceByte(SB)"
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
		handlerSubOpMakeSliceInt(),
		handlerSubOpMakeSliceFloat(),
		handlerSubOpMakeSliceString(),
		handlerSubOpMakeSliceBool(),
		handlerSubOpMakeSliceUint(),
		handlerSubOpMakeSliceByte(),
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

// 3-operand sub-ops (MakeSlice variants take length from C and capacity from ext.A).

// handlerSubOpMakeSliceInt returns the MakeSliceInt sub-op handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpMakeSliceInt() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerSubOpMakeSliceInt",
		"handlerSubOpMakeSliceInt builds []int64 of length C and cap ext.A into general[B] via asmCallMakeSliceInt.",
		goSymbolMakeSliceInt)
}

// handlerSubOpMakeSliceFloat returns the MakeSliceFloat sub-op handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpMakeSliceFloat() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerSubOpMakeSliceFloat",
		"handlerSubOpMakeSliceFloat builds []float64 of length C and cap ext.A into general[B] via asmCallMakeSliceFloat.",
		goSymbolMakeSliceFloat)
}

// handlerSubOpMakeSliceString returns the MakeSliceString sub-op handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpMakeSliceString() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerSubOpMakeSliceString",
		"handlerSubOpMakeSliceString builds []string of length C and cap ext.A into general[B] via asmCallMakeSliceString.",
		goSymbolMakeSliceString)
}

// handlerSubOpMakeSliceBool returns the MakeSliceBool sub-op handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpMakeSliceBool() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerSubOpMakeSliceBool",
		"handlerSubOpMakeSliceBool builds []bool of length C and cap ext.A into general[B] via asmCallMakeSliceBool.",
		goSymbolMakeSliceBool)
}

// handlerSubOpMakeSliceUint returns the MakeSliceUint sub-op handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpMakeSliceUint() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerSubOpMakeSliceUint",
		"handlerSubOpMakeSliceUint builds []uint64 of length C and cap ext.A into general[B] via asmCallMakeSliceUint.",
		goSymbolMakeSliceUint)
}

// handlerSubOpMakeSliceByte returns the MakeSliceByte sub-op handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpMakeSliceByte() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerSubOpMakeSliceByte",
		"handlerSubOpMakeSliceByte builds []byte of length C and cap ext.A into general[B] via asmCallMakeSliceByte.",
		goSymbolMakeSliceByte)
}
