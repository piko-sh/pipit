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
	// contextOffsetSlicesIntBase names the CTX_SLICES_INT_BASE offset of the slicesInt bank
	// base pointer in dispatchContext. The LoadTypedSliceHeaderLength primitive formats this
	// string directly into the emitted instruction operand.
	contextOffsetSlicesIntBase = "CTX_SLICES_INT_BASE"

	// contextOffsetSlicesFloatBase names the CTX_SLICES_FLOAT_BASE offset of the slicesFloat
	// bank base pointer in dispatchContext.
	contextOffsetSlicesFloatBase = "CTX_SLICES_FLOAT_BASE"

	// contextOffsetSlicesStringBase names the CTX_SLICES_STRING_BASE offset of the
	// slicesString bank base pointer in dispatchContext.
	contextOffsetSlicesStringBase = "CTX_SLICES_STRING_BASE"

	// contextOffsetSlicesBoolBase names the CTX_SLICES_BOOL_BASE offset of the slicesBool
	// bank base pointer in dispatchContext.
	contextOffsetSlicesBoolBase = "CTX_SLICES_BOOL_BASE"

	// contextOffsetSlicesUintBase names the CTX_SLICES_UINT_BASE offset of the slicesUint
	// bank base pointer in dispatchContext.
	contextOffsetSlicesUintBase = "CTX_SLICES_UINT_BASE"

	// contextOffsetSlicesByteBase names the CTX_SLICES_BYTE_BASE offset of the slicesByte
	// bank base pointer in dispatchContext.
	contextOffsetSlicesByteBase = "CTX_SLICES_BYTE_BASE"

	// elementSizeShiftStride1 is the log2 stride for byte-element typed slices.
	elementSizeShiftStride1 uint8 = 0

	// elementSizeShiftStride8 is the log2 stride for 8-byte-element typed slices (int64,
	// float64, uint64). Centralised so the per-bank handler factories stay short and the
	// link between bank and stride is auditable in one place.
	elementSizeShiftStride8 uint8 = 3

	// elementSizeShiftStride16 is the log2 stride for 16-byte-element typed slices
	// (complex128).
	elementSizeShiftStride16 uint8 = 4

	// goSymbolAppendSliceIntDirect is the Plan-9 ASM symbol of the
	// asmCallAppendSliceIntDirect Go trampoline (append element to a []int64 typed-bank
	// slice).
	goSymbolAppendSliceIntDirect = "·asmCallAppendSliceIntDirect(SB)"

	// goSymbolAppendSliceFloatDirect is the Plan-9 ASM symbol of the
	// asmCallAppendSliceFloatDirect Go trampoline.
	goSymbolAppendSliceFloatDirect = "·asmCallAppendSliceFloatDirect(SB)"

	// goSymbolAppendSliceStringDirect is the Plan-9 ASM symbol of the
	// asmCallAppendSliceStringDirect Go trampoline.
	goSymbolAppendSliceStringDirect = "·asmCallAppendSliceStringDirect(SB)"

	// goSymbolAppendSliceBoolDirect is the Plan-9 ASM symbol of the
	// asmCallAppendSliceBoolDirect Go trampoline.
	goSymbolAppendSliceBoolDirect = "·asmCallAppendSliceBoolDirect(SB)"

	// goSymbolAppendSliceUintDirect is the Plan-9 ASM symbol of the
	// asmCallAppendSliceUintDirect Go trampoline.
	goSymbolAppendSliceUintDirect = "·asmCallAppendSliceUintDirect(SB)"

	// goSymbolAppendSliceByteDirect is the Plan-9 ASM symbol of the
	// asmCallAppendSliceByteDirect Go trampoline (the element is read from the uint bank and
	// truncated to a byte).
	goSymbolAppendSliceByteDirect = "·asmCallAppendSliceByteDirect(SB)"
)

// handlerSliceGetIntDirect builds the tier-0 ints[A] = slicesInt[B][ints[C]] handler with
// bounds check.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSliceGetIntDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSliceGetIntDirect",
		Comment:   "handlerSliceGetIntDirect sets ints[A] = slicesInt[B][ints[C]] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceIntGetDirect(emitter, contextOffsetSlicesIntBase)
		},
	}
}

