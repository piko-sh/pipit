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
		handlerLoadUint64Constant(),
		handlerLoadUint64Immediate(),
		handlerAddUint64Constant(),
		handlerSubtractUint64Constant(),
		handlerBitAndUint64Constant(),
		handlerDivideUint64(),
		handlerRemainderUint64(),
		handlerLoadInt64Constant(),
		handlerLoadFloat64Constant(),
		handlerLoadStringConstant(),
		handlerLoadBoolConstant(),
		handlerLoadBoolImmediateAsInt64(),
		handlerLoadInt64Immediate(),
		handlerAddInt64(),
		handlerSubtractInt64(),
		handlerMultiplyInt64(),
		handlerDivideInt64(),
		handlerRemainderInt64(),
		handlerBitAndInt64(),
		handlerBitOrInt64(),
		handlerBitXorInt64(),
		handlerBitAndNotInt64(),
		handlerShiftLeftInt64(),
		handlerShiftRightInt64(),
		handlerAddFloat64(),
		handlerSubtractFloat64(),
		handlerMultiplyFloat64(),
		handlerDivideFloat64(),
		handlerAddUint64(),
		handlerSubtractUint64(),
		handlerMultiplyUint64(),
		handlerBitAndUint64(),
		handlerBitOrUint64(),
		handlerBitXorUint64(),
		handlerBitAndNotUint64(),
		handlerShiftLeftUint64(),
		handlerShiftRightUint64(),
	}
}

// handlerLoadInt64Constant builds the LoadInt64Constant handler, loading
// intConstants[B|(C<<8)] into ints[A].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerLoadInt64Constant() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLoadInt64Constant", Comment: "handlerLoadInt64Constant loads intConstants[B|(C<<8)] into ints[A].",
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

// handlerLoadFloat64Constant builds the LoadFloat64Constant handler, loading
// floatConstants[B|(C<<8)] into floats[A].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerLoadFloat64Constant() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLoadFloat64Constant", Comment: "handlerLoadFloat64Constant loads floatConstants[B|(C<<8)] into floats[A].",
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

// handlerLoadUint64Constant builds the handler definition for the tier-0
// LoadUint64Constant opcode, which computes uints[A] = uintConstants[B|(C<<8)].
//
// Returns the handler definition.
func handlerLoadUint64Constant() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLoadUint64Constant", Comment: "handlerLoadUint64Constant loads uintConstants[B|(C<<8)] into uints[A].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitLoadUint64Constant(emitter)
		},
	}
}

// handlerLoadUint64Immediate builds the handler definition for the tier-1 small-constant
// loader.
//
// Returns the handler definition.
func handlerLoadUint64Immediate() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLoadUint64Immediate", Comment: "handlerLoadUint64Immediate sets uints[B] = uint64(C).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitLoadUint64Immediate(emitter)
		},
	}
}

// handlerAddUint64Constant builds the handler definition for the tier-1 sub-op uints[B] =
// uints[C] + uintConstants[ext], consuming the extension word.
//
// Returns the handler definition.
func handlerAddUint64Constant() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerAddUint64Constant", Comment: "handlerAddUint64Constant sets uints[B] = uints[C] + uintConstants[ext wide index].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitUintConstArith(emitter, "add")
		},
	}
}

// handlerSubtractUint64Constant builds the handler definition for the tier-1 sub-op
// uints[B] = uints[C] - uintConstants[ext], consuming the extension word.
//
// Returns the handler definition.
func handlerSubtractUint64Constant() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerSubtractUint64Constant", Comment: "handlerSubtractUint64Constant sets uints[B] = uints[C] - uintConstants[ext wide index].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitUintConstArith(emitter, "sub")
		},
	}
}

// handlerBitAndUint64Constant builds the handler definition for the tier-1 sub-op
// uints[B] = uints[C] & uintConstants[ext], consuming the extension word.
//
// Returns the handler definition.
func handlerBitAndUint64Constant() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerBitAndUint64Constant", Comment: "handlerBitAndUint64Constant sets uints[B] = uints[C] & uintConstants[ext wide index].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitUintConstArith(emitter, "and")
		},
	}
}

// handlerDivideUint64 builds the handler definition for the tier-0 DivideUint64 opcode
// with a zero-divisor guard.
//
// Returns the handler definition.
func handlerDivideUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerDivideUint64", Comment: "handlerDivideUint64 sets uints[A] = uints[B] / uints[C].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitUintDivide(emitter)
		},
	}
}

