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

// tierSub1Specs holds the tier-1 rows (two-operand operations).
var tierSub1Specs = []OpSpec{
	sub1(SubOpDrillTier2).noGoHandler().flags(specMeta),
	sub1(SubOpMathSin).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMathCos).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMathExp).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMathTan).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMathMod).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpStrconvFormatBool).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpStrconvFormatInt).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpStrconvItoa).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpRealPartComplex128).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpImaginaryPartComplex128).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpConvertBytesToString).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMakeMethodExpression).cost(costModerate).pure(),
	sub1(SubOpCapacityGeneral).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpNegateComplex128).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMoveComplex128).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMakeSliceInt64).cost(costVeryHeavy).shim().pure(),
	sub1(SubOpLengthSliceInt64).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMakeSliceFloat64).cost(costVeryHeavy).shim().pure(),
	sub1(SubOpGetSliceFloat64Element).cost(costModerate).asmIn("tier1JumpTable").pure(),
	sub1(SubOpSetSliceFloat64Element).cost(costModerate).asmIn("tier1JumpTable").mutates().
		noHandOff(),
	sub1(SubOpLengthSliceFloat64).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMakeSliceString).cost(costVeryHeavy).shim().pure(),
	sub1(SubOpGetSliceStringElement).cost(costModerate).asmIn("tier1JumpTable").pure(),
	sub1(SubOpSetSliceStringElement).cost(costModerate).
		exit().mutates().
		copiesThenRevokes(RouteContainerStore, HandOffIdentityKept),
	sub1(SubOpLengthSliceString).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMakeSliceBool).cost(costVeryHeavy).shim().pure(),
	sub1(SubOpGetSliceBoolElement).cost(costModerate).asmIn("tier1JumpTable").pure(),
	sub1(SubOpSetSliceBoolElement).cost(costModerate).asmIn("tier1JumpTable").mutates().
		noHandOff(),
	sub1(SubOpLengthSliceBool).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMakeSliceUint64).cost(costVeryHeavy).shim().pure(),
	sub1(SubOpGetSliceUint64Element).cost(costModerate).asmIn("tier1JumpTable").pure(),
	sub1(SubOpSetSliceUint64Element).cost(costModerate).asmIn("tier1JumpTable").mutates().
		noHandOff(),
	sub1(SubOpLengthSliceUint64).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpBoxSliceInt64).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMoveInt64).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMoveFloat64).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMoveString).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMoveBool).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMoveUint64).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpBoxInt64).cost(costFree).shim().pure(),
	sub1(SubOpUnboxInt64).cost(costFree).shim().pure(),
	sub1(SubOpBoxFloat64).cost(costFree).shim().pure(),
	sub1(SubOpUnboxFloat64).cost(costFree).pure(),
	sub1(SubOpBoxString).cost(costFree).shim().pure(),
	sub1(SubOpUnboxString).cost(costFree).pure(),
	sub1(SubOpNegateInt64).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpNegateFloat64).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpBitNotInt64).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpBitNotUint64).cost(costCheap).pure(),
	sub1(SubOpConvertInt64ToFloat64).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpConvertFloat64ToInt64).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpNotInt64).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpConvertBoolToInt64).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpConvertInt64ToBool).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpConvertInt64ToUint64).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpConvertUint64ToInt64).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpConvertUint64ToFloat64).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpConvertFloat64ToUint64).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMathSqrt).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMathAbs).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMathFloor).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMathCeil).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMathTrunc).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMathRound).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpLengthString).cost(costMedium).asmIn("tier1JumpTable").pure(),
	sub1(SubOpConvertRuneToString).cost(costMedium).pure(),
	sub1(SubOpStringsToUpper).cost(costMedium).pure(),
	sub1(SubOpStringsToLower).cost(costMedium).pure(),
	sub1(SubOpStringsTrimSpace).cost(costMedium).pure(),
	sub1(SubOpLengthGeneral).cost(costCheap).asmIn("tier1JumpTable").shim().flags(SpecShimSuppressed).pure(),
	sub1(SubOpConvertStringToBytes).cost(costMedium).pure(),
	sub1(SubOpUnsafeStringData).cost(costFree).mutates().
		noHandOff(),
	sub1(SubOpUnsafeSliceData).cost(costFree).mutates().
		noHandOff(),
	sub1(SubOpJump).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpLoadInt64Immediate).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpLoadBoolImmediateAsInt64).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpIncrementInt64ThenJumpIfLessThan).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpLessThanLengthStringThenJumpIfFalse).cost(costMedium).
		asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpLoadZero).cost(costFree).asmIn("tier1JumpTable").shim().flags(SpecShimSuppressed).pure(),
	sub1(SubOpMakeChannel).cost(costVeryHeavy).pure(),
	sub1(SubOpDeleteFromMap).cost(costModerate).shim().mutates().noHandOff(),
	sub1(SubOpReceiveFromChannel).cost(costModerate).mutates().
		noHandOff(),
	sub1(SubOpBindMethodByName).cost(costModerate).pure(),
	sub1(SubOpSpill).cost(costFree).pure(),
	sub1(SubOpReload).cost(costFree).pure(),
	sub1(SubOpGetStructFieldInt64Wide).cost(costMedium).pure(),
	sub1(SubOpGetStructFieldUint64Wide).cost(costMedium).pure(),
	sub1(SubOpGetStructFieldFloat64Wide).cost(costMedium).pure(),
	sub1(SubOpGetStructFieldBoolWide).cost(costMedium).pure(),
	sub1(SubOpGetStructFieldStringWide).cost(costMedium).shim().pure(),
	sub1(SubOpSetStructFieldInt64Wide).cost(costMedium).mutates().
		noHandOff(),
	sub1(SubOpSetStructFieldUint64Wide).cost(costMedium).mutates().
		noHandOff(),
	sub1(SubOpSetStructFieldFloat64Wide).cost(costMedium).mutates().
		noHandOff(),
	sub1(SubOpSetStructFieldBoolWide).cost(costMedium).mutates().
		noHandOff(),
	sub1(SubOpSetStructFieldStringWide).cost(costMedium).shim().mutates().
		copiesThenRevokes(RouteContainerStore, HandOffIdentityKept),
	sub1(SubOpAddUint64Constant).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpSubtractUint64Constant).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpBitAndUint64Constant).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpLoadUint64Immediate).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpAppendUint64Wide).cost(costExpensive).shim().mutates().noHandOff(),
	sub1(SubOpAppendInt64Wide).cost(costExpensive).mutates().noHandOff(),
	sub1(SubOpAppendStringWide).cost(costExpensive).mutates().
		copiesThenRevokes(routeAppendHostBacking, HandOffIdentityKept),
	sub1(SubOpAppendFloat64Wide).cost(costExpensive).mutates().noHandOff(),
	sub1(SubOpAppendBoolWide).cost(costExpensive).mutates().noHandOff(),
	sub1(SubOpAppendByteViaPointer).cost(costExpensive).shim().mutates().
		noHandOff(),
	sub1(SubOpAppendSpreadByteViaPointer).cost(costExpensive).shim().mutates().
		noHandOff(),
	sub1(SubOpIncrementStructFieldInt64).cost(costMedium).asmIn("tier1JumpTable").mutates().
		noHandOff(),
	sub1(SubOpDecrementStructFieldInt64).cost(costMedium).asmIn("tier1JumpTable").mutates().
		noHandOff(),
	sub1(SubOpIncrementStructFieldUint64).cost(costMedium).asmIn("tier1JumpTable").mutates().
		noHandOff(),
	sub1(SubOpDecrementStructFieldUint64).cost(costMedium).asmIn("tier1JumpTable").mutates().
		noHandOff(),
	sub1(SubOpMakeSliceByte).cost(costVeryHeavy).shim().pure(),
	sub1(SubOpGetSliceByteElement).cost(costModerate).asmIn("tier1JumpTable").pure(),
	sub1(SubOpSetSliceByteElement).cost(costModerate).asmIn("tier1JumpTable").mutates().
		noHandOff(),
	sub1(SubOpLengthSliceByte).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpSliceSliceByte).cost(costModerate).asmIn("tier1JumpTable").pure(),
	sub1(SubOpLessThanInt64ThenJumpIfFalse).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpLessOrEqualInt64ThenJumpIfFalse).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpGreaterThanInt64ThenJumpIfFalse).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpGreaterOrEqualInt64ThenJumpIfFalse).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpEqualInt64ThenJumpIfFalse).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpNotEqualInt64ThenJumpIfFalse).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpLessOrEqualInt64ConstantThenJumpIfFalse).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpLessThanInt64ConstantThenJumpIfFalse).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpEqualInt64ConstantThenJumpIfFalse).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpEqualInt64ConstantThenJumpIfTrue).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpGreaterThanInt64ConstantThenJumpIfFalse).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpCall).cost(costExpensive).asmIn("tier1JumpTable").pure().
		calls().borrows(),
	sub1(SubOpTailCall).cost(costExpensive).asmIn("tier1JumpTable").pure().
		calls().noHandOff(),
	sub1(SubOpCallNative).cost(costExpensive).pure().
		calls().borrows().copiesThenRevokes(routeNativeArgument, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	sub1(SubOpCallBuiltin).cost(costExpensive).pure().calls().noHandOff(),
	sub1(SubOpCallMethod).cost(costExpensive).asmIn("tier1JumpTable").pure().
		calls().borrows(),
	sub1(SubOpCallIIFE).cost(costExpensive).pure().
		calls().copiesThenRevokes(routeClosureCapture, HandOffHeapPassthrough, HandOffIdentityKept),
	sub1(SubOpCallScalar).asmIn("tier1JumpTable").pure().
		calls().noHandOff(),
	sub1(SubOpCallMethodInlineable).cost(costExpensive).pure().
		calls().borrows().copiesThenRevokes(routePanicCapture, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	sub1(SubOpEqualStringConstantThenJumpIfFalse).cost(costModerate).flags(SpecJump).pure(),
	sub1(SubOpSelect).cost(costExpensive).pure().
		publishes().copiesThenRevokes(routeChannelSend, HandOffHeapPassthrough, HandOffIdentityKept),
	sub1(SubOpLoadBoolConstant).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpResetSharedCell).cost(costModerate).shim().mutates().
		noHandOff(),
	sub1(SubOpSetSharedCell).cost(costModerate).shim().mutates().
		copiesThenRevokes(routeSharedStore, HandOffSharedStore).copiesThenRevokes(routeClosureCapture, HandOffHeapPassthrough, HandOffIdentityKept),
	sub1(SubOpSliceGeneral).cost(costModerate).shim().pure(),
	sub1(SubOpRangeInitialise).cost(costModerate).shim().pure(),
	sub1(SubOpRangeNext).cost(costModerate).shim().pure(),
	sub1(SubOpGetGlobalWide).cost(costModerate).asmIn("tier1JumpTable").shim().flags(SpecShimSuppressed).pure(),
	sub1(SubOpSetGlobalWide).cost(costModerate).asmIn("tier1JumpTable").shim().flags(SpecShimSuppressed).mutates().
		copiesThenRevokes(routeGlobalStore, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	sub1(SubOpEqualInterfaceNil).cost(costCheap).shim().pure(),
	sub1(SubOpNotEqualInterfaceNil).cost(costCheap).shim().pure(),
	sub1(SubOpBoxSliceByte).cost(costFree).shim().pure(),
	sub1(SubOpConvertSliceByteToString).cost(costMedium).shim().pure(),
	sub1(SubOpEqualUint64ImmediateThenJumpIfFalse).cost(costCheap).
		asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpSimdDotProductFloat64).cost(costExpensive).pure(),
	sub1(SubOpSimdSumFloat64).cost(costExpensive).pure(),
	sub1(SubOpSimdAddFloat64).cost(costExpensive).mutates().
		noHandOff(),
	sub1(SubOpSimdScaleFloat64).cost(costExpensive).mutates().
		noHandOff(),
	sub1(SubOpUnboxSliceFloat64).cost(costFree).shim().pure(),
	sub1(SubOpUnboxSliceInt64).cost(costFree).shim().pure(),
	sub1(SubOpUnboxSliceString).cost(costFree).shim().pure(),
	sub1(SubOpUnboxSliceBool).cost(costFree).shim().pure(),
	sub1(SubOpUnboxSliceUint64).cost(costFree).shim().pure(),
	sub1(SubOpUnboxSliceByte).cost(costFree).shim().pure(),
	sub1(SubOpBoxSliceFloat64).cost(costFree).shim().pure(),
	sub1(SubOpBoxSliceString).cost(costFree).shim().pure(),
	sub1(SubOpBoxSliceBool).cost(costFree).shim().pure(),
	sub1(SubOpBoxSliceUint64).cost(costFree).shim().pure(),
	sub1(SubOpMoveSliceInt64).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMoveSliceFloat64).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMoveSliceString).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMoveSliceBool).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMoveSliceUint64).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpMoveSliceByte).cost(costFree).asmIn("tier1JumpTable").pure(),
	sub1(SubOpAppendSliceInt64).cost(costExpensive).asmIn("tier1JumpTable").mutates().
		noHandOff(),
	sub1(SubOpAppendSliceFloat64).cost(costExpensive).
		asmIn("tier1JumpTable").mutates().noHandOff(),
	sub1(SubOpAppendSliceString).cost(costExpensive).
		asmIn("tier1JumpTable").mutates().copiesThenRevokes(routeAppendHostBacking, HandOffIdentityKept),
	sub1(SubOpAppendSliceBool).cost(costExpensive).asmIn("tier1JumpTable").mutates().
		noHandOff(),
	sub1(SubOpAppendSliceUint64).cost(costExpensive).asmIn("tier1JumpTable").mutates().
		noHandOff(),
	sub1(SubOpAppendSliceByte).cost(costExpensive).asmIn("tier1JumpTable").mutates().
		noHandOff(),
	sub1(SubOpSliceSliceInt64).cost(costModerate).asmIn("tier1JumpTable").pure(),
	sub1(SubOpSliceSliceFloat64).cost(costModerate).asmIn("tier1JumpTable").pure(),
	sub1(SubOpSliceSliceString).cost(costModerate).asmIn("tier1JumpTable").pure(),
	sub1(SubOpSliceSliceBool).cost(costModerate).asmIn("tier1JumpTable").pure(),
	sub1(SubOpSliceSliceUint64).cost(costModerate).asmIn("tier1JumpTable").pure(),
	sub1(SubOpCopySliceInt64).cost(costExpensive).mutates().
		noHandOff(),
	sub1(SubOpCopySliceFloat64).cost(costExpensive).mutates().
		noHandOff(),
	sub1(SubOpCopySliceString).cost(costExpensive).mutates().
		revokes(routeCopyBuiltin, HandOffUntracedContainer),
	sub1(SubOpCopySliceBool).cost(costExpensive).mutates().
		noHandOff(),
	sub1(SubOpCopySliceUint64).cost(costExpensive).mutates().
		noHandOff(),
	sub1(SubOpCopySliceByte).cost(costExpensive).mutates().
		noHandOff(),
	sub1(SubOpAppendUint64WideInPlace).cost(costExpensive).mutates().
		noHandOff(),
	sub1(SubOpAppendSpreadByteInPlace).cost(costExpensive).mutates().
		noHandOff(),
	sub1(SubOpGetStructFieldSliceInt64).cost(costMedium).shim().pure(),
	sub1(SubOpGetStructFieldSliceFloat64).cost(costMedium).shim().pure(),
	sub1(SubOpGetStructFieldSliceUint64).cost(costMedium).shim().pure(),
	sub1(SubOpGetStructFieldSliceString).cost(costMedium).shim().pure(),
	sub1(SubOpGetStructFieldSliceBool).cost(costMedium).shim().pure(),
	sub1(SubOpGetStructFieldSliceByte).cost(costMedium).shim().pure(),
	sub1(SubOpSetStructFieldSliceInt64).cost(costMedium).mutates().
		copiesThenRevokes(RouteContainerStore, HandOffHeapPassthrough, HandOffIdentityKept),
	sub1(SubOpSetStructFieldSliceFloat64).cost(costMedium).mutates().
		copiesThenRevokes(RouteContainerStore, HandOffHeapPassthrough, HandOffIdentityKept),
	sub1(SubOpSetStructFieldSliceUint64).cost(costMedium).mutates().
		copiesThenRevokes(RouteContainerStore, HandOffHeapPassthrough, HandOffIdentityKept),
	sub1(SubOpSetStructFieldSliceString).cost(costMedium).mutates().
		copiesThenRevokes(RouteContainerStore, HandOffHeapPassthrough, HandOffIdentityKept),
	sub1(SubOpSetStructFieldSliceBool).cost(costMedium).mutates().
		copiesThenRevokes(RouteContainerStore, HandOffHeapPassthrough, HandOffIdentityKept),
	sub1(SubOpSetStructFieldSliceByte).cost(costMedium).mutates().
		copiesThenRevokes(RouteContainerStore, HandOffHeapPassthrough, HandOffIdentityKept),
	sub1(SubOpCapacitySliceInt64).cost(costCheap).pure(),
	sub1(SubOpCapacitySliceFloat64).cost(costCheap).pure(),
	sub1(SubOpCapacitySliceString).cost(costCheap).pure(),
	sub1(SubOpCapacitySliceBool).cost(costCheap).pure(),
	sub1(SubOpCapacitySliceUint64).cost(costCheap).pure(),
	sub1(SubOpCapacitySliceByte).cost(costCheap).pure(),
	sub1(SubOpMakeSliceHeap).cost(costVeryHeavy).shim().pure(),
	sub1(SubOpTypeSwitchJump).cost(costModerate).shim().flags(SpecJump).pure(),
	sub1(SubOpRoundToFloat32).cost(costCheap).pure(),
	sub1(SubOpRoundToComplex64).cost(costCheap).pure(),
	sub1(SubOpEqualInterfaceStrict).cost(costCheap).pure(),
	sub1(SubOpNotEqualInterfaceStrict).cost(costCheap).pure(),
	sub1(SubOpAddressField).cost(costMedium).shim().mutates().noHandOff(),
	sub1(SubOpIncrementInt64ThenJumpIfLessThanConstant).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpSetSliceByteElementRun).cost(costModerate).asmIn("tier1JumpTable").mutates().
		noHandOff(),
	sub1(SubOpGetSliceByteElementInt64).cost(costModerate).asmIn("tier1JumpTable").pure(),
	sub1(SubOpGetStructFieldUint64Run).cost(costModerate).shim().pure(),
	sub1(SubOpAddUint32Constant).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpSubtractUint32Constant).cost(costCheap).asmIn("tier1JumpTable").pure(),
	sub1(SubOpEqualUint64ThenJumpIfFalse).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpNotEqualUint64ThenJumpIfFalse).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpIncrementInt64ThenJumpIfLessOrEqual).cost(costCheap).asmIn("tier1JumpTable").flags(SpecJump).pure(),
	sub1(SubOpGetElementFieldUint64Run).cost(costModerate).shim().pure(),
}
