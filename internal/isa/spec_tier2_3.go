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

// tierSub2Specs holds the tier-2 rows (one-operand operations).
var tierSub2Specs = []OpSpec{
	sub2(SubOpTier2DrillTier3).noGoHandler().flags(specMeta),
	sub2(SubOpTier2IncrementInt64).cost(costCheap).asmIn("tier2JumpTable").pure(),
	sub2(SubOpTier2DecrementInt64).cost(costCheap).asmIn("tier2JumpTable").pure(),
	sub2(SubOpTier2IncrementUint64).cost(costCheap).asmIn("tier2JumpTable").pure(),
	sub2(SubOpTier2DecrementUint64).cost(costCheap).asmIn("tier2JumpTable").pure(),
	sub2(SubOpTier2IncrementInt32).cost(costCheap).asmIn("tier2JumpTable").pure(),
	sub2(SubOpTier2DecrementInt32).cost(costCheap).asmIn("tier2JumpTable").pure(),
	sub2(SubOpTier2Panic).cost(costModerate).pure().
		publishes().copiesThenRevokes(routePanicCapture, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	sub2(SubOpTier2Recover).cost(costModerate).pure(),
	sub2(SubOpTier2SetZeroGeneral).cost(costFree).mutates().noHandOff(),
	sub2(SubOpTier2CloseChannel).cost(costModerate).mutates().
		noHandOff(),
	sub2(SubOpTier2LoadNil).cost(costFree).pure(),
	sub2(SubOpTier2Return).cost(costCheap).asmIn("tier2JumpTable").pure().
		publishes().copiesThenRevokes(RouteHostReturn, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	sub2(SubOpTier2MakeInterfaceMethodExpression).cost(costModerate).pure(),
	sub2(SubOpTier2MakeMap).cost(costVeryHeavy).shim().pure(),
	sub2(SubOpTier2InRangeUint64ThenJumpIfFalse).cost(costCheap).
		asmIn("tier2JumpTable").flags(SpecJump).pure(),
	sub2(SubOpTier2AllocateStructLiteral).cost(costVeryHeavy).asmIn("tier2JumpTable").pure(),
	sub2(SubOpTier2SynchroniseClosureCapturedVariables).cost(costModerate).shim().mutates().
		noHandOff(),
	sub2(SubOpTier2PanicRangeFunctionContinuedInt64).cost(costCheap).pure(),
	sub2(SubOpTier2CheckShiftCount).cost(costCheap).pure(),
	sub2(SubOpTier2JumpIfFalseBool).cost(costCheap).asmIn("tier2JumpTable").flags(SpecJump).pure(),
	sub2(SubOpTier2JumpIfTrueBool).cost(costCheap).asmIn("tier2JumpTable").flags(SpecJump).pure(),
}

// tierSub3Specs holds the tier-3 rows (zero-operand operations).
var tierSub3Specs = []OpSpec{
	sub3(SubOpTier3NoOperation).cost(costFree).flags(specMeta).pure(),
	sub3(SubOpTier3ReturnVoid).cost(costCheap).asmIn("tier3JumpTable").pure().
		publishes().copiesThenRevokes(RouteHostReturn, HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept),
	sub3(SubOpTier3SynchroniseIIFECapturedVariables).cost(costModerate).mutates().
		noHandOff(),
}
