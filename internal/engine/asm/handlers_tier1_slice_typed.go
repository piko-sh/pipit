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

	// goSymbolAppendSliceInt64 is the Plan-9 ASM symbol of the asmCallAppendSliceInt64 Go
	// trampoline (append element to a []int64 typed-bank slice).
	goSymbolAppendSliceInt64 = "·asmCallAppendSliceInt64(SB)"

	// goSymbolAppendSliceFloat64 is the Plan-9 ASM symbol of the asmCallAppendSliceFloat64
	// Go trampoline.
	goSymbolAppendSliceFloat64 = "·asmCallAppendSliceFloat64(SB)"

	// goSymbolAppendSliceString is the Plan-9 ASM symbol of the asmCallAppendSliceString Go
	// trampoline.
	goSymbolAppendSliceString = "·asmCallAppendSliceString(SB)"

	// goSymbolAppendSliceBool is the Plan-9 ASM symbol of the asmCallAppendSliceBool Go
	// trampoline.
	goSymbolAppendSliceBool = "·asmCallAppendSliceBool(SB)"

	// goSymbolAppendSliceUint64 is the Plan-9 ASM symbol of the asmCallAppendSliceUint64 Go
	// trampoline.
	goSymbolAppendSliceUint64 = "·asmCallAppendSliceUint64(SB)"

	// goSymbolAppendSliceByte is the Plan-9 ASM symbol of the asmCallAppendSliceByte Go
	// trampoline (the element is read from the uint bank and truncated to a byte).
	goSymbolAppendSliceByte = "·asmCallAppendSliceByte(SB)"
)

// handlerGetSliceInt64Element builds the tier-0 ints[A] = slicesInt[B][ints[C]] handler
// with bounds check.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerGetSliceInt64Element() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerGetSliceInt64Element",
		Comment:   "handlerGetSliceInt64Element sets ints[A] = slicesInt[B][ints[C]] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitGetSliceInt64Element(emitter, contextOffsetSlicesIntBase)
		},
	}
}

// handlerSetSliceInt64Element builds the tier-0 slicesInt[A][ints[B]] = ints[C] handler
// with bounds check.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSetSliceInt64Element() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSetSliceInt64Element",
		Comment:   "handlerSetSliceInt64Element sets slicesInt[A][ints[B]] = ints[C] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSetSliceInt64Element(emitter, contextOffsetSlicesIntBase)
		},
	}
}

// handlerGetSliceInt64ElementUnchecked builds the tier-0 ints[A] = slicesInt[B][ints[C]]
// handler without a bounds check (BCE-proven sites only).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerGetSliceInt64ElementUnchecked() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerGetSliceInt64ElementUnchecked",
		Comment:   "handlerGetSliceInt64ElementUnchecked sets ints[A] = slicesInt[B][ints[C]] without a bounds check (BCE-proven).",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitGetSliceInt64ElementUnchecked(emitter, contextOffsetSlicesIntBase)
		},
	}
}

// handlerSetSliceInt64ElementUnchecked builds the tier-0 slicesInt[A][ints[B]] = ints[C]
// handler without a bounds check (BCE-proven sites only).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSetSliceInt64ElementUnchecked() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSetSliceInt64ElementUnchecked",
		Comment:   "handlerSetSliceInt64ElementUnchecked sets slicesInt[A][ints[B]] = ints[C] without a bounds check (BCE-proven).",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSetSliceInt64ElementUnchecked(emitter, contextOffsetSlicesIntBase)
		},
	}
}

