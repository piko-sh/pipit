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

const (
	// OpNop is the alias for the tier-3 no-op encoding {0, 0, 0, 0}.
	OpNop = OpDrillTier1

	// OpcodeCount is the number of opcodes. It is declared as int so array-sizing compiles
	// even when the active opcode set fills the uint8 range.
	OpcodeCount = int(OpTypeSwitchCase) + 1
)

const (
	// OpDrillTier1 is the main-tier drill marker. When the op byte is 0, the dispatcher
	// descends into the tier-1 sub-opcode space using operand A as the discriminator.
	OpDrillTier1 Opcode = iota

	// OpExt is an extension word carrying a 24-bit payload formed from A|(B<<8)|(C<<16).
	// Used for wide jump offsets, large constant indices, and multi-word encodings.
	OpExt

	// OpLoadIntConst loads intConstants[B|(C<<8)] into ints[A].
	OpLoadIntConst

	// OpLoadFloatConst loads floatConstants[B|(C<<8)] into floats[A].
	OpLoadFloatConst

	// OpAddInt sets ints[A] = ints[B] + ints[C].
	OpAddInt

	// OpSubInt sets ints[A] = ints[B] - ints[C].
	OpSubInt

	// OpMulInt sets ints[A] = ints[B] * ints[C].
	OpMulInt

	// OpDivInt sets ints[A] = ints[B] / ints[C]. Panics on zero divisor.
	OpDivInt

	// OpRemInt sets ints[A] = ints[B] % ints[C]. Panics on zero divisor.
	OpRemInt

	// OpBitAnd sets ints[A] = ints[B] & ints[C].
	OpBitAnd

	// OpBitOr sets ints[A] = ints[B] | ints[C].
	OpBitOr

	// OpBitXor sets ints[A] = ints[B] ^ ints[C].
	OpBitXor

	// OpBitAndNot sets ints[A] = ints[B] &^ ints[C].
	OpBitAndNot

	// OpShiftLeft sets ints[A] = ints[B] << uint(ints[C]).
	OpShiftLeft

	// OpShiftRight sets ints[A] = ints[B] >> uint(ints[C]).
	OpShiftRight

	// OpAddFloat sets floats[A] = floats[B] + floats[C].
	OpAddFloat

	// OpSubFloat sets floats[A] = floats[B] - floats[C].
	OpSubFloat

	// OpMulFloat sets floats[A] = floats[B] * floats[C].
	OpMulFloat

	// OpDivFloat sets floats[A] = floats[B] / floats[C].
	OpDivFloat

	// OpEqInt sets ints[A] = (ints[B] == ints[C]) ? 1 : 0.
	OpEqInt

	// OpNeInt sets ints[A] = (ints[B] != ints[C]) ? 1 : 0.
	OpNeInt

	// OpLtInt sets ints[A] = (ints[B] < ints[C]) ? 1 : 0.
	OpLtInt

	// OpLeInt sets ints[A] = (ints[B] <= ints[C]) ? 1 : 0.
	OpLeInt

	// OpGtInt sets ints[A] = (ints[B] > ints[C]) ? 1 : 0.
	OpGtInt

	// OpGeInt sets ints[A] = (ints[B] >= ints[C]) ? 1 : 0.
	OpGeInt

	// OpEqFloat sets ints[A] = (floats[B] == floats[C]) ? 1 : 0.
	OpEqFloat

	// OpNeFloat sets ints[A] = (floats[B] != floats[C]) ? 1 : 0.
	OpNeFloat

	// OpLtFloat sets ints[A] = (floats[B] < floats[C]) ? 1 : 0.
	OpLtFloat

	// OpLeFloat sets ints[A] = (floats[B] <= floats[C]) ? 1 : 0.
	OpLeFloat

	// OpGtFloat sets ints[A] = (floats[B] > floats[C]) ? 1 : 0.
	OpGtFloat

	// OpGeFloat sets ints[A] = (floats[B] >= floats[C]) ? 1 : 0.
	OpGeFloat

	// OpJumpIfTrue jumps if ints[A] != 0. Offset in B|(C<<8).
	OpJumpIfTrue

	// OpJumpIfFalse jumps if ints[A] == 0. Offset in B|(C<<8).
	OpJumpIfFalse

	// OpSubIntConst sets ints[A] = ints[B] - intConstants[C]. Fuses OpLoadIntConst +
	// OpSubInt when the constant index fits in 8 bits.
	OpSubIntConst

	// OpAddIntConst sets ints[A] = ints[B] + intConstants[C]. Fuses OpLoadIntConst +
	// OpAddInt when the constant index fits in 8 bits.
	OpAddIntConst

	// OpMulIntConst sets ints[A] = ints[B] * intConstants[C]. Fuses OpLoadIntConst +
	// OpMulInt when the constant index fits in 8 bits.
	OpMulIntConst

	// OpAddIntJump sets ints[A] = ints[B] + intConstants[C] and unconditionally jumps by
	// offset in the following OpExt. Fuses OpAddIntConst + (OpDrillTier1, SubOpJump).
	OpAddIntJump

	// OpStringIndex sets uints[A] = uint64(strings[B][ints[C]]). Panics with
	// errIndexOutOfRange if index is out of bounds.
	OpStringIndex

	// OpEqString sets ints[A] = (strings[B] == strings[C]) ? 1 : 0.
	OpEqString

	// OpNeString sets ints[A] = (strings[B] != strings[C]) ? 1 : 0.
	OpNeString

	// OpSliceString sets strings[A] = strings[B][low:high]. C encodes flags (bit 0 = low
	// present, bit 1 = high present) and an OpExt follows with A=lowRegister,
	// B=highRegister.
	OpSliceString

	// OpStringIndexToInt sets ints[A] = int64(strings[B][ints[C]]). Fuses OpStringIndex +
	// isa.SubOpUintToInt to avoid the intermediate uint register and one tier-2 trampoline
	// in string loops.
	OpStringIndexToInt

	// OpMoveGeneral copies general[B] to general[A]. C selects the snapshot mode: 0 =
	// dynamic (runtime kind switch), 1 = alias (reference-safe type), 2 = snapshot
	// (struct/array), 3 = read-only range candidate resolved at compile time.
	OpMoveGeneral

	// OpLoadStringConst loads stringConstants[B|(C<<8)] into strings[A].
	OpLoadStringConst

	// OpLoadGeneralConst loads generalConstants[B|(C<<8)] into general[A].
	OpLoadGeneralConst

	// OpLoadUintConst loads UintConstants[B|(C<<8)] into uints[A].
	OpLoadUintConst

	// OpLoadComplexConst loads ComplexConstants[B|(C<<8)] into complex[A].
	OpLoadComplexConst

	// OpAddUint sets uints[A] = uints[B] + uints[C].
	OpAddUint

	// OpSubUint sets uints[A] = uints[B] - uints[C].
	OpSubUint

	// OpMulUint sets uints[A] = uints[B] * uints[C].
	OpMulUint

	// OpDivUint sets uints[A] = uints[B] / uints[C]. Panics on zero divisor.
	OpDivUint

	// OpRemUint sets uints[A] = uints[B] % uints[C]. Panics on zero divisor.
	OpRemUint

	// OpBitAndUint sets uints[A] = uints[B] & uints[C].
	OpBitAndUint

	// OpBitOrUint sets uints[A] = uints[B] | uints[C].
	OpBitOrUint

	// OpBitXorUint sets uints[A] = uints[B] ^ uints[C].
	OpBitXorUint

	// OpBitAndNotUint sets uints[A] = uints[B] &^ uints[C].
	OpBitAndNotUint

	// OpShiftLeftUint sets uints[A] = uints[B] << uints[C].
	OpShiftLeftUint

	// OpShiftRightUint sets uints[A] = uints[B] >> uints[C].
	OpShiftRightUint

	// OpEqUint sets ints[A] = (uints[B] == uints[C]) ? 1 : 0.
	OpEqUint

	// OpNeUint sets ints[A] = (uints[B] != uints[C]) ? 1 : 0.
	OpNeUint

	// OpLtUint sets ints[A] = (uints[B] < uints[C]) ? 1 : 0.
	OpLtUint

	// OpLeUint sets ints[A] = (uints[B] <= uints[C]) ? 1 : 0.
	OpLeUint

	// OpGtUint sets ints[A] = (uints[B] > uints[C]) ? 1 : 0.
	OpGtUint

	// OpGeUint sets ints[A] = (uints[B] >= uints[C]) ? 1 : 0.
	OpGeUint

	// OpAddComplex sets complex[A] = complex[B] + complex[C].
	OpAddComplex

	// OpSubComplex sets complex[A] = complex[B] - complex[C].
	OpSubComplex

	// OpMulComplex sets complex[A] = complex[B] * complex[C].
	OpMulComplex

	// OpDivComplex sets complex[A] = complex[B] / complex[C].
	OpDivComplex

	// OpEqComplex sets ints[A] = (complex[B] == complex[C]) ? 1 : 0.
	OpEqComplex

	// OpNeComplex sets ints[A] = (complex[B] != complex[C]) ? 1 : 0.
	OpNeComplex

	// OpBuildComplex sets complex[A] = complex(floats[B], floats[C]).
	OpBuildComplex

	// OpConcatString sets strings[A] = strings[B] + strings[C].
	OpConcatString

	// OpConcatRuneString sets strings[A] = strings[B] + string(rune(ints[C])). Fuses
	// isa.SubOpRuneToString + OpConcatString with in-place arena extension.
	OpConcatRuneString

	// OpLtString sets ints[A] = (strings[B] < strings[C]) ? 1 : 0.
	OpLtString

	// OpLeString sets ints[A] = (strings[B] <= strings[C]) ? 1 : 0.
	OpLeString

	// OpGtString sets ints[A] = (strings[B] > strings[C]) ? 1 : 0.
	OpGtString

	// OpGeString sets ints[A] = (strings[B] >= strings[C]) ? 1 : 0.
	OpGeString

	// OpEqGeneral sets ints[A] = (general[B] == general[C]) ? 1 : 0 via reflect.
	OpEqGeneral

	// OpNeGeneral sets ints[A] = (general[B] != general[C]) ? 1 : 0 via reflect, using the
	// same equality logic as OpEqGeneral.
	OpNeGeneral

	// OpLtGeneral sets ints[A] = (general[B] < general[C]) ? 1 : 0 via reflect.
	OpLtGeneral

	// OpLeGeneral sets ints[A] = (general[B] <= general[C]) ? 1 : 0 via reflect.
	OpLeGeneral

	// OpGtGeneral sets ints[A] = (general[B] > general[C]) ? 1 : 0 via reflect.
	OpGtGeneral

	// OpGeGeneral sets ints[A] = (general[B] >= general[C]) ? 1 : 0 via reflect.
	OpGeGeneral

	// OpAdd sets general[A] = general[B] + general[C] via reflect.
	OpAdd

	// OpSub sets general[A] = general[B] - general[C] via reflect.
	OpSub

	// OpMul sets general[A] = general[B] * general[C] via reflect.
	OpMul

	// OpDiv sets general[A] = general[B] / general[C] via reflect.
	OpDiv

	// OpRem sets general[A] = general[B] % general[C] via reflect.
	OpRem

	// OpTruncateNarrow truncates a narrow integer register in place. B is the bit width (8,
	// 16, or 32) and C selects uint (0, mask only) or int (1, mask then sign-extend).
	OpTruncateNarrow

	// OpPackInterface wraps a typed value into an interface.
	OpPackInterface

	// OpUnpackInterface extracts the concrete value from an interface.
	OpUnpackInterface

	// OpTestNilJumpTrue tests if general[A] is nil/invalid and jumps by signed offset
	// B|(C<<8) if true.
	OpTestNilJumpTrue

	// OpTestNilJumpFalse tests if general[A] is nil/invalid and jumps by signed offset
	// B|(C<<8) if false (i.e. not nil).
	OpTestNilJumpFalse

	// OpMakeClosure creates a closure in general[A] from function index B|(C<<8). Upvalue
	// descriptors are in the CompiledFunction.
	OpMakeClosure

	// OpGetUpvalue loads upvalue[B] into the register at A. C encodes the register kind.
	OpGetUpvalue

	// OpSetUpvalue stores the register at A into upvalue[B]. C encodes the register kind.
	OpSetUpvalue

	// OpDefer pushes a deferred call. general[A] is the function, B is the number of
	// arguments.
	OpDefer

	// OpGo spawns a goroutine calling general[A] with B arguments.
	OpGo

	// OpMakeSlice creates a slice: general[A] = make([]T, ints[B], ints[C]). The type T is
	// looked up from the function's type table via an extension instruction whose operand C
	// carries MakeSliceExtHeapFlag when the backing must live on the Go heap.
	OpMakeSlice

	// OpIndex reads element: target = general[A][ints[B]]. C encodes the destination
	// register kind and index via extension.
	OpIndex

	// OpIndexSet writes element: general[A][ints[B]] = source.
	OpIndexSet

	// OpMapIndex reads map element: target = general[A][key].
	OpMapIndex

	// OpMapSet writes map element: general[A][key] = value.
	OpMapSet

	// OpMapIndexOk reads map element with ok flag: general[A] = general[B][general[C]],
	// ints[extensionWord.a] = ok (0 or 1).
	OpMapIndexOk

	// OpAppend appends: general[A] = append(general[B], arguments...).
	OpAppend

	// OpAppendSpread sets general[A] = append(general[B], general[C]...) via
	// reflect.AppendSlice.
	OpAppendSpread

	// OpAppendByteFast is the byte-builder append fast path (A=dest general, B=source []byte
	// general, C=byte uint register), emitted only when the element type is statically byte.
	OpAppendByteFast

	// OpCopy copies: ints[A] = copy(general[B], general[C]).
	OpCopy

	// OpSliceGetInt reads an integer element from a slice/array without reflect boxing.
	// ints[A] = general[B].Index(ints[C]).Int() (or .Uint() for unsigned element types).
	OpSliceGetInt

	// OpSliceSetInt writes an integer value to a slice/array element without reflect boxing.
	// general[A].Index(ints[B]).SetInt(ints[C]) (or .SetUint() for unsigned element types).
	OpSliceSetInt

	// OpSliceGetFloat reads a float element from a slice/array without reflect boxing.
	// floats[A] = general[B].Index(ints[C]).Float().
	OpSliceGetFloat

	// OpSliceSetFloat writes a float value to a slice/array element without reflect boxing.
	// general[A].Index(ints[B]).SetFloat(floats[C]).
	OpSliceSetFloat

	// OpSliceGetString reads a string element from a slice/array. strings[A] =
	// general[B].Index(ints[C]).String().
	OpSliceGetString

	// OpSliceSetString writes a string value to a slice/array element.
	// general[A].Index(ints[B]).SetString(strings[C]).
	OpSliceSetString

	// OpSliceGetBool reads a bool element from a slice/array. bools[A] =
	// general[B].Index(ints[C]).Bool().
	OpSliceGetBool

	// OpSliceSetBool writes a bool value to a slice/array element.
	// general[A].Index(ints[B]).SetBool(bools[C]).
	OpSliceSetBool

	// OpSliceGetUint reads a uint element from a slice/array. uints[A] =
	// general[B].Index(ints[C]).Uint().
	OpSliceGetUint

	// OpSliceSetUint writes a uint value to a slice/array element.
	// general[A].Index(ints[B]).SetUint(uints[C]).
	OpSliceSetUint

	// OpLoadCompositeZeroReuse loads a composite-zero constant, reusing the destination
	// register's existing storage when it already holds a value of the same type.
	// PromoteCompositeZeroReuse emits this only after proving the site is frame-local and
	// unaliased.
	OpLoadCompositeZeroReuse

	// OpRangeNextSliceByte advances a typed range step over a slicesByte entry. A=index int,
	// B=source slicesByte, C=destination uint.
	OpRangeNextSliceByte

	// OpMapGetIntInt reads ints[A] = map[int]int in general[B] with key ints[C].
	OpMapGetIntInt

	// OpMapSetIntInt writes general[A][ints[B]] = ints[C] for map[int]int.
	OpMapSetIntInt

	// OpMapGetStringInt reads ints[A] = map[string]int in general[B] with key strings[C].
	// Bypasses reflect.MapIndex via the cached typed map handle on vm.typedHandleCache.
	OpMapGetStringInt

	// OpMapSetStringInt writes general[A][strings[B]] = ints[C] for map[string]int. Bypasses
	// reflect.SetMapIndex via the cached typed map handle.
	OpMapSetStringInt

	// OpMapGetStringString reads strings[A] = map[string]string in general[B] with key
	// strings[C].
	OpMapGetStringString

	// OpMapSetStringString writes general[A][strings[B]] = strings[C] for map[string]string.
	OpMapSetStringString

	// OpMapGetIntString reads strings[A] = map[int]string in general[B] with key ints[C].
	OpMapGetIntString

	// OpMapSetIntString writes general[A][ints[B]] = strings[C] for map[int]string.
	OpMapSetIntString

	// OpMapIndexOkIntInt reads ints[A] = map[int]int in general[B] with key ints[C], plus an
	// extension word whose A field is the int register holding the ok flag.
	OpMapIndexOkIntInt

	// OpMapIndexOkStringInt reads ints[A] = map[string]int in general[B] with key
	// strings[C], plus an extension word whose A field is the int register holding the ok
	// flag.
	OpMapIndexOkStringInt

	// OpMapIndexOkStringString reads strings[A] = map[string]string in general[B] with key
	// strings[C], plus an extension word whose A field is the int register holding the ok
	// flag.
	OpMapIndexOkStringString

	// OpMapIndexOkIntString reads strings[A] = map[int]string in general[B] with key
	// ints[C], plus an extension word whose A field is the int register holding the ok flag.
	OpMapIndexOkIntString

	// OpMapGetIntGeneral reads general[A] = map[int]V via general[B][ints[C]], where V is
	// any general-bank type.
	OpMapGetIntGeneral

	// OpMapIndexOkIntGeneral is the comma-ok form of OpMapGetIntGeneral, with the ok flag in
	// extension word A.
	OpMapIndexOkIntGeneral

	// OpMapGetStringGeneral reads general[A] = map[string]V in general[B] with key
	// strings[C], where V is any general-bank type.
	OpMapGetStringGeneral

	// OpMapIndexOkStringGeneral is the comma-ok form of OpMapGetStringGeneral, with the ok
	// flag in extension word A.
	OpMapIndexOkStringGeneral

	// OpMapSetStringGeneral writes general[A][strings[B]] = general[C] for map[string]V
	// where V is in the general bank.
	OpMapSetStringGeneral

	// OpMapAddIntInt fuses get-add-set for map[int]int, performing general[A][ints[B]] +=
	// ints[C] in one map probe. Absent keys are treated as 0.
	OpMapAddIntInt

	// OpMapAddStringInt fuses get-add-set for map[string]int, performing
	// general[A][strings[B]] += ints[C] in one map probe. Absent keys are treated as 0.
	OpMapAddStringInt

	// OpGetField reads general[B].Field(C) into the target register. A encodes the
	// destination and the register kind comes from an extension word.
	OpGetField

	// OpSetField writes source register to general[A].Field(B).
	OpSetField

	// OpSetFieldInt writes ints[C] to general[A].Field(B).
	OpSetFieldInt

	// OpGetFieldInt reads ints[A] = general[B].Field(C).Int().
	OpGetFieldInt

	// OpBindMethod creates a bound method value in general[A] with receiver general[B]. C is
	// the embedded field traversal count and extension words carry function and field
	// indices.
	OpBindMethod

	// OpChannelSend sends a value on general[A].
	OpChannelSend

	// OpAddr takes the address: general[A] = &register[B]. C encodes the source register
	// kind.
	OpAddr

	// OpDeref dereferences *general[B] into the target register. A encodes the destination
	// register index and C encodes its kind.
	OpDeref

	// OpAllocIndirect heap-escapes a variable via reflect.New of the type at the OpExt type
	// table index. general[A] = the pointer; B = source register index; C = source
	// RegisterKind.
	OpAllocIndirect

	// OpTypeAssert performs general[A] = general[B].(T). ints[C] receives the ok flag for
	// the comma-ok form; the type T index comes from an extension instruction.
	OpTypeAssert

	// OpConvert performs type conversion: general[A] = T(general[B]).
	OpConvert

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

	// OpStrContainsRune sets bools[A] = strings.ContainsRune(strings[B], rune(ints[C])).
	OpStrContainsRune

	// OpStrContains sets bools[A] = strings.Contains(strings[B], strings[C]).
	OpStrContains

	// OpStrHasPrefix sets bools[A] = strings.HasPrefix(strings[B], strings[C]).
	OpStrHasPrefix

	// OpStrHasSuffix sets bools[A] = strings.HasSuffix(strings[B], strings[C]).
	OpStrHasSuffix

	// OpStrEqualFold sets bools[A] = strings.EqualFold(strings[B], strings[C]).
	OpStrEqualFold

	// OpStrIndex sets ints[A] = strings.Index(strings[B], strings[C]).
	OpStrIndex

	// OpStrCount sets ints[A] = strings.Count(strings[B], strings[C]).
	OpStrCount

	// OpStrTrimPrefix sets strings[A] = strings.TrimPrefix(strings[B], strings[C]).
	OpStrTrimPrefix

	// OpStrTrimSuffix sets strings[A] = strings.TrimSuffix(strings[B], strings[C]).
	OpStrTrimSuffix

	// OpStrTrim sets strings[A] = strings.Trim(strings[B], strings[C]).
	OpStrTrim

	// OpStrIndexRune sets ints[A] = strings.IndexRune(strings[B], rune(ints[C])).
	OpStrIndexRune

	// OpStrRepeat sets strings[A] = strings.Repeat(strings[B], int(ints[C])).
	OpStrRepeat

	// OpStrLastIndex sets ints[A] = strings.LastIndex(strings[B], strings[C]).
	OpStrLastIndex

	// OpStrJoin sets strings[A] = strings.Join(general[B], strings[C]).
	OpStrJoin

	// OpStrSplit sets general[A] = strings.Split(strings[B], strings[C]).
	OpStrSplit

	// OpStrReplaceAll sets strings[A] = strings.ReplaceAll(strings[B], strings[C],
	// strings[extensionWord.a]). The replacement string register index is in the following
	// OpExt.
	OpStrReplaceAll

	// OpMathPow sets floats[A] = math.Pow(floats[B], floats[C]).
	OpMathPow

	// OpSliceGetIntDirect sets ints[A] = slicesInt[B][ints[C]] via direct Go indexing with
	// bounds checking.
	OpSliceGetIntDirect

	// OpSliceSetIntDirect sets slicesInt[A][ints[B]] = ints[C] via direct Go indexing.
	OpSliceSetIntDirect

	// OpRangeNextSliceInt advances a typed range over a slicesInt entry. A=index int,
	// B=source slicesInt, C=destination int.
	OpRangeNextSliceInt

	// OpGetStructFieldIntT0 is the tier-0 fast-path int struct-field read (A=dest int,
	// B=source general, C=structLayoutTable index), falling back to the tier-1 sub-op when
	// the index exceeds uint8.
	OpGetStructFieldIntT0

	// OpSetStructFieldIntT0 is the tier-0 fast-path int struct-field write. Operand
	// A=structReg general, B=value int register, C=structLayoutTable index (uint8).
	OpSetStructFieldIntT0

	// OpGetStructFieldUint is the tier-0 fast-path uint struct-field read. Same operand
	// layout as OpGetStructFieldIntT0 but writes into the uints bank.
	OpGetStructFieldUint

	// OpSetStructFieldUint is the tier-0 fast-path uint struct-field write. Operand
	// A=structReg general, B=value uint register, C=structLayoutTable index (uint8).
	OpSetStructFieldUint

	// OpGetStructFieldFloat is the tier-0 fast-path float struct-field read. Same operand
	// layout as OpGetStructFieldUint but writes into the floats bank.
	OpGetStructFieldFloat

	// OpSetStructFieldFloat is the tier-0 fast-path float struct-field write.
	OpSetStructFieldFloat

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

	// OpCopyStructFieldGeneralT0 is the fused general-bank field copy (A=srcRecv general,
	// B=dstRecv general, C=srcLayoutIdx) with the dstLayoutIdx in the following OpExt.
	OpCopyStructFieldGeneralT0

	// OpSliceIndexStructFieldInt is the tier-0 fused slice[i].field read for int leaves
	// (A=dest int, B=source slice general, C=index int).
	OpSliceIndexStructFieldInt

	// OpSliceIndexStructFieldUint mirrors OpSliceIndexStructFieldInt for uint-kind leaf
	// fields. Destination is uint register A.
	OpSliceIndexStructFieldUint

	// OpSliceIndexStructFieldFloat mirrors OpSliceIndexStructFieldInt for float-kind leaf
	// fields. Destination is float register A.
	OpSliceIndexStructFieldFloat

	// OpSliceIndexStructFieldBool mirrors OpSliceIndexStructFieldInt for bool-kind leaf
	// fields. Destination is bool register A.
	OpSliceIndexStructFieldBool

	// OpSliceIndexStructFieldString mirrors OpSliceIndexStructFieldInt for string-kind
	// leaves. Destination is string register A.
	OpSliceIndexStructFieldString

	// OpMapIndexOkJumpIfFalseIntInt fuses OpMapIndexOkIntInt + OpJumpIfFalse. The extension
	// word carries the ok register, jump offset low and high bytes.
	OpMapIndexOkJumpIfFalseIntInt

	// OpMapIndexOkJumpIfFalseStringInt mirrors OpMapIndexOkJumpIfFalseIntInt for
	// map[string]int with string keys.
	OpMapIndexOkJumpIfFalseStringInt

	// OpMapIndexOkJumpIfFalseStringString mirrors OpMapIndexOkJumpIfFalseIntInt for
	// map[string]string (string keys + string values).
	OpMapIndexOkJumpIfFalseStringString

	// OpMapIndexOkJumpIfFalseIntString mirrors OpMapIndexOkJumpIfFalseIntInt for
	// map[int]string (int keys + string values).
	OpMapIndexOkJumpIfFalseIntString

	// OpMapIndexOkJumpIfFalseIntGeneral mirrors OpMapIndexOkJumpIfFalseIntInt for map[int]V
	// where V is in the general bank.
	OpMapIndexOkJumpIfFalseIntGeneral

	// OpMapIndexOkJumpIfFalseStringGeneral mirrors OpMapIndexOkJumpIfFalseIntGeneral for
	// map[string]V with string keys.
	OpMapIndexOkJumpIfFalseStringGeneral

	// OpSwapStructFieldsGeneralT0 fuses `t.x, t.y = t.y, t.x` for two general-bank fields
	// into a single dispatch (A=struct general, B=layout index X, C=layout index Y),
	// requiring both fields to have the same kind.
	OpSwapStructFieldsGeneralT0

	// OpGetStructFieldRawPointerT0 specialises OpGetStructFieldGeneral for cycle-broken
	// interface{} fields that always hold a pointer.
	OpGetStructFieldRawPointerT0

	// OpSliceGetIntDirectUnchecked is the bounds-elided variant of OpSliceGetIntDirect.
	// Emitted only when the access is provably in range.
	OpSliceGetIntDirectUnchecked

	// OpSliceSetIntDirectUnchecked is the bounds-elided variant of OpSliceSetIntDirect.
	// Emitted only when the access is provably in range.
	OpSliceSetIntDirectUnchecked

	// OpSliceGetIntUnchecked is the bounds-elided variant of OpSliceGetInt. Emitted only
	// when the BCE pass proves the access is in range.
	OpSliceGetIntUnchecked

	// OpSliceSetIntUnchecked is the bounds-elided variant of OpSliceSetInt. Emitted only
	// when the access is provably in range.
	OpSliceSetIntUnchecked

	// OpPackTyped boxes a typed-bank value into the general bank (A=general dest, B=source
	// register, C=source RegisterKind) with a following OpExt carrying the type index.
	OpPackTyped

	// OpAppendByteFastInPlace is the byte-builder in-place append, covering both the `x =
	// append(x, b)` and `*p = append(*p, b)` forms. The alias analysis pass demotes it back
	// to OpAppendByteFast at any site it cannot prove alias-free.
	OpAppendByteFastInPlace

	// OpAppendInPlace is the generic in-place sibling of OpAppend, covering both direct and
	// pointer forms. Falls back to OpAppend when the source header is not arena-owned.
	OpAppendInPlace

	// OpAppendSpreadInPlace is the spread sibling of OpAppendInPlace, handling `x =
	// append(x, source...)` for arbitrary element types.
	OpAppendSpreadInPlace

	// OpMapSetIntGeneral writes general[A][ints[B]] = general[C] for map[int]V where V is in
	// the general bank.
	OpMapSetIntGeneral

	// OpGetStructFieldIndexGeneral fuses an array-typed struct-field read with an element
	// index. Sets general[A] = receiver[B].field(layout[C]).elem[ints[ext1.c]].
	OpGetStructFieldIndexGeneral

	// OpSetStructFieldIndexGeneral is the write-side twin of OpGetStructFieldIndexGeneral.
	// Sets receiver[A].field(layout[C]).elem[ints[ext1.c]] = general[B].
	OpSetStructFieldIndexGeneral

	// OpDerefSliceGetInt reads one element of an int slice reached through a pointer,
	// setting ints[A] = (*general[B])[ints[C]].
	OpDerefSliceGetInt

	// OpDerefSliceSetInt is the write-side twin: (*general[A])[ints[B]] = ints[C]. Element
	// stores into int slices carry no pointers, so no write barrier applies.
	OpDerefSliceSetInt

	// OpGetStructFieldSliceLen reads the length of a slice-kind struct field without
	// materialising a slice header. Sets ints[A] = len(receiver[B].field(layout[C])).
	OpGetStructFieldSliceLen

	// OpGetStructFieldSliceIndexScalar reads a scalar sub-field of one element from a
	// slice-typed struct field without materialising a slice header or boxing the element.
	OpGetStructFieldSliceIndexScalar

	// OpAppendStructFast appends a pointer-free struct or array element to a general-bank
	// slice. The extension word indexes the element type in the type table.
	OpAppendStructFast

	// OpAppendIntFast appends ints[C] to the general-bank slice in general[B], writing the
	// result to general[A].
	OpAppendIntFast

	// OpAppendFloatFast is the float-bank twin of OpAppendIntFast.
	OpAppendFloatFast

	// OpAppendStringFast is the string-bank twin of OpAppendIntFast. The element passes the
	// arena string barrier inside the handler.
	OpAppendStringFast

	// OpTypeSwitchCase is one row of a SubOpTypeSwitchJump table. A indexes the case type
	// and B|(C<<8) is the signed offset to the case body.
	OpTypeSwitchCase
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

	// SubOpRealComplex sets floats[B] = real(complex[C]).
	SubOpRealComplex

	// SubOpImagComplex sets floats[B] = imag(complex[C]).
	SubOpImagComplex

	// SubOpBytesToString sets strings[B] = string(general[C]) for a []byte source.
	SubOpBytesToString

	// SubOpMakeMethodExpr builds a method expression value in general[B] from general[C].
	SubOpMakeMethodExpr

	// SubOpCap sets ints[B] = cap(general[C]) for any capacity-bearing collection bank.
	SubOpCap

	// SubOpNegComplex sets complex[B] = -complex[C].
	SubOpNegComplex

	// SubOpMoveComplex copies complex[C] to complex[B].
	SubOpMoveComplex

	// SubOpMakeSliceInt creates a typed []int64 slice in the slicesInt bank. B=dest
	// slicesInt, C=length int, capacity in following OpExt.
	SubOpMakeSliceInt

	// SubOpLenSliceIntDirect sets ints[B] = int64(len(slicesInt[C])).
	SubOpLenSliceIntDirect

	// SubOpMakeSliceFloat creates a typed []float64 slice in the slicesFloat bank:
	// slicesFloat[B] = make([]float64, ints[C], ints[ext.A]).
	SubOpMakeSliceFloat

	// SubOpSliceGetFloatDirect reads a float element from a typed []float64 slice without
	// crossing the reflect boundary: floats[B] = slicesFloat[C][ints[ext.A]]. The index is
	// in the extension word so the three primary operands cover destination, source and the
	// umbrella sub-opcode.
	SubOpSliceGetFloatDirect

	// SubOpSliceSetFloatDirect writes a float value to a typed []float64 slice element:
	// slicesFloat[B][ints[C]] = floats[ext.A].
	SubOpSliceSetFloatDirect

	// SubOpLenSliceFloatDirect sets ints[B] = int64(len(slicesFloat[C])).
	SubOpLenSliceFloatDirect

	// SubOpMakeSliceString creates a typed []string slice in the slicesString bank.
	SubOpMakeSliceString

	// SubOpSliceGetStringDirect reads a string element from a typed []string slice:
	// strings[B] = slicesString[C][ints[ext.A]].
	SubOpSliceGetStringDirect

	// SubOpSliceSetStringDirect writes a string value to a typed []string slice element:
	// slicesString[B][ints[C]] = strings[ext.A].
	SubOpSliceSetStringDirect

	// SubOpLenSliceStringDirect sets ints[B] = int64(len(slicesString[C])).
	SubOpLenSliceStringDirect

	// SubOpMakeSliceBool creates a typed []bool slice in the slicesBool bank.
	SubOpMakeSliceBool

	// SubOpSliceGetBoolDirect reads a bool element from a typed []bool slice: bools[B] =
	// slicesBool[C][ints[ext.A]].
	SubOpSliceGetBoolDirect

	// SubOpSliceSetBoolDirect writes a bool value to a typed []bool slice element:
	// slicesBool[B][ints[C]] = bools[ext.A].
	SubOpSliceSetBoolDirect

	// SubOpLenSliceBoolDirect sets ints[B] = int64(len(slicesBool[C])).
	SubOpLenSliceBoolDirect

	// SubOpMakeSliceUint creates a typed []uint64 slice in the slicesUint bank.
	SubOpMakeSliceUint

	// SubOpSliceGetUintDirect reads a uint element from a typed []uint64 slice: uints[B] =
	// slicesUint[C][ints[ext.A]].
	SubOpSliceGetUintDirect

	// SubOpSliceSetUintDirect writes a uint value to a typed []uint64 slice element:
	// slicesUint[B][ints[C]] = uints[ext.A].
	SubOpSliceSetUintDirect

	// SubOpLenSliceUintDirect sets ints[B] = int64(len(slicesUint[C])).
	SubOpLenSliceUintDirect

	// SubOpBoxSliceInt converts a typed []int64 slice in the slicesInt bank to a
	// reflect.Value in the general bank: general[B] = reflect.ValueOf(slicesInt[C]). Used at
	// boundaries where a typed slice is consumed by a reflect-bank operation (native
	// function call, map value insert, interface conversion).
	SubOpBoxSliceInt

	// SubOpMoveInt copies ints[C] to ints[B].
	SubOpMoveInt

	// SubOpMoveFloat copies floats[C] to floats[B].
	SubOpMoveFloat

	// SubOpMoveString copies strings[C] to strings[B].
	SubOpMoveString

	// SubOpMoveBool copies bools[C] to bools[B].
	SubOpMoveBool

	// SubOpMoveUint copies uints[C] to uints[B].
	SubOpMoveUint

	// SubOpMoveIntToGeneral boxes ints[C] into general[B].
	SubOpMoveIntToGeneral

	// SubOpMoveGeneralToInt unboxes general[C] into ints[B].
	SubOpMoveGeneralToInt

	// SubOpMoveFloatToGeneral boxes floats[C] into general[B].
	SubOpMoveFloatToGeneral

	// SubOpMoveGeneralToFloat unboxes general[C] into floats[B].
	SubOpMoveGeneralToFloat

	// SubOpMoveStringToGeneral boxes strings[C] into general[B].
	SubOpMoveStringToGeneral

	// SubOpMoveGeneralToString unboxes general[C] into strings[B].
	SubOpMoveGeneralToString

	// SubOpNegInt sets ints[B] = -ints[C].
	SubOpNegInt

	// SubOpNegFloat sets floats[B] = -floats[C].
	SubOpNegFloat

	// SubOpBitNot sets ints[B] = ^ints[C].
	SubOpBitNot

	// SubOpBitNotUint sets uints[B] = ^uints[C].
	SubOpBitNotUint

	// SubOpIntToFloat converts ints[C] to floats[B].
	SubOpIntToFloat

	// SubOpFloatToInt converts floats[C] to ints[B].
	SubOpFloatToInt

	// SubOpNot inverts bools[C] into ints[B] (treated as bool by the caller; Go-side handler
	// reads/writes the int bank as bool).
	SubOpNot

	// SubOpBoolToInt converts bools[C] to ints[B].
	SubOpBoolToInt

	// SubOpIntToBool converts ints[C] to bools[B].
	SubOpIntToBool

	// SubOpIntToUint converts ints[C] to uints[B].
	SubOpIntToUint

	// SubOpUintToInt converts uints[C] to ints[B].
	SubOpUintToInt

	// SubOpUintToFloat converts uints[C] to floats[B].
	SubOpUintToFloat

	// SubOpFloatToUint converts floats[C] to uints[B].
	SubOpFloatToUint

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

	// SubOpLenString sets ints[B] = len(strings[C]).
	SubOpLenString

	// SubOpRuneToString sets strings[B] = string(rune(ints[C])).
	SubOpRuneToString

	// SubOpStrToUpper sets strings[B] = strings.ToUpper(strings[C]).
	SubOpStrToUpper

	// SubOpStrToLower sets strings[B] = strings.ToLower(strings[C]).
	SubOpStrToLower

	// SubOpStrTrimSpace sets strings[B] = strings.TrimSpace(strings[C]).
	SubOpStrTrimSpace

	// SubOpLen sets ints[B] = len(general[C]) for any collection bank.
	SubOpLen

	// SubOpStringToBytes sets general[B] = []byte(strings[C]).
	SubOpStringToBytes

	// SubOpUnsafeStringData sets general[B] to the *byte data pointer of strings[C].
	SubOpUnsafeStringData

	// SubOpUnsafeSliceData sets general[B] to the *element data pointer of the slice in
	// general[C].
	SubOpUnsafeSliceData

	// SubOpJump unconditionally jumps by the signed offset B|(C<<8).
	SubOpJump

	// SubOpLoadIntConstSmall sets ints[B] = int64(C), materialising a small literal inline.
	SubOpLoadIntConstSmall

	// SubOpLoadBool sets ints[B] = C (0 or 1), materialising a boolean literal inline.
	SubOpLoadBool

	// SubOpIncIntJumpLt fuses increment + compare + branch for the for-loop back-edge.
	// Increments ints[B] and jumps (offset in OpExt) when ints[B] < ints[C].
	SubOpIncIntJumpLt

	// SubOpLenStringLtJumpFalse fuses len + compare + branch for string-loop conditions.
	// Jumps when ints[B] >= len(strings[C]).
	SubOpLenStringLtJumpFalse

	// SubOpLoadZero zeroes the register at index B in the bank named by kind C.
	SubOpLoadZero

	// SubOpMakeChannel allocates a new channel of the type indexed by general[B] with
	// capacity ints[C].
	SubOpMakeChannel

	// SubOpMapDelete deletes general[C] from the map at general[B].
	SubOpMapDelete

	// SubOpChannelReceive receives a value from the channel at general[B] into the
	// destination encoded by ints[C].
	SubOpChannelReceive

	// SubOpGetMethod resolves the method named by the following OpExt word on general[B] and
	// stores the bound method in general[C].
	SubOpGetMethod

	// SubOpSpill writes the register at index B (in bank kind C) into a reserved spill slot
	// encoded in the following OpExt word.
	SubOpSpill

	// SubOpReload reads from the spill slot encoded in the following OpExt word into the
	// register at index B (in bank kind C).
	SubOpReload

	// SubOpGetStructFieldInt reads an integer-kind struct field via the structLayoutTable.
	// B=dest int, C=source general (struct), with the layout index in the following OpExt.
	SubOpGetStructFieldInt

	// SubOpGetStructFieldUint reads an unsigned-integer struct field via structLayoutTable
	// into uints[B]. Covers uint8 through uint64, uint and uintptr.
	SubOpGetStructFieldUint

	// SubOpGetStructFieldFloat reads a float-kind struct field via the structLayoutTable
	// into floats[B]. Covers float32 and float64.
	SubOpGetStructFieldFloat

	// SubOpGetStructFieldBool reads a bool-kind struct field via the structLayoutTable
	// mechanism. Stores into bools[B].
	SubOpGetStructFieldBool

	// SubOpGetStructFieldString reads a string-kind struct field via the structLayoutTable
	// into strings[B].
	SubOpGetStructFieldString

	// SubOpSetStructFieldInt writes an integer-kind struct field via the structLayoutTable.
	// B=dest general (struct), C=source int, with the layout index in the following OpExt.
	SubOpSetStructFieldInt

	// SubOpSetStructFieldUint writes an unsigned-integer-kind struct field via the
	// structLayoutTable mechanism.
	SubOpSetStructFieldUint

	// SubOpSetStructFieldFloat writes a float-kind struct field via the structLayoutTable
	// mechanism.
	SubOpSetStructFieldFloat

	// SubOpSetStructFieldBool writes a bool-kind struct field via the structLayoutTable
	// mechanism.
	SubOpSetStructFieldBool

	// SubOpSetStructFieldString writes a string-kind struct field via the structLayoutTable
	// mechanism.
	SubOpSetStructFieldString

	// SubOpAddUintConst sets uints[A] = uints[B] + uintConstants[C].
	SubOpAddUintConst

	// SubOpSubUintConst sets uints[A] = uints[B] - uintConstants[C].
	SubOpSubUintConst

	// SubOpBitAndUintConst sets uints[A] = uints[B] & uintConstants[C].
	SubOpBitAndUintConst

	// SubOpLoadUintConstSmall sets uints[B] = uint64(C), materialising a small unsigned
	// literal inline without a constant-pool lookup.
	SubOpLoadUintConstSmall

	// SubOpAppendUint appends uints[ext.A] to a uint-element slice in general[C], storing
	// the result in general[B].
	SubOpAppendUint

	// SubOpAppendInt appends ints[ext.A] to an int-element slice in general[C], storing the
	// result in general[B].
	SubOpAppendInt

	// SubOpAppendString is the tier-1 sibling for string elements: general[B] =
	// append(general[C], strings[ext.A]).
	SubOpAppendString

	// SubOpAppendFloat is the tier-1 sibling for float elements: general[B] =
	// append(general[C], float*(floats[ext.A])).
	SubOpAppendFloat

	// SubOpAppendBool is the tier-1 sibling for bool elements: general[B] =
	// append(general[C], bools[ext.A]).
	SubOpAppendBool

	// SubOpStarAppendByteFast fuses `*p = append(*p, b)` into a single op (B=pointer
	// general, C=byte uint).
	SubOpStarAppendByteFast

	// SubOpStarAppendByteSpread fuses `*p = append(*p, X...)` for []byte. B=pointer general,
	// C=source slice general.
	SubOpStarAppendByteSpread

	// SubOpIncStructFieldInt fuses `s.field++` for an int-kind struct field (B=receiver
	// general, C=structLayoutTable index).
	SubOpIncStructFieldInt

	// SubOpDecStructFieldInt is the decrement sibling of SubOpIncStructFieldInt. Same
	// encoding, same constraints, `s.field--` semantics.
	SubOpDecStructFieldInt

	// SubOpIncStructFieldUint mirrors SubOpIncStructFieldInt for uint-kind fields. Same
	// encoding.
	SubOpIncStructFieldUint

	// SubOpDecStructFieldUint is the decrement sibling of SubOpIncStructFieldUint.
	SubOpDecStructFieldUint

	// SubOpMakeSliceByte builds slicesByte[B] = make([]byte, ints[C], ints[ext.A]).
	SubOpMakeSliceByte

	// SubOpSliceGetByteDirect sets uints[B] = uint64(slicesByte[C][ints[ext.A]]).
	SubOpSliceGetByteDirect

	// SubOpSliceSetByteDirect sets slicesByte[B][ints[ext.A]] = byte(uints[ext.B]).
	SubOpSliceSetByteDirect

	// SubOpLenSliceByteDirect sets ints[B] = int64(len(slicesByte[C])).
	SubOpLenSliceByteDirect

	// SubOpSliceByteSlice performs a three-way slice on slicesByte[C] via ext words.
	SubOpSliceByteSlice

	// SubOpLtIntJumpFalse compares ints[B] < ints[C] and jumps by the offset in the
	// following OpExt if false. Fuses OpLtInt + OpJumpIfFalse when neither operand is a
	// constant.
	SubOpLtIntJumpFalse

	// SubOpLeIntJumpFalse compares ints[B] <= ints[C] and jumps by the offset in the
	// following OpExt if false. Fuses OpLeInt + OpJumpIfFalse.
	SubOpLeIntJumpFalse

	// SubOpGtIntJumpFalse compares ints[B] > ints[C] and jumps by the offset in the
	// following OpExt if false. Fuses OpGtInt + OpJumpIfFalse.
	SubOpGtIntJumpFalse

	// SubOpGeIntJumpFalse compares ints[B] >= ints[C] and jumps by the offset in the
	// following OpExt if false. Fuses OpGeInt + OpJumpIfFalse.
	SubOpGeIntJumpFalse

	// SubOpEqIntJumpFalse compares ints[B] == ints[C] and jumps by the offset in the
	// following OpExt if false. Fuses OpEqInt + OpJumpIfFalse.
	SubOpEqIntJumpFalse

	// SubOpNeIntJumpFalse compares ints[B] != ints[C] and jumps by the offset in the
	// following OpExt if false. Fuses OpNeInt + OpJumpIfFalse.
	SubOpNeIntJumpFalse

	// SubOpLeIntConstJumpFalse compares ints[B] <= intConstants[C] and jumps by the offset
	// in the following OpExt if false. Fuses OpLoadIntConst + OpLeInt + OpJumpIfFalse.
	SubOpLeIntConstJumpFalse

	// SubOpLtIntConstJumpFalse compares ints[B] < intConstants[C] and jumps by the offset in
	// the following OpExt if false. Fuses OpLoadIntConst + OpLtInt + OpJumpIfFalse.
	SubOpLtIntConstJumpFalse

	// SubOpEqIntConstJumpFalse compares ints[B] == intConstants[C] and jumps by the offset
	// in the following OpExt if false. Fuses OpLoadIntConst + OpEqInt + OpJumpIfFalse.
	SubOpEqIntConstJumpFalse

	// SubOpEqIntConstJumpTrue compares ints[B] == intConstants[C] and jumps by the offset in
	// the following OpExt if true. Fuses OpLoadIntConst + OpEqInt + OpJumpIfTrue.
	SubOpEqIntConstJumpTrue

	// SubOpGeIntConstJumpFalse compares ints[B] >= intConstants[C] and jumps by the offset
	// in the following OpExt if false. Fuses OpLoadIntConst + OpGeInt + OpJumpIfFalse.
	SubOpGeIntConstJumpFalse

	// SubOpGtIntConstJumpFalse compares ints[B] > intConstants[C] and jumps by the offset in
	// the following OpExt if false. Fuses OpLoadIntConst + OpGtInt + OpJumpIfFalse.
	SubOpGtIntConstJumpFalse

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

	// SubOpEqStringConstJumpFalse compares strings[B] == stringConstants[C] and jumps by
	// offset in the following OpExt if false (not equal). Fuses OpLoadStringConst +
	// OpEqString + OpJumpIfFalse.
	SubOpEqStringConstJumpFalse

	// SubOpSelect executes a select statement over B cases and writes the index of the
	// chosen case to ints[C]. Extension words encode the cases: a header {direction, channel
	// register, hasOk} per case, followed for a receive by {destination, kind, ok register}
	// and for a send by {value register, kind, 0}.
	SubOpSelect

	// SubOpLoadBoolConst loads BoolConstants[C] into bools[B].
	SubOpLoadBoolConst

	// SubOpResetSharedCell invalidates the shared upvalue cell for the register at index B
	// in bank C, forcing the next OpMakeClosure to create a fresh cell. Used for Go 1.22+
	// per-iteration loop variable scoping.
	SubOpResetSharedCell

	// SubOpWriteSharedCell copies the current register value (bank C, index B) into the
	// corresponding shared upvalue cell if one exists.
	SubOpWriteSharedCell

	// SubOpSliceOp slices: general[B] = general[C][low:high:max]. The bounds come from int
	// registers named by the following extension words: {flags, low register, high register}
	// and, when the flags carry SliceMaxBitFlag, {max register, 0, 0}.
	SubOpSliceOp

	// SubOpRangeInit initialises a range iterator over the collection in general[C] and
	// stores the iterator state in general[B].
	SubOpRangeInit

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

	// SubOpEqInterfaceNil sets ints[B] = (!general[C].IsValid()) ? 1 : 0.
	SubOpEqInterfaceNil

	// SubOpNeInterfaceNil sets ints[B] = (general[C].IsValid()) ? 1 : 0.
	SubOpNeInterfaceNil

	// SubOpBoxSliceByte sets general[B] = reflect.ValueOf(slicesByte[C]).
	SubOpBoxSliceByte

	// SubOpSliceByteToString converts the typed-bank []byte at slicesByte[C] to strings[B].
	SubOpSliceByteToString

	// SubOpEqUintConstJumpFalse fuses a uint equality test with a conditional jump (B=value
	// uint, C=8-bit immediate).
	SubOpEqUintConstJumpFalse

	// SubOpSimdDotProductFloat64 computes sum += a[i] * b[i] for two equal-length []float64
	// slices (B=dest float, C=slice A general, ext.A=slice B general).
	SubOpSimdDotProductFloat64

	// SubOpSimdSumSliceFloat64 computes sum += a[i] for a []float64. Primary word: B =
	// destination float register, C = slice A (general bank).
	SubOpSimdSumSliceFloat64

	// SubOpSimdAddSliceFloat64 computes destination[i] = a[i] + b[i] for three equal-length
	// []float64 slices. B=dest slice (general), C=slice A, ext.A=slice B.
	SubOpSimdAddSliceFloat64

	// SubOpSimdScaleSliceFloat64 computes s[i] *= k in place for a []float64 and a scalar k.
	// Primary word: B = slice (general bank), C = scalar k (float register).
	SubOpSimdScaleSliceFloat64

	// SubOpAdoptGeneralToSlicesFloat extracts a []float64 from general[C] into
	// slicesFloat[B].
	SubOpAdoptGeneralToSlicesFloat

	// SubOpAdoptGeneralToSlicesInt extracts a []int64 from general[C] into slicesInt[B].
	SubOpAdoptGeneralToSlicesInt

	// SubOpAdoptGeneralToSlicesString mirrors SubOpAdoptGeneralToSlicesFloat for the
	// slicesString bank ([]string).
	SubOpAdoptGeneralToSlicesString

	// SubOpAdoptGeneralToSlicesBool mirrors SubOpAdoptGeneralToSlicesFloat for the
	// slicesBool bank ([]bool).
	SubOpAdoptGeneralToSlicesBool

	// SubOpAdoptGeneralToSlicesUint mirrors SubOpAdoptGeneralToSlicesFloat for the
	// slicesUint bank ([]uint64).
	SubOpAdoptGeneralToSlicesUint

	// SubOpAdoptGeneralToSlicesByte mirrors SubOpAdoptGeneralToSlicesFloat for the
	// slicesByte bank ([]byte).
	SubOpAdoptGeneralToSlicesByte

	// SubOpBoxSliceFloat sets general[B] = reflect.ValueOf(slicesFloat[C]).
	SubOpBoxSliceFloat

	// SubOpBoxSliceString mirrors SubOpBoxSliceFloat for the slicesString bank.
	SubOpBoxSliceString

	// SubOpBoxSliceBool mirrors SubOpBoxSliceFloat for the slicesBool bank.
	SubOpBoxSliceBool

	// SubOpBoxSliceUint mirrors SubOpBoxSliceFloat for the slicesUint bank.
	SubOpBoxSliceUint

	// SubOpMoveSliceInt copies slicesInt[C] to slicesInt[B].
	SubOpMoveSliceInt

	// SubOpMoveSliceFloat copies slicesFloat[C] to slicesFloat[B].
	SubOpMoveSliceFloat

	// SubOpMoveSliceString copies slicesString[C] to slicesString[B].
	SubOpMoveSliceString

	// SubOpMoveSliceBool copies slicesBool[C] to slicesBool[B].
	SubOpMoveSliceBool

	// SubOpMoveSliceUint copies slicesUint[C] to slicesUint[B].
	SubOpMoveSliceUint

	// SubOpMoveSliceByte copies slicesByte[C] to slicesByte[B].
	SubOpMoveSliceByte

	// SubOpAppendSliceIntDirect appends ints[ext.A] to slicesInt[C], storing the result in
	// slicesInt[B].
	SubOpAppendSliceIntDirect

	// SubOpAppendSliceFloatDirect appends a float64 to a typed-bank []float64 slice. Same
	// encoding as SubOpAppendSliceIntDirect but reads slicesFloat / floats and writes
	// slicesFloat.
	SubOpAppendSliceFloatDirect

	// SubOpAppendSliceStringDirect appends a string to a typed-bank []string slice.
	SubOpAppendSliceStringDirect

	// SubOpAppendSliceBoolDirect appends a bool to a typed-bank []bool slice.
	SubOpAppendSliceBoolDirect

	// SubOpAppendSliceUintDirect appends a uint64 to a typed-bank []uint64 slice.
	SubOpAppendSliceUintDirect

	// SubOpAppendSliceByteDirect appends a byte to a typed-bank []byte slice. Reads element
	// from uints (pipit's storage for uint8 scalar values).
	SubOpAppendSliceByteDirect

	// SubOpSliceSliceIntDirect performs `source[low:high:cap]` on slicesInt[C] and writes
	// the result to slicesInt[B], with bounds in the following extension word.
	SubOpSliceSliceIntDirect

	// SubOpSliceSliceFloatDirect mirrors SubOpSliceSliceIntDirect for slicesFloat.
	SubOpSliceSliceFloatDirect

	// SubOpSliceSliceStringDirect mirrors SubOpSliceSliceIntDirect for slicesString.
	SubOpSliceSliceStringDirect

	// SubOpSliceSliceBoolDirect mirrors SubOpSliceSliceIntDirect for slicesBool.
	SubOpSliceSliceBoolDirect

	// SubOpSliceSliceUintDirect mirrors SubOpSliceSliceIntDirect for slicesUint.
	SubOpSliceSliceUintDirect

	// SubOpCopySliceIntDirect implements `copy(destination, source)` between two typed
	// slicesInt headers (A=count int, B=dest slicesInt, C=source slicesInt).
	SubOpCopySliceIntDirect

	// SubOpCopySliceFloatDirect mirrors SubOpCopySliceIntDirect for slicesFloat.
	SubOpCopySliceFloatDirect

	// SubOpCopySliceStringDirect mirrors SubOpCopySliceIntDirect for slicesString.
	SubOpCopySliceStringDirect

	// SubOpCopySliceBoolDirect mirrors SubOpCopySliceIntDirect for slicesBool.
	SubOpCopySliceBoolDirect

	// SubOpCopySliceUintDirect mirrors SubOpCopySliceIntDirect for slicesUint.
	SubOpCopySliceUintDirect

	// SubOpCopySliceByteDirect mirrors SubOpCopySliceIntDirect for slicesByte.
	SubOpCopySliceByteDirect

	// SubOpAppendUintInPlace is the in-place sibling of SubOpAppendUint (B=C=slice general,
	// ext.A=element uint).
	SubOpAppendUintInPlace

	// SubOpAppendByteSpreadInPlace fuses `x = append(x, src...)` in place for []byte
	// (B=slice general, C=source general).
	SubOpAppendByteSpreadInPlace

	// SubOpGetStructFieldSliceInt reads a []int64 struct field directly into slicesInt[B]
	// (C=receiver general, layout index in the following OpExt).
	SubOpGetStructFieldSliceInt

	// SubOpGetStructFieldSliceFloat - float64 element bank counterpart of
	// SubOpGetStructFieldSliceInt. Reads into slicesFloat[B].
	SubOpGetStructFieldSliceFloat

	// SubOpGetStructFieldSliceUint - uint64 element bank counterpart. Reads into
	// slicesUint[B].
	SubOpGetStructFieldSliceUint

	// SubOpGetStructFieldSliceString - string element bank counterpart. Reads into
	// slicesString[B].
	SubOpGetStructFieldSliceString

	// SubOpGetStructFieldSliceBool - bool element bank counterpart. Reads into
	// slicesBool[B].
	SubOpGetStructFieldSliceBool

	// SubOpGetStructFieldSliceByte - byte (uint8) element bank counterpart. Reads into
	// slicesByte[B].
	SubOpGetStructFieldSliceByte

	// SubOpSetStructFieldSliceInt writes slicesInt[C] into the receiver's []int64 field
	// (B=receiver general, layout index in the following OpExt).
	SubOpSetStructFieldSliceInt

	// SubOpSetStructFieldSliceFloat - float64 counterpart.
	SubOpSetStructFieldSliceFloat

	// SubOpSetStructFieldSliceUint - uint64 counterpart.
	SubOpSetStructFieldSliceUint

	// SubOpSetStructFieldSliceString - string counterpart.
	SubOpSetStructFieldSliceString

	// SubOpSetStructFieldSliceBool - bool counterpart.
	SubOpSetStructFieldSliceBool

	// SubOpSetStructFieldSliceByte - byte counterpart.
	SubOpSetStructFieldSliceByte

	// SubOpCapSliceIntDirect sets ints[B] = int64(cap(slicesInt[C])).
	SubOpCapSliceIntDirect

	// SubOpCapSliceFloatDirect - float64 counterpart of SubOpCapSliceIntDirect.
	SubOpCapSliceFloatDirect

	// SubOpCapSliceStringDirect - string counterpart.
	SubOpCapSliceStringDirect

	// SubOpCapSliceBoolDirect - bool counterpart.
	SubOpCapSliceBoolDirect

	// SubOpCapSliceUintDirect - uint64 counterpart.
	SubOpCapSliceUintDirect

	// SubOpCapSliceByteDirect - byte counterpart.
	SubOpCapSliceByteDirect

	// SubOpMakeSliceHeap builds a typed-bank slice on the Go heap for locals that escape.
	SubOpMakeSliceHeap

	// SubOpTypeSwitchJump dispatches a concrete-type switch through a jump table formed by
	// the OpTypeSwitchCase rows that follow. Sets general[B] = general[C] narrowed to the
	// matching case type, then jumps to that case's body.
	SubOpTypeSwitchJump

	// SubOpRoundFloat32 rounds floats[C] to float32 precision and stores it in floats[B].
	// float32 values live in the float64 bank; every operation that produces a float32
	// result re-rounds through this op so accumulated error matches Go.
	SubOpRoundFloat32

	// SubOpRoundComplex64 rounds both parts of complex[C] to float32 precision and stores
	// the result in complex[B].
	SubOpRoundComplex64

	// SubOpEqInterfaceStrict compares general[B] == general[C] using strict interface
	// equality and stores the result in ints[A] of the trailing OpExt word.
	SubOpEqInterfaceStrict

	// SubOpNeInterfaceStrict is the != mirror of SubOpEqInterfaceStrict.
	SubOpNeInterfaceStrict
)

