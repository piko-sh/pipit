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

package isa

import "math"

const (
	// OpNoOperation is the alias for the tier-3 no-op encoding {0, 0, 0, 0}.
	OpNoOperation = OpDrillTier1

	// OpcodeCount is the number of opcodes. It is declared as int so array-sizing compiles
	// even when the active opcode set fills the uint8 range.
	OpcodeCount = int(OpSetElementFieldFloat64) + 1
)

const (
	// OpDrillTier1 is the main-tier drill marker. When the op byte is 0, the dispatcher
	// descends into the tier-1 sub-opcode space using operand A as the discriminator.
	OpDrillTier1 Opcode = iota

	// OpExtension is an extension word carrying a 24-bit payload formed from
	// A|(B<<8)|(C<<16). Used for wide jump offsets, large constant indices, and multi-word
	// encodings.
	OpExtension

	// OpLoadInt64Constant loads intConstants[B|(C<<8)] into ints[A].
	OpLoadInt64Constant

	// OpLoadFloat64Constant loads floatConstants[B|(C<<8)] into floats[A].
	OpLoadFloat64Constant

	// OpAddInt64 sets ints[A] = ints[B] + ints[C].
	OpAddInt64

	// OpSubtractInt64 sets ints[A] = ints[B] - ints[C].
	OpSubtractInt64

	// OpMultiplyInt64 sets ints[A] = ints[B] * ints[C].
	OpMultiplyInt64

	// OpDivideInt64 sets ints[A] = ints[B] / ints[C]. Panics on zero divisor.
	OpDivideInt64

	// OpRemainderInt64 sets ints[A] = ints[B] % ints[C]. Panics on zero divisor.
	OpRemainderInt64

	// OpBitAndInt64 sets ints[A] = ints[B] & ints[C].
	OpBitAndInt64

	// OpBitOrInt64 sets ints[A] = ints[B] | ints[C].
	OpBitOrInt64

	// OpBitXorInt64 sets ints[A] = ints[B] ^ ints[C].
	OpBitXorInt64

	// OpBitAndNotInt64 sets ints[A] = ints[B] &^ ints[C].
	OpBitAndNotInt64

	// OpShiftLeftInt64 sets ints[A] = ints[B] << uint(ints[C]).
	OpShiftLeftInt64

	// OpShiftRightInt64 sets ints[A] = ints[B] >> uint(ints[C]).
	OpShiftRightInt64

	// OpAddFloat64 sets floats[A] = floats[B] + floats[C].
	OpAddFloat64

	// OpSubtractFloat64 sets floats[A] = floats[B] - floats[C].
	OpSubtractFloat64

	// OpMultiplyFloat64 sets floats[A] = floats[B] * floats[C].
	OpMultiplyFloat64

	// OpDivideFloat64 sets floats[A] = floats[B] / floats[C].
	OpDivideFloat64

	// OpEqualInt64 sets ints[A] = (ints[B] == ints[C]) ? 1 : 0.
	OpEqualInt64

	// OpNotEqualInt64 sets ints[A] = (ints[B] != ints[C]) ? 1 : 0.
	OpNotEqualInt64

	// OpLessThanInt64 sets ints[A] = (ints[B] < ints[C]) ? 1 : 0.
	OpLessThanInt64

	// OpLessOrEqualInt64 sets ints[A] = (ints[B] <= ints[C]) ? 1 : 0.
	OpLessOrEqualInt64

	// OpGreaterThanInt64 sets ints[A] = (ints[B] > ints[C]) ? 1 : 0.
	OpGreaterThanInt64

	// OpGreaterOrEqualInt64 sets ints[A] = (ints[B] >= ints[C]) ? 1 : 0.
	OpGreaterOrEqualInt64

	// OpEqualFloat64 sets ints[A] = (floats[B] == floats[C]) ? 1 : 0.
	OpEqualFloat64

	// OpNotEqualFloat64 sets ints[A] = (floats[B] != floats[C]) ? 1 : 0.
	OpNotEqualFloat64

	// OpLessThanFloat64 sets ints[A] = (floats[B] < floats[C]) ? 1 : 0.
	OpLessThanFloat64

	// OpLessOrEqualFloat64 sets ints[A] = (floats[B] <= floats[C]) ? 1 : 0.
	OpLessOrEqualFloat64

	// OpGreaterThanFloat64 sets ints[A] = (floats[B] > floats[C]) ? 1 : 0.
	OpGreaterThanFloat64

	// OpGreaterOrEqualFloat64 sets ints[A] = (floats[B] >= floats[C]) ? 1 : 0.
	OpGreaterOrEqualFloat64

	// OpJumpIfTrue jumps if ints[A] != 0. Offset in B|(C<<8).
	OpJumpIfTrue

	// OpJumpIfFalse jumps if ints[A] == 0. Offset in B|(C<<8).
	OpJumpIfFalse

	// OpSubtractInt64Constant sets ints[A] = ints[B] - intConstants[C]. Fuses
	// OpLoadInt64Constant + OpSubtractInt64 when the constant index fits in 8 bits.
	OpSubtractInt64Constant

	// OpAddInt64Constant sets ints[A] = ints[B] + intConstants[C]. Fuses OpLoadInt64Constant
	// + OpAddInt64 when the constant index fits in 8 bits.
	OpAddInt64Constant

	// OpMultiplyInt64Constant sets ints[A] = ints[B] * intConstants[C]. Fuses
	// OpLoadInt64Constant + OpMultiplyInt64 when the constant index fits in 8 bits.
	OpMultiplyInt64Constant

	// OpAddInt64ConstantThenJump sets ints[A] = ints[B] + intConstants[C] and
	// unconditionally jumps by offset in the following OpExtension. Fuses OpAddInt64Constant
	// + (OpDrillTier1, SubOpJump).
	OpAddInt64ConstantThenJump

	// OpGetStringByte sets uints[A] = uint64(strings[B][ints[C]]). Panics with
	// errIndexOutOfRange if index is out of bounds.
	OpGetStringByte

	// OpEqualString sets ints[A] = (strings[B] == strings[C]) ? 1 : 0.
	OpEqualString

	// OpNotEqualString sets ints[A] = (strings[B] != strings[C]) ? 1 : 0.
	OpNotEqualString

	// OpSliceString sets strings[A] = strings[B][low:high]. C encodes flags (bit 0 = low
	// present, bit 1 = high present) and an OpExtension follows with A=lowRegister,
	// B=highRegister.
	OpSliceString

	// OpGetStringByteAsInt64 sets ints[A] = int64(strings[B][ints[C]]). Fuses
	// OpGetStringByte + isa.SubOpConvertUint64ToInt64 to avoid the intermediate uint
	// register and one tier-2 trampoline in string loops.
	OpGetStringByteAsInt64

	// OpMoveGeneral copies general[B] to general[A]. C selects the snapshot mode: 0 =
	// dynamic (runtime kind switch), 1 = alias (reference-safe type), 2 = snapshot
	// (struct/array), 3 = read-only range candidate resolved at compile time, 4 = debugger
	// snapshot accompanying captured program fields.
	OpMoveGeneral

	// OpLoadStringConstant loads stringConstants[B|(C<<8)] into strings[A].
	OpLoadStringConstant

	// OpLoadGeneralConstant loads generalConstants[B|(C<<8)] into general[A].
	OpLoadGeneralConstant

	// OpLoadUint64Constant loads UintConstants[B|(C<<8)] into uints[A].
	OpLoadUint64Constant

	// OpLoadComplex128Constant loads ComplexConstants[B|(C<<8)] into complex[A].
	OpLoadComplex128Constant

	// OpAddUint64 sets uints[A] = uints[B] + uints[C].
	OpAddUint64

	// OpSubtractUint64 sets uints[A] = uints[B] - uints[C].
	OpSubtractUint64

	// OpMultiplyUint64 sets uints[A] = uints[B] * uints[C].
	OpMultiplyUint64

	// OpDivideUint64 sets uints[A] = uints[B] / uints[C]. Panics on zero divisor.
	OpDivideUint64

	// OpRemainderUint64 sets uints[A] = uints[B] % uints[C]. Panics on zero divisor.
	OpRemainderUint64

	// OpBitAndUint64 sets uints[A] = uints[B] & uints[C].
	OpBitAndUint64

	// OpBitOrUint64 sets uints[A] = uints[B] | uints[C].
	OpBitOrUint64

	// OpBitXorUint64 sets uints[A] = uints[B] ^ uints[C].
	OpBitXorUint64

	// OpBitAndNotUint64 sets uints[A] = uints[B] &^ uints[C].
	OpBitAndNotUint64

	// OpShiftLeftUint64 sets uints[A] = uints[B] << uints[C].
	OpShiftLeftUint64

	// OpShiftRightUint64 sets uints[A] = uints[B] >> uints[C].
	OpShiftRightUint64

	// OpEqualUint64 sets ints[A] = (uints[B] == uints[C]) ? 1 : 0.
	OpEqualUint64

	// OpNotEqualUint64 sets ints[A] = (uints[B] != uints[C]) ? 1 : 0.
	OpNotEqualUint64

	// OpLessThanUint64 sets ints[A] = (uints[B] < uints[C]) ? 1 : 0.
	OpLessThanUint64

	// OpLessOrEqualUint64 sets ints[A] = (uints[B] <= uints[C]) ? 1 : 0.
	OpLessOrEqualUint64

	// OpGreaterThanUint64 sets ints[A] = (uints[B] > uints[C]) ? 1 : 0.
	OpGreaterThanUint64

	// OpGreaterOrEqualUint64 sets ints[A] = (uints[B] >= uints[C]) ? 1 : 0.
	OpGreaterOrEqualUint64

	// OpAddComplex128 sets complex[A] = complex[B] + complex[C].
	OpAddComplex128

	// OpSubtractComplex128 sets complex[A] = complex[B] - complex[C].
	OpSubtractComplex128

	// OpMultiplyComplex128 sets complex[A] = complex[B] * complex[C].
	OpMultiplyComplex128

	// OpDivideComplex128 sets complex[A] = complex[B] / complex[C].
	OpDivideComplex128

	// OpEqualComplex128 sets ints[A] = (complex[B] == complex[C]) ? 1 : 0.
	OpEqualComplex128

	// OpNotEqualComplex128 sets ints[A] = (complex[B] != complex[C]) ? 1 : 0.
	OpNotEqualComplex128

	// OpMakeComplex128 sets complex[A] = complex(floats[B], floats[C]).
	OpMakeComplex128

	// OpConcatenateString sets strings[A] = strings[B] + strings[C].
	OpConcatenateString

	// OpConcatenateStringRune sets strings[A] = strings[B] + string(rune(ints[C])). Fuses
	// isa.SubOpConvertRuneToString + OpConcatenateString with in-place arena extension.
	OpConcatenateStringRune

	// OpLessThanString sets ints[A] = (strings[B] < strings[C]) ? 1 : 0.
	OpLessThanString

	// OpLessOrEqualString sets ints[A] = (strings[B] <= strings[C]) ? 1 : 0.
	OpLessOrEqualString

	// OpGreaterThanString sets ints[A] = (strings[B] > strings[C]) ? 1 : 0.
	OpGreaterThanString

	// OpGreaterOrEqualString sets ints[A] = (strings[B] >= strings[C]) ? 1 : 0.
	OpGreaterOrEqualString

	// OpEqualGeneral sets ints[A] = (general[B] == general[C]) ? 1 : 0 via reflect.
	OpEqualGeneral

	// OpNotEqualGeneral sets ints[A] = (general[B] != general[C]) ? 1 : 0 via reflect, using
	// the same equality logic as OpEqualGeneral.
	OpNotEqualGeneral

	// OpLessThanGeneral sets ints[A] = (general[B] < general[C]) ? 1 : 0 via reflect.
	OpLessThanGeneral

	// OpLessOrEqualGeneral sets ints[A] = (general[B] <= general[C]) ? 1 : 0 via reflect.
	OpLessOrEqualGeneral

	// OpGreaterThanGeneral sets ints[A] = (general[B] > general[C]) ? 1 : 0 via reflect.
	OpGreaterThanGeneral

	// OpGreaterOrEqualGeneral sets ints[A] = (general[B] >= general[C]) ? 1 : 0 via reflect.
	OpGreaterOrEqualGeneral

	// OpAddGeneral sets general[A] = general[B] + general[C] via reflect.
	OpAddGeneral

	// OpSubtractGeneral sets general[A] = general[B] - general[C] via reflect.
	OpSubtractGeneral

	// OpMultiplyGeneral sets general[A] = general[B] * general[C] via reflect.
	OpMultiplyGeneral

	// OpDivideGeneral sets general[A] = general[B] / general[C] via reflect.
	OpDivideGeneral

	// OpRemainderGeneral sets general[A] = general[B] % general[C] via reflect.
	OpRemainderGeneral

	// OpTruncateToWidth truncates a narrow integer register in place. B is the bit width (8,
	// 16, or 32) and C selects uint (0, mask only) or int (1, mask then sign-extend).
	OpTruncateToWidth

	// OpBoxAsInterface wraps a typed value into an interface.
	OpBoxAsInterface

	// OpUnboxFromInterface extracts the concrete value from an interface.
	OpUnboxFromInterface

	// OpJumpIfNil tests if general[A] is nil/invalid and jumps by signed offset B|(C<<8) if
	// true.
	OpJumpIfNil

	// OpJumpIfNotNil tests if general[A] is nil/invalid and jumps by signed offset B|(C<<8)
	// if false (i.e. not nil).
	OpJumpIfNotNil

	// OpMakeClosure creates a closure in general[A] from function index B|(C<<8). Upvalue
	// descriptors are in the CompiledFunction.
	OpMakeClosure

	// OpGetCapturedVariable loads upvalue[B] into the register at A. C encodes the register
	// kind.
	OpGetCapturedVariable

	// OpSetCapturedVariable stores the register at A into upvalue[B]. C encodes the register
	// kind.
	OpSetCapturedVariable

	// OpDefer pushes a deferred call. general[A] is the function, B is the number of
	// arguments.
	OpDefer

	// OpGo spawns a goroutine calling general[A] with B arguments.
	OpGo

	// OpMakeSlice creates a slice: general[A] = make([]T, ints[B], ints[C]). The type T is
	// looked up from the function's type table via an extension instruction whose operand C
	// carries MakeSliceExtHeapFlag when the backing must live on the Go heap.
	OpMakeSlice

	// OpGetElement reads an element, target = general[A][ints[B]]. C encodes the destination
	// register kind and index via extension.
	OpGetElement

	// OpSetElement writes an element, general[A][ints[B]] = source.
	OpSetElement

	// OpGetMap reads a map element, target = general[A][key].
	OpGetMap

	// OpSetMap writes a map element, general[A][key] = value.
	OpSetMap

	// OpGetMapOk reads a map element with its ok flag, general[A] = general[B][general[C]],
	// ints[extensionWord.a] = ok (0 or 1).
	OpGetMapOk

	// OpAppend appends: general[A] = append(general[B], arguments...).
	OpAppend

	// OpAppendSpread sets general[A] = append(general[B], general[C]...) via
	// reflect.AppendSlice.
	OpAppendSpread

	// OpAppendByte is the byte-builder append fast path (A=dest general, B=source []byte
	// general, C=byte uint register), emitted only when the element type is statically byte.
	OpAppendByte

	// OpCopyGeneral copies a slice, ints[A] = copy(general[B], general[C]).
	OpCopyGeneral

	// OpGetElementInt64 reads an integer element from a slice/array without reflect boxing.
	// ints[A] = general[B].Index(ints[C]).Int() (or .Uint() for unsigned element types).
	OpGetElementInt64

	// OpSetElementInt64 writes an integer value to a slice/array element without reflect
	// boxing. general[A].Index(ints[B]).SetInt(ints[C]) (or .SetUint() for unsigned element
	// types).
	OpSetElementInt64

	// OpGetElementFloat64 reads a float element from a slice/array without reflect boxing.
	// floats[A] = general[B].Index(ints[C]).Float().
	OpGetElementFloat64

	// OpSetElementFloat64 writes a float value to a slice/array element without reflect
	// boxing. general[A].Index(ints[B]).SetFloat(floats[C]).
	OpSetElementFloat64

	// OpGetElementString reads a string element from a slice/array. strings[A] =
	// general[B].Index(ints[C]).String().
	OpGetElementString

	// OpSetElementString writes a string value to a slice/array element.
	// general[A].Index(ints[B]).SetString(strings[C]).
	OpSetElementString

	// OpGetElementBool reads a bool element from a slice/array. bools[A] =
	// general[B].Index(ints[C]).Bool().
	OpGetElementBool

	// OpSetElementBool writes a bool value to a slice/array element.
	// general[A].Index(ints[B]).SetBool(bools[C]).
	OpSetElementBool

	// OpGetElementUint64 reads a uint element from a slice/array. uints[A] =
	// general[B].Index(ints[C]).Uint().
	OpGetElementUint64

	// OpSetElementUint64 writes a uint value to a slice/array element.
	// general[A].Index(ints[B]).SetUint(uints[C]).
	OpSetElementUint64

	// OpLoadCompositeZeroInPlace loads a composite-zero constant, reusing the destination
	// register's existing storage when it already holds a value of the same type.
	// PromoteCompositeZeroReuse emits this only after proving the site is frame-local and
	// unaliased.
	OpLoadCompositeZeroInPlace

	// OpRangeNextSliceByte advances a typed range step over a slicesByte entry. A=index int,
	// B=source slicesByte, C=destination uint.
	OpRangeNextSliceByte

	// OpGetMapInt64Int64 reads ints[A] = map[int]int in general[B] with key ints[C].
	OpGetMapInt64Int64

	// OpSetMapInt64Int64 writes general[A][ints[B]] = ints[C] for map[int]int.
	OpSetMapInt64Int64

	// OpGetMapStringInt64 reads ints[A] = map[string]int in general[B] with key strings[C].
	// Bypasses reflect.MapIndex via the cached typed map handle on vm.typedHandleCache.
	OpGetMapStringInt64

	// OpSetMapStringInt64 writes general[A][strings[B]] = ints[C] for map[string]int.
	// Bypasses reflect.SetMapIndex via the cached typed map handle.
	OpSetMapStringInt64

	// OpGetMapStringString reads strings[A] = map[string]string in general[B] with key
	// strings[C].
	OpGetMapStringString

	// OpSetMapStringString writes general[A][strings[B]] = strings[C] for map[string]string.
	OpSetMapStringString

	// OpGetMapInt64String reads strings[A] = map[int]string in general[B] with key ints[C].
	OpGetMapInt64String

	// OpSetMapInt64String writes general[A][ints[B]] = strings[C] for map[int]string.
	OpSetMapInt64String

	// OpGetMapInt64Int64Ok reads ints[A] = map[int]int in general[B] with key ints[C], plus
	// an extension word whose A field is the int register holding the ok flag.
	OpGetMapInt64Int64Ok

	// OpGetMapStringInt64Ok reads ints[A] = map[string]int in general[B] with key
	// strings[C], plus an extension word whose A field is the int register holding the ok
	// flag.
	OpGetMapStringInt64Ok

	// OpGetMapStringStringOk reads strings[A] = map[string]string in general[B] with key
	// strings[C], plus an extension word whose A field is the int register holding the ok
	// flag.
	OpGetMapStringStringOk

	// OpGetMapInt64StringOk reads strings[A] = map[int]string in general[B] with key
	// ints[C], plus an extension word whose A field is the int register holding the ok flag.
	OpGetMapInt64StringOk

	// OpGetMapInt64General reads general[A] = map[int]V via general[B][ints[C]], where V is
	// any general-bank type.
	OpGetMapInt64General

	// OpGetMapInt64GeneralOk is the comma-ok form of OpGetMapInt64General, with the ok flag
	// in extension word A.
	OpGetMapInt64GeneralOk

	// OpGetMapStringGeneral reads general[A] = map[string]V in general[B] with key
	// strings[C], where V is any general-bank type.
	OpGetMapStringGeneral

	// OpGetMapStringGeneralOk is the comma-ok form of OpGetMapStringGeneral, with the ok
	// flag in extension word A.
	OpGetMapStringGeneralOk

	// OpSetMapStringGeneral writes general[A][strings[B]] = general[C] for map[string]V
	// where V is in the general bank.
	OpSetMapStringGeneral

	// OpAddToMapInt64Int64 fuses get-add-set for map[int]int, performing general[A][ints[B]]
	// += ints[C] in one map probe. Absent keys are treated as 0.
	OpAddToMapInt64Int64

	// OpAddToMapStringInt64 fuses get-add-set for map[string]int, performing
	// general[A][strings[B]] += ints[C] in one map probe. Absent keys are treated as 0.
	OpAddToMapStringInt64

	// OpGetField reads general[B].Field(C) into the target register. A encodes the
	// destination and the register kind comes from an extension word.
	OpGetField

	// OpSetField writes source register to general[A].Field(B).
	OpSetField

	// OpSetFieldInt64 writes ints[C] to general[A].Field(B).
	OpSetFieldInt64

	// OpGetFieldInt64 reads ints[A] = general[B].Field(C).Int().
	OpGetFieldInt64

	// OpBindMethod creates a bound method value in general[A] with receiver general[B]. C is
	// the embedded field traversal count and extension words carry function and field
	// indices.
	OpBindMethod

	// OpSendToChannel sends a value on general[A].
	OpSendToChannel

	// OpAddress takes an address, general[A] = &register[B]. C encodes the source register
	// kind.
	OpAddress

	// OpDereference dereferences *general[B] into the target register. A encodes the
	// destination register index and C encodes its kind.
	OpDereference

	// OpAllocateIndirect heap-escapes a variable via reflect.New of the type at the
	// OpExtension type table index.
	//
	// general[A] receives the pointer. B is the source register index, and C is the source
	// RegisterKind.
	OpAllocateIndirect

	// OpTypeAssert performs general[A] = general[B].(T). ints[C] receives the ok flag for
	// the comma-ok form; the type T index comes from an extension instruction.
	OpTypeAssert

	// OpConvertGeneral performs a type conversion, general[A] = T(general[B]).
	OpConvertGeneral

	// OpGetGlobal loads a package-level variable into the register at index A in bank C. B
	// is the global variable index.
	OpGetGlobal

	// OpSetGlobal stores the register at index A (bank C) into the package-level variable at
	// index B.
	OpSetGlobal

	// OpUnsafeString sets strings[A] = unsafe.String(general[B], ints[C]).
	OpUnsafeString

	// OpUnsafeSlice sets general[A] = unsafe.Slice(general[B], ints[C]).
	OpUnsafeSlice

	// OpUnsafeAdd sets general[A] = unsafe.Add(general[B], ints[C]).
	OpUnsafeAdd

	// OpStringsContainsRune sets bools[A] = strings.ContainsRune(strings[B], rune(ints[C])).
	OpStringsContainsRune

	// OpStringsContains sets bools[A] = strings.Contains(strings[B], strings[C]).
	OpStringsContains

	// OpStringsHasPrefix sets bools[A] = strings.HasPrefix(strings[B], strings[C]).
	OpStringsHasPrefix

	// OpStringsHasSuffix sets bools[A] = strings.HasSuffix(strings[B], strings[C]).
	OpStringsHasSuffix

	// OpStringsEqualFold sets bools[A] = strings.EqualFold(strings[B], strings[C]).
	OpStringsEqualFold

	// OpStringsIndex sets ints[A] = strings.Index(strings[B], strings[C]).
	OpStringsIndex

	// OpStringsCount sets ints[A] = strings.Count(strings[B], strings[C]).
	OpStringsCount

	// OpStringsTrimPrefix sets strings[A] = strings.TrimPrefix(strings[B], strings[C]).
	OpStringsTrimPrefix

	// OpStringsTrimSuffix sets strings[A] = strings.TrimSuffix(strings[B], strings[C]).
	OpStringsTrimSuffix

	// OpStringsTrim sets strings[A] = strings.Trim(strings[B], strings[C]).
	OpStringsTrim

	// OpStringsIndexRune sets ints[A] = strings.IndexRune(strings[B], rune(ints[C])).
	OpStringsIndexRune

	// OpStringsRepeat sets strings[A] = strings.Repeat(strings[B], int(ints[C])).
	OpStringsRepeat

	// OpStringsLastIndex sets ints[A] = strings.LastIndex(strings[B], strings[C]).
	OpStringsLastIndex

	// OpStringsJoin sets strings[A] = strings.Join(general[B], strings[C]).
	OpStringsJoin

	// OpStringsSplit sets general[A] = strings.Split(strings[B], strings[C]).
	OpStringsSplit

	// OpStringsReplaceAll sets strings[A] = strings.ReplaceAll(strings[B], strings[C],
	// strings[extensionWord.a]). The replacement string register index is in the following
	// OpExtension.
	OpStringsReplaceAll

	// OpMathPow sets floats[A] = math.Pow(floats[B], floats[C]).
	OpMathPow

	// OpGetSliceInt64Element sets ints[A] = slicesInt[B][ints[C]] via direct Go indexing
	// with bounds checking.
	OpGetSliceInt64Element

	// OpSetSliceInt64Element sets slicesInt[A][ints[B]] = ints[C] via direct Go indexing.
	OpSetSliceInt64Element

	// OpRangeNextSliceInt64 advances a typed range over a slicesInt entry. A=index int,
	// B=source slicesInt, C=destination int.
	OpRangeNextSliceInt64

	// OpGetStructFieldInt64 is the tier-0 fast-path int struct-field read (A=dest int,
	// B=source general, C=structLayoutTable index), falling back to the tier-1 sub-op when
	// the index exceeds uint8.
	OpGetStructFieldInt64

	// OpSetStructFieldInt64 is the tier-0 fast-path int struct-field write. Operand
	// A=structReg general, B=value int register, C=structLayoutTable index (uint8).
	OpSetStructFieldInt64

	// OpGetStructFieldUint64 is the tier-0 fast-path uint struct-field read. Same operand
	// layout as OpGetStructFieldInt64 but writes into the uints bank.
	OpGetStructFieldUint64

	// OpSetStructFieldUint64 is the tier-0 fast-path uint struct-field write. Operand
	// A=structReg general, B=value uint register, C=structLayoutTable index (uint8).
	OpSetStructFieldUint64

	// OpGetStructFieldFloat64 is the tier-0 fast-path float struct-field read. Same operand
	// layout as OpGetStructFieldUint64 but writes into the floats bank.
	OpGetStructFieldFloat64

	// OpSetStructFieldFloat64 is the tier-0 fast-path float struct-field write.
	OpSetStructFieldFloat64

	// OpGetStructFieldBool is the tier-0 fast-path bool struct-field read.
	OpGetStructFieldBool

	// OpSetStructFieldBool is the tier-0 fast-path bool struct-field write.
	OpSetStructFieldBool

	// OpGetStructFieldGeneral is the tier-0 fast-path read for a pointer- or interface-typed
	// struct field (A=dest general, B=source general, C=structLayoutTable index).
	OpGetStructFieldGeneral

	// OpSetStructFieldGeneral is the tier-0 fast-path write for a pointer- or
	// interface-typed struct field (A=struct general, B=value general, C=structLayoutTable
	// index).
	OpSetStructFieldGeneral

	// OpCopyStructFieldGeneral is the fused general-bank field copy (A=srcRecv general,
	// B=dstRecv general, C=srcLayoutIdx) with the dstLayoutIdx in the following OpExtension.
	OpCopyStructFieldGeneral

	// OpGetElementFieldInt64 is the tier-0 fused slice[i].field read for int leaves (A=dest
	// int, B=source slice general, C=index int).
	OpGetElementFieldInt64

	// OpGetElementFieldUint64 mirrors OpGetElementFieldInt64 for uint-kind leaf fields.
	// Destination is uint register A.
	OpGetElementFieldUint64

	// OpGetElementFieldFloat64 mirrors OpGetElementFieldInt64 for float-kind leaf fields.
	// Destination is float register A.
	OpGetElementFieldFloat64

	// OpGetElementFieldBool mirrors OpGetElementFieldInt64 for bool-kind leaf fields.
	// Destination is bool register A.
	OpGetElementFieldBool

	// OpGetElementFieldString mirrors OpGetElementFieldInt64 for string-kind leaves.
	// Destination is string register A.
	OpGetElementFieldString

	// OpGetMapInt64Int64OkThenJumpIfFalse fuses OpGetMapInt64Int64Ok + OpJumpIfFalse. The
	// extension word carries the ok register, jump offset low and high bytes.
	OpGetMapInt64Int64OkThenJumpIfFalse

	// OpGetMapStringInt64OkThenJumpIfFalse mirrors OpGetMapInt64Int64OkThenJumpIfFalse for
	// map[string]int with string keys.
	OpGetMapStringInt64OkThenJumpIfFalse

	// OpGetMapStringStringOkThenJumpIfFalse mirrors OpGetMapInt64Int64OkThenJumpIfFalse for
	// map[string]string (string keys + string values).
	OpGetMapStringStringOkThenJumpIfFalse

	// OpGetMapInt64StringOkThenJumpIfFalse mirrors OpGetMapInt64Int64OkThenJumpIfFalse for
	// map[int]string (int keys + string values).
	OpGetMapInt64StringOkThenJumpIfFalse

	// OpGetMapInt64GeneralOkThenJumpIfFalse mirrors OpGetMapInt64Int64OkThenJumpIfFalse for
	// map[int]V where V is in the general bank.
	OpGetMapInt64GeneralOkThenJumpIfFalse

	// OpGetMapStringGeneralOkThenJumpIfFalse mirrors OpGetMapInt64GeneralOkThenJumpIfFalse
	// for map[string]V with string keys.
	OpGetMapStringGeneralOkThenJumpIfFalse

	// OpSwapStructFieldsGeneral fuses `t.x, t.y = t.y, t.x` for two general-bank fields into
	// a single dispatch (A=struct general, B=layout index X, C=layout index Y), requiring
	// both fields to have the same kind.
	OpSwapStructFieldsGeneral

	// OpGetStructFieldRawPointer specialises OpGetStructFieldGeneral for cycle-broken
	// interface{} fields that always hold a pointer.
	OpGetStructFieldRawPointer

	// OpGetSliceInt64ElementUnchecked is the bounds-elided variant of
	// OpGetSliceInt64Element. Emitted only when the access is provably in range.
	OpGetSliceInt64ElementUnchecked

	// OpSetSliceInt64ElementUnchecked is the bounds-elided variant of
	// OpSetSliceInt64Element. Emitted only when the access is provably in range.
	OpSetSliceInt64ElementUnchecked

	// OpGetElementInt64Unchecked is the bounds-elided variant of OpGetElementInt64. Emitted
	// only when the BCE pass proves the access is in range.
	OpGetElementInt64Unchecked

	// OpSetElementInt64Unchecked is the bounds-elided variant of OpSetElementInt64. Emitted
	// only when the access is provably in range.
	OpSetElementInt64Unchecked

	// OpBoxAsType boxes a typed-bank value into the general bank (A=general dest, B=source
	// register, C=source RegisterKind) with a following OpExtension carrying the type index.
	OpBoxAsType

	// OpAppendByteInPlace is the byte-builder in-place append, covering both the `x =
	// append(x, b)` and `*p = append(*p, b)` forms. The alias analysis pass demotes it back
	// to OpAppendByte at any site it cannot prove alias-free.
	OpAppendByteInPlace

	// OpAppendInPlace is the generic in-place sibling of OpAppend, covering both direct and
	// pointer forms. Falls back to OpAppend when the source header is not arena-owned.
	OpAppendInPlace

	// OpAppendSpreadInPlace is the spread sibling of OpAppendInPlace, handling `x =
	// append(x, source...)` for arbitrary element types.
	OpAppendSpreadInPlace

	// OpSetMapInt64General writes general[A][ints[B]] = general[C] for map[int]V where V is
	// in the general bank.
	OpSetMapInt64General

	// OpGetStructFieldElementGeneral fuses an array-typed struct-field read with an element
	// index. Sets general[A] = receiver[B].field(layout[C]).elem[ints[ext1.c]].
	OpGetStructFieldElementGeneral

	// OpSetStructFieldElementGeneral is the write-side twin of
	// OpGetStructFieldElementGeneral. Sets receiver[A].field(layout[C]).elem[ints[ext1.c]] =
	// general[B].
	OpSetStructFieldElementGeneral

	// OpGetElementInt64ViaPointer reads one element of an int slice reached through a
	// pointer, setting ints[A] = (*general[B])[ints[C]].
	OpGetElementInt64ViaPointer

	// OpSetElementInt64ViaPointer is the write-side twin, (*general[A])[ints[B]] = ints[C].
	// Element stores into int slices carry no pointers, so no write barrier applies.
	OpSetElementInt64ViaPointer

	// OpLengthStructField reads the length of a slice-kind struct field without
	// materialising a slice header. Sets ints[A] = len(receiver[B].field(layout[C])).
	OpLengthStructField

	// OpGetStructFieldElementField reads a scalar sub-field of one element from a
	// slice-typed struct field without materialising a slice header or boxing the element.
	OpGetStructFieldElementField

	// OpAppendStruct appends a pointer-free struct or array element to a general-bank slice.
	// The extension word indexes the element type in the type table.
	OpAppendStruct

	// OpAppendInt64 appends ints[C] to the general-bank slice in general[B], writing the
	// result to general[A].
	OpAppendInt64

	// OpAppendFloat64 is the float-bank twin of OpAppendInt64.
	OpAppendFloat64

	// OpAppendString is the string-bank twin of OpAppendInt64. The element passes the arena
	// string barrier inside the handler.
	OpAppendString

	// OpTypeSwitchCase is one row of a SubOpTypeSwitchJump table. A indexes the case type
	// and B|(C<<8) is the signed offset to the case body.
	OpTypeSwitchCase

	// OpAddInt32 sets ints[A] = int64(int32(ints[B] + ints[C])). Fuses OpAddInt64 with the
	// 32-bit OpTruncateToWidth of its result.
	OpAddInt32

	// OpSubtractInt32 sets ints[A] = int64(int32(ints[B] - ints[C])).
	OpSubtractInt32

	// OpMultiplyInt32 sets ints[A] = int64(int32(ints[B] * ints[C])).
	OpMultiplyInt32

	// OpAddInt32Constant sets ints[A] = int64(int32(ints[B] + intConstants[C])).
	OpAddInt32Constant

	// OpSubtractInt32Constant sets ints[A] = int64(int32(ints[B] - intConstants[C])).
	OpSubtractInt32Constant

	// OpAddUint32 sets uints[A] = uint64(uint32(uints[B] + uints[C])).
	OpAddUint32

	// OpMultiplyInt64ConstantThenAdd sets ints[A] = ints[extension.A] + ints[B] *
	// intConstants[C].
	OpMultiplyInt64ConstantThenAdd

	// OpAppendStringToMapString appends strings[C] to the slice the map general[A] holds
	// under strings[B] and stores the slice back.
	OpAppendStringToMapString

	// OpSubtractUint32 sets uints[A] = uint64(uint32(uints[B] - uints[C])).
	OpSubtractUint32

	// OpMultiplyUint32 sets uints[A] = uint64(uint32(uints[B] * uints[C])).
	OpMultiplyUint32

	// OpSetElementFieldInt64 stores ints[A] into an integer field of the struct element
	// ints[C] of the slice in general[B], truncated to the field's width.
	//
	// One OpExtension word follows. Bytes A and B hold the struct-layout index of the field.
	// Byte C names a register of the general bank the operation may pass the element
	// through. A store that succeeds leaves that register as it found it. An index out of
	// range and a nil slice fail as OpGetElement fails.
	OpSetElementFieldInt64

	// OpSetElementFieldFloat64 is OpSetElementFieldInt64 for floats[A] and a float field.
	OpSetElementFieldFloat64
)