// tier1SliceTypedHandlers returns the handler definitions for the tier-1 umbrella sub-ops
// that read or update typed-slice register banks without leaving the ASM dispatch loop.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler list.
func tier1SliceTypedHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		handlerGetSliceInt64Element(),
		handlerSetSliceInt64Element(),
		handlerGetSliceInt64ElementUnchecked(),
		handlerSetSliceInt64ElementUnchecked(),
		handlerLengthSliceInt64(),
		handlerLengthSliceFloat64(),
		handlerLengthSliceString(),
		handlerLengthSliceBool(),
		handlerLengthSliceUint64(),
		handlerLengthSliceByte(),
		handlerGetSliceFloat64Element(),
		handlerSetSliceFloat64Element(),
		handlerGetSliceUint64Element(),
		handlerSetSliceUint64Element(),
		handlerGetSliceBoolElement(),
		handlerSetSliceBoolElement(),
		handlerGetSliceStringElement(),
		handlerGetSliceByteElement(),
		handlerGetSliceByteElementInt64(),
		handlerSetSliceByteElement(),
		handlerSetSliceByteElementRun(),
		handlerSliceSliceByte(),
		handlerMoveSliceInt64(),
		handlerMoveSliceFloat64(),
		handlerMoveSliceString(),
		handlerMoveSliceBool(),
		handlerMoveSliceUint64(),
		handlerMoveSliceByte(),
		handlerSliceSliceInt64(),
		handlerSliceSliceFloat64(),
		handlerSliceSliceString(),
		handlerSliceSliceBool(),
		handlerSliceSliceUint64(),
		handlerAppendSliceInt64(),
		handlerAppendSliceFloat64(),
		handlerAppendSliceString(),
		handlerAppendSliceBool(),
		handlerAppendSliceUint64(),
		handlerAppendSliceByte(),
	}
}

// handlerAppendSliceInt64 builds the tier-1 handler for isa.SubOpAppendSliceInt64
// computes slicesInt[B] = append(slicesInt[C], ints[ext.A]). 3-operand shim wrapping
// asmCallAppendSliceInt64 so the grow path can take Go's mallocgc.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerAppendSliceInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerAppendSliceInt64",
		"handlerAppendSliceInt64 - 3-operand shim wrapping ·asmCallAppendSliceInt64 (typed-bank []int64 append).",
		goSymbolAppendSliceInt64)
}

// handlerAppendSliceFloat64 builds the tier-1 handler for isa.SubOpAppendSliceFloat64
// computes slicesFloat[B] = append(slicesFloat[C], floats[ext.A]).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerAppendSliceFloat64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerAppendSliceFloat64",
		"handlerAppendSliceFloat64 - 3-operand shim wrapping ·asmCallAppendSliceFloat64 (typed-bank []float64 append).",
		goSymbolAppendSliceFloat64)
}

// handlerAppendSliceString builds the tier-1 handler for isa.SubOpAppendSliceString
// computes slicesString[B] = append(slicesString[C], strings[ext.A]). The trampoline
// routes the element through materialiseString so an arena-borrowed header gets a proper
// backing before being held by the destination slice.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerAppendSliceString() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerAppendSliceString",
		"handlerAppendSliceString - 3-operand shim wrapping ·asmCallAppendSliceString (typed-bank []string append).",
		goSymbolAppendSliceString)
}

// handlerAppendSliceBool builds the tier-1 handler for isa.SubOpAppendSliceBool computes
// slicesBool[B] = append(slicesBool[C], bools[ext.A]).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerAppendSliceBool() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerAppendSliceBool",
		"handlerAppendSliceBool - 3-operand shim wrapping ·asmCallAppendSliceBool (typed-bank []bool append).",
		goSymbolAppendSliceBool)
}

// handlerAppendSliceUint64 builds the tier-1 handler for isa.SubOpAppendSliceUint64
// computes slicesUint[B] = append(slicesUint[C], uints[ext.A]).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerAppendSliceUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerAppendSliceUint64",
		"handlerAppendSliceUint64 - 3-operand shim wrapping ·asmCallAppendSliceUint64 (typed-bank []uint64 append).",
		goSymbolAppendSliceUint64)
}

// handlerAppendSliceByte builds the tier-1 handler for isa.SubOpAppendSliceByte computes
// slicesByte[B] = append(slicesByte[C], byte(uints[ext.A])). The element is read from the
// uint bank and truncated to a byte, matching the convention slicesByte uses for its
// element ABI.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerAppendSliceByte() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim("handlerAppendSliceByte",
		"handlerAppendSliceByte - 3-operand shim wrapping ·asmCallAppendSliceByte (typed-bank []byte append).",
		goSymbolAppendSliceByte)
}