// handlerSliceSetIntDirect builds the tier-0 slicesInt[A][ints[B]] = ints[C] handler with
// bounds check.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSliceSetIntDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSliceSetIntDirect",
		Comment:   "handlerSliceSetIntDirect sets slicesInt[A][ints[B]] = ints[C] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceIntSetDirect(emitter, contextOffsetSlicesIntBase)
		},
	}
}

// handlerSliceGetIntDirectUnchecked builds the tier-0 ints[A] = slicesInt[B][ints[C]]
// handler without a bounds check (BCE-proven sites only).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSliceGetIntDirectUnchecked() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSliceGetIntDirectUnchecked",
		Comment:   "handlerSliceGetIntDirectUnchecked sets ints[A] = slicesInt[B][ints[C]] without a bounds check (BCE-proven).",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceIntGetDirectUnchecked(emitter, contextOffsetSlicesIntBase)
		},
	}
}

// handlerSliceSetIntDirectUnchecked builds the tier-0 slicesInt[A][ints[B]] = ints[C]
// handler without a bounds check (BCE-proven sites only).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSliceSetIntDirectUnchecked() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSliceSetIntDirectUnchecked",
		Comment:   "handlerSliceSetIntDirectUnchecked sets slicesInt[A][ints[B]] = ints[C] without a bounds check (BCE-proven).",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceIntSetDirectUnchecked(emitter, contextOffsetSlicesIntBase)
		},
	}
}

// tier1SliceTypedHandlers returns the handler definitions for the tier-1 umbrella sub-ops
// that read or update typed-slice register banks without leaving the ASM dispatch loop.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler list.
func tier1SliceTypedHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		handlerSliceGetIntDirect(),
		handlerSliceSetIntDirect(),
		handlerSliceGetIntDirectUnchecked(),
		handlerSliceSetIntDirectUnchecked(),
		handlerSubOpLenSliceIntDirect(),
		handlerSubOpLenSliceFloatDirect(),
		handlerSubOpLenSliceStringDirect(),
		handlerSubOpLenSliceBoolDirect(),
		handlerSubOpLenSliceUintDirect(),
		handlerSubOpLenSliceByteDirect(),
		handlerSubOpSliceGetFloatDirect(),
		handlerSubOpSliceSetFloatDirect(),
		handlerSubOpSliceGetUintDirect(),
		handlerSubOpSliceSetUintDirect(),
		handlerSubOpSliceGetBoolDirect(),
		handlerSubOpSliceSetBoolDirect(),
		handlerSubOpSliceGetStringDirect(),
		handlerSubOpSliceGetByteDirect(),
		handlerSubOpSliceSetByteDirect(),
		handlerSubOpSliceByteSlice(),
		handlerSubOpMoveSliceInt(),
		handlerSubOpMoveSliceFloat(),
		handlerSubOpMoveSliceString(),
		handlerSubOpMoveSliceBool(),
		handlerSubOpMoveSliceUint(),
		handlerSubOpMoveSliceByte(),
		handlerSubOpSliceSliceIntDirect(),
		handlerSubOpSliceSliceFloatDirect(),
		handlerSubOpSliceSliceStringDirect(),
		handlerSubOpSliceSliceBoolDirect(),
		handlerSubOpSliceSliceUintDirect(),
		handlerSubOpAppendSliceIntDirect(),
		handlerSubOpAppendSliceFloatDirect(),
		handlerSubOpAppendSliceStringDirect(),
		handlerSubOpAppendSliceBoolDirect(),
		handlerSubOpAppendSliceUintDirect(),
		handlerSubOpAppendSliceByteDirect(),
	}
}

// handlerSubOpAppendSliceIntDirect builds the tier-1 handler for
// isa.SubOpAppendSliceIntDirect: slicesInt[B] = append(slicesInt[C], ints[ext.A]).
// 3-operand shim wrapping asmCallAppendSliceIntDirect so the grow path can take Go's
// mallocgc; the trampoline pattern matches the existing make-slice family.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpAppendSliceIntDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerSubOpAppendSliceIntDirect",
		"handlerSubOpAppendSliceIntDirect - 3-operand shim wrapping ·asmCallAppendSliceIntDirect (typed-bank []int64 append).",
		goSymbolAppendSliceIntDirect)
}