const (
	// SubOpDrillTier2 is the tier-1 drill marker, reserving iota=0 so the "byte == 0 means
	// descend" convention is shared by every tier.
	SubOpDrillTier2 SubOpcode = iota

	// SubOpMathSin sets floats[B] = math.Sin(floats[C]).
	SubOpMathSin

	// SubOpMathCos sets floats[B] = math.Cos(floats[C]).
	SubOpMathCos

	// SubOpMathExp sets floats[B] = math.Exp(floats[C]).
	SubOpMathExp

	// SubOpMathTan sets floats[B] = math.Tan(floats[C]).
	SubOpMathTan

	// SubOpMathMod sets floats[B] = math.Mod(floats[C], floats[ext.A]).
	SubOpMathMod

	// SubOpStrconvFormatBool sets strings[B] = strconv.FormatBool(bools[C]).
	SubOpStrconvFormatBool

	// SubOpStrconvFormatInt sets strings[B] = strconv.FormatInt(ints[C], int(ints[ext.A])).
	SubOpStrconvFormatInt

	// SubOpStrconvItoa sets strings[B] = strconv.Itoa(int(ints[C])).
	SubOpStrconvItoa

	// SubOpRealPartComplex128 sets floats[B] = real(complex[C]).
	SubOpRealPartComplex128

	// SubOpImaginaryPartComplex128 sets floats[B] = imag(complex[C]).
	SubOpImaginaryPartComplex128

	// SubOpConvertBytesToString sets strings[B] = string(general[C]) for a []byte source.
	SubOpConvertBytesToString

	// SubOpMakeMethodExpression builds a method expression value in general[B] from
	// general[C].
	SubOpMakeMethodExpression

	// SubOpCapacityGeneral sets ints[B] = cap(general[C]) for any capacity-bearing
	// collection bank.
	SubOpCapacityGeneral

	// SubOpNegateComplex128 sets complex[B] = -complex[C].
	SubOpNegateComplex128

	// SubOpMoveComplex128 copies complex[C] to complex[B].
	SubOpMoveComplex128

	// SubOpMakeSliceInt64 creates a typed []int64 slice in the slicesInt bank. B=dest
	// slicesInt, C=length int, capacity in following OpExtension.
	SubOpMakeSliceInt64

	// SubOpLengthSliceInt64 sets ints[B] = int64(len(slicesInt[C])).
	SubOpLengthSliceInt64

	// SubOpMakeSliceFloat64 creates a typed []float64 slice in the slicesFloat bank.
	SubOpMakeSliceFloat64

	// SubOpGetSliceFloat64Element reads a float element from a typed []float64 slice without
	// crossing the reflect boundary. The index is in the extension word so the three primary
	// operands cover destination, source and the umbrella sub-opcode.
	SubOpGetSliceFloat64Element

	// SubOpSetSliceFloat64Element writes a float value to a typed []float64 slice element.
	SubOpSetSliceFloat64Element

	// SubOpLengthSliceFloat64 sets ints[B] = int64(len(slicesFloat[C])).
	SubOpLengthSliceFloat64

	// SubOpMakeSliceString creates a typed []string slice in the slicesString bank.
	SubOpMakeSliceString

	// SubOpGetSliceStringElement reads a string element from a typed []string slice.
	SubOpGetSliceStringElement

	// SubOpSetSliceStringElement writes a string value to a typed []string slice element.
	SubOpSetSliceStringElement

	// SubOpLengthSliceString sets ints[B] = int64(len(slicesString[C])).
	SubOpLengthSliceString

	// SubOpMakeSliceBool creates a typed []bool slice in the slicesBool bank.
	SubOpMakeSliceBool

	// SubOpGetSliceBoolElement reads a bool element from a typed []bool slice, bools[B] =
	// slicesBool[C][ints[ext.A]].
	SubOpGetSliceBoolElement

	// SubOpSetSliceBoolElement writes a bool value to a typed []bool slice element.
	SubOpSetSliceBoolElement

	// SubOpLengthSliceBool sets ints[B] = int64(len(slicesBool[C])).
	SubOpLengthSliceBool

	// SubOpMakeSliceUint64 creates a typed []uint64 slice in the slicesUint bank.
	SubOpMakeSliceUint64

	// SubOpGetSliceUint64Element reads a uint element from a typed []uint64 slice, uints[B]
	// = slicesUint[C][ints[ext.A]].
	SubOpGetSliceUint64Element

	// SubOpSetSliceUint64Element writes a uint value to a typed []uint64 slice element.
	SubOpSetSliceUint64Element

	// SubOpLengthSliceUint64 sets ints[B] = int64(len(slicesUint[C])).
	SubOpLengthSliceUint64

	// SubOpBoxSliceInt64 converts a typed []int64 slice in the slicesInt bank to a
	// reflect.Value in the general bank.
	SubOpBoxSliceInt64

	// SubOpMoveInt64 copies ints[C] to ints[B].
	SubOpMoveInt64

	// SubOpMoveFloat64 copies floats[C] to floats[B].
	SubOpMoveFloat64

	// SubOpMoveString copies strings[C] to strings[B].
	SubOpMoveString

	// SubOpMoveBool copies bools[C] to bools[B].
	SubOpMoveBool

	// SubOpMoveUint64 copies uints[C] to uints[B].
	SubOpMoveUint64

	// SubOpBoxInt64 boxes ints[C] into general[B].
	SubOpBoxInt64

	// SubOpUnboxInt64 unboxes general[C] into ints[B].
	SubOpUnboxInt64

	// SubOpBoxFloat64 boxes floats[C] into general[B].
	SubOpBoxFloat64

	// SubOpUnboxFloat64 unboxes general[C] into floats[B].
	SubOpUnboxFloat64

	// SubOpBoxString boxes strings[C] into general[B].
	SubOpBoxString

	// SubOpUnboxString unboxes general[C] into strings[B].
	SubOpUnboxString

	// SubOpNegateInt64 sets ints[B] = -ints[C].
	SubOpNegateInt64

	// SubOpNegateFloat64 sets floats[B] = -floats[C].
	SubOpNegateFloat64

	// SubOpBitNotInt64 sets ints[B] = ^ints[C].
	SubOpBitNotInt64

	// SubOpBitNotUint64 sets uints[B] = ^uints[C].
	SubOpBitNotUint64

	// SubOpConvertInt64ToFloat64 converts ints[C] to floats[B].
	SubOpConvertInt64ToFloat64

	// SubOpConvertFloat64ToInt64 converts floats[C] to ints[B].
	SubOpConvertFloat64ToInt64

	// SubOpNotInt64 sets ints[B] to 1 when ints[C] is zero and to 0 otherwise, the logical
	// NOT of a boolean held in the int bank (a bool operand is converted to and from the int
	// bank around it).
	SubOpNotInt64

	// SubOpConvertBoolToInt64 converts bools[C] to ints[B].
	SubOpConvertBoolToInt64

	// SubOpConvertInt64ToBool converts ints[C] to bools[B].
	SubOpConvertInt64ToBool

	// SubOpConvertInt64ToUint64 converts ints[C] to uints[B].
	SubOpConvertInt64ToUint64

	// SubOpConvertUint64ToInt64 converts uints[C] to ints[B].
	SubOpConvertUint64ToInt64

	// SubOpConvertUint64ToFloat64 converts uints[C] to floats[B].
	SubOpConvertUint64ToFloat64

	// SubOpConvertFloat64ToUint64 converts floats[C] to uints[B].
	SubOpConvertFloat64ToUint64

	// SubOpMathSqrt sets floats[B] = math.Sqrt(floats[C]).
	SubOpMathSqrt

	// SubOpMathAbs sets floats[B] = math.Abs(floats[C]).
	SubOpMathAbs

	// SubOpMathFloor sets floats[B] = math.Floor(floats[C]).
	SubOpMathFloor

	// SubOpMathCeil sets floats[B] = math.Ceil(floats[C]).
	SubOpMathCeil

	// SubOpMathTrunc sets floats[B] = math.Trunc(floats[C]).
	SubOpMathTrunc

	// SubOpMathRound sets floats[B] = math.Round(floats[C]).
	SubOpMathRound

	// SubOpLengthString sets ints[B] = len(strings[C]).
	SubOpLengthString

	// SubOpConvertRuneToString sets strings[B] = string(rune(ints[C])).
	SubOpConvertRuneToString

	// SubOpStringsToUpper sets strings[B] = strings.ToUpper(strings[C]).
	SubOpStringsToUpper

	// SubOpStringsToLower sets strings[B] = strings.ToLower(strings[C]).
	SubOpStringsToLower

	// SubOpStringsTrimSpace sets strings[B] = strings.TrimSpace(strings[C]).
	SubOpStringsTrimSpace

	// SubOpLengthGeneral sets ints[B] = len(general[C]) for any collection bank.
	SubOpLengthGeneral

	// SubOpConvertStringToBytes sets general[B] = []byte(strings[C]).
	SubOpConvertStringToBytes

	// SubOpUnsafeStringData sets general[B] to the *byte data pointer of strings[C].
	SubOpUnsafeStringData

	// SubOpUnsafeSliceData sets general[B] to the *element data pointer of the slice in
	// general[C].
	SubOpUnsafeSliceData

	// SubOpJump unconditionally jumps by the signed offset B|(C<<8).
	SubOpJump

	// SubOpLoadInt64Immediate sets ints[B] = int64(C), materialising a small literal inline.
	SubOpLoadInt64Immediate

	// SubOpLoadBoolImmediateAsInt64 sets ints[B] = C (0 or 1), materialising a boolean
	// literal inline.
	SubOpLoadBoolImmediateAsInt64

	// SubOpIncrementInt64ThenJumpIfLessThan fuses increment + compare + branch for the
	// for-loop back-edge. Increments ints[B] and jumps (offset in OpExtension) when ints[B]
	// < ints[C].
	SubOpIncrementInt64ThenJumpIfLessThan

	// SubOpLessThanLengthStringThenJumpIfFalse fuses len + compare + branch for string-loop
	// conditions. Jumps when ints[B] >= len(strings[C]).
	SubOpLessThanLengthStringThenJumpIfFalse

	// SubOpLoadZero zeroes the register at index B in the bank named by kind C.
	SubOpLoadZero

	// SubOpMakeChannel allocates a new channel of the type indexed by general[B] with
	// capacity ints[C].
	SubOpMakeChannel

	// SubOpDeleteFromMap deletes general[C] from the map at general[B].
	SubOpDeleteFromMap

	// SubOpReceiveFromChannel receives a value from the channel at general[B] into the
	// destination encoded by ints[C].
	SubOpReceiveFromChannel

	// SubOpBindMethodByName resolves the method named by the following OpExtension word on
	// general[B] and stores the bound method in general[C].
	SubOpBindMethodByName

	// SubOpSpill writes the register at index B (in bank kind C) into a reserved spill slot
	// encoded in the following OpExtension word.
	SubOpSpill

	// SubOpReload reads from the spill slot encoded in the following OpExtension word into
	// the register at index B (in bank kind C).
	SubOpReload

	// SubOpGetStructFieldInt64Wide reads an integer-kind struct field via the
	// structLayoutTable. B=dest int, C=source general (struct), with the layout index in the
	// following OpExtension.
	SubOpGetStructFieldInt64Wide

	// SubOpGetStructFieldUint64Wide reads an unsigned-integer struct field via
	// structLayoutTable into uints[B]. Covers uint8 through uint64, uint and uintptr.
	SubOpGetStructFieldUint64Wide

	// SubOpGetStructFieldFloat64Wide reads a float-kind struct field via the
	// structLayoutTable into floats[B]. Covers float32 and float64.
	SubOpGetStructFieldFloat64Wide

	// SubOpGetStructFieldBoolWide reads a bool-kind struct field via the structLayoutTable
	// mechanism. Stores into bools[B].
	SubOpGetStructFieldBoolWide

	// SubOpGetStructFieldStringWide reads a string-kind struct field via the
	// structLayoutTable into strings[B].
	SubOpGetStructFieldStringWide

	// SubOpSetStructFieldInt64Wide writes an integer-kind struct field via the
	// structLayoutTable. B=dest general (struct), C=source int, with the layout index in the
	// following OpExtension.
	SubOpSetStructFieldInt64Wide

	// SubOpSetStructFieldUint64Wide writes an unsigned-integer-kind struct field via the
	// structLayoutTable mechanism.
	SubOpSetStructFieldUint64Wide

	// SubOpSetStructFieldFloat64Wide writes a float-kind struct field via the
	// structLayoutTable mechanism.
	SubOpSetStructFieldFloat64Wide

	// SubOpSetStructFieldBoolWide writes a bool-kind struct field via the structLayoutTable
	// mechanism.
	SubOpSetStructFieldBoolWide

	// SubOpSetStructFieldStringWide writes a string-kind struct field via the
	// structLayoutTable mechanism.
	SubOpSetStructFieldStringWide

	// SubOpAddUint64Constant sets uints[B] = uints[C] + constant, the constant being the
	// entry of the uint constant pool that bytes A and B of the OpExtension word that
	// follows index.
	SubOpAddUint64Constant

	// SubOpSubtractUint64Constant sets uints[B] = uints[C] - constant, the constant being
	// named as SubOpAddUint64Constant names its own.
	SubOpSubtractUint64Constant

	// SubOpBitAndUint64Constant sets uints[B] = uints[C] & constant, the constant being
	// named as SubOpAddUint64Constant names its own.
	SubOpBitAndUint64Constant

	// SubOpLoadUint64Immediate sets uints[B] = uint64(C), materialising a small unsigned
	// literal inline without a constant-pool lookup.
	SubOpLoadUint64Immediate

	// SubOpAppendUint64Wide appends uints[ext.A] to a uint-element slice in general[C],
	// storing the result in general[B].
	SubOpAppendUint64Wide

	// SubOpAppendInt64Wide appends ints[ext.A] to an int-element slice in general[C],
	// storing the result in general[B].
	SubOpAppendInt64Wide

	// SubOpAppendStringWide is the tier-1 sibling for string elements, general[B] =
	// append(general[C], strings[ext.A]).
	SubOpAppendStringWide

	// SubOpAppendFloat64Wide is the tier-1 sibling for float elements, general[B] =
	// append(general[C], float*(floats[ext.A])).
	SubOpAppendFloat64Wide

	// SubOpAppendBoolWide is the tier-1 sibling for bool elements, general[B] =
	// append(general[C], bools[ext.A]).
	SubOpAppendBoolWide

	// SubOpAppendByteViaPointer fuses `*p = append(*p, b)` into a single op (B=pointer
	// general, C=byte uint).
	SubOpAppendByteViaPointer

	// SubOpAppendSpreadByteViaPointer fuses `*p = append(*p, X...)` for []byte. B=pointer
	// general, C=source slice general.
	SubOpAppendSpreadByteViaPointer

	// SubOpIncrementStructFieldInt64 fuses `s.field++` for an int-kind struct field
	// (B=receiver general, C=structLayoutTable index).
	SubOpIncrementStructFieldInt64

	// SubOpDecrementStructFieldInt64 is the decrement sibling of
	// SubOpIncrementStructFieldInt64. Same encoding, same constraints, `s.field--`
	// semantics.
	SubOpDecrementStructFieldInt64

	// SubOpIncrementStructFieldUint64 mirrors SubOpIncrementStructFieldInt64 for uint-kind
	// fields. Same encoding.
	SubOpIncrementStructFieldUint64

	// SubOpDecrementStructFieldUint64 is the decrement sibling of
	// SubOpIncrementStructFieldUint64.
	SubOpDecrementStructFieldUint64

	// SubOpMakeSliceByte builds slicesByte[B] = make([]byte, ints[C], ints[ext.A]).
	SubOpMakeSliceByte

	// SubOpGetSliceByteElement sets uints[B] = uint64(slicesByte[C][ints[ext.A]]).
	SubOpGetSliceByteElement

	// SubOpSetSliceByteElement sets slicesByte[B][ints[C]+ext.B] = byte(uints[ext.A]). The
	// extension word's byte B is an unsigned displacement, zero for a plain store.
	SubOpSetSliceByteElement

	// SubOpLengthSliceByte sets ints[B] = int64(len(slicesByte[C])).
	SubOpLengthSliceByte

	// SubOpSliceSliceByte performs a three-way slice on slicesByte[C] via ext words.
	SubOpSliceSliceByte

	// SubOpLessThanInt64ThenJumpIfFalse compares ints[B] < ints[C] and jumps by the offset
	// in the following OpExtension if false.
	SubOpLessThanInt64ThenJumpIfFalse

	// SubOpLessOrEqualInt64ThenJumpIfFalse compares ints[B] <= ints[C] and jumps by the
	// offset in the following OpExtension if false. Fuses OpLessOrEqualInt64 +
	// OpJumpIfFalse.
	SubOpLessOrEqualInt64ThenJumpIfFalse

	// SubOpGreaterThanInt64ThenJumpIfFalse compares ints[B] > ints[C] and jumps by the
	// offset in the following OpExtension if false. Fuses OpGreaterThanInt64 +
	// OpJumpIfFalse.
	SubOpGreaterThanInt64ThenJumpIfFalse

	// SubOpGreaterOrEqualInt64ThenJumpIfFalse compares ints[B] >= ints[C] and jumps by the
	// offset in the following OpExtension if false. Fuses OpGreaterOrEqualInt64 +
	// OpJumpIfFalse.
	SubOpGreaterOrEqualInt64ThenJumpIfFalse

	// SubOpEqualInt64ThenJumpIfFalse compares ints[B] == ints[C] and jumps by the offset in
	// the following OpExtension if false. Fuses OpEqualInt64 + OpJumpIfFalse.
	SubOpEqualInt64ThenJumpIfFalse

	// SubOpNotEqualInt64ThenJumpIfFalse compares ints[B] != ints[C] and jumps by the offset
	// in the following OpExtension if false. Fuses OpNotEqualInt64 + OpJumpIfFalse.
	SubOpNotEqualInt64ThenJumpIfFalse

	// SubOpLessOrEqualInt64ConstantThenJumpIfFalse compares ints[B] <= intConstants[C] and
	// jumps by the offset in the following OpExtension if false. Fuses OpLoadInt64Constant +
	// OpLessOrEqualInt64 + OpJumpIfFalse.
	SubOpLessOrEqualInt64ConstantThenJumpIfFalse

	// SubOpLessThanInt64ConstantThenJumpIfFalse compares ints[B] < intConstants[C] and jumps
	// by the offset in the following OpExtension if false. Fuses OpLoadInt64Constant +
	// OpLessThanInt64 + OpJumpIfFalse.
	SubOpLessThanInt64ConstantThenJumpIfFalse

	// SubOpEqualInt64ConstantThenJumpIfFalse compares ints[B] == intConstants[C] and jumps
	// by the offset in the following OpExtension if false. Fuses OpLoadInt64Constant +
	// OpEqualInt64 + OpJumpIfFalse.
	SubOpEqualInt64ConstantThenJumpIfFalse

	// SubOpEqualInt64ConstantThenJumpIfTrue compares ints[B] == intConstants[C] and jumps by
	// the offset in the following OpExtension if true. Fuses OpLoadInt64Constant +
	// OpEqualInt64 + OpJumpIfTrue.
	SubOpEqualInt64ConstantThenJumpIfTrue

	// SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse compares ints[B] >= intConstants[C]
	// and jumps by the offset in the following OpExtension if false. Fuses
	// OpLoadInt64Constant + OpGreaterOrEqualInt64 + OpJumpIfFalse.
	SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse

	// SubOpGreaterThanInt64ConstantThenJumpIfFalse compares ints[B] > intConstants[C] and
	// jumps by the offset in the following OpExtension if false. Fuses OpLoadInt64Constant +
	// OpGreaterThanInt64 + OpJumpIfFalse.
	SubOpGreaterThanInt64ConstantThenJumpIfFalse

	// SubOpCall calls a compiled function through the call site indexed by B|C<<8, which
	// names the callee, the argument registers and the result registers.
	SubOpCall

	// SubOpTailCall performs an optimised tail call that reuses the current frame instead of
	// pushing a new one.
	SubOpTailCall

	// SubOpCallNative calls a native reflect.Value function through the call site indexed by
	// B|C<<8; the site names the register holding the function and the arguments.
	SubOpCallNative

	// SubOpCallBuiltin calls the built-in function identified by B with C arguments, each
	// carried by a following extension word as {register, kind, 0}.
	SubOpCallBuiltin

	// SubOpCallMethod dispatches a method call using the runtime method table with call site
	// index B|C<<8. The method name string constant index is in an extension word.
	SubOpCallMethod

	// SubOpCallIIFE calls an immediately invoked function expression with call site index
	// B|(C<<8), fusing OpMakeClosure + SubOpCall.
	SubOpCallIIFE

	// SubOpCallScalar is a lean sibling of SubOpCall for calls whose signature fits entirely
	// in typed register banks.
	SubOpCallScalar

	// SubOpCallMethodInlineable mirrors SubOpCallMethod but consults a per-site
	// inlineDescriptor cache, running known shapes inline and falling back to standard
	// dispatch otherwise.
	SubOpCallMethodInlineable

	// SubOpEqualStringConstantThenJumpIfFalse compares strings[B] == stringConstants[C] and
	// jumps by offset in the following OpExtension if false (not equal). Fuses
	// OpLoadStringConstant + OpEqualString + OpJumpIfFalse.
	SubOpEqualStringConstantThenJumpIfFalse

	// SubOpSelect executes a select statement over B cases and writes the index of the
	// chosen case to ints[C]. Extension words encode the cases: a header {direction, channel
	// register, hasOk} per case, followed for a receive by {destination, kind, ok register}
	// and for a send by {value register, kind, 0}.
	SubOpSelect

	// SubOpLoadBoolConstant loads BoolConstants[C] into bools[B].
	SubOpLoadBoolConstant

	// SubOpResetSharedCell invalidates the shared upvalue cell for the register at index B
	// in bank C, forcing the next OpMakeClosure to create a fresh cell. Used for Go 1.22+
	// per-iteration loop variable scoping.
	SubOpResetSharedCell

	// SubOpSetSharedCell copies the current register value (bank C, index B) into the
	// corresponding shared upvalue cell if one exists.
	SubOpSetSharedCell

	// SubOpSliceGeneral slices a value, general[B] = general[C][low:high:max]. The bounds
	// come from int registers named by the following extension words {flags, low register,
	// high register} and, when the flags carry SliceMaxBitFlag, {max register, 0, 0}.
	SubOpSliceGeneral

	// SubOpRangeInitialise initialises a range iterator over the collection in general[C]
	// and stores the iterator state in general[B].
	SubOpRangeInitialise

	// SubOpRangeNext advances the iterator in general[B], writes the key and value to the
	// registers named by the two following extension words, and sets ints[C] to 0 when the
	// iteration is complete.
	SubOpRangeNext

	// SubOpGetGlobalWide loads a package-level variable into the register at index B in bank
	// C. The global variable index is a 16-bit value read from the following extension word
	// (A|(B<<8)).
	SubOpGetGlobalWide

	// SubOpSetGlobalWide stores the register at index B (bank C) into the package-level
	// variable. The global variable index is a 16-bit value read from the following
	// extension word (A|(B<<8)).
	SubOpSetGlobalWide

	// SubOpEqualInterfaceNil sets ints[B] = (!general[C].IsValid()) ? 1 : 0.
	SubOpEqualInterfaceNil

	// SubOpNotEqualInterfaceNil sets ints[B] = (general[C].IsValid()) ? 1 : 0.
	SubOpNotEqualInterfaceNil

	// SubOpBoxSliceByte sets general[B] = reflect.ValueOf(slicesByte[C]).
	SubOpBoxSliceByte

	// SubOpConvertSliceByteToString converts the typed-bank []byte at slicesByte[C] to
	// strings[B].
	SubOpConvertSliceByteToString

	// SubOpEqualUint64ImmediateThenJumpIfFalse fuses a uint equality test with a conditional
	// jump (B=value uint, C=8-bit immediate).
	SubOpEqualUint64ImmediateThenJumpIfFalse

	// SubOpSimdDotProductFloat64 computes sum += a[i] * b[i] for two equal-length []float64
	// slices (B=dest float, C=slice A general, ext.A=slice B general).
	SubOpSimdDotProductFloat64

	// SubOpSimdSumFloat64 computes sum += a[i] for a []float64. In the primary word B is the
	// destination float register and C the slice A (general bank).
	SubOpSimdSumFloat64

	// SubOpSimdAddFloat64 computes destination[i] = a[i] + b[i] for three equal-length
	// []float64 slices. B=dest slice (general), C=slice A, ext.A=slice B.
	SubOpSimdAddFloat64

	// SubOpSimdScaleFloat64 computes s[i] *= k in place for a []float64 and a scalar k. The
	// primary word has B = slice (general bank), C = scalar k (float register).
	SubOpSimdScaleFloat64

	// SubOpUnboxSliceFloat64 extracts a []float64 from general[C] into slicesFloat[B].
	SubOpUnboxSliceFloat64

	// SubOpUnboxSliceInt64 extracts a []int64 from general[C] into slicesInt[B].
	SubOpUnboxSliceInt64

	// SubOpUnboxSliceString mirrors SubOpUnboxSliceFloat64 for the slicesString bank
	// ([]string).
	SubOpUnboxSliceString

	// SubOpUnboxSliceBool mirrors SubOpUnboxSliceFloat64 for the slicesBool bank ([]bool).
	SubOpUnboxSliceBool

	// SubOpUnboxSliceUint64 mirrors SubOpUnboxSliceFloat64 for the slicesUint bank
	// ([]uint64).
	SubOpUnboxSliceUint64

	// SubOpUnboxSliceByte mirrors SubOpUnboxSliceFloat64 for the slicesByte bank ([]byte).
	SubOpUnboxSliceByte

	// SubOpBoxSliceFloat64 sets general[B] = reflect.ValueOf(slicesFloat[C]).
	SubOpBoxSliceFloat64

	// SubOpBoxSliceString mirrors SubOpBoxSliceFloat64 for the slicesString bank.
	SubOpBoxSliceString

	// SubOpBoxSliceBool mirrors SubOpBoxSliceFloat64 for the slicesBool bank.
	SubOpBoxSliceBool

	// SubOpBoxSliceUint64 mirrors SubOpBoxSliceFloat64 for the slicesUint bank.
	SubOpBoxSliceUint64

	// SubOpMoveSliceInt64 copies slicesInt[C] to slicesInt[B].
	SubOpMoveSliceInt64

	// SubOpMoveSliceFloat64 copies slicesFloat[C] to slicesFloat[B].
	SubOpMoveSliceFloat64

	// SubOpMoveSliceString copies slicesString[C] to slicesString[B].
	SubOpMoveSliceString

	// SubOpMoveSliceBool copies slicesBool[C] to slicesBool[B].
	SubOpMoveSliceBool

	// SubOpMoveSliceUint64 copies slicesUint[C] to slicesUint[B].
	SubOpMoveSliceUint64

	// SubOpMoveSliceByte copies slicesByte[C] to slicesByte[B].
	SubOpMoveSliceByte

	// SubOpAppendSliceInt64 appends ints[ext.A] to slicesInt[C], storing the result in
	// slicesInt[B].
	SubOpAppendSliceInt64

	// SubOpAppendSliceFloat64 appends a float64 to a typed-bank []float64 slice. Same
	// encoding as SubOpAppendSliceInt64 but reads slicesFloat / floats and writes
	// slicesFloat.
	SubOpAppendSliceFloat64

	// SubOpAppendSliceString appends a string to a typed-bank []string slice.
	SubOpAppendSliceString

	// SubOpAppendSliceBool appends a bool to a typed-bank []bool slice.
	SubOpAppendSliceBool

	// SubOpAppendSliceUint64 appends a uint64 to a typed-bank []uint64 slice.
	SubOpAppendSliceUint64

	// SubOpAppendSliceByte appends a byte to a typed-bank []byte slice. Reads element from
	// uints (pipit's storage for uint8 scalar values).
	SubOpAppendSliceByte

	// SubOpSliceSliceInt64 performs `source[low:high:cap]` on slicesInt[C] and writes the
	// result to slicesInt[B], with bounds in the following extension word.
	SubOpSliceSliceInt64

	// SubOpSliceSliceFloat64 mirrors SubOpSliceSliceInt64 for slicesFloat.
	SubOpSliceSliceFloat64

	// SubOpSliceSliceString mirrors SubOpSliceSliceInt64 for slicesString.
	SubOpSliceSliceString

	// SubOpSliceSliceBool mirrors SubOpSliceSliceInt64 for slicesBool.
	SubOpSliceSliceBool

	// SubOpSliceSliceUint64 mirrors SubOpSliceSliceInt64 for slicesUint.
	SubOpSliceSliceUint64

	// SubOpCopySliceInt64 implements `copy(destination, source)` between two typed slicesInt
	// headers (A=count int, B=dest slicesInt, C=source slicesInt).
	SubOpCopySliceInt64

	// SubOpCopySliceFloat64 mirrors SubOpCopySliceInt64 for slicesFloat.
	SubOpCopySliceFloat64

	// SubOpCopySliceString mirrors SubOpCopySliceInt64 for slicesString.
	SubOpCopySliceString

	// SubOpCopySliceBool mirrors SubOpCopySliceInt64 for slicesBool.
	SubOpCopySliceBool

	// SubOpCopySliceUint64 mirrors SubOpCopySliceInt64 for slicesUint.
	SubOpCopySliceUint64

	// SubOpCopySliceByte mirrors SubOpCopySliceInt64 for slicesByte.
	SubOpCopySliceByte

	// SubOpAppendUint64WideInPlace is the in-place sibling of SubOpAppendUint64Wide
	// (B=C=slice general, ext.A=element uint).
	SubOpAppendUint64WideInPlace

	// SubOpAppendSpreadByteInPlace fuses `x = append(x, src...)` in place for []byte
	// (B=slice general, C=source general).
	SubOpAppendSpreadByteInPlace

	// SubOpGetStructFieldSliceInt64 reads a []int64 struct field directly into slicesInt[B]
	// (C=receiver general, layout index in the following OpExtension).
	SubOpGetStructFieldSliceInt64

	// SubOpGetStructFieldSliceFloat64 - float64 element bank counterpart of
	// SubOpGetStructFieldSliceInt64. Reads into slicesFloat[B].
	SubOpGetStructFieldSliceFloat64

	// SubOpGetStructFieldSliceUint64 - uint64 element bank counterpart. Reads into
	// slicesUint[B].
	SubOpGetStructFieldSliceUint64

	// SubOpGetStructFieldSliceString - string element bank counterpart. Reads into
	// slicesString[B].
	SubOpGetStructFieldSliceString

	// SubOpGetStructFieldSliceBool - bool element bank counterpart. Reads into
	// slicesBool[B].
	SubOpGetStructFieldSliceBool

	// SubOpGetStructFieldSliceByte - byte (uint8) element bank counterpart. Reads into
	// slicesByte[B].
	SubOpGetStructFieldSliceByte

	// SubOpSetStructFieldSliceInt64 writes slicesInt[C] into the receiver's []int64 field
	// (B=receiver general, layout index in the following OpExtension).
	SubOpSetStructFieldSliceInt64

	// SubOpSetStructFieldSliceFloat64 - float64 counterpart.
	SubOpSetStructFieldSliceFloat64

	// SubOpSetStructFieldSliceUint64 - uint64 counterpart.
	SubOpSetStructFieldSliceUint64

	// SubOpSetStructFieldSliceString - string counterpart.
	SubOpSetStructFieldSliceString

	// SubOpSetStructFieldSliceBool - bool counterpart.
	SubOpSetStructFieldSliceBool

	// SubOpSetStructFieldSliceByte - byte counterpart.
	SubOpSetStructFieldSliceByte

	// SubOpCapacitySliceInt64 sets ints[B] = int64(cap(slicesInt[C])).
	SubOpCapacitySliceInt64

	// SubOpCapacitySliceFloat64 - float64 counterpart of SubOpCapacitySliceInt64.
	SubOpCapacitySliceFloat64

	// SubOpCapacitySliceString - string counterpart.
	SubOpCapacitySliceString

	// SubOpCapacitySliceBool - bool counterpart.
	SubOpCapacitySliceBool

	// SubOpCapacitySliceUint64 - uint64 counterpart.
	SubOpCapacitySliceUint64

	// SubOpCapacitySliceByte - byte counterpart.
	SubOpCapacitySliceByte

	// SubOpMakeSliceHeap builds a typed-bank slice on the Go heap for locals that escape.
	SubOpMakeSliceHeap

	// SubOpTypeSwitchJump dispatches a concrete-type switch through a jump table formed by
	// the OpTypeSwitchCase rows that follow. Sets general[B] = general[C] narrowed to the
	// matching case type, then jumps to that case's body.
	SubOpTypeSwitchJump

	// SubOpRoundToFloat32 rounds floats[C] to float32 precision and stores it in floats[B].
	// float32 values live in the float64 bank.
	SubOpRoundToFloat32

	// SubOpRoundToComplex64 rounds both parts of complex[C] to float32 precision and stores
	// the result in complex[B].
	SubOpRoundToComplex64

	// SubOpEqualInterfaceStrict compares general[B] == general[C] using strict interface
	// equality and stores the result in ints[A] of the trailing OpExtension word.
	SubOpEqualInterfaceStrict

	// SubOpNotEqualInterfaceStrict is the != mirror of SubOpEqualInterfaceStrict.
	SubOpNotEqualInterfaceStrict

	// SubOpAddressField stores the address of a field of general[C] in general[B]. The
	// trailing OpExtension word's A operand supplies the field index.
	SubOpAddressField

	// SubOpIncrementInt64ThenJumpIfLessThanConstant increments ints[B] and jumps by the
	// extension offset when ints[B] < intConstants[C].
	SubOpIncrementInt64ThenJumpIfLessThanConstant

	// SubOpSetSliceByteElementRun stores two to four bytes at consecutive positions of
	// slicesByte[B], starting at ints[C] plus a displacement.
	//
	// One OpExtension word follows for each store, naming its value register of the uint
	// bank in byte A. The first also carries the displacement in byte B and the number of
	// stores in byte C. Stores before one that is out of range have happened when it panics,
	// as with separate SubOpSetSliceByteElement stores.
	SubOpSetSliceByteElementRun

	// SubOpGetSliceByteElementInt64 sets ints[B] = int64(slicesByte[C][ints[ext.A]]). It is
	// SubOpGetSliceByteElement and the conversion of the byte to int as one instruction.
	SubOpGetSliceByteElementInt64

	// SubOpGetStructFieldUint64Run reads two to four unsigned fields of the struct in
	// general[B] into uint registers, after first setting general[B] = general[C] as
	// OpMoveGeneral does when the two registers differ.
	//
	// One OpExtension word follows for each read, naming its destination register of the
	// uint bank in byte A and its struct-layout index in byte B. Byte C of the first word
	// holds the number of reads in its low half and the move's mode in its high half. The
	// reads are OpGetStructFieldUint64 reads made in order, so a failing one leaves what the
	// separate instructions would have left.
	SubOpGetStructFieldUint64Run

	// SubOpAddUint32Constant sets uints[B] = uint64(uint32(uints[C] + constant)), the
	// constant being the entry of the uint constant pool that bytes A and B of the
	// OpExtension word that follows index.
	SubOpAddUint32Constant

	// SubOpSubtractUint32Constant sets uints[B] = uint64(uint32(uints[C] - constant)), the
	// constant being named as SubOpAddUint32Constant names its own.
	SubOpSubtractUint32Constant

	// SubOpEqualUint64ThenJumpIfFalse compares uints[B] == uints[C] and jumps by the offset
	// in the OpExtension word that follows when they differ.
	SubOpEqualUint64ThenJumpIfFalse

	// SubOpNotEqualUint64ThenJumpIfFalse compares uints[B] != uints[C] and jumps by the
	// offset in the OpExtension word that follows when they are equal.
	SubOpNotEqualUint64ThenJumpIfFalse

	// SubOpIncrementInt64ThenJumpIfLessOrEqual increments ints[B] and jumps by the offset in
	// the OpExtension word that follows when ints[B] <= ints[C].
	SubOpIncrementInt64ThenJumpIfLessOrEqual

	// SubOpGetElementFieldUint64Run reads two to four unsigned fields of the struct element
	// ints[C] of the collection in general[B] into uint registers.
	//
	// One OpExtension word follows for each read, naming its destination register of the
	// uint bank in byte A and its struct-layout index in byte B. Byte C of the first word
	// holds the number of reads in its low half and an OpMoveGeneral mode in its high half.
	// Byte C of the second word names a register of the general bank that the operation
	// writes only when a debugger is attached. It then holds the element, copied with that
	// mode. An index out of range and a nil collection fail as OpGetElement fails, and a
	// failing read leaves the registers of the earlier reads written.
	SubOpGetElementFieldUint64Run
)

