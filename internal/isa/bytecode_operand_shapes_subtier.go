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

// operandAccess marks which operand bytes one kind of register access touches.
type operandAccess uint8

const (
	// accessNone marks an operation that touches neither operand register.
	accessNone operandAccess = 0

	// accessB marks operand B. The bit position matches the operand index, so mask turns the
	// set straight into a per-position table.
	accessB operandAccess = 1 << 1

	// accessC marks operand C.
	accessC operandAccess = 1 << 2

	// accessBC marks both operand B and operand C.
	accessBC operandAccess = accessB | accessC
)

// sub1Shape is one row of the general tier-1 table. Rows are written positionally in
// field order: b, c, reads, writes, flags.
type sub1Shape struct {
	// b is the role for operand B.
	b OperandRole

	// c is the role for operand C.
	c OperandRole

	// reads marks the operands that read the register they name.
	reads operandAccess

	// writes marks the operands that write the register they name.
	writes operandAccess

	// flags holds the whole-instruction shape flags.
	flags shapeFlag
}

// sub2Shape is one row of the tier-2 table where only operand C carries a value.
type sub2Shape struct {
	// c is the role for operand C.
	c OperandRole

	// reads marks whether C reads the register it names.
	reads operandAccess

	// writes marks whether C writes the register it names.
	writes operandAccess

	// flags holds the whole-instruction shape flags.
	flags shapeFlag
}

// dstSrcShape is one row of a tier-1 "writes B, reads C" table. Rows are written
// positionally: destination, source.
type dstSrcShape struct {
	// destination is the role of the operand B register written.
	destination OperandRole

	// source is the role of the operand C register read.
	source OperandRole
}

