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
	// labelDivisionByZero is the branch-target label used by integer division handlers when
	// the divisor is zero.
	labelDivisionByZero = "dbz"

	// dataTempScratch0 identifies the scratch register slot allocated for intermediate
	// values in arithmetic handlers. The asmgen architecture port resolves the index to a
	// concrete register; this name exists to give the bytecode handler code a
	// self-documenting alternative to bare integer literals.
	dataTempScratch0 = 2
)

// arithmeticHandlers returns the handler definitions for constant loading, integer and
// uint arithmetic, bitwise ops, and floating-point arithmetic.
//
// Returns []asmgen.HandlerDefinition[BytecodeArchitecturePort] covering all arithmetic
// and data-movement opcodes.
func arithmeticHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		handlerLoadUintConst(),
		handlerSubOpLoadUintConstSmall(),
		handlerSubOpAddUintConst(),
		handlerSubOpSubUintConst(),
		handlerSubOpBitAndUintConst(),
		handlerDivUint(),
		handlerRemUint(),
		handlerLoadIntConst(),
		handlerLoadFloatConst(),
		handlerLoadStringConst(),
		handlerLoadBoolConst(),
		handlerLoadBool(),
		handlerLoadIntConstSmall(),
		handlerAddInt(),
		handlerSubInt(),
		handlerMulInt(),
		handlerDivInt(),
		handlerRemInt(),
		handlerBitAnd(),
		handlerBitOr(),
		handlerBitXor(),
		handlerBitAndNot(),
		handlerShiftLeft(),
		handlerShiftRight(),
		handlerAddFloat(),
		handlerSubFloat(),
		handlerMulFloat(),
		handlerDivFloat(),
		handlerAddUint(),
		handlerSubUint(),
		handlerMulUint(),
		handlerBitAndUint(),
		handlerBitOrUint(),
		handlerBitXorUint(),
		handlerBitAndNotUint(),
		handlerShiftLeftUint(),
		handlerShiftRightUint(),
	}
}

// handlerLoadIntConst builds the LoadIntConst handler, loading intConstants[B|(C<<8)]
// into ints[A].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerLoadIntConst() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLoadIntConst", Comment: "handlerLoadIntConst loads intConstants[B|(C<<8)] into ints[A].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractWideBC(emitter, scratches[1])
			temp := architecture.DataTemporary(dataTempScratch0)
			architecture.LoadConstant(emitter, asmgen.RegisterBankInteger, scratches[1], temp)
			architecture.StoreToBank(emitter, asmgen.RegisterBankInteger, temp, scratches[0])
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerLoadFloatConst builds the LoadFloatConst handler, loading
// floatConstants[B|(C<<8)] into floats[A].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerLoadFloatConst() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLoadFloatConst", Comment: "handlerLoadFloatConst loads floatConstants[B|(C<<8)] into floats[A].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractWideBC(emitter, scratches[1])
			architecture.LoadFloatConstantToBank(emitter, scratches[0], scratches[1])
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerLoadUintConst builds the handler definition for the tier-0 LoadUintConst opcode:
// uints[A] = uintConstants[B|(C<<8)].
//
// Returns the handler definition.
func handlerLoadUintConst() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLoadUintConst", Comment: "handlerLoadUintConst loads uintConstants[B|(C<<8)] into uints[A].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitLoadUintConst(emitter)
		},
	}
}

// handlerSubOpLoadUintConstSmall builds the handler definition for the tier-1
// small-constant loader: uints[B] = uint64(C).
//
// Returns the handler definition.
func handlerSubOpLoadUintConstSmall() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerSubOpLoadUintConstSmall", Comment: "handlerSubOpLoadUintConstSmall sets uints[B] = uint64(C).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitLoadUintConstSmall(emitter)
		},
	}
}

// handlerSubOpAddUintConst builds the handler definition for the tier-1 sub-op uints[B] =
// uints[C] + uintConstants[ext], consuming the extension word.
//
// Returns the handler definition.
func handlerSubOpAddUintConst() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerSubOpAddUintConst", Comment: "handlerSubOpAddUintConst sets uints[B] = uints[C] + uintConstants[ext wide index].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitUintConstArith(emitter, "add")
		},
	}
}