// handlerMoveSliceInt64 builds the tier-1 handler for isa.SubOpMoveSliceInt64.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerMoveSliceInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return moveSliceHandler("handlerMoveSliceInt64", contextOffsetSlicesIntBase, "slicesInt")
}

// handlerMoveSliceFloat64 builds the tier-1 handler for isa.SubOpMoveSliceFloat64.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerMoveSliceFloat64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return moveSliceHandler("handlerMoveSliceFloat64", contextOffsetSlicesFloatBase, "slicesFloat")
}

// handlerMoveSliceString builds the tier-1 handler for isa.SubOpMoveSliceString.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerMoveSliceString() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return moveSliceHandler("handlerMoveSliceString", contextOffsetSlicesStringBase, "slicesString")
}

// handlerMoveSliceBool builds the tier-1 handler for isa.SubOpMoveSliceBool.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerMoveSliceBool() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return moveSliceHandler("handlerMoveSliceBool", contextOffsetSlicesBoolBase, "slicesBool")
}

// handlerMoveSliceUint64 builds the tier-1 handler for isa.SubOpMoveSliceUint64.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerMoveSliceUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return moveSliceHandler("handlerMoveSliceUint64", contextOffsetSlicesUintBase, "slicesUint")
}

// handlerMoveSliceByte builds the tier-1 handler for isa.SubOpMoveSliceByte.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerMoveSliceByte() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return moveSliceHandler("handlerMoveSliceByte", contextOffsetSlicesByteBase, "slicesByte")
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
	return guardPointerBankWriter(asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   name + " copies " + bankLabel + "[C] to " + bankLabel + "[B] for the tier-1 typed-slice header move sub-op.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceMove(emitter, contextOffset)
		},
	})
}

// handlerSliceSliceInt64 builds the tier-1 handler for isa.SubOpSliceSliceInt64 computes
// slicesInt[A] = slicesInt[C][low:high]. Element stride 8 bytes.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSliceSliceInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return sliceSliceHandler("handlerSliceSliceInt64", contextOffsetSlicesIntBase, "slicesInt", elementSizeShiftStride8)
}

// handlerSliceSliceFloat64 builds the tier-1 handler for isa.SubOpSliceSliceFloat64
// computes slicesFloat[A] = slicesFloat[C][low:high]. Element stride 8 bytes.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSliceSliceFloat64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return sliceSliceHandler("handlerSliceSliceFloat64", contextOffsetSlicesFloatBase, "slicesFloat", elementSizeShiftStride8)
}

// handlerSliceSliceString builds the tier-1 handler for isa.SubOpSliceSliceString
// computes slicesString[A] = slicesString[C][low:high]. Element stride 16 bytes (Go
// string header with data pointer and length).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSliceSliceString() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return sliceSliceHandler("handlerSliceSliceString", contextOffsetSlicesStringBase, "slicesString", elementSizeShiftStride16)
}

// handlerSliceSliceBool builds the tier-1 handler for isa.SubOpSliceSliceBool computes
// slicesBool[A] = slicesBool[C][low:high]. Element stride 1 byte.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSliceSliceBool() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return sliceSliceHandler("handlerSliceSliceBool", contextOffsetSlicesBoolBase, "slicesBool", elementSizeShiftStride1)
}

// handlerSliceSliceUint64 builds the tier-1 handler for isa.SubOpSliceSliceUint64
// computes slicesUint[A] = slicesUint[C][low:high]. Element stride 8 bytes.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSliceSliceUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return sliceSliceHandler("handlerSliceSliceUint64", contextOffsetSlicesUintBase, "slicesUint", elementSizeShiftStride8)
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
	return guardPointerBankWriter(asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   name + " performs " + bankLabel + "[A] = " + bankLabel + "[C][low:high] with bounds check (any two-index form).",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceSliceSlice(emitter, contextOffset, elementSizeShift)
		},
	})
}

// handlerGetSliceFloat64Element builds the handler for the tier-1 sub-op
// isa.SubOpGetSliceFloat64Element, which sets floats[B] = slicesFloat[C][ints[ext.A]]
// with bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerGetSliceFloat64Element() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerGetSliceFloat64Element",
		Comment:   "handlerGetSliceFloat64Element sets floats[B] = slicesFloat[C][ints[ext.A]] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceFloatGet(emitter, contextOffsetSlicesFloatBase)
		},
	}
}

