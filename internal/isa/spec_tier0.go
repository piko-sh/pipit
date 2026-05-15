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
	main0(OpDrillTier1, "DRILL_TIER1").cost(costMedium).flags(specMeta),
	main0(OpExt, "EXT").cost(costFree).handler("handleExt").pure(),
	main0(OpLoadIntConst, "LOAD_INT_CONST").cost(costFree).handler("handleLoadIntConst").asm("handlerLoadIntConst").pure(),
	main0(OpLoadFloatConst, "LOAD_FLOAT_CONST").cost(costFree).handler("handleLoadFloatConst").asm("handlerLoadFloatConst").pure(),
	main0(OpAddInt, "ADD_INT").handler("handleAddInt").asm("handlerAddInt").pure(),
	main0(OpSubInt, "SUB_INT").handler("handleSubInt").asm("handlerSubInt").pure(),
	main0(OpMulInt, "MUL_INT").handler("handleMulInt").asm("handlerMulInt").pure(),
	main0(OpDivInt, "DIV_INT").handler("handleDivInt").asm("handlerDivInt").pure(),
	main0(OpRemInt, "REM_INT").handler("handleRemInt").asm("handlerRemInt").pure(),
	main0(OpBitAnd, "BIT_AND").handler("handleBitAnd").asm("handlerBitAnd").pure(),
	main0(OpBitOr, "BIT_OR").handler("handleBitOr").asm("handlerBitOr").pure(),
	main0(OpBitXor, "BIT_XOR").handler("handleBitXor").asm("handlerBitXor").pure(),
	main0(OpBitAndNot, "BIT_AND_NOT").handler("handleBitAndNot").asm("handlerBitAndNot").pure(),
	main0(OpShiftLeft, "SHIFT_LEFT").handler("handleShiftLeft").asm("handlerShiftLeft").pure(),
	main0(OpShiftRight, "SHIFT_RIGHT").handler("handleShiftRight").asm("handlerShiftRight").pure(),
	main0(OpAddFloat, "ADD_FLOAT").handler("handleAddFloat").asm("handlerAddFloat").pure(),
	main0(OpSubFloat, "SUB_FLOAT").handler("handleSubFloat").asm("handlerSubFloat").pure(),
	main0(OpMulFloat, "MUL_FLOAT").handler("handleMulFloat").asm("handlerMulFloat").pure(),
	main0(OpDivFloat, "DIV_FLOAT").handler("handleDivFloat").asm("handlerDivFloat").pure(),
	main0(OpEqInt, "EQ_INT").handler("handleEqInt").asm("handlerEqInt").pure(),
	main0(OpNeInt, "NE_INT").handler("handleNeInt").asm("handlerNeInt").pure(),
	main0(OpLtInt, "LT_INT").handler("handleLtInt").asm("handlerLtInt").pure(),
	main0(OpLeInt, "LE_INT").handler("handleLeInt").asm("handlerLeInt").pure(),
	main0(OpGtInt, "GT_INT").handler("handleGtInt").asm("handlerGtInt").pure(),
	main0(OpGeInt, "GE_INT").handler("handleGeInt").asm("handlerGeInt").pure(),
	main0(OpEqFloat, "EQ_FLOAT").handler("handleEqFloat").asm("handlerEqFloat").pure(),
	main0(OpNeFloat, "NE_FLOAT").handler("handleNeFloat").asm("handlerNeFloat").pure(),
	main0(OpLtFloat, "LT_FLOAT").handler("handleLtFloat").asm("handlerLtFloat").pure(),
	main0(OpLeFloat, "LE_FLOAT").handler("handleLeFloat").asm("handlerLeFloat").pure(),
	main0(OpGtFloat, "GT_FLOAT").handler("handleGtFloat").asm("handlerGtFloat").pure(),
	main0(OpGeFloat, "GE_FLOAT").handler("handleGeFloat").asm("handlerGeFloat").pure(),
	main0(OpJumpIfTrue, "JUMP_IF_TRUE").handler("handleJumpIfTrue").asm("handlerJumpIfTrue").flags(SpecJump).pure(),
	main0(OpJumpIfFalse, "JUMP_IF_FALSE").handler("handleJumpIfFalse").asm("handlerJumpIfFalse").flags(SpecJump).pure(),
	main0(OpSubIntConst, "SUB_INT_CONST").handler("handleSubIntConst").asm("handlerSubIntConst").pure(),
	main0(OpAddIntConst, "ADD_INT_CONST").handler("handleAddIntConst").asm("handlerAddIntConst").pure(),
	main0(OpMulIntConst, "MUL_INT_CONST").handler("handleMulIntConst").asm("handlerMulIntConst").pure(),
	main0(OpAddIntJump, "ADD_INT_JUMP").handler("handleAddIntJump").asm("handlerAddIntJump").flags(SpecJump).pure(),
	main0(OpStringIndex, "STRING_INDEX").handler("handleStringIndex").asm("handlerStringIndex").pure(),
	main0(OpEqString, "EQ_STRING").cost(costMedium).handler("handleEqString").asm("handlerEqString").pure(),
	main0(OpNeString, "NE_STRING").cost(costMedium).handler("handleNeString").asm("handlerNeString").pure(),
	main0(OpSliceString, "SLICE_STRING").cost(costMedium).handler("handleSliceString").asm("handlerSliceString").pure(),
	main0(OpStringIndexToInt, "STRING_INDEX_TO_INT").cost(costMedium).handler("handleStringIndexToInt").asm("handlerStringIndexToInt").pure(),
	main0(OpMoveGeneral, "MOVE_GENERAL").cost(costFree).handler("handleMoveGeneral").asm("handlerMoveGeneral").shim("HandleMoveGeneral", "MoveGeneral").flags(SpecShimSuppressed).pure(),
	main0(OpLoadStringConst, "LOAD_STRING_CONST").cost(costFree).handler("handleLoadStringConst").asm("handlerLoadStringConst").pure(),
	main0(OpLoadGeneralConst, "LOAD_GENERAL_CONST").cost(costFree).handler("handleLoadGeneralConst").shim("HandleLoadGeneralConst", "LoadGeneralConst").pure(),
	main0(OpLoadUintConst, "LOAD_UINT_CONST").cost(costFree).handler("handleLoadUintConst").asm("handlerLoadUintConst").pure(),
	main0(OpLoadComplexConst, "LOAD_COMPLEX_CONST").cost(costFree).handler("handleLoadComplexConst").shim("HandleLoadComplexConst", "LoadComplexConst").pure(),
	main0(OpAddUint, "ADD_UINT").handler("handleAddUint").asm("handlerAddUint").pure(),
	main0(OpSubUint, "SUB_UINT").handler("handleSubUint").asm("handlerSubUint").pure(),
	main0(OpMulUint, "MUL_UINT").handler("handleMulUint").asm("handlerMulUint").pure(),
	main0(OpDivUint, "DIV_UINT").handler("handleDivUint").asm("handlerDivUint").pure(),
	main0(OpRemUint, "REM_UINT").handler("handleRemUint").asm("handlerRemUint").pure(),
	main0(OpBitAndUint, "BIT_AND_UINT").handler("handleBitAndUint").asm("handlerBitAndUint").pure(),
	main0(OpBitOrUint, "BIT_OR_UINT").handler("handleBitOrUint").asm("handlerBitOrUint").pure(),
	main0(OpBitXorUint, "BIT_XOR_UINT").handler("handleBitXorUint").asm("handlerBitXorUint").pure(),
	main0(OpBitAndNotUint, "BIT_AND_NOT_UINT").handler("handleBitAndNotUint").asm("handlerBitAndNotUint").pure(),
	main0(OpShiftLeftUint, "SHIFT_LEFT_UINT").handler("handleShiftLeftUint").asm("handlerShiftLeftUint").pure(),
	main0(OpShiftRightUint, "SHIFT_RIGHT_UINT").handler("handleShiftRightUint").asm("handlerShiftRightUint").pure(),
	main0(OpEqUint, "EQ_UINT").handler("handleEqUint").asm("handlerEqUint").pure(),
	main0(OpNeUint, "NE_UINT").handler("handleNeUint").asm("handlerNeUint").pure(),
	main0(OpLtUint, "LT_UINT").handler("handleLtUint").asm("handlerLtUint").pure(),
	main0(OpLeUint, "LE_UINT").handler("handleLeUint").asm("handlerLeUint").pure(),
	main0(OpGtUint, "GT_UINT").handler("handleGtUint").asm("handlerGtUint").pure(),
	main0(OpGeUint, "GE_UINT").handler("handleGeUint").asm("handlerGeUint").pure(),
	main0(OpAddComplex, "ADD_COMPLEX").handler("handleAddComplex").shim("HandleAddComplex", "AddComplex").pure(),
	main0(OpSubComplex, "SUB_COMPLEX").handler("handleSubComplex").shim("HandleSubComplex", "SubComplex").pure(),
	main0(OpMulComplex, "MUL_COMPLEX").handler("handleMulComplex").shim("HandleMulComplex", "MulComplex").pure(),
	main0(OpDivComplex, "DIV_COMPLEX").handler("handleDivComplex").shim("HandleDivComplex", "DivComplex").pure(),
	main0(OpEqComplex, "EQ_COMPLEX").handler("handleEqComplex").shim("HandleEqComplex", "EqComplex").pure(),
	main0(OpNeComplex, "NE_COMPLEX").handler("handleNeComplex").shim("HandleNeComplex", "NeComplex").pure(),
	main0(OpBuildComplex, "BUILD_COMPLEX").handler("handleBuildComplex").shim("HandleBuildComplex", "BuildComplex").pure(),
	main0(OpConcatString, "CONCAT_STRING").cost(costMedium).handler("handleConcatString").shim("HandleConcatString", "ConcatString").pure(),
	main0(OpConcatRuneString, "CONCAT_RUNE_STRING").cost(costMedium).handler("handleConcatRuneString").shim("HandleConcatRuneString", "ConcatRuneString").pure(),
	main0(OpLtString, "LT_STRING").cost(costMedium).handler("handleLtString").pure(),
	main0(OpLeString, "LE_STRING").cost(costMedium).handler("handleLeString").pure(),
	main0(OpGtString, "GT_STRING").cost(costMedium).handler("handleGtString").pure(),
	main0(OpGeString, "GE_STRING").cost(costMedium).handler("handleGeString").pure(),
	main0(OpEqGeneral, "EQ_GENERAL").cost(costMedium).handler("handleEqGeneral").asm("handlerEqGeneral").shim("HandleEqGeneral", "EqGeneral").flags(SpecShimSuppressed).pure(),
	main0(OpNeGeneral, "NE_GENERAL").cost(costMedium).handler("handleNeGeneral").shim("HandleNeGeneral", "NeGeneral").pure(),
	main0(OpLtGeneral, "LT_GENERAL").cost(costMedium).handler("handleLtGeneral").shim("HandleLtGeneral", "LtGeneral").pure(),
	main0(OpLeGeneral, "LE_GENERAL").cost(costMedium).handler("handleLeGeneral").shim("HandleLeGeneral", "LeGeneral").pure(),
	main0(OpGtGeneral, "GT_GENERAL").cost(costMedium).handler("handleGtGeneral").shim("HandleGtGeneral", "GtGeneral").pure(),
	main0(OpGeGeneral, "GE_GENERAL").cost(costMedium).handler("handleGeGeneral").shim("HandleGeGeneral", "GeGeneral").pure(),
	main0(OpAdd, "ADD").cost(costMedium).handler("handleAdd").pure(),
	main0(OpSub, "SUB").cost(costMedium).handler("handleSub").pure(),
	main0(OpMul, "MUL").cost(costMedium).handler("handleMul").pure(),
	main0(OpDiv, "DIV").cost(costMedium).handler("handleDiv").pure(),
	main0(OpRem, "REM").cost(costMedium).handler("handleRem").pure(),
	main0(OpTruncateNarrow, "TRUNCATE_NARROW").handler("HandleTruncateNarrow").asm("handlerTruncateNarrow").pure(),
	main0(OpPackInterface, "PACK_INTERFACE").cost(costModerate).handler("handlePackInterface").shim("HandlePackInterface", "PackInterface").pure(),
	main0(OpUnpackInterface, "UNPACK_INTERFACE").cost(costModerate).handler("handleUnpackInterface").shim("HandleUnpackInterface", "UnpackInterface").pure(),
	main0(OpTestNilJumpTrue, "TEST_NIL_JUMP_TRUE").cost(costModerate).handler("handleTestNilJumpTrue").asm("handlerTestNilJumpTrue").shim("HandleTestNilJumpTrue", "TestNilJumpTrue").
		flags(SpecJump | SpecShimSuppressed).pure(),
	main0(OpTestNilJumpFalse, "TEST_NIL_JUMP_FALSE").cost(costModerate).handler("handleTestNilJumpFalse").asm("handlerTestNilJumpFalse").shim("HandleTestNilJumpFalse", "TestNilJumpFalse").
		flags(SpecJump | SpecShimSuppressed).pure(),
	main0(OpMakeClosure, "MAKE_CLOSURE").cost(costModerate).handler("handleMakeClosure").shim("HandleMakeClosure", "MakeClosure").mutates(),
	main0(OpGetUpvalue, "GET_UPVALUE").cost(costModerate).handler("handleGetUpvalue").asm("handlerGetUpvalueScalar").shim("HandleGetUpvalue", "GetUpvalue").flags(SpecShimSuppressed).pure(),
	main0(OpSetUpvalue, "SET_UPVALUE").cost(costModerate).handler("handleSetUpvalue").shim("HandleSetUpvalue", "SetUpvalue").mutates(),
	main0(OpDefer, "DEFER").cost(costModerate).handler("handleDefer").pure(),
	main0(OpGo, "GO").cost(costExpensive).handler("handleGo").pure(),
	main0(OpMakeSlice, "MAKE_SLICE").cost(costVeryHeavy).handler("handleMakeSlice").shim("HandleMakeSlice", "MakeSlice").pure(),
	main0(OpIndex, "INDEX").cost(costModerate).handler("handleIndex").shim("HandleIndex", "Index").pure(),
	main0(OpIndexSet, "INDEX_SET").cost(costModerate).handler("handleIndexSet").shim("HandleIndexSet", "IndexSet").mutates(),
	main0(OpMapIndex, "MAP_INDEX").cost(costModerate).handler("handleMapIndex").exit("handlerMapIndexExit", "exitMapIndex").pure(),
	main0(OpMapSet, "MAP_SET").cost(costModerate).handler("handleMapSet").shim("HandleMapSet", "MapSet").mutates(),
	main0(OpMapIndexOk, "MAP_INDEX_OK").cost(costModerate).handler("handleMapIndexOk").shim("HandleMapIndexOk", "MapIndexOk").pure(),
	main0(OpAppend, "APPEND").cost(costExpensive).handler("handleAppend").exit("handlerAppendExit", "exitAppend").mutates(),
	main0(OpAppendSpread, "APPEND_SPREAD").cost(costExpensive).handler("handleAppendSpread").shim("HandleAppendSpread", "AppendSpread").mutates(),
	main0(OpAppendByteFast, "APPEND_BYTE_FAST").cost(costExpensive).handler("handleAppendByteFast").exit("handlerAppendByteFastExit", "exitAppendByteFast").mutates(),
	main0(OpCopy, "COPY").cost(costModerate).handler("handleCopy").shim("HandleCopy", "Copy").mutates(),
	main0(OpSliceGetInt, "SLICE_GET_INT").cost(costModerate).handler("handleSliceGetInt").shim("HandleSliceGetInt", "SliceGetInt").pure(),
	main0(OpSliceSetInt, "SLICE_SET_INT").cost(costModerate).handler("handleSliceSetInt").shim("HandleSliceSetInt", "SliceSetInt").mutates(),
	main0(OpSliceGetFloat, "SLICE_GET_FLOAT").cost(costModerate).handler("handleSliceGetFloat").shim("HandleSliceGetFloat", "SliceGetFloat").pure(),
	main0(OpSliceSetFloat, "SLICE_SET_FLOAT").cost(costModerate).handler("handleSliceSetFloat").shim("HandleSliceSetFloat", "SliceSetFloat").mutates(),
	main0(OpSliceGetString, "SLICE_GET_STRING").cost(costModerate).handler("handleSliceGetString").shim("HandleSliceGetString", "SliceGetString").pure(),
	main0(OpSliceSetString, "SLICE_SET_STRING").cost(costModerate).handler("handleSliceSetString").shim("HandleSliceSetString", "SliceSetString").mutates(),
	main0(OpSliceGetBool, "SLICE_GET_BOOL").cost(costModerate).handler("handleSliceGetBool").shim("HandleSliceGetBool", "SliceGetBool").pure(),
	main0(OpSliceSetBool, "SLICE_SET_BOOL").cost(costModerate).handler("handleSliceSetBool").shim("HandleSliceSetBool", "SliceSetBool").mutates(),
	main0(OpSliceGetUint, "SLICE_GET_UINT").cost(costModerate).handler("handleSliceGetUint").shim("HandleSliceGetUint", "SliceGetUint").pure(),
	main0(OpSliceSetUint, "SLICE_SET_UINT").cost(costModerate).handler("handleSliceSetUint").shim("HandleSliceSetUint", "SliceSetUint").mutates(),
	main0(OpLoadCompositeZeroReuse, "LOAD_COMPOSITE_ZERO_REUSE").cost(costFree).handler("handleLoadCompositeZeroReuse").mutates(),
	main0(OpRangeNextSliceByte, "RANGE_NEXT_SLICE_BYTE").handler("handleRangeNextSliceByte").asm("handlerRangeNextSliceByte").flags(SpecJump).pure(),
	main0(OpMapGetIntInt, "MAP_GET_INT_INT").cost(costModerate).handler("handleMapGetIntInt").shim("HandleMapGetIntInt", "MapGetIntInt").pure(),
	main0(OpMapSetIntInt, "MAP_SET_INT_INT").cost(costModerate).handler("handleMapSetIntInt").shim("HandleMapSetIntInt", "MapSetIntInt").mutates(),
	main0(OpMapGetStringInt, "MAP_GET_STRING_INT").cost(costModerate).handler("handleMapGetStringInt").shim("HandleMapGetStringInt", "MapGetStringInt").pure(),
	main0(OpMapSetStringInt, "MAP_SET_STRING_INT").cost(costModerate).handler("handleMapSetStringInt").shim("HandleMapSetStringInt", "MapSetStringInt").mutates(),
	main0(OpMapGetStringString, "MAP_GET_STRING_STRING").cost(costModerate).handler("handleMapGetStringString").shim("HandleMapGetStringString", "MapGetStringString").pure(),
	main0(OpMapSetStringString, "MAP_SET_STRING_STRING").cost(costModerate).handler("handleMapSetStringString").shim("HandleMapSetStringString", "MapSetStringString").mutates(),
	main0(OpMapGetIntString, "MAP_GET_INT_STRING").cost(costModerate).handler("handleMapGetIntString").shim("HandleMapGetIntString", "MapGetIntString").pure(),
	main0(OpMapSetIntString, "MAP_SET_INT_STRING").cost(costModerate).handler("handleMapSetIntString").shim("HandleMapSetIntString", "MapSetIntString").mutates(),
	main0(OpMapIndexOkIntInt, "MAP_INDEX_OK_INT_INT").cost(costModerate).handler("handleMapIndexOkIntInt").shim("HandleMapIndexOkIntInt", "MapIndexOkIntInt").pure(),
	main0(OpMapIndexOkStringInt, "MAP_INDEX_OK_STRING_INT").cost(costModerate).handler("handleMapIndexOkStringInt").shim("HandleMapIndexOkStringInt", "MapIndexOkStringInt").pure(),
	main0(OpMapIndexOkStringString, "MAP_INDEX_OK_STRING_STRING").cost(costModerate).handler("handleMapIndexOkStringString").shim("HandleMapIndexOkStringString", "MapIndexOkStringString").pure(),
	main0(OpMapIndexOkIntString, "MAP_INDEX_OK_INT_STRING").cost(costModerate).handler("handleMapIndexOkIntString").shim("HandleMapIndexOkIntString", "MapIndexOkIntString").pure(),
	main0(OpMapGetIntGeneral, "MAP_GET_INT_GENERAL").cost(costModerate).handler("handleMapGetIntGeneral").shim("HandleMapGetIntGeneral", "MapGetIntGeneral").pure(),
	main0(OpMapIndexOkIntGeneral, "MAP_INDEX_OK_INT_GENERAL").cost(costModerate).handler("handleMapIndexOkIntGeneral").shim("HandleMapIndexOkIntGeneral", "MapIndexOkIntGeneral").pure(),
	main0(OpMapGetStringGeneral, "MAP_GET_STRING_GENERAL").handler("handleMapGetStringGeneral").shim("HandleMapGetStringGeneral", "MapGetStringGeneral").pure(),
	main0(OpMapIndexOkStringGeneral, "MAP_INDEX_OK_STRING_GENERAL").handler("handleMapIndexOkStringGeneral").shim("HandleMapIndexOkStringGeneral", "MapIndexOkStringGeneral").pure(),
	main0(OpMapSetStringGeneral, "MAP_SET_STRING_GENERAL").handler("handleMapSetStringGeneral").shim("HandleMapSetStringGeneral", "MapSetStringGeneral").mutates(),
	main0(OpMapAddIntInt, "MAP_ADD_INT_INT").handler("handleMapAddIntInt").shim("HandleMapAddIntInt", "MapAddIntInt").mutates(),
	main0(OpMapAddStringInt, "MAP_ADD_STRING_INT").handler("handleMapAddStringInt").shim("HandleMapAddStringInt", "MapAddStringInt").mutates(),
	main0(OpGetField, "GET_FIELD").cost(costMedium).handler("handleGetField").exit("handlerGetFieldExit", "exitGetField").pure(),
	main0(OpSetField, "SET_FIELD").cost(costMedium).handler("handleSetField").exit("handlerSetFieldExit", "exitSetField").mutates(),
	main0(OpSetFieldInt, "SET_FIELD_INT").cost(costMedium).handler("handleSetFieldInt").mutates(),
	main0(OpGetFieldInt, "GET_FIELD_INT").cost(costMedium).handler("handleGetFieldInt").pure(),
	main0(OpBindMethod, "BIND_METHOD").cost(costModerate).handler("handleBindMethod").shim("HandleBindMethod", "BindMethod").pure(),
	main0(OpChannelSend, "CHAN_SEND").cost(costModerate).handler("HandleChannelSend").mutates(),
	main0(OpAddr, "ADDR").cost(costMedium).handler("handleAddr").shim("HandleAddr", "Addr").mutates(),
	main0(OpDeref, "DEREF").cost(costMedium).handler("handleDeref").shim("HandleDeref", "Deref").pure(),
	main0(OpAllocIndirect, "ALLOC_INDIRECT").cost(costModerate).handler("handleAllocIndirect").shim("HandleAllocIndirect", "AllocIndirect").pure(),
	main0(OpTypeAssert, "TYPE_ASSERT").cost(costModerate).handler("handleTypeAssert").shim("HandleTypeAssert", "TypeAssert").pure(),
	main0(OpConvert, "CONVERT").cost(costMedium).handler("handleConvert").shim("HandleConvert", "Convert").pure(),
	main0(OpGetGlobal, "GET_GLOBAL").cost(costModerate).handler("handleGetGlobal").shim("HandleGetGlobal", "GetGlobal").pure(),
	main0(OpSetGlobal, "SET_GLOBAL").cost(costModerate).handler("handleSetGlobal").shim("HandleSetGlobal", "SetGlobal").mutates(),
	main0(OpUnsafeString, "UNSAFE_STRING").cost(costModerate).handler("handleUnsafeString").shim("HandleUnsafeString", "UnsafeString").mutates(),
	main0(OpUnsafeSlice, "UNSAFE_SLICE").cost(costModerate).handler("handleUnsafeSlice").shim("HandleUnsafeSlice", "UnsafeSlice").mutates(),
	main0(OpUnsafeAdd, "UNSAFE_ADD").cost(costModerate).handler("handleUnsafeAdd").shim("HandleUnsafeAdd", "UnsafeAdd").pure(),
	main0(OpStrContainsRune, "STR_CONTAINS_RUNE").cost(costModerate).handler("handleStrContainsRune").shim("HandleStrContainsRune", "StrContainsRune").pure(),
	main0(OpStrContains, "STR_CONTAINS").cost(costModerate).handler("handleStrContains").shim("HandleStrContains", "StrContains").pure(),
	main0(OpStrHasPrefix, "STR_HAS_PREFIX").cost(costModerate).handler("handleStrHasPrefix").shim("HandleStrHasPrefix", "StrHasPrefix").pure(),
	main0(OpStrHasSuffix, "STR_HAS_SUFFIX").cost(costModerate).handler("handleStrHasSuffix").shim("HandleStrHasSuffix", "StrHasSuffix").pure(),
	main0(OpStrEqualFold, "STR_EQUAL_FOLD").cost(costModerate).handler("handleStrEqualFold").shim("HandleStrEqualFold", "StrEqualFold").pure(),
	main0(OpStrIndex, "STR_INDEX").cost(costModerate).handler("handleStrIndex").shim("HandleStrIndex", "StrIndex").pure(),
	main0(OpStrCount, "STR_COUNT").cost(costModerate).handler("handleStrCount").shim("HandleStrCount", "StrCount").pure(),
	main0(OpStrTrimPrefix, "STR_TRIM_PREFIX").cost(costModerate).handler("handleStrTrimPrefix").shim("HandleStrTrimPrefix", "StrTrimPrefix").pure(),
	main0(OpStrTrimSuffix, "STR_TRIM_SUFFIX").cost(costModerate).handler("handleStrTrimSuffix").shim("HandleStrTrimSuffix", "StrTrimSuffix").pure(),
	main0(OpStrTrim, "STR_TRIM").cost(costModerate).handler("handleStrTrim").shim("HandleStrTrim", "StrTrim").pure(),
	main0(OpStrIndexRune, "STR_INDEX_RUNE").cost(costModerate).handler("handleStrIndexRune").shim("HandleStrIndexRune", "StrIndexRune").pure(),
	main0(OpStrRepeat, "STR_REPEAT").cost(costModerate).handler("handleStrRepeat").shim("HandleStrRepeat", "StrRepeat").pure(),
	main0(OpStrLastIndex, "STR_LAST_INDEX").cost(costModerate).handler("handleStrLastIndex").shim("HandleStrLastIndex", "StrLastIndex").pure(),
	main0(OpStrJoin, "STR_JOIN").cost(costModerate).handler("handleStrJoin").shim("HandleStrJoin", "StrJoin").pure(),
	main0(OpStrSplit, "STR_SPLIT").cost(costModerate).handler("handleStrSplit").shim("HandleStrSplit", "StrSplit").pure(),
	main0(OpStrReplaceAll, "STR_REPLACE_ALL").cost(costModerate).handler("handleStrReplaceAll").shim("HandleStrReplaceAll", "StrReplaceAll").pure(),
	main0(OpMathPow, "MATH_POW").cost(costMedium).handler("handleMathPow").shim("HandleMathPow", "MathPow").pure(),
	main0(OpSliceGetIntDirect, "SLICE_GET_INT_DIRECT").handler("handleSliceGetIntDirect").asm("handlerSliceGetIntDirect").pure(),
	main0(OpSliceSetIntDirect, "SLICE_SET_INT_DIRECT").handler("handleSliceSetIntDirect").asm("handlerSliceSetIntDirect").mutates(),
	main0(OpRangeNextSliceInt, "RANGE_NEXT_SLICE_INT").handler("handleRangeNextSliceInt").flags(SpecJump).pure(),
	main0(OpGetStructFieldIntT0, "GET_STRUCT_FIELD_INT_T0").handler("handleGetStructFieldIntT0").asm("handlerGetStructFieldIntT0").shim("HandleGetStructFieldIntT0", "GetStructFieldIntT0").
		flags(SpecShimNarrow | SpecShimSuppressed).pure(),
	main0(OpSetStructFieldIntT0, "SET_STRUCT_FIELD_INT_T0").handler("handleSetStructFieldIntT0").asm("handlerSetStructFieldIntT0").shim("HandleSetStructFieldIntT0", "SetStructFieldIntT0").
		flags(SpecShimNarrow | SpecShimSuppressed).mutates(),
	main0(OpGetStructFieldUint, "GET_STRUCT_FIELD_UINT_T0").handler("handleGetStructFieldUintT0").asm("handlerGetStructFieldUintT0").
		shim("HandleGetStructFieldUintT0", "GetStructFieldUintT0").
		flags(SpecShimNarrow | SpecShimSuppressed).pure(),
	main0(OpSetStructFieldUint, "SET_STRUCT_FIELD_UINT_T0").handler("handleSetStructFieldUintT0").asm("handlerSetStructFieldUintT0").
		shim("HandleSetStructFieldUintT0", "SetStructFieldUintT0").
		flags(SpecShimNarrow | SpecShimSuppressed).mutates(),
	main0(OpGetStructFieldFloat, "GET_STRUCT_FIELD_FLOAT_T0").handler("handleGetStructFieldFloatT0").asm("handlerGetStructFieldFloatT0").
		shim("HandleGetStructFieldFloatT0", "GetStructFieldFloatT0").flags(SpecShimNarrow | SpecShimSuppressed).pure(),
	main0(OpSetStructFieldFloat, "SET_STRUCT_FIELD_FLOAT_T0").handler("handleSetStructFieldFloatT0").asm("handlerSetStructFieldFloatT0").
		shim("HandleSetStructFieldFloatT0", "SetStructFieldFloatT0").flags(SpecShimNarrow | SpecShimSuppressed).mutates(),
	main0(OpGetStructFieldBool, "GET_STRUCT_FIELD_BOOL_T0").handler("handleGetStructFieldBoolT0").asm("handlerGetStructFieldBoolT0").
		shim("HandleGetStructFieldBoolT0", "GetStructFieldBoolT0").
		flags(SpecShimNarrow | SpecShimSuppressed).pure(),
	main0(OpSetStructFieldBool, "SET_STRUCT_FIELD_BOOL_T0").handler("handleSetStructFieldBoolT0").asm("handlerSetStructFieldBoolT0").
		shim("HandleSetStructFieldBoolT0", "SetStructFieldBoolT0").
		flags(SpecShimNarrow | SpecShimSuppressed).mutates(),
	main0(OpGetStructFieldGeneral, "GET_STRUCT_FIELD_GENERAL_T0").handler("handleGetStructFieldGeneralT0").asm("handlerGetStructFieldGeneralT0").
		shim("HandleGetStructFieldGeneralT0", "GetStructFieldGeneralT0").flags(SpecShimNarrow | SpecShimSuppressed).pure(),
	main0(OpSetStructFieldGeneral, "SET_STRUCT_FIELD_GENERAL_T0").handler("handleSetStructFieldGeneralT0").asm("handlerSetStructFieldGeneralT0").
		shim("HandleSetStructFieldGeneralT0", "SetStructFieldGeneralT0").flags(SpecShimNarrow | SpecShimSuppressed).mutates(),
	main0(OpCopyStructFieldGeneralT0, "COPY_STRUCT_FIELD_GENERAL_T0").handler("handleCopyStructFieldGeneralT0").shim("HandleCopyStructFieldGeneralT0", "CopyStructFieldGeneralT0").
		flags(SpecShimNarrow).mutates(),
	main0(OpSliceIndexStructFieldInt, "SLICE_INDEX_STRUCT_FIELD_INT").handler("handleSliceIndexStructFieldInt").asm("handlerSliceIndexStructFieldInt").
		shim("HandleSliceIndexStructFieldInt", "SliceIndexStructFieldInt").flags(SpecShimSuppressed).pure(),
	main0(OpSliceIndexStructFieldUint, "SLICE_INDEX_STRUCT_FIELD_UINT").handler("handleSliceIndexStructFieldUint").pure(),
	main0(OpSliceIndexStructFieldFloat, "SLICE_INDEX_STRUCT_FIELD_FLOAT").handler("handleSliceIndexStructFieldFloat").asm("handlerSliceIndexStructFieldFloat").
		shim("HandleSliceIndexStructFieldFloat", "SliceIndexStructFieldFloat").flags(SpecShimSuppressed).pure(),
	main0(OpSliceIndexStructFieldBool, "SLICE_INDEX_STRUCT_FIELD_BOOL").handler("handleSliceIndexStructFieldBool").pure(),
	main0(OpSliceIndexStructFieldString, "SLICE_INDEX_STRUCT_FIELD_STRING").handler("handleSliceIndexStructFieldString").pure(),
	main0(OpMapIndexOkJumpIfFalseIntInt, "MAP_INDEX_OK_JUMP_IF_FALSE_INT_INT").cost(costModerate).handler("handleMapIndexOkJumpIfFalseIntInt").
		shim("HandleMapIndexOkJumpIfFalseIntInt", "MapIndexOkJumpIfFalseIntInt").pure(),
	main0(OpMapIndexOkJumpIfFalseStringInt, "MAP_INDEX_OK_JUMP_IF_FALSE_STRING_INT").cost(costModerate).handler("handleMapIndexOkJumpIfFalseStringInt").
		shim("HandleMapIndexOkJumpIfFalseStringInt", "MapIndexOkJumpIfFalseStringInt").pure(),
	main0(OpMapIndexOkJumpIfFalseStringString, "MAP_INDEX_OK_JUMP_IF_FALSE_STRING_STRING").cost(costModerate).handler("handleMapIndexOkJumpIfFalseStringString").
		shim("HandleMapIndexOkJumpIfFalseStringString", "MapIndexOkJumpIfFalseStringString").pure(),
	main0(OpMapIndexOkJumpIfFalseIntString, "MAP_INDEX_OK_JUMP_IF_FALSE_INT_STRING").cost(costModerate).handler("handleMapIndexOkJumpIfFalseIntString").
		shim("HandleMapIndexOkJumpIfFalseIntString", "MapIndexOkJumpIfFalseIntString").pure(),
	main0(OpMapIndexOkJumpIfFalseIntGeneral, "MAP_INDEX_OK_JUMP_IF_FALSE_INT_GENERAL").cost(costModerate).handler("handleMapIndexOkJumpIfFalseIntGeneral").
		shim("HandleMapIndexOkJumpIfFalseIntGeneral", "MapIndexOkJumpIfFalseIntGeneral").pure(),
	main0(OpMapIndexOkJumpIfFalseStringGeneral, "MAP_INDEX_OK_JUMP_IF_FALSE_STRING_GENERAL").cost(costModerate).handler("handleMapIndexOkJumpIfFalseStringGeneral").
		shim("HandleMapIndexOkJumpIfFalseStringGeneral", "MapIndexOkJumpIfFalseStringGeneral").pure(),
	main0(OpSwapStructFieldsGeneralT0, "SWAP_STRUCT_FIELDS_GENERAL_T0").handler("handleSwapStructFieldsGeneralT0").shim("HandleSwapStructFieldsGeneralT0", "SwapStructFieldsGeneralT0").
		flags(SpecShimNarrow).mutates(),
	main0(OpGetStructFieldRawPointerT0, "GET_STRUCT_FIELD_RAW_POINTER_T0").handler("handleGetStructFieldRawPointerT0").asm("handlerGetStructFieldRawPointerT0").
		shim("HandleGetStructFieldRawPointerT0", "GetStructFieldRawPointerT0").flags(SpecShimNarrow | SpecShimSuppressed).pure(),
	main0(OpSliceGetIntDirectUnchecked, "SLICE_GET_INT_DIRECT_UNCHECKED").handler("handleSliceGetIntDirectUnchecked").asm("handlerSliceGetIntDirectUnchecked").pure(),
	main0(OpSliceSetIntDirectUnchecked, "SLICE_SET_INT_DIRECT_UNCHECKED").handler("handleSliceSetIntDirectUnchecked").asm("handlerSliceSetIntDirectUnchecked").mutates(),
	main0(OpSliceGetIntUnchecked, "SLICE_GET_INT_UNCHECKED").handler("handleSliceGetIntUnchecked").pure(),
	main0(OpSliceSetIntUnchecked, "SLICE_SET_INT_UNCHECKED").handler("handleSliceSetIntUnchecked").mutates(),
	main0(OpPackTyped, "PACK_TYPED").cost(costModerate).handler("handlePackTyped").pure(),
	main0(OpAppendByteFastInPlace, "APPEND_BYTE_FAST_INPLACE").cost(costExpensive).handler("handleAppendByteFastInPlace").mutates(),
	main0(OpAppendInPlace, "APPEND_INPLACE").cost(costExpensive).handler("handleAppendInPlace").mutates(),
	main0(OpAppendSpreadInPlace, "APPEND_SPREAD_INPLACE").cost(costExpensive).handler("handleAppendSpreadInPlace").mutates(),
	main0(OpMapSetIntGeneral, "MAP_SET_INT_GENERAL").handler("handleMapSetIntGeneral").shim("HandleMapSetIntGeneral", "MapSetIntGeneral").mutates(),
	main0(OpGetStructFieldIndexGeneral, "GET_STRUCT_FIELD_INDEX_GENERAL").handler("handleGetStructFieldIndexGeneral").
		shim("HandleGetStructFieldIndexGeneral", "GetStructFieldIndexGeneral").pure(),
	main0(OpSetStructFieldIndexGeneral, "SET_STRUCT_FIELD_INDEX_GENERAL").handler("handleSetStructFieldIndexGeneral").
		shim("HandleSetStructFieldIndexGeneral", "SetStructFieldIndexGeneral").mutates(),
	main0(OpDerefSliceGetInt, "DEREF_SLICE_GET_INT").handler("handleDerefSliceGetInt").asm("handlerDerefSliceGetInt").shim("HandleDerefSliceGetInt", "DerefSliceGetInt").
		flags(SpecShimSuppressed).pure(),
	main0(OpDerefSliceSetInt, "DEREF_SLICE_SET_INT").handler("handleDerefSliceSetInt").asm("handlerDerefSliceSetInt").shim("HandleDerefSliceSetInt", "DerefSliceSetInt").
		flags(SpecShimSuppressed).mutates(),
	main0(OpGetStructFieldSliceLen, "GET_STRUCT_FIELD_SLICE_LEN").handler("handleGetStructFieldSliceLen").asm("handlerGetStructFieldSliceLen").
		shim("HandleGetStructFieldSliceLen", "GetStructFieldSliceLen").flags(SpecShimNarrow | SpecShimSuppressed).pure(),
	main0(OpGetStructFieldSliceIndexScalar, "GET_STRUCT_FIELD_SLICE_INDEX_SCALAR").handler("handleGetStructFieldSliceIndexScalar").asm("handlerGetStructFieldSliceIndexScalar").
		shim("HandleGetStructFieldSliceIndexScalar", "GetStructFieldSliceIndexScalar").flags(SpecShimSuppressed).pure(),
	main0(OpAppendStructFast, "APPEND_STRUCT_FAST").cost(costExpensive).handler("handleAppendStructFast").exit("handlerAppendStructFastExit", "exitAppendStructFast").mutates(),
	main0(OpAppendIntFast, "APPEND_INT_FAST").cost(costExpensive).handler("handleAppendInt").exit("handlerAppendIntFastExit", "exitAppendIntFast").mutates(),
	main0(OpAppendFloatFast, "APPEND_FLOAT_FAST").cost(costExpensive).handler("handleAppendFloat").exit("handlerAppendFloatFastExit", "exitAppendFloatFast").mutates(),
	main0(OpAppendStringFast, "APPEND_STRING_FAST").cost(costExpensive).handler("handleAppendString").exit("handlerAppendStringFastExit", "exitAppendStringFast").mutates(),
	main0(OpTypeSwitchCase, "TYPE_SWITCH_CASE").flags(SpecJump).pure(),
}
