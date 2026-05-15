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
	// Example: "handlerAddInt".
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

	// EmitTruncateNarrow emits the body of handlerTruncateNarrow.
	//
	// This is the tier-0 ASM lift for opTruncateNarrow. The handler narrows the register
	// slot at index A to B bits, sign-extending on the int bank (a shift-left then
	// arithmetic shift-right by 64-B) and zero-masking on the uint bank (AND against
	// (1<<B)-1). Operand bytes are A = register index, B = bit width (8/16/32), C =
	// registerKind (0=int, 5=uint). Both branches terminate with DISPATCH_NEXT so the
	// handler stays in the ASM dispatch loop.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitTruncateNarrow(emitter *asmgen.Emitter)

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
	// uses arithmetic shift (SARQ / ASR) to preserve sign. Both value and amount operands
	// are read from the uint bank, matching opShiftLeftUint / opShiftRightUint semantics.
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

	// UintInPlace emits an in-place uint64 operation on the uint register bank.
	//
	// Covers INC and DEC. Mirrors IntegerInPlace but addresses the uint bank via
	// CTX_UINTS_BASE (which is not pinned in a register, unlike int's R8 / float's R9), so
	// the implementation needs a scratch register to hold the loaded base. Used by tier-2
	// handlerSubOpTier2IncUint / handlerSubOpTier2DecUint.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes operation (string) which is the in-place operation name (INC, DEC).
	//
	// Takes indexRegister (string) which is the uint bank slot to modify.
	//
	// Takes baseScratch (string) which is a scratch register for loading the uint bank base.
	UintInPlace(emitter *asmgen.Emitter, operation string, indexRegister string, baseScratch string)

	// IntegerDivide emits instructions for signed integer division with a division-by-zero
	// guard that branches to the given label.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes dividendIndex (string) which is the dividend bank slot.
	//
	// Takes divisorIndex (string) which is the divisor bank slot.
	//
	// Takes quotientDestinationIndex (string) which is the destination slot for the
	// quotient.
	//
	// Takes remainderDestinationIndex (string) which is the destination slot for the
	// remainder.
	//
	// Takes zeroLabel (string) which is the branch target for division by zero.
	IntegerDivide(emitter *asmgen.Emitter, dividendIndex, divisorIndex, quotientDestinationIndex, remainderDestinationIndex, zeroLabel string)

	// IntegerShift emits instructions for a variable-amount integer shift in the specified
	// direction (LEFT or RIGHT).
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes direction (string) which is LEFT or RIGHT.
	//
	// Takes destinationIndex (string) which is the destination bank slot.
	//
	// Takes valueIndex (string) which is the value to shift.
	//
	// Takes amountIndex (string) which is the shift amount.
	IntegerShift(emitter *asmgen.Emitter, direction string, destinationIndex, valueIndex, amountIndex string)

	// IntegerCompareAndSet emits instructions to compare two integer register values and
	// write a boolean result (1 or 0) into the destination register.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes condition (string) which is the comparison relation.
	//
	// Takes destinationIndex (string) which is the destination slot for the boolean result.
	//
	// Takes leftIndex (string) which is the left operand bank slot.
	//
	// Takes rightIndex (string) which is the right operand bank slot.
	IntegerCompareAndSet(emitter *asmgen.Emitter, condition string, destinationIndex, leftIndex, rightIndex string)

	// IntegerCompareAndBranch emits instructions to compare two integer register values and
	// branch to the given label if the condition holds.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes condition (string) which is the comparison relation.
	//
	// Takes leftIndex (string) which is the left operand bank slot.
	//
	// Takes rightIndex (string) which is the right operand bank slot.
	//
	// Takes label (string) which is the branch target label.
	IntegerCompareAndBranch(emitter *asmgen.Emitter, condition string, leftIndex, rightIndex, label string)

	// IntegerCompareConstantAndBranch emits instructions to compare an integer register
	// value against a constant pool entry and branch if the condition holds.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes condition (string) which is the comparison relation.
	//
	// Takes registerIndex (string) which is the register operand bank slot.
	//
	// Takes constantIndex (string) which is the constant pool index.
	//
	// Takes label (string) which is the branch target label.
	IntegerCompareConstantAndBranch(emitter *asmgen.Emitter, condition string, registerIndex, constantIndex, label string)

	// FloatBinaryOperation emits instructions for a binary floating-point operation (ADD,
	// SUB, MUL, DIV) on register bank values.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes operation (string) which is the floating-point operation name.
	//
	// Takes destinationIndex (string) which is the destination float bank slot.
	//
	// Takes leftSourceIndex (string) which is the left operand bank slot.
	//
	// Takes rightSourceIndex (string) which is the right operand bank slot.
	FloatBinaryOperation(emitter *asmgen.Emitter, operation string, destinationIndex, leftSourceIndex, rightSourceIndex string)

	// FloatUnaryOperation emits instructions for a unary floating-point operation (SQRT,
	// ABS, FLOOR, CEIL, TRUNC, NEG, ROUND) on a register value.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes operation (string) which is the unary floating-point operation name.
	//
	// Takes destinationIndex (string) which is the destination float bank slot.
	//
	// Takes sourceIndex (string) which is the source operand bank slot.
	FloatUnaryOperation(emitter *asmgen.Emitter, operation string, destinationIndex, sourceIndex string)

	// FloatCompareAndSet emits instructions to compare two float register values and write a
	// boolean result into an integer destination register.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes condition (string) which is the comparison relation.
	//
	// Takes integerDestinationIndex (string) which is the int bank slot for the boolean
	// result.
	//
	// Takes floatLeftIndex (string) which is the left float operand bank slot.
	//
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
	//
	// Takes direction (string) which is the conversion direction.
	//
	// Takes destinationIndex (string) which is the destination bank slot.
	//
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
	//
	// Takes destinationIndex (string) which is the int bank slot for the length result.
	//
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
	//
	// Takes destinationIndex (string) which is the destination string bank slot.
	//
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
	//
	// Takes destinationIndex (string) which is the destination string bank slot.
	//
	// Takes constantIndex (string) which is the string constant pool index.
	StringConstLoad(emitter *asmgen.Emitter, destinationIndex, constantIndex string)

	// BoolConstLoad emits a copy of a bool constant into the bool bank.
	//
	// Copies a single byte from boolConstants[constantIndex] into bools[destinationIndex].
	// The constant table base is loaded from CTX_BOOL_CONSTS_BASE and the bools register
	// bank base from CTX_BOOLS_BASE. Both indices are 1-byte-strided.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes destinationIndex (string) which is the destination bool bank slot.
	//
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
	//
	// Takes indexRegister (string) which holds the uint bank slot index.
	//
	// Takes destinationRegister (string) which receives the loaded uint64 value.
	//
	// Takes baseScratch (string) which is a scratch register for the bank base pointer.
	LoadFromUintBank(emitter *asmgen.Emitter, indexRegister, destinationRegister, baseScratch string)

	// StoreToUintBank emits instructions to store a uint64 from the source register into the
	// uint register bank.
	//
	// Requires a scratch for the bank base load.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes sourceRegister (string) which holds the uint64 value to store.
	//
	// Takes indexRegister (string) which holds the uint bank slot index.
	//
	// Takes baseScratch (string) which is a scratch register for the bank base pointer.
	StoreToUintBank(emitter *asmgen.Emitter, sourceRegister, indexRegister, baseScratch string)

	// LoadFromBoolBank emits instructions to load a single bool byte from the bool register
	// bank into the low byte of the destination register, zero-extending to int64.
	//
	// Bools are stored as one byte each, so the access uses a different stride than the
	// 8-byte typed banks.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes indexRegister (string) which holds the bool bank slot index.
	//
	// Takes destinationRegister (string) which receives the loaded bool value,
	// zero-extended.
	//
	// Takes baseScratch (string) which is a scratch register for the bank base pointer.
	LoadFromBoolBank(emitter *asmgen.Emitter, indexRegister, destinationRegister, baseScratch string)

	// StoreToBoolBank emits instructions to store the low byte of the source register into
	// the bool register bank.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes sourceRegister (string) which holds the bool value to store.
	//
	// Takes indexRegister (string) which holds the bool bank slot index.
	//
	// Takes baseScratch (string) which is a scratch register for the bank base pointer.
	StoreToBoolBank(emitter *asmgen.Emitter, sourceRegister, indexRegister, baseScratch string)

	// BitwiseNotInPlace emits a bitwise complement of the given register, writing the result
	// back into the same register.
	//
	// Used by handlers that need to NOT a value already in a scratch register without going
	// through bank load/store.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes register (string) which is the register to complement in place.
	BitwiseNotInPlace(emitter *asmgen.Emitter, register string)

	// LogicalSetNonZero emits instructions that write 1 to the low byte of
	// destinationRegister if sourceRegister is non-zero, 0 otherwise.
	//
	// Used by IntToBool to convert a non-zero int into a true bool value (mirroring Go's
	// `bool(int)` semantics).
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes destinationRegister (string) which receives 1 if source is non-zero, 0
	// otherwise.
	//
	// Takes sourceRegister (string) which holds the value to test.
	LogicalSetNonZero(emitter *asmgen.Emitter, destinationRegister, sourceRegister string)

	// LogicalNot emits instructions to compute the logical negation of an integer value,
	// writing 1 if the source is zero and 0 otherwise.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes destinationIndex (string) which is the destination bank slot for the result.
	//
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
	//
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
	//
	// Takes goSymbol (string) which is the Go function symbol to call.
	EmitInlineGoCallThreeOperandShim(emitter *asmgen.Emitter, goSymbol string)

	// EmitTypedSliceFloatSet emits the entire body of a tier-1 umbrella sub-op of the form
	// slicesFloat[B][ints[C]] = floats[ext.A] with bounds checking. Mirrors
	// EmitTypedSliceFloatGet with operand B being the destination slice (not source) and the
	// extension word's A field being the source value register (not the index register).
	//
	// Used by handlerSubOpSliceSetFloatDirect.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceFloatSet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceUintGet emits the entire body of a tier-1 umbrella sub-op of the form
	// uints[B] = slicesUint[C][ints[ext.A]] with bounds checking.
	//
	// Element size is 8 bytes; the destination uint bank base is context-loaded from
	// CTX_UINTS_BASE.
	//
	// Used by handlerSubOpSliceGetUintDirect.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceUintGet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceUintSet emits the entire body of a tier-1 umbrella sub-op of the form
	// slicesUint[B][ints[C]] = uints[ext.A] with bounds checking.
	//
	// Element size 8 bytes; the source uint bank base is context-loaded from CTX_UINTS_BASE.
	//
	// Used by handlerSubOpSliceSetUintDirect.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceUintSet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceBoolGet emits the entire body of a tier-1 umbrella sub-op of the form
	// bools[B] = slicesBool[C][ints[ext.A]] with bounds checking.
	//
	// Element size is 1 byte; the destination bool bank base is context-loaded from
	// CTX_BOOLS_BASE.
	//
	// Used by handlerSubOpSliceGetBoolDirect.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceBoolGet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceBoolSet emits the entire body of a tier-1 umbrella sub-op of the form
	// slicesBool[B][ints[C]] = bools[ext.A] with bounds checking.
	//
	// Element size 1 byte; the source bool bank base is context-loaded from CTX_BOOLS_BASE.
	//
	// Used by handlerSubOpSliceSetBoolDirect.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceBoolSet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceByteGet emits the body of uints[B] = slicesByte[C][ints[ext.A]].
	//
	// Tier-1 umbrella sub-op with bounds checking. Element size is 1 byte; the destination
	// uint bank base is context-loaded from CTX_UINTS_BASE. Byte values are zero-extended to
	// uint64 matching the typed-slice routing convention.
	//
	// Used by handlerSubOpSliceGetByteDirect.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceByteGet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceByteSet emits the body of slicesByte[B][ints[C]] = byte(uints[ext.A]).
	//
	// Tier-1 umbrella sub-op with bounds checking. Element size 1 byte; the source uint bank
	// base is context-loaded from CTX_UINTS_BASE. Only the low byte of the uint64 source is
	// stored.
	//
	// Used by handlerSubOpSliceSetByteDirect.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceByteSet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceByteSlice emits the body of subOpSliceByteSlice.
	//
	// Implements slicesByte[A] = slicesByte[C][low:high] (no max-bound). Reads the source
	// 24-byte slice header, applies the low/high bounds from ints[ext.b] / ints[ext.c],
	// writes a fresh 24-byte header to slicesByte[A]. flags must be exactly low+high
	// (sliceLowBoundFlag | sliceHighBoundFlag); any other shape (no-flag, max-bound)
	// branches to *pathBFallback so the Go-side handleSubOpSliceByteSlice produces the
	// canonical error or handles the rare 3-argument case.
	//
	// Used by handlerSubOpSliceByteSlice.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceByteSlice(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceMove emits the body of subOpMoveSlice<Kind>.
	//
	// Implements slicesX[B] = slicesX[C].
	//
	// A same-bank typed-slice header move. Reads the 24-byte source slice header and writes
	// it to the destination slot. No bounds check; pure register-to-register move. The
	// contextOffset selects which typed-slice bank (slicesInt/Float/String/Bool/Uint/Byte)
	// participates; all six banks share identical layout (24-byte slice header per slot) so
	// one primitive parameterised on the bank base offset suffices.
	//
	// Used by handlerSubOpMoveSlice<Kind> for all six typed-slice banks.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceMove(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceSliceSlice emits the body of subOpSliceSlice<Kind>Direct.
	//
	// Implements slicesX[A] = slicesX[C][low:high] as a bounds-checked sub-slice on a
	// typed-slice bank with stride-aware data pointer adjustment. Mirrors
	// EmitTypedSliceByteSlice but with configurable element size: the source Data pointer is
	// shifted by (low << elementSizeShift) bytes. Only the low+high flag combination (== 3)
	// is handled in ASM; any other shape branches to *pathBFallback so the Go-side handler
	// produces the canonical error or handles the rare 3-argument case. elementSizeShift
	// selects the stride: 0 (stride 1) for slicesBool, 3 (stride 8) for
	// slicesInt/Float/Uint, 4 (stride 16) for slicesString. slicesByte continues to use
	// EmitTypedSliceByteSlice, which fuses the stride-1 case for tighter code.
	//
	// Used by handlerSubOpSliceSlice<Kind>Direct.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	//
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
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedRangeNextByte(emitter *asmgen.Emitter, contextOffset string)

	// EmitEqUintConstJumpFalse emits the fused compare-and-branch for
	// isa.SubOpEqUintConstJumpFalse, comparing uints[B] with the immediate byte in operand C
	// and jumping on mismatch.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitEqUintConstJumpFalse(emitter *asmgen.Emitter)

	// EmitRangeCheckUintJumpFalse emits the body of subOpRangeCheckUintJumpFalse.
	//
	// The fused range-check super-instruction. Reads uints[B] (the value), peeks the first
	// extension word for (loConst, hiConst), and on out-of-range applies the 16-bit signed
	// jump offset packed into the second extension word. The fused op also covers the 5
	// trailing opNops that occupy the remaining bytecode slots, so on either path PC
	// advances past all 7 trailing word slots before dispatching.
	//
	// Pure ASM; no bounds_fail path.
	//
	// Used by handlerSubOpRangeCheckUintJumpFalse.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitRangeCheckUintJumpFalse(emitter *asmgen.Emitter)

	// EmitTypedSliceStringGet emits the body of strings[B] = slicesString[C][ints[ext.A]].
	//
	// Tier-1 umbrella sub-op with bounds checking. Element size is 16 bytes (Go string
	// header: data pointer + length); the destination string bank base is context-loaded
	// from CTX_STRINGS_BASE. The 16-byte copy is emitted as two 8-byte loads/stores.
	//
	// Used by handlerSubOpSliceGetStringDirect.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceStringGet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceFloatGet emits the entire body of a tier-1 umbrella sub-op of the form
	// floats[B] = slicesFloat[C][ints[ext.A]] with bounds checking.
	//
	// The sequence extracts B (destination float bank index) and C (source slicesFloat slot
	// index) from the current instruction word, loads the slice header at slicesFloat[C]
	// from the typed-slice bank at contextOffset, peeks the next instruction word to read
	// its A field as the int-bank register holding the element index (without advancing PC),
	// loads the element index from ints[ext.A], performs an unsigned compare of index
	// against length, and on failure branches to *pathBFallback so the Go-side handler can
	// produce the proper error message. On success it loads the float64 element, stores it
	// to floats[B], advances PC past the consumed extension word, and tail-calls
	// DISPATCH_NEXT.
	//
	// Used by handlerSubOpSliceGetFloatDirect.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceFloatGet(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceIntGetDirect emits the entire body of the tier-0 opcode ints[A] =
	// slicesInt[B][ints[C]] with bounds checking.
	//
	// Unlike the tier-1 umbrella forms there is no extension word: all three operands come
	// from the current instruction word, so PC is not advanced beyond the normal dispatch
	// increment. An out-of-range index branches to *pathBFallback so the Go handler re-runs
	// the op and raises the proper index-out-of-range panic.
	//
	// Used by handlerSliceGetIntDirect.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceIntGetDirect(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceIntSetDirect emits the entire body of the tier-0 opcode
	// slicesInt[A][ints[B]] = ints[C] with bounds checking. See EmitTypedSliceIntGetDirect
	// for the tier-0 conventions.
	//
	// Used by handlerSliceSetIntDirect.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceIntSetDirect(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceIntGetDirectUnchecked emits the tier-0 opcode ints[A] =
	// slicesInt[B][ints[C]] without the bounds check: the compiler's BCE pass only emits the
	// unchecked form when a dominating comparison proves the index in range.
	//
	// Used by handlerSliceGetIntDirectUnchecked.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceIntGetDirectUnchecked(emitter *asmgen.Emitter, contextOffset string)

	// EmitTypedSliceIntSetDirectUnchecked emits the tier-0 opcode slicesInt[A][ints[B]] =
	// ints[C] without the bounds check. See EmitTypedSliceIntGetDirectUnchecked.
	//
	// Used by handlerSliceSetIntDirectUnchecked.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	EmitTypedSliceIntSetDirectUnchecked(emitter *asmgen.Emitter, contextOffset string)

	// EmitSliceIndexStructFieldScalar emits the tier-0 body for the fused slice[i].field
	// scalar readers (float and int families), with the element-type identity check and shim
	// fallback for every other shape.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes kindA (string) which is the first operand kind selector.
	//
	// Takes kindB (string) which is the second operand kind selector.
	//
	// Takes destIsFloat (bool) which indicates whether the destination bank is float.
	//
	// Takes shimSymbol (string) which is the Go shim symbol for unsupported shapes.
	EmitSliceIndexStructFieldScalar(emitter *asmgen.Emitter, kindA, kindB string, destIsFloat bool, shimSymbol string)

	// EmitDerefSliceIntAccess emits the tier-0 body for opDerefSliceGetInt (isSet=false) or
	// opDerefSliceSetInt with a live bounds check, falling back to the Go shim for
	// unsupported shapes and range panics.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes isSet (bool) which selects set mode (true) or get mode (false).
	//
	// Takes shimSymbol (string) which is the Go shim symbol for fallback.
	EmitDerefSliceIntAccess(emitter *asmgen.Emitter, isSet bool, shimSymbol string)

	// EmitGetStructFieldGeneralPointerT0 emits the tier-0 body for opGetStructFieldGeneral
	// restricted to plain pointer-kind fields, constructing the typed pointer destination
	// value under the write-barrier guard.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitGetStructFieldGeneralPointerT0(emitter *asmgen.Emitter)

	// EmitSetStructFieldGeneralT0 emits the tier-0 body for opSetStructFieldGeneral covering
	// plain pointer-kind fields and cycle-broken `any` fields, storing the value's pointer
	// words under the write-barrier guard.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitSetStructFieldGeneralT0(emitter *asmgen.Emitter)

	// EmitGetStructFieldRawPointerT0 emits the tier-0 body for opGetStructFieldRawPointerT0,
	// re-wrapping a cycle-broken eface field as a direct pointer-kind value under the
	// write-barrier guard.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitGetStructFieldRawPointerT0(emitter *asmgen.Emitter)

	// EmitEqGeneral emits the tier-0 body for opEqGeneral, testing fast general-equality
	// arms (both invalid, differing type words, pointer-identity kinds) and storing 0/1 into
	// ints[A], with interface operands and other kinds falling back to the Go shim.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitEqGeneral(emitter *asmgen.Emitter)

	// EmitMoveGeneralAlias emits the tier-0 body for opMoveGeneral's alias mode, performing
	// a raw 24-byte general-slot copy gated on the runtime write-barrier flag, with snapshot
	// and boundary modes (and the marking phase) falling back to the Go shim.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitMoveGeneralAlias(emitter *asmgen.Emitter)

	// EmitTestNilJump emits the tier-0 body for opTestNilJumpTrue (jumpWhenNil=true) or
	// opTestNilJumpFalse, nil-testing general[A] across the nilable kind range and branching
	// by the signed 16-bit offset in B|C<<8 with the poll-budget back-edge exit.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes jumpWhenNil (bool) which selects jump-on-nil (true) or jump-on-non-nil (false).
	EmitTestNilJump(emitter *asmgen.Emitter, jumpWhenNil bool)

	// EmitStructFieldScalarGetT0 emits the tier-0 fast path for a scalar struct-field GET
	// opcode dest[A] = *(receiverBase + layout[C].Offset), with layout bounds/flags/kind
	// gates and pointer/addressable-struct receiver resolution falling back to the named
	// tier-2 shim for unsupported shapes.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes kindA (string) which is the first operand kind selector.
	//
	// Takes kindB (string) which is the second operand kind selector.
	//
	// Takes bank (string) which names the destination register bank.
	//
	// Takes shimSymbol (string) which is the tier-2 shim symbol for unsupported shapes.
	EmitStructFieldScalarGetT0(emitter *asmgen.Emitter, kindA, kindB, bank, shimSymbol string)

	// EmitStructFieldScalarSetT0 emits the tier-0 fast path for a scalar struct-field SET
	// opcode *(receiverBase + layout[C].Offset) = source[B], mirroring
	// EmitStructFieldScalarGetT0() with bank naming the source bank.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes kindA (string) which is the first operand kind selector.
	//
	// Takes kindB (string) which is the second operand kind selector.
	//
	// Takes bank (string) which names the source register bank.
	//
	// Takes shimSymbol (string) which is the tier-2 shim symbol for unsupported shapes.
	EmitStructFieldScalarSetT0(emitter *asmgen.Emitter, kindA, kindB, bank, shimSymbol string)

	// EmitStructFieldSliceLen emits the tier-0 fast path for opGetStructFieldSliceLen:
	// ints[A] = len(receiver[B].field(layout[C])) for slice-kind fields with zero layout
	// flags, reading the header's length word in place with no allocation. Unsupported
	// shapes jump to the named tier-2 shim.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes shimSymbol (string) which is the tier-2 shim symbol for unsupported shapes.
	EmitStructFieldSliceLen(emitter *asmgen.Emitter, shimSymbol string)

	// EmitStructFieldSliceIndexScalar emits the tier-0 fast path for
	// opGetStructFieldSliceIndexScalar, loading a full-width scalar sub-field from a slice
	// element within a struct field directly into the destination bank.
	//
	// bank(ext2.C)[A] = receiver[B].field(layout[C])[ints[ext1.C]].subfield(ext2.wide). The
	// program counter only advances past the two extension words on the success path, so
	// shim fallbacks (including out-of-range indexes, which the Go handler raises as
	// interpreted panics) re-run the op from the original counter.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes shimSymbol (string) which is the tier-2 shim symbol for unsupported shapes.
	EmitStructFieldSliceIndexScalar(emitter *asmgen.Emitter, shimSymbol string)

	// EmitGetUpvalueScalar emits the tier-0 fast path for opGetUpvalue on direct scalar
	// cells, loading bank(C)[A] from frame.upvalues[B].cellField(C) where C names the
	// destination registerKind and selects both the upvalueCell scalar field to read and the
	// bank to write.
	//
	// Indirect cells, out-of-range indexes, nil cell pointers and non-scalar kinds jump to
	// the named tier-2 shim. Scalar bank stores carry no pointers, so no write barrier is
	// required.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes shimSymbol (string) which is the tier-2 shim symbol for unsupported shapes.
	EmitGetUpvalueScalar(emitter *asmgen.Emitter, shimSymbol string)

	// EmitLoadUintConst emits the tier-0 body for uints[A] = uintConstants[B|(C<<8)]. The
	// pool base comes from CTX_UINT_CONSTS_BASE and the destination bank from
	// CTX_UINTS_BASE; indexes are compiler-generated so no bounds check is emitted
	// (mirroring the pinned-register int-const loader).
	//
	// Used by handlerLoadUintConst.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitLoadUintConst(emitter *asmgen.Emitter)

	// EmitLoadUintConstSmall emits the tier-1 body for uints[B] = uint64(C), the
	// immediate-operand small-constant loader.
	//
	// Used by handlerSubOpLoadUintConstSmall.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitLoadUintConstSmall(emitter *asmgen.Emitter)

	// EmitUintConstArith emits the tier-1 body for uints[B] = uints[C] op
	// uintConstants[ext.A|(ext.B<<8)], consuming the extension word. The operation selector
	// must be one of "add", "sub", or "and".
	//
	// Used by handlerSubOpAddUintConst / SubUintConst / BitAndUintConst.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes operation (string) which is the arithmetic operation (add, sub, or and).
	EmitUintConstArith(emitter *asmgen.Emitter, operation string)

	// EmitUintDivide emits the tier-0 body for uints[A] = uints[B] / uints[C] with a
	// zero-divisor guard that exits through DIV_BY_ZERO_EXIT so the Go side raises the
	// interpreted panic.
	//
	// Used by handlerDivUint.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitUintDivide(emitter *asmgen.Emitter)

	// EmitUintRemainder emits the tier-0 body for uints[A] = uints[B] % uints[C] with the
	// same zero-divisor guard as EmitUintDivide.
	//
	// Used by handlerRemUint.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitUintRemainder(emitter *asmgen.Emitter)

	// EmitComplexCopy emits the entire body of a tier-1 umbrella sub-op of the form
	// complex[B] = complex[C]. Each complex128 slot is 16 bytes (real + imag); the copy is
	// two 8-byte loads + two 8-byte stores from the complex bank base loaded from
	// contextOffset.
	//
	// Used by handlerSubOpMoveComplex.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the complex
	// bank.
	EmitComplexCopy(emitter *asmgen.Emitter, contextOffset string)

	// EmitComplexNegate emits the body of complex[B] = -complex[C].
	//
	// Tier-1 umbrella sub-op. Implemented as a 16-byte load + bitwise XOR of the IEEE 754
	// sign bit (0x8000000000000000) on each float64 half + 16-byte store. Negating a
	// complex128 negates both real and imag, equivalent to flipping the sign bit on each
	// half.
	//
	// Used by handlerSubOpNegComplex.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the complex
	// bank.
	EmitComplexNegate(emitter *asmgen.Emitter, contextOffset string)

	// LoadComplexHalfToFloatBank emits instructions to read one float64 half of a complex128
	// element from the complex register bank into a destination float64 register.
	//
	// The complex bank base pointer lives at the given byte offset within the
	// dispatchContext (R15 on amd64, R19 on arm64). Each complex128 slot is 16 bytes (real
	// then imag). halfOffset is "0" for the real half or "8" for the imag half. The emitted
	// sequence loads the bank base into an arch-chosen scratch, computes
	// &complex[indexRegister] (multiplying by 16), reads the 8-byte float64 at halfOffset,
	// and stores it into the float bank at destinationFloatIndexRegister.
	//
	// Used by tier-1 sub-ops handlerSubOpRealComplex and handlerSubOpImagComplex to read a
	// single float64 half of a complex128 without leaving the ASM dispatch loop.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the complex
	// bank.
	//
	// Takes indexRegister (string) which holds the complex bank slot index.
	//
	// Takes halfOffset (string) which is "0" for real or "8" for imaginary.
	//
	// Takes destinationFloatIndexRegister (string) which is the float bank slot to write.
	LoadComplexHalfToFloatBank(emitter *asmgen.Emitter, contextOffset, indexRegister, halfOffset, destinationFloatIndexRegister string)

	// LoadTypedSliceHeaderLength emits instructions to read the length field of a slice
	// header stored in a typed-slice register bank.
	//
	// The bank base pointer lives at the given byte offset within the dispatchContext (R15
	// on amd64, R19 on arm64). Each slot is a Go slice header (24 bytes: data pointer +
	// length + capacity). The emitted sequence loads the bank base into an arch-chosen
	// scratch, computes &bank[indexRegister] (multiplying by 24), and writes the 8-byte
	// length field at offset 8 into destinationRegister.
	//
	// Used by tier-1 sub-ops handlerSubOpLenSliceIntDirect / handlerSubOpLenSliceFloatDirect
	// / handlerSubOpLenSliceStringDirect / handlerSubOpLenSliceBoolDirect /
	// handlerSubOpLenSliceUintDirect to compute len() of typed-slice bank slots without
	// leaving the ASM dispatch loop.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	//
	// Takes contextOffset (string) which is the dispatch context byte offset of the slice
	// bank.
	//
	// Takes indexRegister (string) which holds the slice bank slot index.
	//
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

	// EmitCallMethodInline emits the body of handlerCallMethodInline: the inline call
	// handler variant for SubOpCallMethod that selects the callee entry by the receiver's
	// type word from the site's per-type table before pushing the frame in assembly, and
	// exits to the Go tier-2 path on a miss.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitCallMethodInline(emitter *asmgen.Emitter)

	// EmitAllocStructLiteral emits the body of handlerSubOpAllocStructLiteral: general[C] =
	// zeroed pointer-free struct of the type indexed by the extension word, carved from the
	// arena generic-bytes slab, with EXIT_TIER2 fallbacks to the Go handler.
	//
	// Takes emitter (*asmgen.Emitter) which receives the generated instructions.
	EmitAllocStructLiteral(emitter *asmgen.Emitter)

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