const (
	// SubOpTier2DrillTier3 is the tier-2 sub-opcode reserved at index 0 to drill from tier 2
	// down into tier 3. Following the "index 0 means drill down" convention, tier 2 dispatch
	// sees operand B == 0 and falls through to tier 3 dispatch using operand C as the tier-3
	// sub-opcode discriminator.
	SubOpTier2DrillTier3 SubOpcodeTier2 = iota

	// SubOpTier2IncrementInt64 increments the int register named by operand C. Lives in tier
	// 2 because the op uses only one register operand.
	SubOpTier2IncrementInt64

	// SubOpTier2DecrementInt64 decrements the int register named by operand C.
	SubOpTier2DecrementInt64

	// SubOpTier2IncrementUint64 increments the uint register named by operand C.
	SubOpTier2IncrementUint64

	// SubOpTier2DecrementUint64 decrements the uint register named by operand C.
	SubOpTier2DecrementUint64

	// SubOpTier2Panic raises a panic with the value in the general register named by operand
	// C.
	SubOpTier2Panic

	// SubOpTier2Recover stores the active panic value in the general register named by
	// operand C, or an invalid value if not panicking.
	SubOpTier2Recover

	// SubOpTier2SetZeroGeneral sets the value general[C] holds to its zero value in place,
	// through to its storage (used by the assign-through optimisation to clear composite
	// values in place).
	SubOpTier2SetZeroGeneral

	// SubOpTier2CloseChannel closes the channel held by the general register named by
	// operand C.
	SubOpTier2CloseChannel

	// SubOpTier2LoadNil writes a typed nil into the general register named by operand C.
	SubOpTier2LoadNil

	// SubOpTier2Return returns from the current function with operand C values. The only
	// operand (the return-value count) sits in C of the tier-2 encoding.
	SubOpTier2Return

	// SubOpTier2MakeInterfaceMethodExpression builds a method expression on an interface
	// type into general[C]. The function type and method name follow in two extension words.
	SubOpTier2MakeInterfaceMethodExpression

	// SubOpTier2MakeMap allocates a new map into general[C]. The following extension word
	// carries the type-table index in A|B<<8 and a log2 size hint in C.
	SubOpTier2MakeMap

	// SubOpTier2InRangeUint64ThenJumpIfFalse fuses a `value >= lo && value <= hi` range
	// check with a conditional jump. C=value uint register, with lo/hi constants and the
	// jump offset in two following extension words.
	SubOpTier2InRangeUint64ThenJumpIfFalse

	// SubOpTier2AllocateStructLiteral allocates a zeroed pointer-free struct literal in the
	// register arena (general[C] = new(T), type index in the extension word).
	SubOpTier2AllocateStructLiteral

	// SubOpTier2SynchroniseClosureCapturedVariables copies shared upvalue cells from the
	// closure in general[C] back into this frame's registers.
	SubOpTier2SynchroniseClosureCapturedVariables

	// SubOpTier2IncrementInt32 sets ints[C] = int64(int32(ints[C] + 1)). Fuses
	// SubOpTier2IncrementInt64 with the 32-bit OpTruncateToWidth of its register.
	SubOpTier2IncrementInt32

	// SubOpTier2DecrementInt32 sets ints[C] = int64(int32(ints[C] - 1)).
	SubOpTier2DecrementInt32

	// SubOpTier2PanicRangeFunctionContinuedInt64 checks a range-over-func loop's state flag
	// in ints[C].
	SubOpTier2PanicRangeFunctionContinuedInt64

	// SubOpTier2CheckShiftCount panics with Go's "negative shift amount" error when ints[C]
	// is below zero.
	SubOpTier2CheckShiftCount

	// SubOpTier2JumpIfFalseBool jumps by the offset in the OpExtension word that follows
	// when bools[C] is false.
	SubOpTier2JumpIfFalseBool

	// SubOpTier2JumpIfTrueBool jumps by the offset in the OpExtension word that follows when
	// bools[C] is true.
	SubOpTier2JumpIfTrueBool
)

