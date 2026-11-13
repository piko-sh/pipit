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
		SubOpSelect:                                   {RoleImmediate, RoleRegInt, accessNone, accessC, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpCallBuiltin:                              {RoleImmediate, RoleImmediate, accessNone, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpMathMod:                                  {RoleRegFloat, RoleRegFloat, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpStrconvFormatInt:                         {RoleRegString, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpMakeMethodExpression:                     {RoleRegGeneral, RoleImmediate, accessNone, accessB, ShapeFlagFollowsExtension},
		SubOpMakeSliceInt64:                           {RoleRegSliceInt, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpMakeSliceFloat64:                         {roleRegSliceFloat, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpGetSliceFloat64Element:                   {RoleRegFloat, roleRegSliceFloat, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSetSliceFloat64Element:                   {roleRegSliceFloat, RoleRegInt, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpMakeSliceString:                          {roleRegSliceString, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpGetSliceStringElement:                    {RoleRegString, roleRegSliceString, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSetSliceStringElement:                    {roleRegSliceString, RoleRegInt, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpMakeSliceBool:                            {roleRegSliceBool, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpGetSliceBoolElement:                      {RoleRegBool, roleRegSliceBool, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSetSliceBoolElement:                      {roleRegSliceBool, RoleRegInt, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpMakeSliceUint64:                          {roleRegSliceUint, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpGetSliceUint64Element:                    {RoleRegUint, roleRegSliceUint, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSetSliceUint64Element:                    {roleRegSliceUint, RoleRegInt, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpLoadBoolConstant:                         {RoleRegBool, roleConstIndex, accessNone, accessB, 0},
		SubOpSliceGeneral:                             {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpGetGlobalWide:                            {RoleRegDynamic, RoleKindMarker, accessNone, accessB, ShapeFlagFollowsExtension},
		SubOpSetGlobalWide:                            {RoleRegDynamic, RoleKindMarker, accessB, accessNone, ShapeFlagFollowsExtension},
		SubOpResetSharedCell:                          {RoleRegDynamic, RoleKindMarker, accessNone, accessNone, 0},
		SubOpSetSharedCell:                            {RoleRegDynamic, RoleKindMarker, accessB, accessNone, 0},
		SubOpRangeNext:                                {RoleRegGeneral, RoleRegDynamic, accessB, accessC, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpJump:                                     {roleJumpOffsetLow, roleJumpOffsetHigh, accessNone, accessNone, ShapeFlagControlFlow | ShapeFlagTerminator},
		SubOpLoadInt64Immediate:                       {RoleRegInt, RoleImmediate, accessNone, accessB, 0},
		SubOpLoadBoolImmediateAsInt64:                 {RoleRegInt, RoleImmediate, accessNone, accessB, 0},
		SubOpIncrementInt64ThenJumpIfLessThan:         {RoleRegInt, RoleRegInt, accessBC, accessB, ShapeFlagFollowsExtension | ShapeFlagControlFlow},
		SubOpIncrementInt64ThenJumpIfLessThanConstant: {RoleRegInt, roleConstIndex, accessB, accessB, ShapeFlagFollowsExtension | ShapeFlagControlFlow},
		SubOpIncrementInt64ThenJumpIfLessOrEqual:      {RoleRegInt, RoleRegInt, accessBC, accessB, ShapeFlagFollowsExtension | ShapeFlagControlFlow},
		SubOpLessThanLengthStringThenJumpIfFalse:      {RoleRegInt, RoleRegString, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagControlFlow},
		SubOpLoadZero:                                 {RoleRegDynamic, RoleKindMarker, accessNone, accessB, 0},
		SubOpMakeChannel:                              {RoleRegGeneral, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpDeleteFromMap:                            {RoleRegGeneral, RoleRegGeneral, accessBC, accessNone, 0},
		SubOpReceiveFromChannel:                       {RoleRegGeneral, RoleRegInt, accessB, accessC, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpBindMethodByName:                         {RoleRegGeneral, RoleRegGeneral, accessC, accessBC, ShapeFlagFollowsExtension},
		SubOpSpill:                                    {RoleRegDynamic, RoleKindMarker, accessB, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpReload:                                   {RoleRegDynamic, RoleKindMarker, accessNone, accessB, ShapeFlagFollowsExtension},
		SubOpSetStructFieldInt64Wide:                  {RoleRegGeneral, RoleRegInt, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldUint64Wide:                 {RoleRegGeneral, RoleRegUint, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldFloat64Wide:                {RoleRegGeneral, RoleRegFloat, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldBoolWide:                   {RoleRegGeneral, RoleRegBool, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldStringWide:                 {RoleRegGeneral, RoleRegString, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpLoadUint64Immediate:                      {RoleRegUint, RoleImmediate, accessNone, accessB, 0},
		SubOpAppendUint64Wide:                         {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendInt64Wide:                          {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendStringWide:                         {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendFloat64Wide:                        {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendBoolWide:                           {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendByteViaPointer:                     {RoleRegGeneral, RoleRegUint, accessBC, accessNone, 0},
		SubOpAppendSpreadByteViaPointer:               {RoleRegGeneral, RoleRegGeneral, accessBC, accessNone, 0},
		SubOpIncrementStructFieldInt64:                {RoleRegGeneral, roleFieldIndex, accessB, accessNone, 0},
		SubOpDecrementStructFieldInt64:                {RoleRegGeneral, roleFieldIndex, accessB, accessNone, 0},
		SubOpIncrementStructFieldUint64:               {RoleRegGeneral, roleFieldIndex, accessB, accessNone, 0},
		SubOpDecrementStructFieldUint64:               {RoleRegGeneral, roleFieldIndex, accessB, accessNone, 0},
		SubOpMakeSliceByte:                            {roleRegSliceByte, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpGetSliceByteElement:                      {RoleRegUint, roleRegSliceByte, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSetSliceByteElement:                      {roleRegSliceByte, RoleRegInt, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetSliceByteElementRun:                   {roleRegSliceByte, RoleRegInt, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpGetSliceByteElementInt64:                 {RoleRegInt, roleRegSliceByte, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpGetStructFieldUint64Run:                  {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpGetElementFieldUint64Run:                 {RoleRegGeneral, RoleRegInt, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpSliceSliceByte:                           {roleRegSliceByte, roleRegSliceByte, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpEqualUint64ImmediateThenJumpIfFalse:      {RoleRegUint, RoleImmediate, accessB, accessNone, ShapeFlagFollowsExtension | ShapeFlagControlFlow},
		SubOpSimdDotProductFloat64:                    {RoleRegFloat, roleRegSliceFloat, accessBC, accessB, ShapeFlagFollowsExtension},
		SubOpSimdSumFloat64:                           {RoleRegFloat, roleRegSliceFloat, accessBC, accessB, ShapeFlagFollowsExtension},
		SubOpSimdAddFloat64:                           {roleRegSliceFloat, roleRegSliceFloat, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSimdScaleFloat64:                         {roleRegSliceFloat, RoleRegFloat, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpAppendSliceInt64:                         {RoleRegSliceInt, RoleRegSliceInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendSliceFloat64:                       {roleRegSliceFloat, roleRegSliceFloat, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendSliceString:                        {roleRegSliceString, roleRegSliceString, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendSliceBool:                          {roleRegSliceBool, roleRegSliceBool, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendSliceUint64:                        {roleRegSliceUint, roleRegSliceUint, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendSliceByte:                          {roleRegSliceByte, roleRegSliceByte, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceSliceInt64:                          {RoleRegSliceInt, RoleRegSliceInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceSliceFloat64:                        {roleRegSliceFloat, roleRegSliceFloat, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceSliceString:                         {roleRegSliceString, roleRegSliceString, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceSliceBool:                           {roleRegSliceBool, roleRegSliceBool, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpSliceSliceUint64:                         {roleRegSliceUint, roleRegSliceUint, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpCopySliceInt64:                           {RoleRegSliceInt, RoleRegSliceInt, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpCopySliceFloat64:                         {roleRegSliceFloat, roleRegSliceFloat, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpCopySliceString:                          {roleRegSliceString, roleRegSliceString, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpCopySliceBool:                            {roleRegSliceBool, roleRegSliceBool, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpCopySliceUint64:                          {roleRegSliceUint, roleRegSliceUint, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpCopySliceByte:                            {roleRegSliceByte, roleRegSliceByte, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpAppendUint64WideInPlace:                  {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpAppendSpreadByteInPlace:                  {RoleRegGeneral, RoleRegGeneral, accessBC, accessB, 0},
		SubOpSetStructFieldSliceInt64:                 {RoleRegGeneral, RoleRegSliceInt, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldSliceFloat64:               {RoleRegGeneral, roleRegSliceFloat, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldSliceUint64:                {RoleRegGeneral, roleRegSliceUint, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldSliceString:                {RoleRegGeneral, roleRegSliceString, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldSliceBool:                  {RoleRegGeneral, roleRegSliceBool, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpSetStructFieldSliceByte:                  {RoleRegGeneral, roleRegSliceByte, accessBC, accessNone, ShapeFlagFollowsExtension},
		SubOpMakeSliceHeap:                            {RoleRegDynamic, RoleRegInt, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpTypeSwitchJump:                           {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagControlFlow | ShapeFlagTerminator},
		SubOpEqualInterfaceStrict:                     {RoleRegGeneral, RoleRegGeneral, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
		SubOpAddressField:                             {RoleRegGeneral, RoleRegGeneral, accessC, accessB, ShapeFlagFollowsExtension},
		SubOpNotEqualInterfaceStrict:                  {RoleRegGeneral, RoleRegGeneral, accessBC, accessNone, ShapeFlagFollowsExtension | ShapeFlagOpaqueWrites},
	}

	// sub1DstSrcShapes holds the tier-1 operations that write B and read C, both plain
	// register operands with no extension word.
	sub1DstSrcShapes = map[SubOpcode]dstSrcShape{
		SubOpMathSin:                  {RoleRegFloat, RoleRegFloat},
		SubOpMathCos:                  {RoleRegFloat, RoleRegFloat},
		SubOpMathExp:                  {RoleRegFloat, RoleRegFloat},
		SubOpMathTan:                  {RoleRegFloat, RoleRegFloat},
		SubOpStrconvFormatBool:        {RoleRegString, RoleRegBool},
		SubOpStrconvItoa:              {RoleRegString, RoleRegInt},
		SubOpRealPartComplex128:       {RoleRegFloat, RoleRegComplex},
		SubOpImaginaryPartComplex128:  {RoleRegFloat, RoleRegComplex},
		SubOpConvertBytesToString:     {RoleRegString, RoleRegGeneral},
		SubOpCapacityGeneral:          {RoleRegInt, RoleRegGeneral},
		SubOpNegateComplex128:         {RoleRegComplex, RoleRegComplex},
		SubOpMoveComplex128:           {RoleRegComplex, RoleRegComplex},
		SubOpLengthSliceInt64:         {RoleRegInt, RoleRegSliceInt},
		SubOpLengthSliceFloat64:       {RoleRegInt, roleRegSliceFloat},
		SubOpLengthSliceString:        {RoleRegInt, roleRegSliceString},
		SubOpLengthSliceBool:          {RoleRegInt, roleRegSliceBool},
		SubOpLengthSliceUint64:        {RoleRegInt, roleRegSliceUint},
		SubOpBoxSliceInt64:            {RoleRegGeneral, RoleRegSliceInt},
		SubOpMoveInt64:                {RoleRegInt, RoleRegInt},
		SubOpMoveFloat64:              {RoleRegFloat, RoleRegFloat},
		SubOpMoveString:               {RoleRegString, RoleRegString},
		SubOpMoveBool:                 {RoleRegBool, RoleRegBool},
		SubOpMoveUint64:               {RoleRegUint, RoleRegUint},
		SubOpBoxInt64:                 {RoleRegGeneral, RoleRegInt},
		SubOpUnboxInt64:               {RoleRegInt, RoleRegGeneral},
		SubOpBoxFloat64:               {RoleRegGeneral, RoleRegFloat},
		SubOpUnboxFloat64:             {RoleRegFloat, RoleRegGeneral},
		SubOpBoxString:                {RoleRegGeneral, RoleRegString},
		SubOpUnboxString:              {RoleRegString, RoleRegGeneral},
		SubOpNegateInt64:              {RoleRegInt, RoleRegInt},
		SubOpNegateFloat64:            {RoleRegFloat, RoleRegFloat},
		SubOpBitNotInt64:              {RoleRegInt, RoleRegInt},
		SubOpBitNotUint64:             {RoleRegUint, RoleRegUint},
		SubOpConvertInt64ToFloat64:    {RoleRegFloat, RoleRegInt},
		SubOpConvertFloat64ToInt64:    {RoleRegInt, RoleRegFloat},
		SubOpNotInt64:                 {RoleRegInt, RoleRegInt},
		SubOpConvertBoolToInt64:       {RoleRegInt, RoleRegBool},
		SubOpConvertInt64ToBool:       {RoleRegBool, RoleRegInt},
		SubOpConvertInt64ToUint64:     {RoleRegUint, RoleRegInt},
		SubOpConvertUint64ToInt64:     {RoleRegInt, RoleRegUint},
		SubOpConvertUint64ToFloat64:   {RoleRegFloat, RoleRegUint},
		SubOpConvertFloat64ToUint64:   {RoleRegUint, RoleRegFloat},
		SubOpMathSqrt:                 {RoleRegFloat, RoleRegFloat},
		SubOpMathAbs:                  {RoleRegFloat, RoleRegFloat},
		SubOpMathFloor:                {RoleRegFloat, RoleRegFloat},
		SubOpMathCeil:                 {RoleRegFloat, RoleRegFloat},
		SubOpMathTrunc:                {RoleRegFloat, RoleRegFloat},
		SubOpMathRound:                {RoleRegFloat, RoleRegFloat},
		SubOpLengthString:             {RoleRegInt, RoleRegString},
		SubOpRangeInitialise:          {RoleRegGeneral, RoleRegGeneral},
		SubOpEqualInterfaceNil:        {RoleRegInt, RoleRegGeneral},
		SubOpNotEqualInterfaceNil:     {RoleRegInt, RoleRegGeneral},
		SubOpConvertRuneToString:      {RoleRegString, RoleRegInt},
		SubOpStringsToUpper:           {RoleRegString, RoleRegString},
		SubOpStringsToLower:           {RoleRegString, RoleRegString},
		SubOpStringsTrimSpace:         {RoleRegString, RoleRegString},
		SubOpLengthGeneral:            {RoleRegInt, RoleRegGeneral},
		SubOpConvertStringToBytes:     {RoleRegGeneral, RoleRegString},
		SubOpUnsafeStringData:         {RoleRegGeneral, RoleRegString},
		SubOpUnsafeSliceData:          {RoleRegGeneral, RoleRegGeneral},
		SubOpLengthSliceByte:          {RoleRegInt, roleRegSliceByte},
		SubOpBoxSliceByte:             {RoleRegGeneral, roleRegSliceByte},
		SubOpConvertSliceByteToString: {RoleRegString, roleRegSliceByte},
		SubOpUnboxSliceFloat64:        {roleRegSliceFloat, RoleRegGeneral},
		SubOpUnboxSliceInt64:          {RoleRegSliceInt, RoleRegGeneral},
		SubOpUnboxSliceString:         {roleRegSliceString, RoleRegGeneral},
		SubOpUnboxSliceBool:           {roleRegSliceBool, RoleRegGeneral},
		SubOpUnboxSliceUint64:         {roleRegSliceUint, RoleRegGeneral},
		SubOpUnboxSliceByte:           {roleRegSliceByte, RoleRegGeneral},
		SubOpBoxSliceFloat64:          {RoleRegGeneral, roleRegSliceFloat},
		SubOpBoxSliceString:           {RoleRegGeneral, roleRegSliceString},
		SubOpBoxSliceBool:             {RoleRegGeneral, roleRegSliceBool},
		SubOpBoxSliceUint64:           {RoleRegGeneral, roleRegSliceUint},
		SubOpMoveSliceInt64:           {RoleRegSliceInt, RoleRegSliceInt},
		SubOpMoveSliceFloat64:         {roleRegSliceFloat, roleRegSliceFloat},
		SubOpMoveSliceString:          {roleRegSliceString, roleRegSliceString},
		SubOpMoveSliceBool:            {roleRegSliceBool, roleRegSliceBool},
		SubOpMoveSliceUint64:          {roleRegSliceUint, roleRegSliceUint},
		SubOpMoveSliceByte:            {roleRegSliceByte, roleRegSliceByte},
		SubOpCapacitySliceInt64:       {RoleRegInt, RoleRegSliceInt},
		SubOpCapacitySliceFloat64:     {RoleRegInt, roleRegSliceFloat},
		SubOpCapacitySliceString:      {RoleRegInt, roleRegSliceString},
		SubOpCapacitySliceBool:        {RoleRegInt, roleRegSliceBool},
		SubOpCapacitySliceUint64:      {RoleRegInt, roleRegSliceUint},
		SubOpCapacitySliceByte:        {RoleRegInt, roleRegSliceByte},
		SubOpRoundToFloat32:           {RoleRegFloat, RoleRegFloat},
		SubOpRoundToComplex64:         {RoleRegComplex, RoleRegComplex},
	}

	// sub1DstSrcExtensionShapes holds the tier-1 "B = f(C)" operations whose layout,
	// constant-pool index or other index travels in the following OpExtension word.
	sub1DstSrcExtensionShapes = map[SubOpcode]dstSrcShape{
		SubOpAddUint64Constant:          {RoleRegUint, RoleRegUint},
		SubOpSubtractUint64Constant:     {RoleRegUint, RoleRegUint},
		SubOpAddUint32Constant:          {RoleRegUint, RoleRegUint},
		SubOpSubtractUint32Constant:     {RoleRegUint, RoleRegUint},
		SubOpBitAndUint64Constant:       {RoleRegUint, RoleRegUint},
		SubOpGetStructFieldInt64Wide:    {RoleRegInt, RoleRegGeneral},
		SubOpGetStructFieldUint64Wide:   {RoleRegUint, RoleRegGeneral},
		SubOpGetStructFieldFloat64Wide:  {RoleRegFloat, RoleRegGeneral},
		SubOpGetStructFieldBoolWide:     {RoleRegBool, RoleRegGeneral},
		SubOpGetStructFieldStringWide:   {RoleRegString, RoleRegGeneral},
		SubOpGetStructFieldSliceInt64:   {RoleRegSliceInt, RoleRegGeneral},
		SubOpGetStructFieldSliceFloat64: {roleRegSliceFloat, RoleRegGeneral},
		SubOpGetStructFieldSliceUint64:  {roleRegSliceUint, RoleRegGeneral},
		SubOpGetStructFieldSliceString:  {roleRegSliceString, RoleRegGeneral},
		SubOpGetStructFieldSliceBool:    {roleRegSliceBool, RoleRegGeneral},
		SubOpGetStructFieldSliceByte:    {roleRegSliceByte, RoleRegGeneral},
	}

	// sub1ConstJumpShapes holds the fused compare-against-constant-and-branch operations,
	// keyed by sub-opcode with the role of the test register read from B.
	sub1ConstJumpShapes = map[SubOpcode]OperandRole{
		SubOpEqualStringConstantThenJumpIfFalse:         RoleRegString,
		SubOpLessOrEqualInt64ConstantThenJumpIfFalse:    RoleRegInt,
		SubOpLessThanInt64ConstantThenJumpIfFalse:       RoleRegInt,
		SubOpEqualInt64ConstantThenJumpIfFalse:          RoleRegInt,
		SubOpEqualInt64ConstantThenJumpIfTrue:           RoleRegInt,
		SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse: RoleRegInt,
		SubOpGreaterThanInt64ConstantThenJumpIfFalse:    RoleRegInt,
	}

	// sub1RegRegJumpShapes holds the fused compare-and-branch operations, keyed by
	// sub-opcode with the role shared by both operand registers.
	sub1RegRegJumpShapes = map[SubOpcode]OperandRole{
		SubOpLessThanInt64ThenJumpIfFalse:       RoleRegInt,
		SubOpLessOrEqualInt64ThenJumpIfFalse:    RoleRegInt,
		SubOpGreaterThanInt64ThenJumpIfFalse:    RoleRegInt,
		SubOpGreaterOrEqualInt64ThenJumpIfFalse: RoleRegInt,
		SubOpEqualInt64ThenJumpIfFalse:          RoleRegInt,
		SubOpNotEqualInt64ThenJumpIfFalse:       RoleRegInt,
		SubOpEqualUint64ThenJumpIfFalse:         RoleRegUint,
		SubOpNotEqualUint64ThenJumpIfFalse:      RoleRegUint,
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
		SubOpTier2MakeInterfaceMethodExpression:       {RoleRegGeneral, accessNone, accessC, ShapeFlagFollowsExtension},
		SubOpTier2MakeMap:                             {RoleRegGeneral, accessNone, accessC, ShapeFlagFollowsExtension},
		SubOpTier2InRangeUint64ThenJumpIfFalse:        {RoleRegUint, accessC, accessNone, ShapeFlagFollowsExtension | ShapeFlagControlFlow},
		SubOpTier2AllocateStructLiteral:               {RoleRegGeneral, accessNone, accessC, ShapeFlagFollowsExtension},
		SubOpTier2IncrementInt64:                      {RoleRegInt, accessC, accessC, 0},
		SubOpTier2DecrementInt64:                      {RoleRegInt, accessC, accessC, 0},
		SubOpTier2IncrementUint64:                     {RoleRegUint, accessC, accessC, 0},
		SubOpTier2DecrementUint64:                     {RoleRegUint, accessC, accessC, 0},
		SubOpTier2IncrementInt32:                      {RoleRegInt, accessC, accessC, 0},
		SubOpTier2DecrementInt32:                      {RoleRegInt, accessC, accessC, 0},
		SubOpTier2Panic:                               {RoleRegGeneral, accessC, accessNone, 0},
		SubOpTier2Recover:                             {RoleRegGeneral, accessNone, accessC, 0},
		SubOpTier2SetZeroGeneral:                      {RoleRegGeneral, accessC, accessNone, 0},
		SubOpTier2CloseChannel:                        {RoleRegGeneral, accessC, accessNone, 0},
		SubOpTier2LoadNil:                             {RoleRegGeneral, accessNone, accessC, 0},
		SubOpTier2Return:                              {RoleImmediate, accessNone, accessNone, ShapeFlagControlFlow | ShapeFlagTerminator},
		SubOpTier2SynchroniseClosureCapturedVariables: {RoleRegGeneral, accessC, accessNone, ShapeFlagOpaqueWrites},
		SubOpTier2PanicRangeFunctionContinuedInt64:    {RoleRegInt, accessC, accessNone, 0},
		SubOpTier2CheckShiftCount:                     {RoleRegInt, accessC, accessNone, 0},
		SubOpTier2JumpIfFalseBool:                     {RoleRegBool, accessC, accessNone, ShapeFlagFollowsExtension | ShapeFlagControlFlow},
		SubOpTier2JumpIfTrueBool:                      {RoleRegBool, accessC, accessNone, ShapeFlagFollowsExtension | ShapeFlagControlFlow},
	}

	// sub3Shapes holds the tier-3 operations. Every byte is a discriminator, so a row
	// carries nothing but the whole-instruction flags.
	sub3Shapes = map[SubOpcodeTier3]shapeFlag{
		SubOpTier3NoOperation:                      0,
		SubOpTier3ReturnVoid:                       ShapeFlagControlFlow | ShapeFlagTerminator,
		SubOpTier3SynchroniseIIFECapturedVariables: ShapeFlagOpaqueWrites,
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