// handlerSetSliceFloat64Element builds the handler for the tier-1 sub-op
// isa.SubOpSetSliceFloat64Element, which sets slicesFloat[B][ints[C]] = floats[ext.A]
// with bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSetSliceFloat64Element() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSetSliceFloat64Element",
		Comment:   "handlerSetSliceFloat64Element sets slicesFloat[B][ints[C]] = floats[ext.A] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceFloatSet(emitter, contextOffsetSlicesFloatBase)
		},
	}
}

// handlerGetSliceUint64Element builds the SubOpGetSliceUint64Element handler, setting
// uints[B] = slicesUint[C][ints[ext.A]] with bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerGetSliceUint64Element() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerGetSliceUint64Element",
		Comment:   "handlerGetSliceUint64Element sets uints[B] = slicesUint[C][ints[ext.A]] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceUintGet(emitter, contextOffsetSlicesUintBase)
		},
	}
}

// handlerSetSliceUint64Element builds the handler for the tier-1 sub-op
// isa.SubOpSetSliceUint64Element, which sets slicesUint[B][ints[C]] = uints[ext.A] with
// bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSetSliceUint64Element() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSetSliceUint64Element",
		Comment:   "handlerSetSliceUint64Element sets slicesUint[B][ints[C]] = uints[ext.A] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceUintSet(emitter, contextOffsetSlicesUintBase)
		},
	}
}

// handlerGetSliceBoolElement builds the SubOpGetSliceBoolElement handler, setting
// bools[B] = slicesBool[C][ints[ext.A]] with bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerGetSliceBoolElement() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerGetSliceBoolElement",
		Comment:   "handlerGetSliceBoolElement sets bools[B] = slicesBool[C][ints[ext.A]] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceBoolGet(emitter, contextOffsetSlicesBoolBase)
		},
	}
}

// handlerSetSliceBoolElement builds the handler for the tier-1 sub-op
// isa.SubOpSetSliceBoolElement, which sets slicesBool[B][ints[C]] = bools[ext.A] with
// bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSetSliceBoolElement() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSetSliceBoolElement",
		Comment:   "handlerSetSliceBoolElement sets slicesBool[B][ints[C]] = bools[ext.A] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceBoolSet(emitter, contextOffsetSlicesBoolBase)
		},
	}
}

// handlerGetSliceStringElement builds the SubOpGetSliceStringElement handler, setting
// strings[B] = slicesString[C][ints[ext.A]] with bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerGetSliceStringElement() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return guardPointerBankWriter(asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerGetSliceStringElement",
		Comment:   "handlerGetSliceStringElement sets strings[B] = slicesString[C][ints[ext.A]] with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceStringGet(emitter, contextOffsetSlicesStringBase)
		},
	})
}

// handlerLengthSliceInt64 builds the handler for the tier-1 sub-op
// isa.SubOpLengthSliceInt64, which sets ints[B] = int64(len(slicesInt[C])) without
// crossing the ASM/Go boundary.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerLengthSliceInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return typedSliceLenHandler(
		"handlerLengthSliceInt64",
		"handlerLengthSliceInt64 sets ints[B] = int64(len(slicesInt[C])).",
		contextOffsetSlicesIntBase,
	)
}

// handlerLengthSliceFloat64 builds the handler for the tier-1 sub-op
// isa.SubOpLengthSliceFloat64, which sets ints[B] = int64(len(slicesFloat[C])) without
// crossing the ASM/Go boundary.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerLengthSliceFloat64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return typedSliceLenHandler(
		"handlerLengthSliceFloat64",
		"handlerLengthSliceFloat64 sets ints[B] = int64(len(slicesFloat[C])).",
		contextOffsetSlicesFloatBase,
	)
}

// handlerLengthSliceString builds the handler for the tier-1 sub-op
// isa.SubOpLengthSliceString, which sets ints[B] = int64(len(slicesString[C])) without
// crossing the ASM/Go boundary.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerLengthSliceString() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return typedSliceLenHandler(
		"handlerLengthSliceString",
		"handlerLengthSliceString sets ints[B] = int64(len(slicesString[C])).",
		contextOffsetSlicesStringBase,
	)
}