// RangeFunctionStateExited is the value a range-over-func loop's state flag takes once
// the range statement has finished, so a body the iterator calls afterwards panics with
// Go's "after whole loop exit" error.
const RangeFunctionStateExited int64 = math.MinInt64

// RangeFunctionStatePanicked is the value a range-over-func loop's state flag holds while
// a call of the body runs.
const RangeFunctionStatePanicked int64 = math.MinInt64 + 1

// RangeFunctionStateRecovered is the value the range statement hands
// SubOpTier2PanicRangeFunctionContinuedInt64 after the iterator returns when the state
// flag still holds RangeFunctionStatePanicked, so the check raises Go's error for an
// iterator that recovered a panic of the body and did not resume panicking.
const RangeFunctionStateRecovered int64 = math.MinInt64 + 2

const (
	// SubOpTier3NoOperation is the zero-operand no-op, encoded as the all-zero word {0, 0,
	// 0, 0}.
	SubOpTier3NoOperation SubOpcodeTier3 = iota

	// SubOpTier3ReturnVoid returns from the current function with no values. Lives in tier 3
	// because the op consumes no register operands.
	SubOpTier3ReturnVoid

	// SubOpTier3SynchroniseIIFECapturedVariables copies upvalue cells of the just-returned
	// IIFE back into this frame's registers.
	SubOpTier3SynchroniseIIFECapturedVariables
)