// handlerRemainderUint64 builds the handler definition for the tier-0 RemainderUint64
// opcode with a zero-divisor guard.
//
// Returns the handler definition.
func handlerRemainderUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerRemainderUint64", Comment: "handlerRemainderUint64 sets uints[A] = uints[B] % uints[C].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitUintRemainder(emitter)
		},
	}
}

// handlerLoadStringConstant builds the LoadStringConstant handler, loading
// stringConstants[B|(C<<8)] into strings[A].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerLoadStringConstant() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return guardPointerBankWriter(asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLoadStringConstant", Comment: "handlerLoadStringConstant loads stringConstants[B|(C<<8)] into strings[A].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractA(emitter, scratches[0])
			architecture.ExtractWideBC(emitter, scratches[1])
			architecture.StringConstLoad(emitter, scratches[0], scratches[1])
			architecture.DispatchNext(emitter)
		},
	})
}

// handlerLoadBoolConstant builds the LoadBoolConstant handler, loading boolConstants[C]
// into bools[B].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerLoadBoolConstant() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLoadBoolConstant", Comment: "handlerLoadBoolConstant loads boolConstants[C] into bools[B].",
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

// handlerLoadBoolImmediateAsInt64 builds the LoadBool handler, setting ints[B] = C (0 or
// 1).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerLoadBoolImmediateAsInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLoadBoolImmediateAsInt64", Comment: "handlerLoadBoolImmediateAsInt64 sets ints[B] = C (0 or 1) in tier-1 form (subOpLoadBoolImmediateAsInt64).",
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

// handlerLoadInt64Immediate builds the LoadIntConstSmall handler, setting ints[B] =
// int64(C) directly from the instruction word without a constant pool lookup.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerLoadInt64Immediate() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLoadInt64Immediate", Comment: "handlerLoadInt64Immediate sets ints[B] = int64(C) in tier-1 form (subOpLoadInt64Immediate).",
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

// handlerAddInt64 builds the handler definition for the AddInt64 opcode, which computes
// ints[A] = ints[B] + ints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerAddInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return integerBinaryHandler("handlerAddInt64", "handlerAddInt64 sets ints[A] = ints[B] + ints[C].", "ADD")
}

// handlerSubtractInt64 builds the handler definition for the SubtractInt64 opcode, which
// computes ints[A] = ints[B] - ints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerSubtractInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return integerBinaryHandler("handlerSubtractInt64", "handlerSubtractInt64 sets ints[A] = ints[B] - ints[C].", "SUB")
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

// handlerAddUint64 builds the AddUint64 handler, computing uints[A] = uints[B] +
// uints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerAddUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return uintBinaryHandler("handlerAddUint64", "handlerAddUint64 sets uints[A] = uints[B] + uints[C].", "ADD")
}

// handlerSubtractUint64 builds the handler definition for the SubtractUint64 opcode,
// which performs unsigned 64-bit integer subtraction, computing uints[A] = uints[B] -
// uints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerSubtractUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return uintBinaryHandler("handlerSubtractUint64", "handlerSubtractUint64 sets uints[A] = uints[B] - uints[C].", "SUB")
}

// handlerMultiplyUint64 builds the MultiplyUint64 handler, computing uints[A] = uints[B]
// * uints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerMultiplyUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return uintBinaryHandler("handlerMultiplyUint64", "handlerMultiplyUint64 sets uints[A] = uints[B] * uints[C].", "MUL")
}

// handlerBitAndUint64 builds the handler definition for the BitAndUint64 opcode, which
// computes uints[A] = uints[B] & uints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitAndUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return uintBinaryHandler("handlerBitAndUint64", "handlerBitAndUint64 sets uints[A] = uints[B] & uints[C].", "AND")
}

// handlerBitOrUint64 builds the handler definition for the BitOrUint64 opcode, which
// computes uints[A] = uints[B] | uints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitOrUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return uintBinaryHandler("handlerBitOrUint64", "handlerBitOrUint64 sets uints[A] = uints[B] | uints[C].", "OR")
}

// handlerBitXorUint64 builds the handler definition for the BitXorUint64 opcode, which
// computes uints[A] = uints[B] ^ uints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitXorUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return uintBinaryHandler("handlerBitXorUint64", "handlerBitXorUint64 sets uints[A] = uints[B] ^ uints[C].", "XOR")
}

// handlerBitAndNotUint64 builds the handler definition for the BitAndNotUint64 opcode.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitAndNotUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return uintBinaryHandler("handlerBitAndNotUint64", "handlerBitAndNotUint64 sets uints[A] = uints[B] &^ uints[C].", "ANDNOT")
}

