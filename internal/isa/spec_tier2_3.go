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
	sub2(SubOpTier2DrillTier3, "DRILL_TIER3").flags(specMeta),
	sub2(SubOpTier2IncInt, "TIER2_INC_INT").cost(costCheap).handler("handleFlatSubOpTier2IncInt").asmIn("tier2JumpTable", "handlerSubOpTier2IncInt").pure(),
	sub2(SubOpTier2DecInt, "TIER2_DEC_INT").cost(costCheap).handler("handleFlatSubOpTier2DecInt").asmIn("tier2JumpTable", "handlerSubOpTier2DecInt").pure(),
	sub2(SubOpTier2IncUint, "TIER2_INC_UINT").cost(costCheap).handler("handleFlatSubOpTier2IncUint").asmIn("tier2JumpTable", "handlerSubOpTier2IncUint").pure(),
	sub2(SubOpTier2DecUint, "TIER2_DEC_UINT").cost(costCheap).handler("handleFlatSubOpTier2DecUint").asmIn("tier2JumpTable", "handlerSubOpTier2DecUint").pure(),
	sub2(SubOpTier2Panic, "TIER2_PANIC").cost(costModerate).handler("handleFlatSubOpTier2Panic").pure(),
	sub2(SubOpTier2Recover, "TIER2_RECOVER").cost(costModerate).handler("handleFlatSubOpTier2Recover").pure(),
	sub2(SubOpTier2SetZero, "TIER2_SET_ZERO").cost(costFree).handler("handleFlatSubOpTier2SetZero").mutates(),
	sub2(SubOpTier2ChannelClose, "TIER2_CHANNEL_CLOSE").cost(costModerate).handler("handleFlatSubOpTier2ChannelClose").mutates(),
	sub2(SubOpTier2LoadNil, "TIER2_LOAD_NIL").cost(costFree).handler("handleFlatSubOpTier2LoadNil").pure(),
	sub2(SubOpTier2Return, "TIER2_RETURN").cost(costCheap).handler("handleFlatSubOpTier2Return").asmIn("tier2JumpTable", "handlerReturnInline").pure(),
	sub2(SubOpTier2MakeInterfaceMethodExpr, "MAKE_IFACE_METHOD_EXPR").cost(costModerate).handler("runMakeInterfaceMethodExpr").pure(),
	sub2(SubOpTier2MakeMap, "MAKE_MAP").cost(costVeryHeavy).handler("handleFlatSubOpTier2MakeMap").pure(),
	sub2(SubOpTier2RangeCheckUintJumpFalse, "RANGE_CHECK_UINT_JUMP_FALSE").cost(costCheap).handler("handleSubOpRangeCheckUintJumpFalse").
		asmIn("tier2JumpTable", "handlerSubOpRangeCheckUintJumpFalse").flags(SpecJump).pure(),
	sub2(SubOpTier2AllocStructLiteral, "ALLOC_STRUCT_LITERAL").cost(costVeryHeavy).handler("handleSubOpAllocStructLiteral").asmIn("tier2JumpTable", "handlerSubOpAllocStructLiteral").pure(),
	sub2(SubOpTier2SyncClosureUpvalues, "SYNC_CLOSURE_UPVALUES").cost(costModerate).handler("handleSyncClosureUpvalues").shim("HandleSyncClosureUpvalues", "SyncClosureUpvalues").mutates(),
}

// tierSub3Specs holds the tier-3 rows (zero-operand operations).
var tierSub3Specs = []OpSpec{
	sub3(SubOpTier3Nop, "TIER3_NOP").cost(costFree).handler("handleFlatSubOpTier3Nop").flags(specMeta),
	sub3(SubOpTier3ReturnVoid, "TIER3_RETURN_VOID").cost(costCheap).handler("handleReturnVoid").asmIn("tier3JumpTable", "handlerReturnVoidInline").pure(),
	sub3(SubOpTier3SyncIIFEUpvalues, "SYNC_IIFE_UPVALUES").cost(costModerate).handler("handleSyncIIFEUpvalues").mutates(),
}