// handlerSubOpSubUintConst builds the handler definition for the tier-1 sub-op uints[B] =
// uints[C] - uintConstants[ext], consuming the extension word.
//
// Returns the handler definition.
func handlerSubOpSubUintConst() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerSubOpSubUintConst", Comment: "handlerSubOpSubUintConst sets uints[B] = uints[C] - uintConstants[ext wide index].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitUintConstArith(emitter, "sub")
		},
	}
}

// handlerSubOpBitAndUintConst builds the handler definition for the tier-1 sub-op
// uints[B] = uints[C] & uintConstants[ext], consuming the extension word.
//
// Returns the handler definition.
func handlerSubOpBitAndUintConst() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerSubOpBitAndUintConst", Comment: "handlerSubOpBitAndUintConst sets uints[B] = uints[C] & uintConstants[ext wide index].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitUintConstArith(emitter, "and")
		},
	}
}

// handlerDivUint builds the handler definition for the tier-0 DivUint opcode with a
// zero-divisor guard.
//
// Returns the handler definition.
func handlerDivUint() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerDivUint", Comment: "handlerDivUint sets uints[A] = uints[B] / uints[C].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitUintDivide(emitter)
		},
	}
}

// handlerRemUint builds the handler definition for the tier-0 RemUint opcode with a
// zero-divisor guard.
//
// Returns the handler definition.
func handlerRemUint() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerRemUint", Comment: "handlerRemUint sets uints[A] = uints[B] % uints[C].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitUintRemainder(emitter)
		},
	}
}

// handlerLoadStringConst builds the LoadStringConst handler, loading
// stringConstants[B|(C<<8)] into strings[A].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerLoadStringConst() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLoadStringConst", Comment: "handlerLoadStringConst loads stringConstants[B|(C<<8)] into strings[A].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractWideBC(emitter, scratches[1])
			architecture.StringConstLoad(emitter, scratches[0], scratches[1])
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerLoadBoolConst builds the LoadBoolConst handler, loading boolConstants[C] into
// bools[B].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerLoadBoolConst() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLoadBoolConst", Comment: "handlerLoadBoolConst loads boolConstants[C] into bools[B].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			architecture.BoolConstLoad(emitter, scratches[0], scratches[1])
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerLoadBool builds the LoadBool handler, setting ints[B] = C (0 or 1).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerLoadBool() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLoadBool", Comment: "handlerLoadBool sets ints[B] = C (0 or 1) in tier-1 form (subOpLoadBool).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			architecture.StoreToBank(emitter, asmgen.RegisterBankInteger, scratches[1], scratches[0])
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerLoadIntConstSmall builds the LoadIntConstSmall handler, setting ints[B] =
// int64(C) directly from the instruction word without a constant pool lookup.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerLoadIntConstSmall() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLoadIntConstSmall", Comment: "handlerLoadIntConstSmall sets ints[B] = int64(C) in tier-1 form (subOpLoadIntConstSmall).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			architecture.StoreToBank(emitter, asmgen.RegisterBankInteger, scratches[1], scratches[0])
			architecture.DispatchNext(emitter)
		},
	}
}

// integerBinaryHandler builds a handler for ints[A] = ints[B] <op> ints[C].
//
// Takes name (string) which is the assembly symbol name.
// Takes comment (string) which is the inline comment for the generated assembly.
// Takes operation (string) which selects the ALU instruction.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for the operation.
func integerBinaryHandler(name, comment, operation string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: name, Comment: comment,
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.IntegerBinaryOperation(emitter, operation, scratches[0], scratches[1], scratches[2])
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerAddInt builds the handler definition for the AddInt opcode: ints[A] = ints[B] +
// ints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerAddInt() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return integerBinaryHandler("handlerAddInt", "handlerAddInt sets ints[A] = ints[B] + ints[C].", "ADD")
}

// handlerSubInt builds the handler definition for the SubInt opcode: ints[A] = ints[B] -
// ints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerSubInt() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return integerBinaryHandler("handlerSubInt", "handlerSubInt sets ints[A] = ints[B] - ints[C].", "SUB")
}