const (
	// SubOpTier2DrillTier3 is the tier-2 sub-opcode reserved at index 0 to drill from tier 2
	// down into tier 3. Following the "index 0 means drill down" convention, tier 2 dispatch
	// sees operand B == 0 and falls through to tier 3 dispatch using operand C as the tier-3
	// sub-opcode discriminator.
	SubOpTier2DrillTier3 SubOpcodeTier2 = iota

	// SubOpTier2IncInt increments the int register named by operand C. Lives in tier 2
	// because the op uses only one register operand.
	SubOpTier2IncInt

	// SubOpTier2DecInt decrements the int register named by operand C.
	SubOpTier2DecInt

	// SubOpTier2IncUint increments the uint register named by operand C.
	SubOpTier2IncUint

	// SubOpTier2DecUint decrements the uint register named by operand C.
	SubOpTier2DecUint

	// SubOpTier2Panic raises a panic with the value in the general register named by operand
	// C.
	SubOpTier2Panic

	// SubOpTier2Recover stores the active panic value in the general register named by
	// operand C, or an invalid value if not panicking.
	SubOpTier2Recover

	// SubOpTier2SetZero zeroes the general register named by operand C (used by the
	// assign-through optimisation to clear composite values in place).
	SubOpTier2SetZero

	// SubOpTier2ChannelClose closes the channel held by the general register named by
	// operand C.
	SubOpTier2ChannelClose

	// SubOpTier2LoadNil writes a typed nil into the general register named by operand C.
	SubOpTier2LoadNil

	// SubOpTier2Return returns from the current function with operand C values. The only
	// operand (the return-value count) sits in C of the tier-2 encoding.
	SubOpTier2Return

	// SubOpTier2MakeInterfaceMethodExpr builds a method expression on an interface type into
	// general[C]. The function type and method name follow in two extension words.
	SubOpTier2MakeInterfaceMethodExpr

	// SubOpTier2MakeMap allocates a new map into general[C]. The following extension word
	// carries the type-table index in A|B<<8 and a log2 size hint in C.
	SubOpTier2MakeMap

	// SubOpTier2RangeCheckUintJumpFalse fuses a `value >= lo && value <= hi` range check
	// with a conditional jump. C=value uint register, with lo/hi constants and the jump
	// offset in two following extension words.
	SubOpTier2RangeCheckUintJumpFalse

	// SubOpTier2AllocStructLiteral allocates a zeroed pointer-free struct literal in the
	// register arena (general[C] = new(T), type index in the extension word).
	SubOpTier2AllocStructLiteral

	// SubOpTier2SyncClosureUpvalues copies shared upvalue cells from the closure in
	// general[C] back into this frame's registers.
	SubOpTier2SyncClosureUpvalues
)

const (
	// SubOpTier3Nop is the zero-operand no-op, encoded as the all-zero word {0, 0, 0, 0}.
	SubOpTier3Nop SubOpcodeTier3 = iota

	// SubOpTier3ReturnVoid returns from the current function with no values. Lives in tier 3
	// because the op consumes no register operands.
	SubOpTier3ReturnVoid

	// SubOpTier3SyncIIFEUpvalues copies upvalue cells of the just-returned IIFE back into
	// this frame's registers.
	SubOpTier3SyncIIFEUpvalues
)

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