// handlerSubOpAppendSliceFloatDirect builds the tier-1 handler for
// isa.SubOpAppendSliceFloatDirect: slicesFloat[B] = append(slicesFloat[C],
// floats[ext.A]).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpAppendSliceFloatDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerSubOpAppendSliceFloatDirect",
		"handlerSubOpAppendSliceFloatDirect - 3-operand shim wrapping ·asmCallAppendSliceFloatDirect (typed-bank []float64 append).",
		goSymbolAppendSliceFloatDirect)
}

// handlerSubOpAppendSliceStringDirect builds the tier-1 handler for
// isa.SubOpAppendSliceStringDirect: slicesString[B] = append(slicesString[C],
// strings[ext.A]). The trampoline routes the element through materialiseString so an
// arena-borrowed header gets a proper backing before being held by the destination slice.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpAppendSliceStringDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerSubOpAppendSliceStringDirect",
		"handlerSubOpAppendSliceStringDirect - 3-operand shim wrapping ·asmCallAppendSliceStringDirect (typed-bank []string append).",
		goSymbolAppendSliceStringDirect)
}

// handlerSubOpAppendSliceBoolDirect builds the tier-1 handler for
// isa.SubOpAppendSliceBoolDirect: slicesBool[B] = append(slicesBool[C], bools[ext.A]).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpAppendSliceBoolDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerSubOpAppendSliceBoolDirect",
		"handlerSubOpAppendSliceBoolDirect - 3-operand shim wrapping ·asmCallAppendSliceBoolDirect (typed-bank []bool append).",
		goSymbolAppendSliceBoolDirect)
}

// handlerSubOpAppendSliceUintDirect builds the tier-1 handler for
// isa.SubOpAppendSliceUintDirect: slicesUint[B] = append(slicesUint[C], uints[ext.A]).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpAppendSliceUintDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerSubOpAppendSliceUintDirect",
		"handlerSubOpAppendSliceUintDirect - 3-operand shim wrapping ·asmCallAppendSliceUintDirect (typed-bank []uint64 append).",
		goSymbolAppendSliceUintDirect)
}

// handlerSubOpAppendSliceByteDirect builds the tier-1 handler for
// isa.SubOpAppendSliceByteDirect: slicesByte[B] = append(slicesByte[C],
// byte(uints[ext.A])). The element is read from the uint bank and truncated to a byte,
// matching the convention slicesByte uses for its element ABI.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpAppendSliceByteDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerSubOpAppendSliceByteDirect",
		"handlerSubOpAppendSliceByteDirect - 3-operand shim wrapping ·asmCallAppendSliceByteDirect (typed-bank []byte append).",
		goSymbolAppendSliceByteDirect)
}

// handlerSubOpMoveSliceInt builds the tier-1 handler for isa.SubOpMoveSliceInt:
// slicesInt[B] = slicesInt[C]. Pure register-to-register slice-header move; no bounds
// check.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpMoveSliceInt() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return moveSliceHandler("handlerSubOpMoveSliceInt", contextOffsetSlicesIntBase, "slicesInt")
}

// handlerSubOpMoveSliceFloat builds the tier-1 handler for isa.SubOpMoveSliceFloat:
// slicesFloat[B] = slicesFloat[C].
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpMoveSliceFloat() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return moveSliceHandler("handlerSubOpMoveSliceFloat", contextOffsetSlicesFloatBase, "slicesFloat")
}

// handlerSubOpMoveSliceString builds the tier-1 handler for isa.SubOpMoveSliceString:
// slicesString[B] = slicesString[C].
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpMoveSliceString() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return moveSliceHandler("handlerSubOpMoveSliceString", contextOffsetSlicesStringBase, "slicesString")
}

// handlerSubOpMoveSliceBool builds the tier-1 handler for isa.SubOpMoveSliceBool:
// slicesBool[B] = slicesBool[C].
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpMoveSliceBool() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return moveSliceHandler("handlerSubOpMoveSliceBool", contextOffsetSlicesBoolBase, "slicesBool")
}

// handlerSubOpMoveSliceUint builds the tier-1 handler for isa.SubOpMoveSliceUint:
// slicesUint[B] = slicesUint[C].
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpMoveSliceUint() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return moveSliceHandler("handlerSubOpMoveSliceUint", contextOffsetSlicesUintBase, "slicesUint")
}

