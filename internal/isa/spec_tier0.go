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

// tierMainSpecs holds the tier-0 rows (three-operand operations).
var tierMainSpecs = []OpSpec{
	main0(OpDrillTier1).noGoHandler().cost(costMedium).flags(specMeta),
	main0(OpExtension).cost(costFree).pure(),
	main0(OpLoadInt64Constant).cost(costFree).asm().pure(),
	main0(OpLoadFloat64Constant).cost(costFree).asm().pure(),
	main0(OpAddInt64).asm().pure(),
	main0(OpSubtractInt64).asm().pure(),
	main0(OpMultiplyInt64).asm().pure(),
	main0(OpDivideInt64).asm().pure(),
	main0(OpRemainderInt64).asm().pure(),
	main0(OpBitAndInt64).asm().pure(),
	main0(OpBitOrInt64).asm().pure(),
	main0(OpBitXorInt64).asm().pure(),
	main0(OpBitAndNotInt64).asm().pure(),
	main0(OpShiftLeftInt64).asm().pure(),
	main0(OpShiftRightInt64).asm().pure(),
	main0(OpAddFloat64).asm().pure(),
	main0(OpSubtractFloat64).asm().pure(),
	main0(OpMultiplyFloat64).asm().pure(),
	main0(OpDivideFloat64).asm().pure(),
	main0(OpEqualInt64).asm().pure(),
	main0(OpNotEqualInt64).asm().pure(),
	main0(OpLessThanInt64).asm().pure(),
	main0(OpLessOrEqualInt64).asm().pure(),
	main0(OpGreaterThanInt64).asm().pure(),
	main0(OpGreaterOrEqualInt64).asm().pure(),
	main0(OpEqualFloat64).asm().pure(),
	main0(OpNotEqualFloat64).asm().pure(),
	main0(OpLessThanFloat64).asm().pure(),
	main0(OpLessOrEqualFloat64).asm().pure(),
	main0(OpGreaterThanFloat64).asm().pure(),
	main0(OpGreaterOrEqualFloat64).asm().pure(),
	main0(OpJumpIfTrue).asm().flags(SpecJump).pure(),
	main0(OpJumpIfFalse).asm().flags(SpecJump).pure(),
	main0(OpSubtractInt64Constant).asm().pure(),
	main0(OpAddInt64Constant).asm().pure(),
	main0(OpMultiplyInt64Constant).asm().pure(),
	main0(OpAddInt64ConstantThenJump).asm().flags(SpecJump).pure(),
	main0(OpMultiplyInt64ConstantThenAdd).asm().pure(),
	main0(OpGetStringByte).asm().pure(),
	main0(OpEqualString).cost(costMedium).asm().pure(),
	main0(OpNotEqualString).cost(costMedium).asm().pure(),
	main0(OpSliceString).cost(costMedium).asm().pure(),
	main0(OpGetStringByteAsInt64).cost(costMedium).asm().pure(),
	main0(OpMoveGeneral).cost(costFree).asm().shim().flags(SpecShimSuppressed).pure(),
	main0(OpLoadStringConstant).cost(costFree).asm().pure(),
	main0(OpLoadGeneralConstant).cost(costFree).shim().pure(),
	main0(OpLoadUint64Constant).cost(costFree).asm().pure(),
	main0(OpLoadComplex128Constant).cost(costFree).shim().pure(),
	main0(OpAddUint64).asm().pure(),
	main0(OpSubtractUint64).asm().pure(),
	main0(OpMultiplyUint64).asm().pure(),
	main0(OpDivideUint64).asm().pure(),
	main0(OpRemainderUint64).asm().pure(),
	main0(OpBitAndUint64).asm().pure(),
	main0(OpBitOrUint64).asm().pure(),
	main0(OpBitXorUint64).asm().pure(),
	main0(OpBitAndNotUint64).asm().pure(),
	main0(OpShiftLeftUint64).asm().pure(),
	main0(OpShiftRightUint64).asm().pure(),
	main0(OpEqualUint64).asm().pure(),
	main0(OpNotEqualUint64).asm().pure(),
	main0(OpLessThanUint64).asm().pure(),
	main0(OpLessOrEqualUint64).asm().pure(),
	main0(OpGreaterThanUint64).asm().pure(),
	main0(OpGreaterOrEqualUint64).asm().pure(),
	main0(OpAddComplex128).shim().pure(),
	main0(OpSubtractComplex128).shim().pure(),
	main0(OpMultiplyComplex128).shim().pure(),
	main0(OpDivideComplex128).shim().pure(),
	main0(OpEqualComplex128).shim().pure(),
	main0(OpNotEqualComplex128).shim().pure(),
	main0(OpMakeComplex128).shim().pure(),
	main0(OpConcatenateString).cost(costMedium).shim().pure(),
	main0(OpConcatenateStringRune).cost(costMedium).shim().pure(),
	main0(OpLessThanString).cost(costMedium).pure(),
	main0(OpLessOrEqualString).cost(costMedium).pure(),
	main0(OpGreaterThanString).cost(costMedium).pure(),
	main0(OpGreaterOrEqualString).cost(costMedium).pure(),
	main0(OpEqualGeneral).cost(costMedium).asm().shim().flags(SpecShimSuppressed).pure(),
	main0(OpNotEqualGeneral).cost(costMedium).shim().pure(),
	main0(OpLessThanGeneral).cost(costMedium).shim().pure(),
	main0(OpLessOrEqualGeneral).cost(costMedium).shim().pure(),
	main0(OpGreaterThanGeneral).cost(costMedium).shim().pure(),
	main0(OpGreaterOrEqualGeneral).cost(costMedium).shim().pure(),
	main0(OpAddGeneral).cost(costMedium).pure(),
	main0(OpSubtractGeneral).cost(costMedium).pure(),
	main0(OpMultiplyGeneral).cost(costMedium).pure(),
	main0(OpDivideGeneral).cost(costMedium).pure(),
	main0(OpRemainderGeneral).cost(costMedium).pure(),
	main0(OpTruncateToWidth).asm().pure(),
	main0(OpBoxAsInterface).cost(costModerate).shim().pure(),
	main0(OpUnboxFromInterface).cost(costModerate).shim().pure(),
	main0(OpJumpIfNil).cost(costModerate).asm().shim().
		flags(SpecJump | SpecShimSuppressed).pure(),
	main0(OpJumpIfNotNil).cost(costModerate).asm().shim().
		flags(SpecJump | SpecShimSuppressed).pure(),
	main0(OpMakeClosure).cost(costModerate).shim().mutates().
		copiesThenRevokes(routeClosureCapture, HandOffHeapPassthrough, HandOffIdentityKept),
	main0(OpGetCapturedVariable).cost(costModerate).asm().shim().flags(SpecShimSuppressed).pure(),
	main0(OpSetCapturedVariable).cost(costModerate).shim().mutates().
		copiesThenRevokes(routeSharedStore, HandOffSharedStore).copiesThenRevokes(routeClosureCapture, HandOffHeapPassthrough, HandOffIdentityKept),
	main0(OpDefer).cost(costModerate).pure().
		calls().copiesThenRevokes(routeClosureCapture, HandOffHeapPassthrough, HandOffIdentityKept),
	main0(OpGo).cost(costExpensive).pure().
		calls().copiesThenRevokes(routeSpawn, HandOffSpawn, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	main0(OpMakeSlice).cost(costVeryHeavy).shim().pure(),
	main0(OpGetElement).cost(costModerate).shim().pure(),
	main0(OpSetElement).cost(costModerate).shim().mutates().
		copiesThenRevokes(RouteContainerStore, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	main0(OpGetMap).cost(costModerate).exit().pure(),
	main0(OpSetMap).cost(costModerate).shim().mutates().
		copiesThenRevokes(RouteContainerStore, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	main0(OpGetMapOk).cost(costModerate).shim().pure(),
	main0(OpAppend).cost(costExpensive).exit().mutates().
		grants().revokes(routeAppendHostBacking, HandOffUntracedContainer),
	main0(OpAppendSpread).cost(costExpensive).shim().mutates().
		revokes(routeAppendHostBacking, HandOffUntracedContainer),
	main0(OpAppendByte).cost(costExpensive).exit().mutates().
		noHandOff(),
	main0(OpCopyGeneral).cost(costModerate).shim().mutates().
		revokes(routeCopyBuiltin, HandOffUntracedContainer),
	main0(OpGetElementInt64).cost(costModerate).asm().shim().flags(SpecShimSuppressed).pure(),
	main0(OpSetElementInt64).cost(costModerate).asm().shim().flags(SpecShimSuppressed).mutates().
		noHandOff(),
	main0(OpGetElementFloat64).cost(costModerate).shim().pure(),
	main0(OpSetElementFloat64).cost(costModerate).shim().mutates().
		noHandOff(),
	main0(OpGetElementString).cost(costModerate).shim().pure(),
	main0(OpSetElementString).cost(costModerate).shim().mutates().
		copiesThenRevokes(RouteContainerStore, HandOffIdentityKept),
	main0(OpGetElementBool).cost(costModerate).shim().pure(),
	main0(OpSetElementBool).cost(costModerate).shim().mutates().
		noHandOff(),
	main0(OpGetElementUint64).cost(costModerate).asm().
		shim().flags(SpecShimSuppressed).pure(),
	main0(OpSetElementUint64).cost(costModerate).asm().shim().flags(SpecShimSuppressed).mutates().
		noHandOff(),
	main0(OpLoadCompositeZeroInPlace).cost(costFree).mutates().
		noHandOff(),
	main0(OpRangeNextSliceByte).asm().flags(SpecJump).pure(),
	main0(OpGetMapInt64Int64).cost(costModerate).shim().pure(),
	main0(OpSetMapInt64Int64).cost(costModerate).shim().mutates().
		noHandOff(),
	main0(OpGetMapStringInt64).cost(costModerate).shim().pure(),
	main0(OpSetMapStringInt64).cost(costModerate).shim().mutates().
		copiesThenRevokes(RouteContainerStore, HandOffIdentityKept),
	main0(OpGetMapStringString).cost(costModerate).shim().pure(),
	main0(OpSetMapStringString).cost(costModerate).shim().mutates().
		copiesThenRevokes(RouteContainerStore, HandOffUntracedContainer, HandOffIdentityKept),
	main0(OpGetMapInt64String).cost(costModerate).shim().pure(),
	main0(OpSetMapInt64String).cost(costModerate).shim().mutates().
		copiesThenRevokes(RouteContainerStore, HandOffUntracedContainer, HandOffIdentityKept),
	main0(OpGetMapInt64Int64Ok).cost(costModerate).shim().pure(),
	main0(OpGetMapStringInt64Ok).cost(costModerate).shim().pure(),
	main0(OpGetMapStringStringOk).cost(costModerate).shim().pure(),
	main0(OpGetMapInt64StringOk).cost(costModerate).shim().pure(),
	main0(OpGetMapInt64General).cost(costModerate).shim().pure(),
	main0(OpGetMapInt64GeneralOk).cost(costModerate).shim().pure(),
	main0(OpGetMapStringGeneral).shim().pure(),
	main0(OpGetMapStringGeneralOk).shim().pure(),
	main0(OpSetMapStringGeneral).shim().mutates().
		grants().copiesThenRevokes(RouteContainerStore, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	main0(OpAddToMapInt64Int64).shim().mutates().
		noHandOff(),
	main0(OpAddToMapStringInt64).shim().mutates().
		copiesThenRevokes(RouteContainerStore, HandOffIdentityKept),
	main0(OpAppendStringToMapString).cost(costExpensive).shim().mutates().
		grants().copiesThenRevokes(RouteContainerStore, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept).
		copiesThenRevokes(routeAppendHostBacking, HandOffIdentityKept),
	main0(OpGetField).cost(costMedium).exit().pure(),
	main0(OpSetField).cost(costMedium).exit().mutates().
		grants().copiesThenRevokes(RouteContainerStore, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	main0(OpSetFieldInt64).cost(costMedium).mutates().noHandOff(),
	main0(OpGetFieldInt64).cost(costMedium).pure(),
	main0(OpBindMethod).cost(costModerate).shim().pure(),
	main0(OpSendToChannel).cost(costModerate).mutates().
		publishes().copiesThenRevokes(routeChannelSend, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	main0(OpAddress).cost(costMedium).shim().mutates().
		grants().copies(RouteContainerStore),
	main0(OpDereference).cost(costMedium).shim().pure(),
	main0(OpAllocateIndirect).cost(costModerate).shim().pure().
		grants().copiesThenRevokes(RouteContainerStore, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	main0(OpTypeAssert).cost(costModerate).shim().pure(),
	main0(OpConvertGeneral).cost(costMedium).shim().pure(),
	main0(OpGetGlobal).cost(costModerate).asm().shim().flags(SpecShimSuppressed).pure(),
	main0(OpSetGlobal).cost(costModerate).asm().shim().flags(SpecShimSuppressed).mutates().
		copiesThenRevokes(routeGlobalStore, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	main0(OpUnsafeString).cost(costModerate).shim().mutates().
		noHandOff(),
	main0(OpUnsafeSlice).cost(costModerate).shim().mutates().
		noHandOff(),
	main0(OpUnsafeAdd).cost(costModerate).shim().pure(),
	main0(OpStringsContainsRune).cost(costModerate).shim().pure(),
	main0(OpStringsContains).cost(costModerate).shim().pure(),
	main0(OpStringsHasPrefix).cost(costModerate).shim().pure(),
	main0(OpStringsHasSuffix).cost(costModerate).shim().pure(),
	main0(OpStringsEqualFold).cost(costModerate).shim().pure(),
	main0(OpStringsIndex).cost(costModerate).shim().pure(),
	main0(OpStringsCount).cost(costModerate).shim().pure(),
	main0(OpStringsTrimPrefix).cost(costModerate).shim().pure(),
	main0(OpStringsTrimSuffix).cost(costModerate).shim().pure(),
	main0(OpStringsTrim).cost(costModerate).shim().pure(),
	main0(OpStringsIndexRune).cost(costModerate).shim().pure(),
	main0(OpStringsRepeat).cost(costModerate).shim().pure(),
	main0(OpStringsLastIndex).cost(costModerate).shim().pure(),
	main0(OpStringsJoin).cost(costModerate).shim().pure(),
	main0(OpStringsSplit).cost(costModerate).shim().pure(),
	main0(OpStringsReplaceAll).cost(costModerate).shim().pure(),
	main0(OpMathPow).cost(costMedium).shim().pure(),
	main0(OpGetSliceInt64Element).asm().pure(),
	main0(OpSetSliceInt64Element).asm().mutates().
		noHandOff(),
	main0(OpRangeNextSliceInt64).flags(SpecJump).pure(),
	main0(OpGetStructFieldInt64).asm().shim().
		flags(SpecShimNarrow | SpecShimSuppressed).pure(),
	main0(OpSetStructFieldInt64).asm().shim().
		flags(SpecShimNarrow | SpecShimSuppressed).mutates().noHandOff(),
	main0(OpGetStructFieldUint64).asm().
		shim().
		flags(SpecShimNarrow | SpecShimSuppressed).pure(),
	main0(OpSetStructFieldUint64).asm().
		shim().
		flags(SpecShimNarrow | SpecShimSuppressed).mutates().noHandOff(),
	main0(OpGetStructFieldFloat64).asm().
		shim().flags(SpecShimNarrow | SpecShimSuppressed).pure(),
	main0(OpSetStructFieldFloat64).asm().
		shim().flags(SpecShimNarrow | SpecShimSuppressed).mutates().
		noHandOff(),
	main0(OpGetStructFieldBool).asm().
		shim().
		flags(SpecShimNarrow | SpecShimSuppressed).pure(),
	main0(OpSetStructFieldBool).asm().
		shim().
		flags(SpecShimNarrow | SpecShimSuppressed).mutates().noHandOff(),
	main0(OpGetStructFieldGeneral).asm().
		shim().flags(SpecShimNarrow | SpecShimSuppressed).pure(),
	main0(OpSetStructFieldGeneral).asm().
		shim().flags(SpecShimNarrow|SpecShimSuppressed).mutates().
		grants().copiesThenRevokes(RouteContainerStore, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	main0(OpCopyStructFieldGeneral).shim().
		flags(SpecShimNarrow).mutates().copiesThenRevokes(RouteContainerStore, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	main0(OpGetElementFieldInt64).asm().
		shim().flags(SpecShimSuppressed).pure(),
	main0(OpGetElementFieldUint64).pure(),
	main0(OpGetElementFieldFloat64).asm().
		shim().flags(SpecShimSuppressed).pure(),
	main0(OpGetElementFieldBool).pure(),
	main0(OpGetElementFieldString).pure(),
	main0(OpGetMapInt64Int64OkThenJumpIfFalse).cost(costModerate).
		shim().flags(SpecJump).pure(),
	main0(OpGetMapStringInt64OkThenJumpIfFalse).cost(costModerate).
		shim().flags(SpecJump).pure(),
	main0(OpGetMapStringStringOkThenJumpIfFalse).cost(costModerate).
		shim().flags(SpecJump).pure(),
	main0(OpGetMapInt64StringOkThenJumpIfFalse).cost(costModerate).
		shim().flags(SpecJump).pure(),
	main0(OpGetMapInt64GeneralOkThenJumpIfFalse).cost(costModerate).
		shim().flags(SpecJump).pure(),
	main0(OpGetMapStringGeneralOkThenJumpIfFalse).cost(costModerate).
		shim().flags(SpecJump).pure(),
	main0(OpSwapStructFieldsGeneral).shim().
		flags(SpecShimNarrow).mutates().noHandOff(),
	main0(OpGetStructFieldRawPointer).asm().
		shim().flags(SpecShimNarrow | SpecShimSuppressed).pure(),
	main0(OpGetSliceInt64ElementUnchecked).asm().pure(),
	main0(OpSetSliceInt64ElementUnchecked).asm().mutates().
		noHandOff(),
	main0(OpGetElementInt64Unchecked).pure(),
	main0(OpSetElementInt64Unchecked).mutates().noHandOff(),
	main0(OpBoxAsType).cost(costModerate).pure(),
	main0(OpAppendByteInPlace).cost(costExpensive).mutates().
		grants(),
	main0(OpAppendInPlace).cost(costExpensive).mutates().
		grants().revokes(routeAppendHostBacking, HandOffUntracedContainer).
		revokes(RouteFieldAppend, HandOffUntracedContainer),
	main0(OpAppendSpreadInPlace).cost(costExpensive).mutates().
		revokes(routeAppendHostBacking, HandOffUntracedContainer),
	main0(OpSetMapInt64General).shim().mutates().
		grants().copiesThenRevokes(RouteContainerStore, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	main0(OpGetStructFieldElementGeneral).
		shim().pure(),
	main0(OpSetStructFieldElementGeneral).
		shim().mutates().
		copiesThenRevokes(RouteContainerStore, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	main0(OpGetElementInt64ViaPointer).asm().shim().
		flags(SpecShimSuppressed).pure(),
	main0(OpSetElementInt64ViaPointer).asm().shim().
		flags(SpecShimSuppressed).mutates().noHandOff(),
	main0(OpLengthStructField).asm().
		shim().flags(SpecShimNarrow | SpecShimSuppressed).pure(),
	main0(OpGetStructFieldElementField).asm().
		shim().flags(SpecShimSuppressed).pure(),
	main0(OpAppendStruct).cost(costExpensive).exit().mutates().
		grants().revokes(routeAppendHostBacking, HandOffUntracedContainer),
	main0(OpAppendInt64).cost(costExpensive).exit().mutates().
		noHandOff(),
	main0(OpAppendFloat64).cost(costExpensive).exit().mutates().
		noHandOff(),
	main0(OpAppendString).cost(costExpensive).exit().mutates().
		copiesThenRevokes(routeAppendHostBacking, HandOffIdentityKept),
	main0(OpTypeSwitchCase).noGoHandler().flags(SpecJump).pure(),
	main0(OpAddInt32).asm().pure(),
	main0(OpSubtractInt32).asm().pure(),
	main0(OpMultiplyInt32).asm().pure(),
	main0(OpAddInt32Constant).asm().pure(),
	main0(OpSubtractInt32Constant).asm().pure(),
	main0(OpAddUint32).asm().pure(),
	main0(OpSubtractUint32).asm().pure(),
	main0(OpMultiplyUint32).asm().pure(),
	main0(OpSetElementFieldInt64).cost(costModerate).asm().shim().flags(SpecShimSuppressed).mutates().noHandOff(),
	main0(OpSetElementFieldFloat64).cost(costModerate).asm().shim().flags(SpecShimSuppressed).mutates().noHandOff(),
}