// uintBinaryHandler builds a handler for uints[A] = uints[B] <op> uints[C].
//
// Takes name (string) which is the assembly symbol name.
// Takes comment (string) which is the inline comment for the generated assembly.
// Takes operation (string) which selects the ALU instruction.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for the operation.
func uintBinaryHandler(name, comment, operation string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: name, Comment: comment,
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.UintBinaryOperation(emitter, operation, scratches[0], scratches[1], scratches[2])
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerAddUint builds the AddUint handler, computing uints[A] = uints[B] + uints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerAddUint() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return uintBinaryHandler("handlerAddUint", "handlerAddUint sets uints[A] = uints[B] + uints[C].", "ADD")
}

// handlerSubUint builds the handler definition for the SubUint opcode, which performs
// unsigned 64-bit integer subtraction: uints[A] = uints[B] - uints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerSubUint() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return uintBinaryHandler("handlerSubUint", "handlerSubUint sets uints[A] = uints[B] - uints[C].", "SUB")
}

// handlerMulUint builds the MulUint handler, computing uints[A] = uints[B] * uints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerMulUint() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return uintBinaryHandler("handlerMulUint", "handlerMulUint sets uints[A] = uints[B] * uints[C].", "MUL")
}

// handlerBitAndUint builds the handler definition for the BitAndUint opcode: uints[A] =
// uints[B] & uints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitAndUint() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return uintBinaryHandler("handlerBitAndUint", "handlerBitAndUint sets uints[A] = uints[B] & uints[C].", "AND")
}

// handlerBitOrUint builds the handler definition for the BitOrUint opcode: uints[A] =
// uints[B] | uints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitOrUint() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return uintBinaryHandler("handlerBitOrUint", "handlerBitOrUint sets uints[A] = uints[B] | uints[C].", "OR")
}

// handlerBitXorUint builds the handler definition for the BitXorUint opcode: uints[A] =
// uints[B] ^ uints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitXorUint() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return uintBinaryHandler("handlerBitXorUint", "handlerBitXorUint sets uints[A] = uints[B] ^ uints[C].", "XOR")
}

// handlerBitAndNotUint builds the handler definition for the BitAndNotUint opcode:
// uints[A] = uints[B] &^ uints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitAndNotUint() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return uintBinaryHandler("handlerBitAndNotUint", "handlerBitAndNotUint sets uints[A] = uints[B] &^ uints[C].", "ANDNOT")
}

// handlerShiftLeftUint builds the handler definition for the ShiftLeftUint opcode:
// uints[A] = uints[B] << uint(ints[C]). The shift amount is read from the int bank
// (mirrors the int variant).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerShiftLeftUint() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerShiftLeftUint", Comment: "handlerShiftLeftUint sets uints[A] = uints[B] << uint(ints[C]).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.UintShift(emitter, "LEFT", scratches[0], scratches[1], scratches[2])
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerShiftRightUint builds the handler definition for the ShiftRightUint opcode:
// uints[A] = uints[B] >> uint(ints[C]). The right shift is logical (zero-fill), distinct
// from the int variant's arithmetic shift (sign-fill).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerShiftRightUint() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerShiftRightUint", Comment: "handlerShiftRightUint sets uints[A] = uints[B] >> uint(ints[C]).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.UintShift(emitter, "RIGHT", scratches[0], scratches[1], scratches[2])
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerMulInt builds the handler definition for the MulInt opcode: ints[A] = ints[B] *
// ints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerMulInt() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return integerBinaryHandler("handlerMulInt", "handlerMulInt sets ints[A] = ints[B] * ints[C].", "MUL")
}

// handlerDivInt builds the handler definition for the DivInt opcode: ints[A] = ints[B] /
// ints[C], with a zero-divisor guard that exits to Go via DivisionByZeroExit.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerDivInt() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerDivInt", Comment: "handlerDivInt sets ints[A] = ints[B] / ints[C].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.IntegerDivide(emitter, scratches[1], scratches[2], scratches[0], "", labelDivisionByZero)
			architecture.DispatchNext(emitter)
			emitter.Blank()
			emitter.Label(labelDivisionByZero)
			architecture.DivisionByZeroExit(emitter)
		},
	}
}