// handlerSubOpMoveSliceByte builds the tier-1 handler for isa.SubOpMoveSliceByte:
// slicesByte[B] = slicesByte[C].
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpMoveSliceByte() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return moveSliceHandler("handlerSubOpMoveSliceByte", contextOffsetSlicesByteBase, "slicesByte")
}

// moveSliceHandler builds a tier-1 handler that copies a 24-byte typed-slice header
// between register slots of the same bank.
//
// Takes name (string) which is the handler function name.
// Takes contextOffset (string) which is the dispatch context offset for the bank.
// Takes bankLabel (string) which is the human-readable bank name for the comment.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func moveSliceHandler(name, contextOffset, bankLabel string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   name + " copies " + bankLabel + "[C] to " + bankLabel + "[B] for the tier-1 typed-slice header move sub-op.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceMove(emitter, contextOffset)
		},
	}
}

// handlerSubOpSliceSliceIntDirect builds the tier-1 handler for
// isa.SubOpSliceSliceIntDirect: slicesInt[A] = slicesInt[C][low:high]. Element stride 8
// bytes.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpSliceSliceIntDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return sliceSliceHandler("handlerSubOpSliceSliceIntDirect", contextOffsetSlicesIntBase, "slicesInt", elementSizeShiftStride8)
}

// handlerSubOpSliceSliceFloatDirect builds the tier-1 handler for
// isa.SubOpSliceSliceFloatDirect: slicesFloat[A] = slicesFloat[C][low:high]. Element
// stride 8 bytes.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpSliceSliceFloatDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return sliceSliceHandler("handlerSubOpSliceSliceFloatDirect", contextOffsetSlicesFloatBase, "slicesFloat", elementSizeShiftStride8)
}

// handlerSubOpSliceSliceStringDirect builds the tier-1 handler for
// isa.SubOpSliceSliceStringDirect: slicesString[A] = slicesString[C][low:high]. Element
// stride 16 bytes (Go string header: data pointer + length).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpSliceSliceStringDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return sliceSliceHandler("handlerSubOpSliceSliceStringDirect", contextOffsetSlicesStringBase, "slicesString", elementSizeShiftStride16)
}

// handlerSubOpSliceSliceBoolDirect builds the tier-1 handler for
// isa.SubOpSliceSliceBoolDirect: slicesBool[A] = slicesBool[C][low:high]. Element stride
// 1 byte.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpSliceSliceBoolDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return sliceSliceHandler("handlerSubOpSliceSliceBoolDirect", contextOffsetSlicesBoolBase, "slicesBool", elementSizeShiftStride1)
}

// handlerSubOpSliceSliceUintDirect builds the tier-1 handler for
// isa.SubOpSliceSliceUintDirect: slicesUint[A] = slicesUint[C][low:high]. Element stride
// 8 bytes.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpSliceSliceUintDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return sliceSliceHandler("handlerSubOpSliceSliceUintDirect", contextOffsetSlicesUintBase, "slicesUint", elementSizeShiftStride8)
}

// sliceSliceHandler builds a tier-1 bounds-checked sub-slice handler for a typed-slice
// bank with a configurable element stride.
//
// Takes name (string) which is the handler function name.
// Takes contextOffset (string) which is the dispatch context offset for the bank.
// Takes bankLabel (string) which is the human-readable bank name for the comment.
// Takes elementSizeShift (uint8) which is the log2 element stride in bytes.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func sliceSliceHandler(name, contextOffset, bankLabel string, elementSizeShift uint8) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   name + " performs " + bankLabel + "[A] = " + bankLabel + "[C][low:high] with bounds check (low+high only).",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceSliceSlice(emitter, contextOffset, elementSizeShift)
		},
	}
}

// handlerSubOpSliceGetFloatDirect builds the handler for the tier-1 sub-op
// isa.SubOpSliceGetFloatDirect, which sets floats[B] = slicesFloat[C][ints[ext.A]] with
// bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpSliceGetFloatDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSubOpSliceGetFloatDirect",
		Comment:   "handlerSubOpSliceGetFloatDirect sets floats[B] = slicesFloat[C][ints[ext.A]] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceFloatGet(emitter, contextOffsetSlicesFloatBase)
		},
	}
}