// handlerLengthSliceBool builds the handler for the tier-1 sub-op
// isa.SubOpLengthSliceBool, which sets ints[B] = int64(len(slicesBool[C])) without
// crossing the ASM/Go boundary.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerLengthSliceBool() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return typedSliceLenHandler(
		"handlerLengthSliceBool",
		"handlerLengthSliceBool sets ints[B] = int64(len(slicesBool[C])).",
		contextOffsetSlicesBoolBase,
	)
}

// handlerLengthSliceUint64 builds the handler for the tier-1 sub-op
// isa.SubOpLengthSliceUint64, which sets ints[B] = int64(len(slicesUint[C])) without
// crossing the ASM/Go boundary.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerLengthSliceUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return typedSliceLenHandler(
		"handlerLengthSliceUint64",
		"handlerLengthSliceUint64 sets ints[B] = int64(len(slicesUint[C])).",
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

// handlerLengthSliceByte builds the handler for the tier-1 sub-op
// isa.SubOpLengthSliceByte, which sets ints[B] = int64(len(slicesByte[C])) without
// crossing the ASM/Go boundary.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerLengthSliceByte() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return typedSliceLenHandler(
		"handlerLengthSliceByte",
		"handlerLengthSliceByte sets ints[B] = int64(len(slicesByte[C])).",
		contextOffsetSlicesByteBase,
	)
}

// handlerGetSliceByteElement builds the SubOpGetSliceByteElement handler, setting
// uints[B] = uint64(slicesByte[C][ints[ext.A]]) with bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerGetSliceByteElement() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerGetSliceByteElement",
		Comment:   "handlerGetSliceByteElement sets uints[B] = uint64(slicesByte[C][ints[ext.A]]) with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceByteGet(emitter, contextOffsetSlicesByteBase)
		},
	}
}

// handlerSetSliceByteElement builds the handler for the tier-1 sub-op
// isa.SubOpSetSliceByteElement, which sets slicesByte[B][ints[C]+ext.B] =
// byte(uints[ext.A]) with bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSetSliceByteElement() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSetSliceByteElement",
		Comment:   "handlerSetSliceByteElement sets slicesByte[B][ints[C]+ext.B] = byte(uints[ext.A]) with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceByteSet(emitter, contextOffsetSlicesByteBase)
		},
	}
}

// handlerGetSliceByteElementInt64 builds the SubOpGetSliceByteElementInt64 handler,
// setting ints[B] = int64(slicesByte[C][ints[ext.A]]) with bounds checking.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerGetSliceByteElementInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerGetSliceByteElementInt64",
		Comment:   "handlerGetSliceByteElementInt64 sets ints[B] = int64(slicesByte[C][ints[ext.A]]) with bounds check.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceByteGetInt(emitter, contextOffsetSlicesByteBase)
		},
	}
}

// handlerSetSliceByteElementRun builds the handler for the tier-1 sub-op
// isa.SubOpSetSliceByteElementRun, which stores two to four bytes at consecutive
// positions of a byte slice. A run that would leave the slice is left to the Go handler,
// which makes the stores that fit before it panics.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSetSliceByteElementRun() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerSetSliceByteElementRun",
		Comment: "handlerSetSliceByteElementRun stores ext1.C bytes at slicesByte[B][ints[C]+ext1.B] onwards, " +
			"the byte of each store from the uint register its own extension word names in A.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTypedSliceByteSetRun(emitter, contextOffsetSlicesByteBase)
		},
	}
}

// handlerSliceSliceByte builds the tier-1 byte-slice handler. Two-index slices run in
// assembly, while three-index slices use the Go handler.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerSliceSliceByte() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return guardPointerBankWriter(asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerSliceSliceByte",
		Comment:   "handlerSliceSliceByte sets slicesByte[A] = slicesByte[C][ints[ext.b]:ints[ext.c]] (no max-bound) inline.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSliceSliceByte(emitter, contextOffsetSlicesByteBase)
		},
	})
}