// handlerRemInt builds the handler definition for the RemInt opcode: ints[A] = ints[B] %
// ints[C], with a zero-divisor guard that exits to Go via DivisionByZeroExit.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerRemInt() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerRemInt", Comment: "handlerRemInt sets ints[A] = ints[B] % ints[C].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.IntegerDivide(emitter, scratches[1], scratches[2], "", scratches[0], labelDivisionByZero)
			architecture.DispatchNext(emitter)
			emitter.Blank()
			emitter.Label(labelDivisionByZero)
			architecture.DivisionByZeroExit(emitter)
		},
	}
}

// handlerBitAnd builds the handler definition for the BitAnd opcode: ints[A] = ints[B] &
// ints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitAnd() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return integerBinaryHandler("handlerBitAnd", "handlerBitAnd sets ints[A] = ints[B] & ints[C].", "AND")
}

// handlerBitOr builds the handler definition for the BitOr opcode: ints[A] = ints[B] |
// ints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitOr() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return integerBinaryHandler("handlerBitOr", "handlerBitOr sets ints[A] = ints[B] | ints[C].", "OR")
}

// handlerBitXor builds the handler definition for the BitXor opcode: ints[A] = ints[B] ^
// ints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitXor() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return integerBinaryHandler("handlerBitXor", "handlerBitXor sets ints[A] = ints[B] ^ ints[C].", "XOR")
}

// handlerBitAndNot builds the handler definition for the BitAndNot opcode: ints[A] =
// ints[B] &^ ints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitAndNot() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return integerBinaryHandler("handlerBitAndNot", "handlerBitAndNot sets ints[A] = ints[B] &^ ints[C].", "ANDNOT")
}

// handlerShiftLeft builds the handler definition for the ShiftLeft opcode: ints[A] =
// ints[B] << uint(ints[C]).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerShiftLeft() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerShiftLeft", Comment: "handlerShiftLeft sets ints[A] = ints[B] << uint(ints[C]).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.IntegerShift(emitter, "LEFT", scratches[0], scratches[1], scratches[2])
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerShiftRight builds the handler definition for the ShiftRight opcode: ints[A] =
// ints[B] >> uint(ints[C]). The shift is arithmetic (sign-fill).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerShiftRight() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerShiftRight", Comment: "handlerShiftRight sets ints[A] = ints[B] >> uint(ints[C]).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.IntegerShift(emitter, "RIGHT", scratches[0], scratches[1], scratches[2])
			architecture.DispatchNext(emitter)
		},
	}
}

// floatBinaryHandler builds a handler for floats[A] = floats[B] <op> floats[C].
//
// Takes name (string) which is the assembly symbol name.
// Takes comment (string) which is the inline comment for the generated assembly.
// Takes operation (string) which selects the floating-point instruction.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for the operation.
func floatBinaryHandler(name, comment, operation string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: name, Comment: comment,
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractB(emitter, scratches[1])
			architecture.ExtractC(emitter, scratches[2])
			architecture.FloatBinaryOperation(emitter, operation, scratches[0], scratches[1], scratches[2])
			architecture.DispatchNext(emitter)
		},
	}
}

// handlerAddFloat builds the handler definition for the AddFloat opcode: floats[A] =
// floats[B] + floats[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerAddFloat() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return floatBinaryHandler("handlerAddFloat", "handlerAddFloat sets floats[A] = floats[B] + floats[C].", "ADD")
}

// handlerSubFloat builds the handler definition for the SubFloat opcode: floats[A] =
// floats[B] - floats[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerSubFloat() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return floatBinaryHandler("handlerSubFloat", "handlerSubFloat sets floats[A] = floats[B] - floats[C].", "SUB")
}

// handlerMulFloat builds the handler definition for the MulFloat opcode: floats[A] =
// floats[B] * floats[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerMulFloat() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return floatBinaryHandler("handlerMulFloat", "handlerMulFloat sets floats[A] = floats[B] * floats[C].", "MUL")
}

// handlerDivFloat builds the handler definition for the DivFloat opcode: floats[A] =
// floats[B] / floats[C]. No zero-divisor guard is needed because IEEE 754 produces +/-Inf
// or NaN rather than faulting.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerDivFloat() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return floatBinaryHandler("handlerDivFloat", "handlerDivFloat sets floats[A] = floats[B] / floats[C].", "DIV")
}