// handlerSubOpSliceSetFloatDirect builds the handler for the tier-1 sub-op
// isa.SubOpSliceSetFloatDirect, which sets slicesFloat[B][ints[C]] = floats[ext.A] with
// bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpSliceSetFloatDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSubOpSliceSetFloatDirect",
		Comment:   "handlerSubOpSliceSetFloatDirect sets slicesFloat[B][ints[C]] = floats[ext.A] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceFloatSet(emitter, contextOffsetSlicesFloatBase)
		},
	}
}

// handlerSubOpSliceGetUintDirect builds the SubOpSliceGetUintDirect handler, setting
// uints[B] = slicesUint[C][ints[ext.A]] with bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpSliceGetUintDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSubOpSliceGetUintDirect",
		Comment:   "handlerSubOpSliceGetUintDirect sets uints[B] = slicesUint[C][ints[ext.A]] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceUintGet(emitter, contextOffsetSlicesUintBase)
		},
	}
}

// handlerSubOpSliceSetUintDirect builds the handler for the tier-1 sub-op
// isa.SubOpSliceSetUintDirect, which sets slicesUint[B][ints[C]] = uints[ext.A] with
// bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpSliceSetUintDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSubOpSliceSetUintDirect",
		Comment:   "handlerSubOpSliceSetUintDirect sets slicesUint[B][ints[C]] = uints[ext.A] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceUintSet(emitter, contextOffsetSlicesUintBase)
		},
	}
}

// handlerSubOpSliceGetBoolDirect builds the SubOpSliceGetBoolDirect handler, setting
// bools[B] = slicesBool[C][ints[ext.A]] with bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpSliceGetBoolDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSubOpSliceGetBoolDirect",
		Comment:   "handlerSubOpSliceGetBoolDirect sets bools[B] = slicesBool[C][ints[ext.A]] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceBoolGet(emitter, contextOffsetSlicesBoolBase)
		},
	}
}

// handlerSubOpSliceSetBoolDirect builds the handler for the tier-1 sub-op
// isa.SubOpSliceSetBoolDirect, which sets slicesBool[B][ints[C]] = bools[ext.A] with
// bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpSliceSetBoolDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSubOpSliceSetBoolDirect",
		Comment:   "handlerSubOpSliceSetBoolDirect sets slicesBool[B][ints[C]] = bools[ext.A] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceBoolSet(emitter, contextOffsetSlicesBoolBase)
		},
	}
}

// handlerSubOpSliceGetStringDirect builds the SubOpSliceGetStringDirect handler, setting
// strings[B] = slicesString[C][ints[ext.A]] with bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpSliceGetStringDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSubOpSliceGetStringDirect",
		Comment:   "handlerSubOpSliceGetStringDirect sets strings[B] = slicesString[C][ints[ext.A]] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceStringGet(emitter, contextOffsetSlicesStringBase)
		},
	}
}

// handlerSubOpLenSliceIntDirect builds the handler for the tier-1 sub-op
// isa.SubOpLenSliceIntDirect, which sets ints[B] = int64(len(slicesInt[C])) without
// crossing the ASM/Go boundary.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpLenSliceIntDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return typedSliceLenHandler(
		"handlerSubOpLenSliceIntDirect",
		"handlerSubOpLenSliceIntDirect sets ints[B] = int64(len(slicesInt[C])).",
		contextOffsetSlicesIntBase,
	)
}

// handlerSubOpLenSliceFloatDirect builds the handler for the tier-1 sub-op
// isa.SubOpLenSliceFloatDirect, which sets ints[B] = int64(len(slicesFloat[C])) without
// crossing the ASM/Go boundary.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpLenSliceFloatDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return typedSliceLenHandler(
		"handlerSubOpLenSliceFloatDirect",
		"handlerSubOpLenSliceFloatDirect sets ints[B] = int64(len(slicesFloat[C])).",
		contextOffsetSlicesFloatBase,
	)
}