// DisplayLabelWidth is the width of the disassembly label column, which is the longest
// printed name in the spec plus the two-character tier prefix ("1" and a colon).
const DisplayLabelWidth = 52

// Opcode identifies a virtual machine operation.
type Opcode uint8

// String returns the mnemonic recorded in the spec row, or UNKNOWN when the value has no
// row or names a reserved slot.
//
// Returns the mnemonic as a string.
func (op Opcode) String() string {
	if row, ok := SpecAt(TierMain, uint8(op)); ok && row.Name != "" {
		return row.Name
	}
	return "UNKNOWN"
}

// SubOpcode identifies which tier-1 body an OpDrillTier1 instruction dispatches to. The
// sub-op id sits in operand A, leaving B and C as the two register operands.
type SubOpcode uint8

// String returns the mnemonic recorded in the spec row, or UNKNOWN_SUBOP when the value
// has no row or names a reserved slot.
//
// Returns the mnemonic as a string.
func (op SubOpcode) String() string {
	if row, ok := SpecAt(TierSub1, uint8(op)); ok && row.Name != "" {
		return row.Name
	}
	return "UNKNOWN_SUBOP"
}

// SubOpcodeTier2 identifies a tier-2 body for a doubly-drilled instruction.
type SubOpcodeTier2 uint8