var (
	// sub1Shapes holds the tier-1 operations whose reads, writes and flags do not follow one
	// of the patterns the narrower tables below capture.
	sub1Shapes = map[SubOpcode]sub1Shape{
		SubOpSelect:                    {RoleImmediate, RoleRegInt, accessNone, accessC, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpCallBuiltin:               {RoleImmediate, RoleImmediate, accessNone, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpMathMod:                   {RoleRegFloat, RoleRegFloat, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpStrconvFormatInt:          {RoleRegString, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpMakeMethodExpr:            {RoleRegGeneral, RoleImmediate, accessNone, accessB, ShapeFlagFollowsExtension},
		SubOpMakeSliceInt:              {RoleRegSliceInt, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpMakeSliceFloat:            {roleRegSliceFloat, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceGetFloatDirect:       {RoleRegFloat, roleRegSliceFloat, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceSetFloatDirect:       {roleRegSliceFloat, RoleRegInt, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpMakeSliceString:           {roleRegSliceString, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceGetStringDirect:      {RoleRegString, roleRegSliceString, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceSetStringDirect:      {roleRegSliceString, RoleRegInt, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpMakeSliceBool:             {roleRegSliceBool, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceGetBoolDirect:        {RoleRegBool, roleRegSliceBool, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceSetBoolDirect:        {roleRegSliceBool, RoleRegInt, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpMakeSliceUint:             {roleRegSliceUint, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceGetUintDirect:        {RoleRegUint, roleRegSliceUint, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceSetUintDirect:        {roleRegSliceUint, RoleRegInt, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpLoadBoolConst:             {RoleRegBool, roleConstIndex, accessNone, accessB, 0},
		SubOpSliceOp:                   {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpGetGlobalWide:             {RoleRegDynamic, RoleKindMarker, accessNone, accessB, ShapeFlagFollowsExtension},
		SubOpSetGlobalWide:             {RoleRegDynamic, RoleKindMarker, accessB, accessNone, ShapeFlagFollowsExtension},
		SubOpResetSharedCell:           {RoleRegDynamic, RoleKindMarker, accessNone, accessNone, 0},
		SubOpWriteSharedCell:           {RoleRegDynamic, RoleKindMarker, accessB, accessNone, 0},
		SubOpRangeNext:                 {RoleRegGeneral, RoleRegDynamic, accessB, accessC, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpJump:                      {roleJumpOffsetLow, roleJumpOffsetHigh, accessNone, accessNone, ShapeFlagControlFlow | ShapeFlagTerminator},
		SubOpLoadIntConstSmall:         {RoleRegInt, RoleImmediate, accessNone, accessB, 0},
		SubOpLoadBool:                  {RoleRegInt, RoleImmediate, accessNone, accessB, 0},
		SubOpIncIntJumpLt:              {RoleRegInt, RoleRegInt, accessBC, accessB, ShapeFlagFollowsExtension | ShapeFlagControlFlow},
		SubOpLenStringLtJumpFalse:      {RoleRegInt, RoleRegString, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagControlFlow},
		SubOpLoadZero:                  {RoleRegDynamic, RoleKindMarker, accessNone, accessB, 0},
		SubOpMakeChannel:               {RoleRegGeneral, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpMapDelete:                 {RoleRegGeneral, RoleRegGeneral, accessBC, accessNone, 0},
		SubOpChannelReceive:            {RoleRegGeneral, RoleRegInt, accessB, accessC, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpGetMethod:                 {RoleRegGeneral, RoleRegGeneral, accessC, accessBC, ShapeFlagFollowsExtension},
		SubOpSpill:                     {RoleRegDynamic, RoleKindMarker, accessB, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpReload:                    {RoleRegDynamic, RoleKindMarker, accessNone, accessB, ShapeFlagFollowsExtension},
		SubOpSetStructFieldInt:         {RoleRegGeneral, RoleRegInt, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldUint:        {RoleRegGeneral, RoleRegUint, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldFloat:       {RoleRegGeneral, RoleRegFloat, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldBool:        {RoleRegGeneral, RoleRegBool, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldString:      {RoleRegGeneral, RoleRegString, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpLoadUintConstSmall:        {RoleRegUint, RoleImmediate, accessNone, accessB, 0},
		SubOpAppendUint:                {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendInt:                 {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendString:              {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendFloat:               {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendBool:                {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpStarAppendByteFast:        {RoleRegGeneral, RoleRegUint, accessBC, accessNone, 0},
		SubOpStarAppendByteSpread:      {RoleRegGeneral, RoleRegGeneral, accessBC, accessNone, 0},
		SubOpIncStructFieldInt:         {RoleRegGeneral, roleFieldIndex, accessB, accessNone, 0},
		SubOpDecStructFieldInt:         {RoleRegGeneral, roleFieldIndex, accessB, accessNone, 0},
		SubOpIncStructFieldUint:        {RoleRegGeneral, roleFieldIndex, accessB, accessNone, 0},
		SubOpDecStructFieldUint:        {RoleRegGeneral, roleFieldIndex, accessB, accessNone, 0},
		SubOpMakeSliceByte:             {roleRegSliceByte, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceGetByteDirect:        {RoleRegUint, roleRegSliceByte, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceSetByteDirect:        {roleRegSliceByte, RoleRegInt, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSliceByteSlice:            {roleRegSliceByte, roleRegSliceByte, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpEqUintConstJumpFalse:      {RoleRegUint, RoleImmediate, accessB, accessNone, ShapeFlagFollowsExtension | ShapeFlagControlFlow},
		SubOpSimdDotProductFloat64:     {RoleRegFloat, roleRegSliceFloat, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSimdSumSliceFloat64:       {RoleRegFloat, roleRegSliceFloat, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSimdAddSliceFloat64:       {roleRegSliceFloat, roleRegSliceFloat, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSimdScaleSliceFloat64:     {roleRegSliceFloat, RoleRegFloat, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpAppendSliceIntDirect:      {RoleRegSliceInt, RoleRegSliceInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendSliceFloatDirect:    {roleRegSliceFloat, roleRegSliceFloat, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendSliceStringDirect:   {roleRegSliceString, roleRegSliceString, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendSliceBoolDirect:     {roleRegSliceBool, roleRegSliceBool, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendSliceUintDirect:     {roleRegSliceUint, roleRegSliceUint, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendSliceByteDirect:     {roleRegSliceByte, roleRegSliceByte, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceSliceIntDirect:       {RoleRegSliceInt, RoleRegSliceInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceSliceFloatDirect:     {roleRegSliceFloat, roleRegSliceFloat, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceSliceStringDirect:    {roleRegSliceString, roleRegSliceString, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceSliceBoolDirect:      {roleRegSliceBool, roleRegSliceBool, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceSliceUintDirect:      {roleRegSliceUint, roleRegSliceUint, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpCopySliceIntDirect:        {RoleRegSliceInt, RoleRegSliceInt, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpCopySliceFloatDirect:      {roleRegSliceFloat, roleRegSliceFloat, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpCopySliceStringDirect:     {roleRegSliceString, roleRegSliceString, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpCopySliceBoolDirect:       {roleRegSliceBool, roleRegSliceBool, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpCopySliceUintDirect:       {roleRegSliceUint, roleRegSliceUint, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpCopySliceByteDirect:       {roleRegSliceByte, roleRegSliceByte, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpAppendUintInPlace:         {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendByteSpreadInPlace:   {RoleRegGeneral, RoleRegGeneral, accessBC, accessB, 0},
		SubOpSetStructFieldSliceInt:    {RoleRegGeneral, RoleRegSliceInt, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldSliceFloat:  {RoleRegGeneral, roleRegSliceFloat, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldSliceUint:   {RoleRegGeneral, roleRegSliceUint, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldSliceString: {RoleRegGeneral, roleRegSliceString, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldSliceBool:   {RoleRegGeneral, roleRegSliceBool, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldSliceByte:   {RoleRegGeneral, roleRegSliceByte, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpMakeSliceHeap:             {RoleRegDynamic, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpTypeSwitchJump:            {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagControlFlow | ShapeFlagTerminator},
		SubOpEqInterfaceStrict:         {RoleRegGeneral, RoleRegGeneral, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpNeInterfaceStrict:         {RoleRegGeneral, RoleRegGeneral, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
	}

	// sub1DstSrcShapes holds the tier-1 operations that write B and read C, both plain
	// register operands with no extension word.
	sub1DstSrcShapes = map[SubOpcode]dstSrcShape{
		SubOpMathSin:                    {RoleRegFloat, RoleRegFloat},
		SubOpMathCos:                    {RoleRegFloat, RoleRegFloat},
		SubOpMathExp:                    {RoleRegFloat, RoleRegFloat},
		SubOpMathTan:                    {RoleRegFloat, RoleRegFloat},
		SubOpStrconvFormatBool:          {RoleRegString, RoleRegBool},
		SubOpStrconvItoa:                {RoleRegString, RoleRegInt},
		SubOpRealComplex:                {RoleRegFloat, RoleRegComplex},
		SubOpImagComplex:                {RoleRegFloat, RoleRegComplex},
		SubOpBytesToString:              {RoleRegString, RoleRegGeneral},
		SubOpCap:                        {RoleRegInt, RoleRegGeneral},
		SubOpNegComplex:                 {RoleRegComplex, RoleRegComplex},
		SubOpMoveComplex:                {RoleRegComplex, RoleRegComplex},
		SubOpLenSliceIntDirect:          {RoleRegInt, RoleRegSliceInt},
		SubOpLenSliceFloatDirect:        {RoleRegInt, roleRegSliceFloat},
		SubOpLenSliceStringDirect:       {RoleRegInt, roleRegSliceString},
		SubOpLenSliceBoolDirect:         {RoleRegInt, roleRegSliceBool},
		SubOpLenSliceUintDirect:         {RoleRegInt, roleRegSliceUint},
		SubOpBoxSliceInt:                {RoleRegGeneral, RoleRegSliceInt},
		SubOpMoveInt:                    {RoleRegInt, RoleRegInt},
		SubOpMoveFloat:                  {RoleRegFloat, RoleRegFloat},
		SubOpMoveString:                 {RoleRegString, RoleRegString},
		SubOpMoveBool:                   {RoleRegBool, RoleRegBool},
		SubOpMoveUint:                   {RoleRegUint, RoleRegUint},
		SubOpMoveIntToGeneral:           {RoleRegGeneral, RoleRegInt},
		SubOpMoveGeneralToInt:           {RoleRegInt, RoleRegGeneral},
		SubOpMoveFloatToGeneral:         {RoleRegGeneral, RoleRegFloat},
		SubOpMoveGeneralToFloat:         {RoleRegFloat, RoleRegGeneral},
		SubOpMoveStringToGeneral:        {RoleRegGeneral, RoleRegString},
		SubOpMoveGeneralToString:        {RoleRegString, RoleRegGeneral},
		SubOpNegInt:                     {RoleRegInt, RoleRegInt},
		SubOpNegFloat:                   {RoleRegFloat, RoleRegFloat},
		SubOpBitNot:                     {RoleRegInt, RoleRegInt},
		SubOpBitNotUint:                 {RoleRegUint, RoleRegUint},
		SubOpIntToFloat:                 {RoleRegFloat, RoleRegInt},
		SubOpFloatToInt:                 {RoleRegInt, RoleRegFloat},
		SubOpNot:                        {RoleRegInt, RoleRegInt},
		SubOpBoolToInt:                  {RoleRegInt, RoleRegBool},
		SubOpIntToBool:                  {RoleRegBool, RoleRegInt},
		SubOpIntToUint:                  {RoleRegUint, RoleRegInt},
		SubOpUintToInt:                  {RoleRegInt, RoleRegUint},
		SubOpUintToFloat:                {RoleRegFloat, RoleRegUint},
		SubOpFloatToUint:                {RoleRegUint, RoleRegFloat},
		SubOpMathSqrt:                   {RoleRegFloat, RoleRegFloat},
		SubOpMathAbs:                    {RoleRegFloat, RoleRegFloat},
		SubOpMathFloor:                  {RoleRegFloat, RoleRegFloat},
		SubOpMathCeil:                   {RoleRegFloat, RoleRegFloat},
		SubOpMathTrunc:                  {RoleRegFloat, RoleRegFloat},
		SubOpMathRound:                  {RoleRegFloat, RoleRegFloat},
		SubOpLenString:                  {RoleRegInt, RoleRegString},
		SubOpRangeInit:                  {RoleRegGeneral, RoleRegGeneral},
		SubOpEqInterfaceNil:             {RoleRegInt, RoleRegGeneral},
		SubOpNeInterfaceNil:             {RoleRegInt, RoleRegGeneral},
		SubOpRuneToString:               {RoleRegString, RoleRegInt},
		SubOpStrToUpper:                 {RoleRegString, RoleRegString},
		SubOpStrToLower:                 {RoleRegString, RoleRegString},
		SubOpStrTrimSpace:               {RoleRegString, RoleRegString},
		SubOpLen:                        {RoleRegInt, RoleRegGeneral},
		SubOpStringToBytes:              {RoleRegGeneral, RoleRegString},
		SubOpUnsafeStringData:           {RoleRegGeneral, RoleRegString},
		SubOpUnsafeSliceData:            {RoleRegGeneral, RoleRegGeneral},
		SubOpAddUintConst:               {RoleRegUint, RoleRegUint},
		SubOpSubUintConst:               {RoleRegUint, RoleRegUint},
		SubOpBitAndUintConst:            {RoleRegUint, RoleRegUint},
		SubOpLenSliceByteDirect:         {RoleRegInt, roleRegSliceByte},
		SubOpBoxSliceByte:               {RoleRegGeneral, roleRegSliceByte},
		SubOpSliceByteToString:          {RoleRegString, roleRegSliceByte},
		SubOpAdoptGeneralToSlicesFloat:  {roleRegSliceFloat, RoleRegGeneral},
		SubOpAdoptGeneralToSlicesInt:    {RoleRegSliceInt, RoleRegGeneral},
		SubOpAdoptGeneralToSlicesString: {roleRegSliceString, RoleRegGeneral},
		SubOpAdoptGeneralToSlicesBool:   {roleRegSliceBool, RoleRegGeneral},
		SubOpAdoptGeneralToSlicesUint:   {roleRegSliceUint, RoleRegGeneral},
		SubOpAdoptGeneralToSlicesByte:   {roleRegSliceByte, RoleRegGeneral},
		SubOpBoxSliceFloat:              {RoleRegGeneral, roleRegSliceFloat},
		SubOpBoxSliceString:             {RoleRegGeneral, roleRegSliceString},
		SubOpBoxSliceBool:               {RoleRegGeneral, roleRegSliceBool},
		SubOpBoxSliceUint:               {RoleRegGeneral, roleRegSliceUint},
		SubOpMoveSliceInt:               {RoleRegSliceInt, RoleRegSliceInt},
		SubOpMoveSliceFloat:             {roleRegSliceFloat, roleRegSliceFloat},
		SubOpMoveSliceString:            {roleRegSliceString, roleRegSliceString},
		SubOpMoveSliceBool:              {roleRegSliceBool, roleRegSliceBool},
		SubOpMoveSliceUint:              {roleRegSliceUint, roleRegSliceUint},
		SubOpMoveSliceByte:              {roleRegSliceByte, roleRegSliceByte},
		SubOpCapSliceIntDirect:          {RoleRegInt, RoleRegSliceInt},
		SubOpCapSliceFloatDirect:        {RoleRegInt, roleRegSliceFloat},
		SubOpCapSliceStringDirect:       {RoleRegInt, roleRegSliceString},
		SubOpCapSliceBoolDirect:         {RoleRegInt, roleRegSliceBool},
		SubOpCapSliceUintDirect:         {RoleRegInt, roleRegSliceUint},
		SubOpCapSliceByteDirect:         {RoleRegInt, roleRegSliceByte},
		SubOpRoundFloat32:               {RoleRegFloat, RoleRegFloat},
		SubOpRoundComplex64:             {RoleRegComplex, RoleRegComplex},
	}

	// sub1DstSrcExtensionShapes holds the tier-1 "B = f(C)" operations whose layout or index
	// travels in the following OpExt word.
	sub1DstSrcExtensionShapes = map[SubOpcode]dstSrcShape{
		SubOpGetStructFieldInt:         {RoleRegInt, RoleRegGeneral},
		SubOpGetStructFieldUint:        {RoleRegUint, RoleRegGeneral},
		SubOpGetStructFieldFloat:       {RoleRegFloat, RoleRegGeneral},
		SubOpGetStructFieldBool:        {RoleRegBool, RoleRegGeneral},
		SubOpGetStructFieldString:      {RoleRegString, RoleRegGeneral},
		SubOpGetStructFieldSliceInt:    {RoleRegSliceInt, RoleRegGeneral},
		SubOpGetStructFieldSliceFloat:  {roleRegSliceFloat, RoleRegGeneral},
		SubOpGetStructFieldSliceUint:   {roleRegSliceUint, RoleRegGeneral},
		SubOpGetStructFieldSliceString: {roleRegSliceString, RoleRegGeneral},
		SubOpGetStructFieldSliceBool:   {roleRegSliceBool, RoleRegGeneral},
		SubOpGetStructFieldSliceByte:   {roleRegSliceByte, RoleRegGeneral},
	}

	// sub1ConstJumpShapes holds the fused compare-against-constant-and-branch operations,
	// keyed by sub-opcode with the role of the test register read from B.
	sub1ConstJumpShapes = map[SubOpcode]OperandRole{
		SubOpEqStringConstJumpFalse: RoleRegString,
		SubOpLeIntConstJumpFalse:    RoleRegInt,
		SubOpLtIntConstJumpFalse:    RoleRegInt,
		SubOpEqIntConstJumpFalse:    RoleRegInt,
		SubOpEqIntConstJumpTrue:     RoleRegInt,
		SubOpGeIntConstJumpFalse:    RoleRegInt,
		SubOpGtIntConstJumpFalse:    RoleRegInt,
	}

	// sub1RegRegJumpShapes holds the fused compare-and-branch operations, keyed by
	// sub-opcode with the role shared by both operand registers.
	sub1RegRegJumpShapes = map[SubOpcode]OperandRole{
		SubOpLtIntJumpFalse: RoleRegInt,
		SubOpLeIntJumpFalse: RoleRegInt,
		SubOpGtIntJumpFalse: RoleRegInt,
		SubOpGeIntJumpFalse: RoleRegInt,
		SubOpEqIntJumpFalse: RoleRegInt,
		SubOpNeIntJumpFalse: RoleRegInt,
	}

	// sub1CallSiteShapes holds the call operations, whose whole payload is the 16-bit
	// call-site index, keyed by sub-opcode with the extra flags each one carries.
	sub1CallSiteShapes = map[SubOpcode]shapeFlag{
		SubOpCall:                 0,
		SubOpCallScalar:           0,
		SubOpCallNative:           0,
		SubOpCallIIFE:             0,
		SubOpTailCall:             ShapeFlagControlFlow | ShapeFlagTerminator,
		SubOpCallMethod:           ShapeFlagFollowsExtension,
		SubOpCallMethodInlineable: ShapeFlagFollowsExtension,
	}

	// sub2Shapes holds the tier-2 operations.
	sub2Shapes = map[SubOpcodeTier2]sub2Shape{
		SubOpTier2MakeInterfaceMethodExpr: {RoleRegGeneral, accessNone, accessC, 0},
		SubOpTier2MakeMap:                 {RoleRegGeneral, accessNone, accessC, ShapeFlagFollowsExtension},
		SubOpTier2RangeCheckUintJumpFalse: {RoleRegUint, accessC, accessNone, ShapeFlagFollowsExtension | ShapeFlagControlFlow},
		SubOpTier2AllocStructLiteral:      {RoleRegGeneral, accessNone, accessC, ShapeFlagFollowsExtension},
		SubOpTier2IncInt:                  {RoleRegInt, accessNone, accessC, 0},
		SubOpTier2DecInt:                  {RoleRegInt, accessNone, accessC, 0},
		SubOpTier2IncUint:                 {RoleRegUint, accessNone, accessC, 0},
		SubOpTier2DecUint:                 {RoleRegUint, accessNone, accessC, 0},
		SubOpTier2Panic:                   {RoleRegGeneral, accessC, accessNone, 0},
		SubOpTier2Recover:                 {RoleRegGeneral, accessNone, accessC, 0},
		SubOpTier2SetZero:                 {RoleRegGeneral, accessC, accessNone, 0},
		SubOpTier2ChannelClose:            {RoleRegGeneral, accessC, accessNone, 0},
		SubOpTier2LoadNil:                 {RoleRegGeneral, accessNone, accessC, 0},
		SubOpTier2Return:                  {RoleImmediate, accessNone, accessNone, ShapeFlagControlFlow | ShapeFlagTerminator},
		SubOpTier2SyncClosureUpvalues:     {RoleRegGeneral, accessC, accessNone, ShapeFlagOpaqueWrites},
	}

	// sub3Shapes holds the tier-3 operations. Every byte is a discriminator, so a row
	// carries nothing but the whole-instruction flags.
	sub3Shapes = map[SubOpcodeTier3]shapeFlag{
		SubOpTier3Nop:              0,
		SubOpTier3ReturnVoid:       ShapeFlagControlFlow | ShapeFlagTerminator,
		SubOpTier3SyncIIFEUpvalues: ShapeFlagOpaqueWrites,
	}
)

// mask expands the access set into the per-operand table the shape descriptors carry.
//
// Returns [NumInstructionOperands]bool which marks each operand the access touches.
func (a operandAccess) mask() [NumInstructionOperands]bool {
	return [NumInstructionOperands]bool{false, a&accessB != 0, a&accessC != 0}
}

// touches reports whether the access covers a given operand.
//
// Takes other (operandAccess) which is the operand to test for.
//
// Returns bool which is true when the access covers that operand.
func (a operandAccess) touches(other operandAccess) bool {
	return a&other != 0
}

// populateSubTierShapes describes every tier-1, tier-2 and tier-3 operation.
//
// Until the tier audit these rows shared the single OpDrillTier1 wildcard, so the
// verifier treated every sub-tier instruction as opaque: no bank checking and no
// register-index bounds checking. The roles below were derived from each handler's
// register accesses and checked against the handlers that carry a non-register operand.
//
// The rows live in the package-level tables rather than in a run of calls here: one entry
// per operation keeps the sub-tier map readable, and a repeated sub-opcode becomes a
// duplicate map key, which is a compile error rather than a silent overwrite.
func populateSubTierShapes() {
	for op, shape := range sub1Shapes {
		describedSub1(op, shape.b, shape.c, shape.reads.mask(), shape.writes.mask(), shape.flags)
	}
	for op, shape := range sub1DstSrcShapes {
		describeSub1DstSrc(op, shape.destination, shape.source)
	}
	for op, shape := range sub1DstSrcExtensionShapes {
		describeSub1DstSrcExtension(op, shape.destination, shape.source)
	}
	for op, source := range sub1ConstJumpShapes {
		describeSub1ConstJump(op, source)
	}
	for op, source := range sub1RegRegJumpShapes {
		describeSub1RegRegJump(op, source)
	}
	for op, flags := range sub1CallSiteShapes {
		describeCallSite(op, flags)
	}
	for op, shape := range sub2Shapes {
		describedSub2(op, shape.c, shape.reads.touches(accessC), shape.writes.touches(accessC), shape.flags)
	}
	for op, flags := range sub3Shapes {
		describedSub3(op, flags)
	}
}