// handlerSubOpLenSliceStringDirect builds the handler for the tier-1 sub-op
// isa.SubOpLenSliceStringDirect, which sets ints[B] = int64(len(slicesString[C])) without
// crossing the ASM/Go boundary.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpLenSliceStringDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return typedSliceLenHandler(
		"handlerSubOpLenSliceStringDirect",
		"handlerSubOpLenSliceStringDirect sets ints[B] = int64(len(slicesString[C])).",
		contextOffsetSlicesStringBase,
	)
}

// handlerSubOpLenSliceBoolDirect builds the handler for the tier-1 sub-op
// isa.SubOpLenSliceBoolDirect, which sets ints[B] = int64(len(slicesBool[C])) without
// crossing the ASM/Go boundary.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpLenSliceBoolDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return typedSliceLenHandler(
		"handlerSubOpLenSliceBoolDirect",
		"handlerSubOpLenSliceBoolDirect sets ints[B] = int64(len(slicesBool[C])).",
		contextOffsetSlicesBoolBase,
	)
}

// handlerSubOpLenSliceUintDirect builds the handler for the tier-1 sub-op
// isa.SubOpLenSliceUintDirect, which sets ints[B] = int64(len(slicesUint[C])) without
// crossing the ASM/Go boundary.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpLenSliceUintDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return typedSliceLenHandler(
		"handlerSubOpLenSliceUintDirect",
		"handlerSubOpLenSliceUintDirect sets ints[B] = int64(len(slicesUint[C])).",
		contextOffsetSlicesUintBase,
	)
}

// typedSliceLenHandler builds a tier-1 typed-slice length handler. Only the context
// offset differs between banks.
//
// Takes name (string) which is the handler function name.
//
// Takes comment (string) which is the handler's doc comment.
//
// Takes contextOffset (string) which is the dispatch context offset for the bank.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func typedSliceLenHandler(name, comment, contextOffset string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   comment,
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			temp := architecture.DataTemporary(dataTempScratch0)
			architecture.LoadTypedSliceHeaderLength(emitter, contextOffset, scratches[1], temp)
			architecture.StoreToBank(emitter, asmgen.RegisterBankInteger, temp, scratches[0])
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerSubOpLenSliceByteDirect builds the handler for the tier-1 sub-op
// isa.SubOpLenSliceByteDirect, which sets ints[B] = int64(len(slicesByte[C])) without
// crossing the ASM/Go boundary.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpLenSliceByteDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return typedSliceLenHandler(
		"handlerSubOpLenSliceByteDirect",
		"handlerSubOpLenSliceByteDirect sets ints[B] = int64(len(slicesByte[C])).",
		contextOffsetSlicesByteBase,
	)
}

// handlerSubOpSliceGetByteDirect builds the SubOpSliceGetByteDirect handler, setting
// uints[B] = uint64(slicesByte[C][ints[ext.A]]) with bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpSliceGetByteDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSubOpSliceGetByteDirect",
		Comment:   "handlerSubOpSliceGetByteDirect sets uints[B] = uint64(slicesByte[C][ints[ext.A]]) with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceByteGet(emitter, contextOffsetSlicesByteBase)
		},
	}
}

// handlerSubOpSliceSetByteDirect builds the handler for the tier-1 sub-op
// isa.SubOpSliceSetByteDirect, which sets slicesByte[B][ints[C]] = byte(uints[ext.A])
// with bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpSliceSetByteDirect() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSubOpSliceSetByteDirect",
		Comment:   "handlerSubOpSliceSetByteDirect sets slicesByte[B][ints[C]] = byte(uints[ext.A]) with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceByteSet(emitter, contextOffsetSlicesByteBase)
		},
	}
}

// handlerSubOpSliceByteSlice builds the handler for the tier-1 sub-op
// isa.SubOpSliceByteSlice, which computes slicesByte[A] =
// slicesByte[C][ints[ext.b]:ints[ext.c]]. Pure ASM body for the low+high case
// (isa.SliceLowBoundFlag | isa.SliceHighBoundFlag); other flag shapes (max-bound,
// low-only, high-only, no-flag) defer to the Go fallback via pathBFallback.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSubOpSliceByteSlice() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSubOpSliceByteSlice",
		Comment:   "handlerSubOpSliceByteSlice sets slicesByte[A] = slicesByte[C][ints[ext.b]:ints[ext.c]] (no max-bound) inline.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceByteSlice(emitter, contextOffsetSlicesByteBase)
		},
	}
}