// handlerShiftLeftUint64 builds the handler definition for the ShiftLeftUint64 opcode.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerShiftLeftUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerShiftLeftUint64", Comment: "handlerShiftLeftUint64 sets uints[A] = uints[B] << uint(ints[C]).",
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

// handlerShiftRightUint64 builds the handler definition for the ShiftRightUint64 opcode.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerShiftRightUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerShiftRightUint64", Comment: "handlerShiftRightUint64 sets uints[A] = uints[B] >> uint(ints[C]).",
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

// handlerMultiplyInt64 builds the handler definition for the MultiplyInt64 opcode, which
// computes ints[A] = ints[B] * ints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerMultiplyInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return integerBinaryHandler("handlerMultiplyInt64", "handlerMultiplyInt64 sets ints[A] = ints[B] * ints[C].", "MUL")
}

// handlerDivideInt64 builds the handler definition for the DivideInt64 opcode, which
// computes ints[A] = ints[B] / ints[C], with a zero-divisor guard that exits to Go via
// DivisionByZeroExit.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerDivideInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerDivideInt64", Comment: "handlerDivideInt64 sets ints[A] = ints[B] / ints[C].",
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

// handlerRemainderInt64 builds the handler definition for the RemainderInt64 opcode,
// which computes ints[A] = ints[B] % ints[C], with a zero-divisor guard that exits to Go
// via DivisionByZeroExit.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerRemainderInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerRemainderInt64", Comment: "handlerRemainderInt64 sets ints[A] = ints[B] % ints[C].",
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

// handlerBitAndInt64 builds the handler definition for the BitAnd opcode, which computes
// ints[A] = ints[B] & ints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitAndInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return integerBinaryHandler("handlerBitAndInt64", "handlerBitAndInt64 sets ints[A] = ints[B] & ints[C].", "AND")
}

// handlerBitOrInt64 builds the handler definition for the BitOr opcode, which computes
// ints[A] = ints[B] | ints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitOrInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return integerBinaryHandler("handlerBitOrInt64", "handlerBitOrInt64 sets ints[A] = ints[B] | ints[C].", "OR")
}

// handlerBitXorInt64 builds the handler definition for the BitXor opcode, which computes
// ints[A] = ints[B] ^ ints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitXorInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return integerBinaryHandler("handlerBitXorInt64", "handlerBitXorInt64 sets ints[A] = ints[B] ^ ints[C].", "XOR")
}

// handlerBitAndNotInt64 builds the handler definition for the BitAndNot opcode, which
// computes ints[A] = ints[B] &^ ints[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerBitAndNotInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return integerBinaryHandler("handlerBitAndNotInt64", "handlerBitAndNotInt64 sets ints[A] = ints[B] &^ ints[C].", "ANDNOT")
}

// handlerShiftLeftInt64 builds the handler definition for the ShiftLeft opcode, which
// computes ints[A] = ints[B] << uint(ints[C]).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerShiftLeftInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerShiftLeftInt64", Comment: "handlerShiftLeftInt64 sets ints[A] = ints[B] << uint(ints[C]).",
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

// handlerShiftRightInt64 builds the handler definition for the ShiftRight opcode, which
// computes ints[A] = ints[B] >> uint(ints[C]). The shift is arithmetic (sign-fill).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerShiftRightInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerShiftRightInt64", Comment: "handlerShiftRightInt64 sets ints[A] = ints[B] >> uint(ints[C]).",
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

// handlerAddFloat64 builds the handler definition for the AddFloat64 opcode, which
// computes floats[A] = floats[B] + floats[C].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerAddFloat64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return floatBinaryHandler("handlerAddFloat64", "handlerAddFloat64 sets floats[A] = floats[B] + floats[C].", "ADD")
}

// handlerSubtractFloat64 builds the handler definition for the SubtractFloat64 opcode.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerSubtractFloat64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return floatBinaryHandler("handlerSubtractFloat64", "handlerSubtractFloat64 sets floats[A] = floats[B] - floats[C].", "SUB")
}

// handlerMultiplyFloat64 builds the handler definition for the MultiplyFloat64 opcode.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerMultiplyFloat64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return floatBinaryHandler("handlerMultiplyFloat64", "handlerMultiplyFloat64 sets floats[A] = floats[B] * floats[C].", "MUL")
}

// handlerDivideFloat64 builds the handler definition for the DivideFloat64 opcode, which
// computes floats[A] = floats[B] / floats[C]. No zero-divisor guard is needed because
// IEEE 754 produces +/-Inf or NaN rather than faulting.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerDivideFloat64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return floatBinaryHandler("handlerDivideFloat64", "handlerDivideFloat64 sets floats[A] = floats[B] / floats[C].", "DIV")
}
