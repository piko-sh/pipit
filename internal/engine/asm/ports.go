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

	archamd64 "pipit.sh/pipit/internal/engine/asm/asmgen_arch_amd64"
	archarm64 "pipit.sh/pipit/internal/engine/asm/asmgen_arch_arm64"
)

// AsmHandlerJumpTableEntry pairs a handler symbol name with its byte offset into the
// dispatch table.
type AsmHandlerJumpTableEntry struct {
	// Name is the Plan-9 ASM symbol name of the handler (without the leading middle dot).
	// For example, "handlerAddInt64".
	Name string

	// TableSymbol identifies which dispatch table the handler is installed into, with an
	// empty value meaning the tier-0 table (asmJumpTable). Sub-op tables store the .abi0
	// address, not the ABIInternal wrapper, because the wrapper's 16-byte stack overhead
	// overflows NOSPLIT under tight tail-JMP loops.
	TableSymbol string

	// Offset is the byte offset into the target table where the handler address is written.
	// For tier-0 entries this is int(opcode) * 8; for tier-1+ entries it is int(subOpcode) *
	// 8 within the matching tier table.
	Offset int
}

// BytecodeArchitecturePort extends the core ArchitecturePort with operations specific to
// the pipit bytecode dispatch loop.
type BytecodeArchitecturePort interface {
	asmgen.ArchitecturePort

	// Convention returns the calling convention for this architecture.
	//
	// Returns RegisterConvention which describes the register layout.
	Convention() asmgen.RegisterConvention

	// ScratchRegisters returns the available general-purpose scratch registers.
	//
	// Returns []string which lists the scratch register names.
	ScratchRegisters() []string

	// FloatScratchRegisters returns the available floating-point scratch registers.
	//
	// Returns []string which lists the float scratch register names.
	FloatScratchRegisters() []string

	// DataTemporary returns a scratch register that does not collide with the first
	// afterOperands operand registers.
	//
	// Takes afterOperands (int) which is the number of operand registers to avoid.
	//
	// Returns string which is the selected scratch register name.
	DataTemporary(afterOperands int) string

	// MoveRegister emits an instruction to move a value between registers.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes source (string) which is the source register.
	//
	// Takes destination (string) which is the destination register.
	MoveRegister(emitter *asmgen.Emitter, source, destination string)

	// LoadImmediate emits an instruction to load an immediate value into a register.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes value (string) which is the immediate value to load.
	//
	// Takes destination (string) which is the destination register.
	LoadImmediate(emitter *asmgen.Emitter, value, destination string)

	// Return emits a return instruction to exit the current handler.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	Return(emitter *asmgen.Emitter)

	// EmitTruncateToWidth emits the body of handlerTruncateToWidth.
	//
	// Operand bytes are A = register index, B = bit width (8/16/32), C = registerKind
	// (0=int, 5=uint).
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitTruncateToWidth(emitter *asmgen.Emitter)

	// EmitJumpTableBootstrap emits the body of a leaf function that copies handler addresses
	// from one or more contiguous source jumpTables into a single destination jumpTable.
	//
	// Each source contributes exactly entriesPerTable 8-byte slots, laid out in source
	// order: source[0]'s entries fill destination slots [0..entriesPerTable), source[1]'s
	// fill the next block, and so on. The destination's total length must therefore be
	// len(sourceSymbols) * entriesPerTable slots.
	//
	// Used for installFlatJumpTableASM: copies the 4 per-tier jumpTables
	// (asm/tier1/tier2/tier3) into the unified flatJumpTable[1024]. The post-copy table is
	// consumed by the ASM dispatch loop's flatDispatchSwitch hot path; the copy itself runs
	// once at init time.
	//
	// Uses memory loads/stores rather than LEAQ-of-symbol so the .abi0 handler addresses in
	// the source tables are preserved verbatim; the source tables are populated by the
	// asmgen-generated initJumpTable / initSubOpJumpTables, both of which use LEAQ to record
	// .abi0 entries directly.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes destSymbol (string) which is the destination jump table symbol.
	//
	// Takes sourceSymbols ([]string) which lists the source table symbols in order.
	//
	// Takes entriesPerTable (int) which is the slot count per source table.
	EmitJumpTableBootstrap(
		emitter *asmgen.Emitter,
		destSymbol string,
		sourceSymbols []string,
		entriesPerTable int,
	)

	// BranchOnCondition emits a conditional branch to the given label.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes condition (string) which is the branch condition code.
	//
	// Takes label (string) which is the target label.
	BranchOnCondition(emitter *asmgen.Emitter, condition string, label string)

	// UnconditionalBranch emits an unconditional jump to the given label.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes label (string) which is the target label.
	UnconditionalBranch(emitter *asmgen.Emitter, label string)

	// TestAndBranch emits a test of a register value followed by a conditional branch to the
	// given label.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes register (string) which is the register to test.
	//
	// Takes condition (string) which is the branch condition code.
	//
	// Takes label (string) which is the target label.
	TestAndBranch(emitter *asmgen.Emitter, register, condition, label string)

	// ExtractA emits instructions to extract operand A from the instruction word into the
	// destination register.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes destination (string) which is the destination register.
	ExtractA(emitter *asmgen.Emitter, destination string)

	// ExtractB emits instructions to extract operand B from the instruction word into the
	// destination register.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes destination (string) which is the destination register.
	ExtractB(emitter *asmgen.Emitter, destination string)

	// ExtractC emits instructions to extract operand C from the instruction word into the
	// destination register.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes destination (string) which is the destination register.
	ExtractC(emitter *asmgen.Emitter, destination string)

	// ExtractWideBC emits instructions to extract the combined 16-bit BC operand from the
	// instruction word into the destination register.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes destination (string) which is the destination register.
	ExtractWideBC(emitter *asmgen.Emitter, destination string)

	// ExtractSignedBC emits instructions to extract a signed 16-bit offset from the BC
	// fields of the instruction word into the destination register.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes destination (string) which is the destination register.
	ExtractSignedBC(emitter *asmgen.Emitter, destination string)

	// LoadFromBank emits instructions to load a value from the specified register bank at
	// the given index into the destination register.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes bank (RegisterBank) which selects the register bank.
	//
	// Takes indexRegister (string) which holds the slot index.
	//
	// Takes destinationRegister (string) which receives the loaded value.
	LoadFromBank(emitter *asmgen.Emitter, bank asmgen.RegisterBank, indexRegister, destinationRegister string)

	// StoreToBank emits instructions to store a value from the source register into the
	// specified register bank at the given index.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes bank (RegisterBank) which selects the register bank.
	//
	// Takes sourceRegister (string) which holds the value to store.
	//
	// Takes indexRegister (string) which holds the slot index.
	StoreToBank(emitter *asmgen.Emitter, bank asmgen.RegisterBank, sourceRegister, indexRegister string)

	// LoadConstant emits instructions to load a constant from the specified constant pool at
	// the given index into the destination register.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes bank (RegisterBank) which selects the constant pool.
	//
	// Takes indexRegister (string) which holds the pool index.
	//
	// Takes destinationRegister (string) which receives the loaded constant.
	LoadConstant(emitter *asmgen.Emitter, bank asmgen.RegisterBank, indexRegister, destinationRegister string)

	// LoadFloatConstantToBank emits instructions to load a float constant from the constant
	// pool and store it directly into the float register bank.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes destinationIndex (string) which is the float bank slot index.
	//
	// Takes constantIndex (string) which is the constant pool index.
	LoadFloatConstantToBank(emitter *asmgen.Emitter, destinationIndex, constantIndex string)

	// LoadContextField emits an instruction to load a field from the dispatchContext at the
	// given byte offset into the destination register.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes offset (string) which is the byte offset into the dispatch context.
	//
	// Takes destinationRegister (string) which receives the loaded value.
	LoadContextField(emitter *asmgen.Emitter, offset, destinationRegister string)

	// StoreContextField emits an instruction to store a register value into the
	// dispatchContext at the given byte offset.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes sourceRegister (string) which holds the value to store.
	//
	// Takes offset (string) which is the byte offset into the dispatch context.
	StoreContextField(emitter *asmgen.Emitter, sourceRegister, offset string)

	// StoreContextImmediate emits an instruction to store an immediate value into the
	// dispatchContext at the given byte offset.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes value (string) which is the immediate value to store.
	//
	// Takes offset (string) which is the byte offset into the dispatch context.
	StoreContextImmediate(emitter *asmgen.Emitter, value, offset string)

	// IntegerBinaryOperation emits instructions for a binary integer ALU operation (ADD,
	// SUB, MUL, etc.) on register bank values.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes operation (string) which is the ALU operation name.
	//
	// Takes destinationIndex (string) which is the destination bank slot.
	//
	// Takes leftSourceIndex (string) which is the left operand bank slot.
	//
	// Takes rightSourceIndex (string) which is the right operand bank slot.
	IntegerBinaryOperation(emitter *asmgen.Emitter, operation string, destinationIndex, leftSourceIndex, rightSourceIndex string)

	// IntegerBinaryOperationConstant emits instructions for a binary integer operation where
	// one operand comes from the constant pool.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes operation (string) which is the ALU operation name.
	//
	// Takes destinationIndex (string) which is the destination bank slot.
	//
	// Takes sourceIndex (string) which is the register operand bank slot.
	//
	// Takes constantIndex (string) which is the constant pool index.
	IntegerBinaryOperationConstant(emitter *asmgen.Emitter, operation string, destinationIndex, sourceIndex, constantIndex string)

	// UintBinaryOperation emits a binary uint64 ALU operation on the uint bank.
	//
	// Covers ADD, SUB, MUL, AND, OR, XOR, ANDNOT. The uint bank's base lives in the
	// dispatchContext at CTX_UINTS_BASE (no preserved register caches it, unlike int at R8 /
	// R23), so the handler loads the base into a scratch register once and addresses
	// uints[B], uints[C], uints[A] through that scratch. Bit-pattern-wise the ALU operation
	// is identical to IntegerBinaryOperation; only the bank addressing differs.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes operation (string) which is the ALU operation name.
	//
	// Takes destinationIndex (string) which is the destination uint bank slot.
	//
	// Takes leftSourceIndex (string) which is the left operand bank slot.
	//
	// Takes rightSourceIndex (string) which is the right operand bank slot.
	UintBinaryOperation(emitter *asmgen.Emitter, operation string, destinationIndex, leftSourceIndex, rightSourceIndex string)

	// UintShift emits a variable-amount uint64 shift on the uint register bank.
	//
	// Direction is LEFT or RIGHT. The right shift is LOGICAL (SHRQ on amd64, LSR on arm64),
	// preserving zero in the vacated high bits - distinct from IntegerShift's RIGHT which
	// uses arithmetic shift (SARQ / ASR) to preserve sign.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes direction (string) which is LEFT or RIGHT.
	//
	// Takes destinationIndex (string) which is the destination uint bank slot.
	//
	// Takes valueIndex (string) which is the value to shift.
	//
	// Takes amountIndex (string) which is the shift amount.
	UintShift(emitter *asmgen.Emitter, direction string, destinationIndex, valueIndex, amountIndex string)

	// UintCompareAndSet emits an unsigned 64-bit comparison of two uint registers.
	//
	// The boolean result (1 when the relation holds, 0 otherwise) is written into the int
	// register bank at destinationIndex. The condition string is "EQ", "NE", "LT", "LE",
	// "GT", "GE" - the adapter maps these to unsigned condition codes (SETB / SETBE / SETA /
	// SETAE on amd64; LO / LS / HI / HS on arm64) for the inequality cases, and to the same
	// codes as the int variant for EQ / NE.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes condition (string) which is the comparison relation (EQ, NE, LT, LE, GT, GE).
	//
	// Takes destinationIndex (string) which is the int bank slot for the boolean result.
	//
	// Takes leftIndex (string) which is the left uint operand bank slot.
	//
	// Takes rightIndex (string) which is the right uint operand bank slot.
	UintCompareAndSet(emitter *asmgen.Emitter, condition string, destinationIndex, leftIndex, rightIndex string)

	// IntegerUnaryOperation emits instructions for a unary integer operation (NEG, NOT) on a
	// register bank value.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes operation (string) which is the unary operation name (NEG, NOT).
	//
	// Takes destinationIndex (string) which is the destination bank slot.
	//
	// Takes sourceIndex (string) which is the source operand bank slot.
	IntegerUnaryOperation(emitter *asmgen.Emitter, operation string, destinationIndex, sourceIndex string)

	// IntegerInPlace emits instructions for an in-place integer operation (INC, DEC) that
	// reads from and writes to the same register.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes operation (string) which is the in-place operation name (INC, DEC).
	//
	// Takes indexRegister (string) which is the bank slot to modify in place.
	IntegerInPlace(emitter *asmgen.Emitter, operation string, indexRegister string)

	// IntegerNarrow32 sign-extends the low 32 bits of an int bank slot back into the slot,
	// which is what a 32-bit OpTruncateToWidth of the int bank does.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes indexRegister (string) which is the bank slot to narrow in place.
	IntegerNarrow32(emitter *asmgen.Emitter, indexRegister string)

	// UintNarrow32 zero-extends the low 32 bits of a uint bank slot back into the slot,
	// which is what a 32-bit OpTruncateToWidth of the uint bank does.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes indexRegister (string) which is the bank slot to narrow in place.
	// Takes baseScratch (string) which receives the loaded uint bank base.
	UintNarrow32(emitter *asmgen.Emitter, indexRegister, baseScratch string)

	// UintInPlace emits an in-place uint64 operation on the uint register bank.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes operation (string) which is the in-place operation name (INC, DEC).
	// Takes indexRegister (string) which is the uint bank slot to modify.
	// Takes baseScratch (string) which is a scratch register for loading the uint bank base.
	UintInPlace(emitter *asmgen.Emitter, operation string, indexRegister string, baseScratch string)

	// IntegerDivide emits instructions for signed integer division with a division-by-zero
	// guard that branches to the given label.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes dividendIndex (string) which is the dividend bank slot.
	// Takes divisorIndex (string) which is the divisor bank slot.
	// Takes quotientDestinationIndex (string) which is the destination slot for the
	// quotient.
	// Takes remainderDestinationIndex (string) which is the destination slot for the
	// remainder.
	// Takes zeroLabel (string) which is the branch target for division by zero.
	IntegerDivide(emitter *asmgen.Emitter, dividendIndex, divisorIndex, quotientDestinationIndex, remainderDestinationIndex, zeroLabel string)

	// IntegerShift emits instructions for a variable-amount integer shift in the specified
	// direction (LEFT or RIGHT).
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes direction (string) which is LEFT or RIGHT.
	// Takes destinationIndex (string) which is the destination bank slot.
	// Takes valueIndex (string) which is the value to shift.
	// Takes amountIndex (string) which is the shift amount.
	IntegerShift(emitter *asmgen.Emitter, direction string, destinationIndex, valueIndex, amountIndex string)

	// IntegerCompareAndSet emits instructions to compare two integer register values and
	// write a boolean result (1 or 0) into the destination register.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes condition (string) which is the comparison relation.
	// Takes destinationIndex (string) which is the destination slot for the boolean result.
	// Takes leftIndex (string) which is the left operand bank slot.
	// Takes rightIndex (string) which is the right operand bank slot.
	IntegerCompareAndSet(emitter *asmgen.Emitter, condition string, destinationIndex, leftIndex, rightIndex string)

	// IntegerCompareAndBranch emits instructions to compare two integer register values and
	// branch to the given label if the condition holds.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes condition (string) which is the comparison relation.
	// Takes leftIndex (string) which is the left operand bank slot.
	// Takes rightIndex (string) which is the right operand bank slot.
	// Takes label (string) which is the branch target label.
	IntegerCompareAndBranch(emitter *asmgen.Emitter, condition string, leftIndex, rightIndex, label string)

	// UintCompareAndBranch emits an equality comparison and conditional branch for uint
	// registers.
	//
	// Only "EQ" and "NE" are accepted.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes condition (string) which is the condition, EQ or NE.
	// Takes leftIndex (string) which is the register holding the left operand's index.
	// Takes rightIndex (string) which is the register holding the right operand's index.
	// Takes label (string) which is the branch target.
	UintCompareAndBranch(emitter *asmgen.Emitter, condition string, leftIndex, rightIndex, label string)

	// IntegerCompareConstantAndBranch emits instructions to compare an integer register
	// value against a constant pool entry and branch if the condition holds.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes condition (string) which is the comparison relation.
	// Takes registerIndex (string) which is the register operand bank slot.
	// Takes constantIndex (string) which is the constant pool index.
	// Takes label (string) which is the branch target label.
	IntegerCompareConstantAndBranch(emitter *asmgen.Emitter, condition string, registerIndex, constantIndex, label string)

	// FloatBinaryOperation emits instructions for a binary floating-point operation (ADD,
	// SUB, MUL, DIV) on register bank values.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes operation (string) which is the floating-point operation name.
	// Takes destinationIndex (string) which is the destination float bank slot.
	// Takes leftSourceIndex (string) which is the left operand bank slot.
	// Takes rightSourceIndex (string) which is the right operand bank slot.
	FloatBinaryOperation(emitter *asmgen.Emitter, operation string, destinationIndex, leftSourceIndex, rightSourceIndex string)

	// FloatUnaryOperation emits instructions for a unary floating-point operation (SQRT,
	// ABS, FLOOR, CEIL, TRUNC, NEG, ROUND) on a register value.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes operation (string) which is the unary floating-point operation name.
	// Takes destinationIndex (string) which is the destination float bank slot.
	// Takes sourceIndex (string) which is the source operand bank slot.
	FloatUnaryOperation(emitter *asmgen.Emitter, operation string, destinationIndex, sourceIndex string)

	// FloatCompareAndSet emits instructions to compare two float register values and write a
	// boolean result into an integer destination register.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes condition (string) which is the comparison relation.
	// Takes integerDestinationIndex (string) which is the int bank slot for the boolean
	// result.
	// Takes floatLeftIndex (string) which is the left float operand bank slot.
	// Takes floatRightIndex (string) which is the right float operand bank slot.
	FloatCompareAndSet(emitter *asmgen.Emitter, condition string, integerDestinationIndex, floatLeftIndex, floatRightIndex string)

	// FloatConversion emits a conversion between integer and float register banks.
	//
	// Supported directions:
	//
	//   - "INTEGER_TO_FLOAT": int64 -> float64
	//   - "FLOAT_TO_INTEGER": float64 -> int64
	//   - "UNSIGNED_TO_FLOAT": uint64 -> float64
	//   - "FLOAT_TO_UNSIGNED": float64 -> uint64
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes direction (string) which is the conversion direction.
	// Takes destinationIndex (string) which is the destination bank slot.
	// Takes sourceIndex (string) which is the source operand bank slot.
	FloatConversion(emitter *asmgen.Emitter, direction string, destinationIndex, sourceIndex string)

	// StringLengthRead emits a read of the Len field of a Go string header.
	//
	// Reads strings[sourceIndex] and stores the int64 result into ints[destinationIndex].
	// The Go string header is 16 bytes (8-byte Data pointer + 8-byte Len), so the source
	// index is scaled by 16 to compute the header address; the Len field lives at offset +8.
	// The caller must ensure sourceIndex and destinationIndex are distinct scratch
	// registers; the implementation destructively shifts sourceIndex by 4.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes destinationIndex (string) which is the int bank slot for the length result.
	// Takes sourceIndex (string) which is the string bank slot to read.
	StringLengthRead(emitter *asmgen.Emitter, destinationIndex, sourceIndex string)

	// StringCopy emits a copy of a 16-byte Go string header between bank slots.
	//
	// Copies from strings[sourceIndex] to strings[destinationIndex]. Both halves of the
	// header (Data pointer at offset +0 and Length at offset +8) are transferred. The caller
	// must ensure sourceIndex and destinationIndex are distinct scratch registers; the
	// implementation destructively shifts both by 4 to compute their byte offsets.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes destinationIndex (string) which is the destination string bank slot.
	// Takes sourceIndex (string) which is the source string bank slot.
	StringCopy(emitter *asmgen.Emitter, destinationIndex, sourceIndex string)

	// StringConstLoad emits a copy of a string constant into the string bank.
	//
	// Copies the 16-byte Go string header from stringConstants[constantIndex] into
	// strings[destinationIndex]. The constant table base is loaded from CTX_STR_CONSTS_BASE
	// and the strings register bank base from CTX_STRINGS_BASE. Caller must supply distinct
	// scratch registers; the implementation destructively shifts both indices by 4 to
	// compute byte offsets.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes destinationIndex (string) which is the destination string bank slot.
	// Takes constantIndex (string) which is the string constant pool index.
	StringConstLoad(emitter *asmgen.Emitter, destinationIndex, constantIndex string)

	// BoolConstLoad emits a copy of a bool constant into the bool bank.
	//
	// Copies a single byte from boolConstants[constantIndex] into bools[destinationIndex].
	// The constant table base is loaded from CTX_BOOL_CONSTS_BASE and the bools register
	// bank base from CTX_BOOLS_BASE. Both indices are 1-byte-strided.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes destinationIndex (string) which is the destination bool bank slot.
	// Takes constantIndex (string) which is the bool constant pool index.
	BoolConstLoad(emitter *asmgen.Emitter, destinationIndex, constantIndex string)

	// LoadFromUintBank emits instructions to load a uint64 from the uint register bank into
	// the destination register.
	//
	// Requires a scratch register because the uint bank base is not held in a preserved
	// register (unlike int's R8 / float's R9), so the handler must load the base from the
	// dispatch context first.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes indexRegister (string) which holds the uint bank slot index.
	// Takes destinationRegister (string) which receives the loaded uint64 value.
	// Takes baseScratch (string) which is a scratch register for the bank base pointer.
	LoadFromUintBank(emitter *asmgen.Emitter, indexRegister, destinationRegister, baseScratch string)

	// StoreToUintBank emits instructions to store a uint64 from the source register into the
	// uint register bank.
	//
	// Requires a scratch for the bank base load.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes sourceRegister (string) which holds the uint64 value to store.
	// Takes indexRegister (string) which holds the uint bank slot index.
	// Takes baseScratch (string) which is a scratch register for the bank base pointer.
	StoreToUintBank(emitter *asmgen.Emitter, sourceRegister, indexRegister, baseScratch string)

	// LoadFromBoolBank emits instructions to load a single bool byte from the bool register
	// bank into the low byte of the destination register, zero-extending to int64.
	//
	// Bools are stored as one byte each, so the access uses a different stride than the
	// 8-byte typed banks.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes indexRegister (string) which holds the bool bank slot index.
	// Takes destinationRegister (string) which receives the loaded bool value,
	// zero-extended.
	// Takes baseScratch (string) which is a scratch register for the bank base pointer.
	LoadFromBoolBank(emitter *asmgen.Emitter, indexRegister, destinationRegister, baseScratch string)

	// StoreToBoolBank emits instructions to store the low byte of the source register into
	// the bool register bank.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes sourceRegister (string) which holds the bool value to store.
	// Takes indexRegister (string) which holds the bool bank slot index.
	// Takes baseScratch (string) which is a scratch register for the bank base pointer.
	StoreToBoolBank(emitter *asmgen.Emitter, sourceRegister, indexRegister, baseScratch string)

	// BitwiseNotInPlace emits a bitwise complement of the given register, writing the result
	// back into the same register.
	//
	// Used by handlers that need to NOT a value already in a scratch register without going
	// through bank load/store.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes register (string) which is the register to complement in place.
	BitwiseNotInPlace(emitter *asmgen.Emitter, register string)

	// LogicalSetNonZero emits instructions that write 1 to the low byte of
	// destinationRegister if sourceRegister is non-zero, 0 otherwise.
	//
	// Used by IntToBool to convert a non-zero int into a true bool value (mirroring Go's
	// `bool(int)` semantics).
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes destinationRegister (string) which receives 1 if source is non-zero, 0
	// otherwise.
	// Takes sourceRegister (string) which holds the value to test.
	LogicalSetNonZero(emitter *asmgen.Emitter, destinationRegister, sourceRegister string)

	// LogicalNot emits instructions to compute the logical negation of an integer value,
	// writing 1 if the source is zero and 0 otherwise.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes destinationIndex (string) which is the destination bank slot for the result.
	// Takes sourceIndex (string) which is the source operand bank slot.
	LogicalNot(emitter *asmgen.Emitter, destinationIndex, sourceIndex string)

	// EmitSubOpStrconvFormatBool emits the body of the subOpStrconvFormatBool handler.
	//
	// Reads bools[C], conditionally selects between the addresses of two package-level
	// static string vars (boolStringTrue / boolStringFalse in engine), and writes the
	// 16-byte string header into strings[B]. No CALL into Go - strconv.FormatBool's return
	// values are statically interned, so the handler stamps them directly.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitSubOpStrconvFormatBool(emitter *asmgen.Emitter)

	// EmitInlineGoCallTwoOperandShim emits a tier-1 inline-call shim of shape "B = goFn(C)".
	//
	// The container is $0 NOSPLIT|NOFRAME, and it opens an abi0 scratch frame with ADJSP $32
	// / ADJSP $-32 (the canonical Go-runtime pattern used in _rt0_amd64_lib). ADJSP updates
	// spdelta in the pcsp table without setting FuncFlagSPWrite, so the GC walker still
	// traverses the frame correctly.
	//
	// The body extracts B (dest) and C (source) from the instruction word, spills R14 (pipit
	// PC) to CTX_SAVED_PC across the CALL, opens the scratch frame, marshals abi0 args (ctx
	// at 0(SP), dest at 8(SP), source at 16(SP); 24(SP) reserved for the returned ctx),
	// CALLs goSymbol, reloads R15 from 24(SP), closes the scratch frame, restores R14 from
	// CTX_SAVED_PC, reloads the bank-base regs from ctx, and tail-jumps via DISPATCH_NEXT.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes goSymbol (string) which is the Go function symbol to call.
	EmitInlineGoCallTwoOperandShim(emitter *asmgen.Emitter, goSymbol string)

	// EmitInlineGoCallThreeOperandShim emits a shim for "B = goFn(C, ext.A)".
	//
	// Three operands, with an extension word read. Mirrors TwoOperandShim with ADJSP $40 /
	// $-40 to fit the 4-argument abi0 frame, and reads the extension word at codeBase +
	// R14*4 before the CALL (advancing R14 past it; the advanced PC is what gets spilled to
	// CTX_SAVED_PC).
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes goSymbol (string) which is the Go function symbol to call.
	EmitInlineGoCallThreeOperandShim(emitter *asmgen.Emitter, goSymbol string)

	// EmitTypedSliceFloatSet emits the entire body of a tier-1 umbrella sub-op of the form
	// slicesFloat[B][ints[C]] = floats[ext.A] with bounds checking. Mirrors
	// EmitTypedSliceFloatGet with operand B being the destination slice (not source) and the
	// extension word's A field being the source value register (not the index register).
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceFloatSet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceUintGet emits the entire body of a tier-1 umbrella sub-op of the form
	// uints[B] = slicesUint[C][ints[ext.A]] with bounds checking.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceUintGet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceUintSet emits the entire body of a tier-1 umbrella sub-op of the form
	// slicesUint[B][ints[C]] = uints[ext.A] with bounds checking.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceUintSet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceBoolGet emits the entire body of a tier-1 umbrella sub-op of the form
	// bools[B] = slicesBool[C][ints[ext.A]] with bounds checking.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceBoolGet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceBoolSet emits the entire body of a tier-1 umbrella sub-op of the form
	// slicesBool[B][ints[C]] = bools[ext.A] with bounds checking.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceBoolSet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceByteGet emits the body of uints[B] = slicesByte[C][ints[ext.A]].
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceByteGet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceByteSet emits the body of slicesByte[B][ints[C]] = byte(uints[ext.A]).
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceByteSet(emitter *asmgen.Emitter, contextOffset string)

	// EmitSliceSliceByte emits bounds-checked two-index byte slicing, falling back to Go for
	// three-index slices or failed checks.
	//
	// Missing bounds default to zero and the source length. Data must stay within the
	// backing array when the new capacity is zero.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitSliceSliceByte(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceMove emits the body of isa.SubOpMoveSlice<Kind>.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceMove(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceSliceSlice emits bounds-checked two-index slicing with a configurable
	// element stride, following EmitSliceSliceByte's bounds and fallback rules.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	// Takes elementSizeShift (uint8) which is the log2 element stride in bytes.
	EmitTypedSliceSliceSlice(emitter *asmgen.Emitter, contextOffset string, elementSizeShift uint8)

	// EmitTypedRangeNextByte emits the body of isa.OpRangeNextSliceByte.
	//
	// The typed range-step over slicesByte. Increments ints[A], compares against
	// len(slicesByte[B]); on end-of-range applies the 24-bit jump offset packed into the
	// next instruction word, else loads the byte at slicesByte[B][ints[A]] into uints[C] and
	// advances PC past the extension word. Pure ASM; no bounds_fail path because the
	// increment + compare cannot read an out-of-range index (the loop body is only entered
	// when index < len).
	//
	// Used by handlerRangeNextSliceByte().
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedRangeNextByte(emitter *asmgen.Emitter, contextOffset string)

	// EmitEqualUint64ImmediateThenJumpIfFalse emits the fused compare-and-branch for
	// isa.SubOpEqualUint64ImmediateThenJumpIfFalse, comparing uints[B] with the immediate
	// byte in operand C and jumping on mismatch.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitEqualUint64ImmediateThenJumpIfFalse(emitter *asmgen.Emitter)

	// EmitInRangeUint64ThenJumpIfFalse emits the body of
	// isa.SubOpTier2InRangeUint64ThenJumpIfFalse.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitInRangeUint64ThenJumpIfFalse(emitter *asmgen.Emitter)

	// EmitTypedSliceStringGet emits the body of strings[B] = slicesString[C][ints[ext.A]].
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceStringGet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceFloatGet emits the entire body of a tier-1 umbrella sub-op of the form
	// floats[B] = slicesFloat[C][ints[ext.A]] with bounds checking.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceFloatGet(emitter *asmgen.Emitter, contextOffset string)

	// EmitGetSliceInt64Element emits the entire body of the tier-0 opcode ints[A] =
	// slicesInt[B][ints[C]] with bounds checking.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitGetSliceInt64Element(emitter *asmgen.Emitter, contextOffset string)

	// EmitSetSliceInt64Element emits the entire body of the tier-0 opcode
	// slicesInt[A][ints[B]] = ints[C] with bounds checking. See EmitGetSliceInt64Element for
	// the tier-0 conventions.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitSetSliceInt64Element(emitter *asmgen.Emitter, contextOffset string)

	// EmitGetSliceInt64ElementUnchecked emits the tier-0 opcode ints[A] =
	// slicesInt[B][ints[C]] without the bounds check. The compiler's BCE pass only emits the
	// unchecked form when a dominating comparison proves the index in range.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitGetSliceInt64ElementUnchecked(emitter *asmgen.Emitter, contextOffset string)

	// EmitSetSliceInt64ElementUnchecked emits the tier-0 opcode slicesInt[A][ints[B]] =
	// ints[C] without the bounds check. See EmitGetSliceInt64ElementUnchecked.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitSetSliceInt64ElementUnchecked(emitter *asmgen.Emitter, contextOffset string)

	// EmitSliceIndexStructFieldScalar emits the tier-0 body for the fused slice[i].field
	// scalar readers (float and int families), with the element-type identity check and shim
	// fallback for every other shape.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes kindA (string) which is the first operand kind selector.
	// Takes kindB (string) which is the second operand kind selector.
	// Takes destIsFloat (bool) which indicates whether the destination bank is float.
	// Takes shimSymbol (string) which is the Go shim symbol for unsupported shapes.
	EmitSliceIndexStructFieldScalar(emitter *asmgen.Emitter, kindA, kindB string, destIsFloat bool, shimSymbol string)

	// EmitSliceIndexStructFieldScalarStore emits the tier-0 body for fused scalar stores to
	// slice[i].field.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes kindA (string) which is the first accepted layout kind.
	// Takes kindB (string) which is the second accepted layout kind, or empty.
	// Takes sourceIsFloat (bool) which indicates whether the source bank is float.
	// Takes shimSymbol (string) which is the Go shim symbol for unsupported shapes.
	EmitSliceIndexStructFieldScalarStore(emitter *asmgen.Emitter, kindA, kindB string, sourceIsFloat bool, shimSymbol string)

	// EmitElementInt64ViaPointerAccess emits the tier-0 body for opGetElementInt64ViaPointer
	// (isSet=false) or opSetElementInt64ViaPointer with a live bounds check, falling back to
	// the Go shim for unsupported shapes and range panics.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes isSet (bool) which selects set mode (true) or get mode (false).
	// Takes shimSymbol (string) which is the Go shim symbol for fallback.
	EmitElementInt64ViaPointerAccess(emitter *asmgen.Emitter, isSet bool, shimSymbol string)

	// EmitGetStructFieldGeneralPointer emits the tier-0 body for opGetStructFieldGeneral
	// restricted to plain pointer-kind fields, constructing the typed pointer destination
	// value under the write-barrier guard.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitGetStructFieldGeneralPointer(emitter *asmgen.Emitter)

	// EmitSetStructFieldGeneral emits the tier-0 body for opSetStructFieldGeneral covering
	// plain pointer-kind fields and cycle-broken `any` fields, storing the value's pointer
	// words under the write-barrier guard.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitSetStructFieldGeneral(emitter *asmgen.Emitter)

	// EmitGetStructFieldRawPointer emits the tier-0 body for opGetStructFieldRawPointer,
	// re-wrapping a cycle-broken eface field as a direct pointer-kind value under the
	// write-barrier guard.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitGetStructFieldRawPointer(emitter *asmgen.Emitter)

	// EmitLengthGeneral emits the tier-1 body for SubOpLengthGeneral.
	//
	// A valid slice value has its length read from the slice header it points at and stored
	// into ints[B]. An invalid value, and every other kind, falls back to the Go shim.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitLengthGeneral(emitter *asmgen.Emitter)

	// EmitEqualGeneral emits the tier-0 body for opEqualGeneral, testing fast
	// general-equality arms (both invalid, differing type words, pointer-identity kinds) and
	// storing 0/1 into ints[A], with interface operands and other kinds falling back to the
	// Go shim.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitEqualGeneral(emitter *asmgen.Emitter)

	// EmitWriteBarrierGuard runs body only while the garbage collector's write barrier is
	// off, otherwise exiting through pathBFallback. The guard preserves registers required
	// at handler entry.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes body (func()) which emits the guarded handler body.
	EmitWriteBarrierGuard(emitter *asmgen.Emitter, body func())

	// EmitMoveGeneralAlias emits the tier-0 body for opMoveGeneral's alias mode, performing
	// a raw 24-byte general-slot copy gated on the runtime write-barrier flag, with snapshot
	// and boundary modes (and the marking phase) falling back to the Go shim.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitMoveGeneralAlias(emitter *asmgen.Emitter)

	// EmitTestNilJump emits the tier-0 body for opJumpIfNil (jumpWhenNil=true) or
	// opJumpIfNotNil, nil-testing general[A] across the nilable kind range and branching by
	// the signed 16-bit offset in B|C<<8 with the poll-budget back-edge exit.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes jumpWhenNil (bool) which selects jump-on-nil (true) or jump-on-non-nil (false).
	EmitTestNilJump(emitter *asmgen.Emitter, jumpWhenNil bool)

	// EmitStructFieldScalarGet emits the tier-0 fast path for a scalar struct-field GET
	// opcode dest[A] = *(receiverBase + layout[C].Offset), with layout bounds/flags/kind
	// gates and pointer/addressable-struct receiver resolution falling back to the named
	// tier-2 shim for unsupported shapes.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes kindA (string) which is the first operand kind selector.
	// Takes kindB (string) which is the second operand kind selector.
	// Takes bank (string) which names the destination register bank.
	// Takes shimSymbol (string) which is the tier-2 shim symbol for unsupported shapes.
	EmitStructFieldScalarGet(emitter *asmgen.Emitter, kindA, kindB, bank, shimSymbol string)

	// EmitStructFieldScalarSet emits the tier-0 fast path for a scalar struct-field SET
	// opcode *(receiverBase + layout[C].Offset) = source[B], mirroring
	// EmitStructFieldScalarGet() with bank naming the source bank.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes kindA (string) which is the first operand kind selector.
	// Takes kindB (string) which is the second operand kind selector.
	// Takes bank (string) which names the source register bank.
	// Takes shimSymbol (string) which is the tier-2 shim symbol for unsupported shapes.
	EmitStructFieldScalarSet(emitter *asmgen.Emitter, kindA, kindB, bank, shimSymbol string)

	// EmitStructFieldSliceLen emits the tier-0 fast path for opLengthStructField, computing
	// ints[A] = len(receiver[B].field(layout[C])) for slice-kind fields with zero layout
	// flags, reading the header's length word in place with no allocation. Unsupported
	// shapes jump to the named tier-2 shim.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes shimSymbol (string) which is the tier-2 shim symbol for unsupported shapes.
	EmitStructFieldSliceLen(emitter *asmgen.Emitter, shimSymbol string)

	// EmitStructFieldSliceIndexScalar emits the tier-0 fast path for
	// opGetStructFieldElementField, loading a full-width scalar sub-field from a slice
	// element within a struct field directly into the destination bank.
	//
	// bank(ext2.C)[A] = receiver[B].field(layout[C])[ints[ext1.C]].subfield(ext2.wide). The
	// program counter only advances past the two extension words on the success path, so
	// shim fallbacks (including out-of-range indexes, which the Go handler raises as
	// interpreted panics) re-run the op from the original counter.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes shimSymbol (string) which is the tier-2 shim symbol for unsupported shapes.
	EmitStructFieldSliceIndexScalar(emitter *asmgen.Emitter, shimSymbol string)

	// EmitLoadZero emits the tier-1 body that zeroes register B of the scalar bank named by
	// C.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes shimSymbol (string) which is the tier-2 shim symbol for everything else.
	EmitLoadZero(emitter *asmgen.Emitter, shimSymbol string)

	// EmitGetGlobal emits the tier-0 fast path for OpGetGlobal: bank(C)[A] = the package
	// variable B + the function's global base for kind C, for the int, float, bool, uint and
	// general banks.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes shimSymbol (string) which is the tier-2 shim symbol for everything else.
	EmitGetGlobal(emitter *asmgen.Emitter, shimSymbol string)

	// EmitGetGlobalWide emits the tier-1 fast path for SubOpGetGlobalWide.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes shimSymbol (string) which is the tier-2 shim symbol for everything else.
	EmitGetGlobalWide(emitter *asmgen.Emitter, shimSymbol string)

	// EmitSetGlobal emits a tier-0 scalar store to an unshared package variable.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes shimSymbol (string) which is the tier-2 shim symbol for everything else.
	EmitSetGlobal(emitter *asmgen.Emitter, shimSymbol string)

	// EmitSetGlobalWide emits the tier-1 fast path for SubOpSetGlobalWide.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes shimSymbol (string) which is the tier-2 shim symbol for everything else.
	EmitSetGlobalWide(emitter *asmgen.Emitter, shimSymbol string)

	// EmitSliceGetTypedElement emits the tier-0 fast path for OpGetElementInt64 (signed) or
	// OpGetElementUint64, which computes bank[A] = element ints[C] of the collection in
	// general[B], read through the front slot of the typed element shape cache
	// (typedElementShapes) exactly as typedElementAt's front hit reads it.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes signed (bool) which selects the OpGetElementInt64 form.
	// Takes shimSymbol (string) which is the tier-2 shim symbol for everything else.
	EmitSliceGetTypedElement(emitter *asmgen.Emitter, signed bool, shimSymbol string)

	// EmitSliceSetTypedElement emits an integer store through the typed-element shape cache.
	//
	// The value in bank[C] is truncated to the element size and stored at
	// general[A][ints[B]]. An index out of range, read-only values, unaddressable arrays and
	// unsupported shapes fall back to the shim. The element holds no pointer, so the store
	// needs no barrier.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes signed (bool) which selects the OpSetElementInt64 form, which stores into any
	// integer element. The other stores into unsigned elements only.
	// Takes shimSymbol (string) which is the tier-2 shim symbol for everything else.
	EmitSliceSetTypedElement(emitter *asmgen.Emitter, signed bool, shimSymbol string)

	// EmitGetCapturedVariableScalar emits the tier-0 fast path for opGetCapturedVariable on
	// direct scalar cells, loading bank(C)[A] from frame.upvalues[B].cellField(C) where C
	// names the destination registerKind and selects both the upvalueCell scalar field to
	// read and the bank to write.
	//
	// Indirect cells, out-of-range indexes, nil cell pointers and non-scalar kinds jump to
	// the named tier-2 shim. Scalar bank stores carry no pointers, so no write barrier is
	// required.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes shimSymbol (string) which is the tier-2 shim symbol for unsupported shapes.
	EmitGetCapturedVariableScalar(emitter *asmgen.Emitter, shimSymbol string)

	// EmitLoadUint64Constant emits the tier-0 body for uints[A] = uintConstants[B|(C<<8)].
	// The pool base comes from CTX_UINT_CONSTS_BASE and the destination bank from
	// CTX_UINTS_BASE.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitLoadUint64Constant(emitter *asmgen.Emitter)

	// EmitLoadUint64Immediate emits the tier-1 body for uints[B] = uint64(C), the
	// immediate-operand small-constant loader.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitLoadUint64Immediate(emitter *asmgen.Emitter)

	// EmitUintConstArith emits the tier-1 body for uints[B] = uints[C] op
	// uintConstants[ext.A|(ext.B<<8)], consuming the extension word. The operation selector
	// must be one of "add", "sub", "and", or "add32" and "sub32", which add and subtract and
	// then keep the low 32 bits of the result.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes operation (string) which is the arithmetic operation.
	EmitUintConstArith(emitter *asmgen.Emitter, operation string)

	// EmitUintDivide emits the tier-0 body for uints[A] = uints[B] / uints[C] with a
	// zero-divisor guard that exits through DIV_BY_ZERO_EXIT so the Go side raises the
	// interpreted panic.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitUintDivide(emitter *asmgen.Emitter)

	// EmitUintRemainder emits the tier-0 body for uints[A] = uints[B] % uints[C] with the
	// same zero-divisor guard as EmitUintDivide.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitUintRemainder(emitter *asmgen.Emitter)

	// EmitComplexCopy emits the entire body of a tier-1 umbrella sub-op of the form
	// complex[B] = complex[C]. Each complex128 slot is 16 bytes (real + imag).
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the complex
	// bank.
	EmitComplexCopy(emitter *asmgen.Emitter, contextOffset string)

	// EmitComplexNegate emits the body of complex[B] = -complex[C].
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the complex
	// bank.
	EmitComplexNegate(emitter *asmgen.Emitter, contextOffset string)

	// LoadComplexHalfToFloatBank emits instructions to read one float64 half of a complex128
	// element from the complex register bank into a destination float64 register.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the complex
	// bank.
	// Takes indexRegister (string) which holds the complex bank slot index.
	// Takes halfOffset (string) which is "0" for real or "8" for imaginary.
	// Takes destinationFloatIndexRegister (string) which is the float bank slot to write.
	LoadComplexHalfToFloatBank(emitter *asmgen.Emitter, contextOffset, indexRegister, halfOffset, destinationFloatIndexRegister string)

	// LoadTypedSliceHeaderLength emits instructions to read the length field of a slice
	// header stored in a typed-slice register bank.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	// Takes indexRegister (string) which holds the slice bank slot index.
	// Takes destinationRegister (string) which receives the slice length.
	LoadTypedSliceHeaderLength(emitter *asmgen.Emitter, contextOffset, indexRegister, destinationRegister string)

	// DispatchNext emits the instruction sequence that fetches the next bytecode instruction
	// and jumps to its handler via the dispatch table.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	DispatchNext(emitter *asmgen.Emitter)

	// DivisionByZeroExit emits the exit sequence for a division-by-zero fault, storing the
	// exit reason and program counter before returning to Go.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	DivisionByZeroExit(emitter *asmgen.Emitter)

	// ExitWithReason emits the exit sequence for the given exit reason constant, storing the
	// reason and program counter before returning to Go.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes reason (string) which is the exit reason constant.
	ExitWithReason(emitter *asmgen.Emitter, reason string)

	// IncrementProgramCounter emits an instruction to advance the program counter by one
	// instruction word.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	IncrementProgramCounter(emitter *asmgen.Emitter)

	// DecrementProgramCounter emits an instruction to move the program counter back by one
	// instruction word.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	DecrementProgramCounter(emitter *asmgen.Emitter)

	// AddToProgramCounter emits instructions to add a signed offset held in the given
	// register to the program counter.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes register (string) which is the register holding the signed offset.
	AddToProgramCounter(emitter *asmgen.Emitter, register string)

	// EmitTypedSliceByteGetInt emits the body of ints[B] = slicesByte[C][ints[ext.A]].
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceByteGetInt(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceByteSetRun emits the body of SubOpSetSliceByteElementRun, two to four
	// byte stores at consecutive positions, with the whole run bounds-checked first and a
	// branch to the Go handler when it does not fit.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes sliceContextOffset (string) which is the dispatch context byte offset of the
	// slice bank.
	EmitTypedSliceByteSetRun(emitter *asmgen.Emitter, sliceContextOffset string)

	// EmitMultiplyConstantThenAdd emits ints[A] = ints[extension.A] + ints[B] *
	// intConstants[C], consuming the extension word. Both source registers are loaded before
	// the destination is stored, so the destination may be either of them.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitMultiplyConstantThenAdd(emitter *asmgen.Emitter)

	// EmitBackEdgePoll emits a back-edge poll of ctx.pollBudget, storing the current PC and
	// exitPoll into the dispatch context and returning to Go when the budget reaches zero so
	// cancellation and the deadline can be polled.
	//
	// Emitted on tier-1 loop back edges so a tight all-ASM loop stays interruptible.
	// Clobbers only flags (amd64) or a scratch register (arm64).
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitBackEdgePoll(emitter *asmgen.Emitter)

	// LoadNextInstructionWord emits instructions to read the next instruction word from the
	// bytecode body into the destination register, advancing the program counter past it.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes destinationRegister (string) which is the register to load the word into.
	LoadNextInstructionWord(emitter *asmgen.Emitter, destinationRegister string)

	// DispatchMacros returns the architecture-specific assembly macro definitions used by
	// the generated dispatch loop.
	//
	// Returns string which is the macro text block.
	DispatchMacros() string

	// InitialiseJumpTableEntry emits instructions to patch a single dispatch table entry
	// with the address of the given handler symbol at the specified byte offset.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes handlerSymbol (string) which is the handler's Plan-9 symbol.
	//
	// Takes tableRegister (string) which is the register holding the table base.
	//
	// Takes offset (int) which is the byte offset into the table.
	InitialiseJumpTableEntry(emitter *asmgen.Emitter, handlerSymbol, tableRegister string, offset int)

	// StringOperations returns the port that provides string-specific assembly emission
	// methods.
	//
	// Returns StringOperationsPort which is the arch-specific string operations port.
	StringOperations() asmgen.StringOperationsPort

	// InitialisationOperations returns the port that provides jump table setup and dispatch
	// loop initialisation methods.
	//
	// Returns InitialisationOperationsPort which is the arch-specific initialisation port.
	InitialisationOperations() asmgen.InitialisationOperationsPort

	// InlineCallOperations returns the port that provides inline call and return assembly
	// emission methods.
	//
	// Returns InlineCallOperationsPort which is the arch-specific inline call port.
	InlineCallOperations() asmgen.InlineCallOperationsPort

	// EmitCallMethodInline emits the body of handlerCallMethod, the inline call handler
	// variant for SubOpCallMethod that selects the callee entry by the receiver's type word
	// from the site's per-type table before pushing the frame in assembly, and exits to the
	// Go tier-2 path on a miss.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitCallMethodInline(emitter *asmgen.Emitter)

	// EmitAllocateStructLiteral emits the body of handlerAllocateStructLiteral, which
	// computes general[C] = zeroed pointer-free struct of the type indexed by the extension
	// word, carved from the arena generic-bytes slab, with EXIT_TIER2 fallbacks to the Go
	// handler.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitAllocateStructLiteral(emitter *asmgen.Emitter)

	// EmitPathBCallShim emits the SHIM half of the tier-2 ASM-call shim+real split.
	//
	// NOFRAME, installed in the dispatch jump table. CALLs the supplied REAL symbol (which
	// marshals abi0 args + reloads dispatch registers), reads CTX_HANDLER_RESULT, and either
	// tail-JMPs DISPATCH_NEXT on opContinue or RETs to Go for the cold-path return-code
	// mapping. The REAL itself is emitted separately by EmitTier2CallShimReal.
	//
	// The shim+real split is required because tail-JMP DISPATCH_NEXT is incompatible with a
	// function declaring its own stack frame (the prologue's SUBQ wouldn't be undone). The
	// SHIM is NOFRAME (just CALL+branch+JMP); the REAL owns the abi0 marshalling frame and
	// RETs back to the SHIM.
	//
	// The argument list is primitive (string + bool) rather than the PathBShimSpec struct so
	// the per-arch implementations (which live in asm/asmgen_arch_amd64 and
	// asm/asmgen_arch_arm64) don't need to import the parent asm package - that would create
	// a cycle (asm -> arch -> asm). See handlers_pathb_shims.go for the PathBShimSpec struct
	// (the asm-package-level wrapper around these primitive args).
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes realSymbol (string) which is the REAL function symbol to CALL.
	//
	// Takes needsFrameRebuild (bool) which indicates whether the shim must reload the frame
	// pointer after the CALL.
	EmitPathBCallShim(emitter *asmgen.Emitter, realSymbol string, needsFrameRebuild bool)

	// EmitTier2CallShimNarrow emits the narrow tier-2 ASM-call shim.
	//
	// Used for handlers whose Go body does NOT read or write frame.programCounter. Skips the
	// two pre-CALL writes that the wide shim does (MOVQ R14 -> CTX_PC, MOVQ R14 ->
	// CTX_SAVED_PC), saving two dependent memory stores per dispatch on the hot opContinue
	// path.
	//
	// The post-CALL dispatch-register reload pattern matches the wide shim: Go's regabi may
	// freely clobber R8/R9/R10/R11/R12/R13 across the CALL, so the hot path reloads all of
	// them regardless.
	//
	// The narrow variant pairs with a Go-side trampoline emitted in emitPathBTrampolineFile
	// that delegates to pathBTrampolineNarrow instead of pathBTrampoline, which together
	// drop the frame.programCounter sync (pre-handler) and writeback (post).
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes trampolineSymbol (string) which is the Go trampoline to CALL.
	EmitTier2CallShimNarrow(emitter *asmgen.Emitter, trampolineSymbol string)

	// EmitTier2CallShimReal emits the REAL half of the tier-2 ASM-call shim+real pair.
	//
	// Owns a $32-0 frame for abi0 outgoing args. Marshals (ctx, instWord) into the abi0
	// argument slots, CALLs the Go trampoline, reloads all dispatch registers (Go ABI
	// clobbers caller-saved regs), and RETs to the SHIM. The SHIM then reads
	// CTX_HANDLER_RESULT and branches.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	// Takes trampolineSymbol (string) which is the Go trampoline to CALL.
	EmitTier2CallShimReal(emitter *asmgen.Emitter, trampolineSymbol string)
}

var (
	_ BytecodeArchitecturePort = (*archamd64.BytecodeAMD64Arch)(nil)

	_ BytecodeArchitecturePort = (*archarm64.BytecodeARM64Arch)(nil)
)