// String returns the mnemonic recorded in the spec row, or UNKNOWN_TIER2 when the value
// has no row or names a reserved slot.
//
// Returns the mnemonic as a string.
func (op SubOpcodeTier2) String() string {
	if row, ok := SpecAt(TierSub2, uint8(op)); ok && row.Name != "" {
		return row.Name
	}
	return "UNKNOWN_TIER2"
}

// SubOpcodeTier3 identifies a tier-3 body for a fully-drilled instruction.
type SubOpcodeTier3 uint8

// String returns the mnemonic recorded in the spec row, or UNKNOWN_TIER3 when the value
// has no row or names a reserved slot.
//
// Returns the mnemonic as a string.
func (op SubOpcodeTier3) String() string {
	if row, ok := SpecAt(TierSub3, uint8(op)); ok && row.Name != "" {
		return row.Name
	}
	return "UNKNOWN_TIER3"
}

// InstructionDisplayName returns the disassembly label for instr, prefixed by the
// dispatch tier ("0:", "1:", "2:", "3:").
//
// Takes instr (Instruction) which is the encoded instruction word.
//
// Returns the display label for instr.
func InstructionDisplayName(instr Instruction) string {
	if instr.Op != OpDrillTier1 {
		return "0:" + instr.Op.String()
	}
	tier1 := SubOpcode(instr.A)
	if tier1 != SubOpDrillTier2 {
		return "1:" + tier1.String()
	}
	tier2 := SubOpcodeTier2(instr.B)
	if tier2 != SubOpTier2DrillTier3 {
		return "2:" + tier2.String()
	}
	tier3 := SubOpcodeTier3(instr.C)
	return "3:" + tier3.String()
}
