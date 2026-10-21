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
	"go/ast"
	"go/token"

	"piko.sh/asmgen"
)

// tier1HandlerDeclarationNames lists the assembly handlers, most of them tier-1, that get
// a Go prototype for go vet's asmdecl analyser by name alone.
var tier1HandlerDeclarationNames = []string{
	"pathBFallback",
	"handlerLoadInt64Constant",
	"handlerLoadBoolImmediateAsInt64",
	"handlerAddInt64",
	"handlerAddUint64",
	"handlerAddInt32",
	"handlerSubtractInt32",
	"handlerMultiplyInt32",
	"handlerAddInt32Constant",
	"handlerSubtractInt32Constant",
	"handlerAddUint32",
	"handlerSubtractUint32",
	"handlerMultiplyUint32",
	"handlerSubtractUint64",
	"handlerMultiplyUint64",
	"handlerBitAndUint64",
	"handlerBitOrUint64",
	"handlerBitXorUint64",
	"handlerBitAndNotUint64",
	"handlerShiftLeftUint64",
	"handlerShiftRightUint64",
	"handlerEqualUint64",
	"handlerNotEqualUint64",
	"handlerLessThanUint64",
	"handlerLessOrEqualUint64",
	"handlerGreaterThanUint64",
	"handlerGreaterOrEqualUint64",
	"handlerSubtractInt64",
	"handlerMultiplyInt64",
	"handlerDivideInt64",
	"handlerRemainderInt64",
	"handlerBitAndInt64",
	"handlerBitOrInt64",
	"handlerBitXorInt64",
	"handlerBitAndNotInt64",
	"handlerShiftLeftInt64",
	"handlerShiftRightInt64",
	"handlerAddFloat64",
	"handlerSubtractFloat64",
	"handlerMultiplyFloat64",
	"handlerDivideFloat64",
	"handlerEqualInt64",
	"handlerNotEqualInt64",
	"handlerLessThanInt64",
	"handlerLessOrEqualInt64",
	"handlerGreaterThanInt64",
	"handlerGreaterOrEqualInt64",
	"handlerJump",
	"handlerJumpIfTrue",
	"handlerJumpIfFalse",
	"handlerCallExit",
	"handlerReturnExit",
	"handlerReturnVoidExit",
	"handlerTailCallExit",
	"handlerTailCallInlineSubroutine",
	"handlerCall",
	"handlerCallInlineSetupGeneralBank",
	"handlerCallInlineClearGeneralBank",
	"handlerReturn",
	"handlerReturnVoid",
	"handlerSubtractInt64Constant",
	"handlerAddInt64Constant",
	"handlerLessOrEqualInt64ConstantThenJumpIfFalse",
	"handlerLessThanInt64ConstantThenJumpIfFalse",
	"handlerEqualInt64ConstantThenJumpIfFalse",
	"handlerEqualInt64ConstantThenJumpIfTrue",
	"handlerGreaterOrEqualInt64ConstantThenJumpIfFalse",
	"handlerGreaterThanInt64ConstantThenJumpIfFalse",
	"handlerLessThanInt64ThenJumpIfFalse",
	"handlerLessOrEqualInt64ThenJumpIfFalse",
	"handlerGreaterThanInt64ThenJumpIfFalse",
	"handlerGreaterOrEqualInt64ThenJumpIfFalse",
	"handlerEqualInt64ThenJumpIfFalse",
	"handlerNotEqualInt64ThenJumpIfFalse",
	"handlerEqualUint64ThenJumpIfFalse",
	"handlerNotEqualUint64ThenJumpIfFalse",
	"handlerMultiplyInt64Constant",
	"handlerAddInt64ConstantThenJump",
	"handlerMultiplyInt64ConstantThenAdd",
	"handlerIncrementInt64ThenJumpIfLessThan",
	"handlerIncrementInt64ThenJumpIfLessOrEqual",
	"handlerIncrementInt64ThenJumpIfLessThanConstant",
	"handlerLoadInt64Immediate",
	"handlerLoadFloat64Constant",
	"handlerLoadStringConstant",
	"handlerLoadBoolConstant",
	"handlerEqualFloat64",
	"handlerNotEqualFloat64",
	"handlerLessThanFloat64",
	"handlerLessOrEqualFloat64",
	"handlerGreaterThanFloat64",
	"handlerGreaterOrEqualFloat64",
	"handlerGetStringByte",
	"handlerEqualString",
	"handlerNotEqualString",
	"handlerSliceString",
	"handlerGetStringByteAsInt64",
	"handlerLessThanLengthStringThenJumpIfFalse",
}

// goDeclaration is one Go forward declaration the generator emits for an assembly symbol
// so go vet's asmdecl analyser can check the TEXT block against a signature.
type goDeclaration struct {
	// Name is the assembly symbol, which must match a handler definition's Name.
	Name string

	// Parameters is the Go parameter list, or nil for none.
	Parameters []*ast.Field

	// DocTail holds doc-comment lines placed after the synopsis, already prefixed with "// "
	// or empty for a blank comment line.
	DocTail []string

	// Architectures, when non-nil, restricts the declaration to the listed architectures. A
	// nil slice puts the declaration in the shared file that every architecture builds.
	Architectures []asmgen.Architecture
}

// tier1SubOpDeclarations lists the sub-op handlers of tiers 1 and 2 that get a Go
// prototype.
var tier1SubOpDeclarations = []goDeclaration{
	{Name: "handlerEqualUint64ImmediateThenJumpIfFalse", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerLengthSliceInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerLengthSliceFloat64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerLengthSliceString", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerLengthSliceBool", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerLengthSliceUint64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerGetSliceFloat64Element", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerSetSliceFloat64Element", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerGetSliceUint64Element", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerSetSliceUint64Element", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerGetSliceBoolElement", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerSetSliceBoolElement", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerGetSliceStringElement", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerRealPartComplex128", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerImaginaryPartComplex128", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMoveComplex128", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerNegateComplex128", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMoveInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMoveFloat64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMoveBool", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMoveUint64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMoveString", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMoveSliceInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMoveSliceFloat64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMoveSliceString", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMoveSliceBool", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMoveSliceUint64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMoveSliceByte", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerSliceSliceInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerSliceSliceFloat64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerSliceSliceString", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerSliceSliceBool", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerSliceSliceUint64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerIncrementInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerDecrementInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerNegateInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerNegateFloat64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerBitNotInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerConvertInt64ToFloat64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerConvertFloat64ToInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerNotInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerLoadZero", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerAddUint32Constant", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerSubtractUint32Constant", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerLengthGeneral", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerConvertBoolToInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerConvertInt64ToBool", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerConvertInt64ToUint64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerConvertUint64ToInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerConvertUint64ToFloat64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerConvertFloat64ToUint64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMathSqrt", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMathAbs", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMathFloor", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMathCeil", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMathTrunc", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMathRound", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerLengthString", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMathSin", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMathCos", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMathExp", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMathTan", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerMathMod", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerStrconvFormatBool", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerStrconvItoa", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerStrconvFormatInt", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerCapacityGeneral", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerConvertBytesToString", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerBoxSliceInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerLengthSliceByte", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerGetSliceByteElement", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerGetSliceByteElementInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerSetSliceByteElement", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerSetSliceByteElementRun", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerSliceSliceByte", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerRangeNextSliceByte", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerIncrementStructFieldInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerDecrementStructFieldInt64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerIncrementStructFieldUint64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerDecrementStructFieldUint64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerInRangeUint64ThenJumpIfFalse", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerJumpIfFalseBool", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerJumpIfTrueBool", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerIncrementUint64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerDecrementUint64", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerIncrementInt32", Parameters: nil, DocTail: nil, Architectures: nil},
	{Name: "handlerDecrementInt32", Parameters: nil, DocTail: nil, Architectures: nil},
}

// initStep is one entry in the ordered package-init sequence.
//
// initSubOpJumpTables must follow installTier1Dispatcher, because the wrappers it
// replaces with .abi0 addresses would otherwise overflow NOSPLIT under a tight loop.
type initStep struct {
	// Call is the call to emit.
	Call *ast.CallExpr

	// Guard, when non-nil, is a boolean expression the call is wrapped in.
	Guard ast.Expr

	// GuardImport, when non-empty, is the import path the guard expression needs.
	GuardImport string

	// Architectures, when non-nil, restricts the step to the listed architectures.
	Architectures []asmgen.Architecture

	// Shared marks a step that installs an architecture-independent table, so it belongs in
	// installDispatchTables rather than inline in each architecture's init.
	Shared bool
}

// goDeclarations returns every Go forward declaration the dispatch files must carry.
//
// Returns []goDeclaration which is the ordered declaration list, shared entries first.
func goDeclarations() []goDeclaration {
	declarations := []goDeclaration{
		{
			Name: "dispatchLoop",
			Parameters: []*ast.Field{{
				Names: []*ast.Ident{ast.NewIdent("ctx")},
				Type:  &ast.StarExpr{X: ast.NewIdent("dispatchContext")},
			}},
			DocTail: []string{
				"",
				"Takes ctx (*dispatchContext) which provides the register file and program counter",
				"state for dispatch.",
			},
			Architectures: nil,
		},
		{
			Name:       "initJumpTable",
			Parameters: jumpTableParameters(),
			DocTail: []string{
				"",
				"Takes table (*[opcodeTableSize]uintptr) which is the fixed-size array to populate",
				"with handler addresses.",
			},
			Architectures: nil,
		},
		{
			Name:          "initSubOpJumpTables",
			Parameters:    nil,
			DocTail:       nil,
			Architectures: nil,
		},
		{
			Name:       "initJumpTableSSE41",
			Parameters: jumpTableParameters(),
			DocTail: []string{
				"",
				"Takes table (*[opcodeTableSize]uintptr) which is the fixed-size array to patch with",
				"SSE4.1 handler addresses.",
			},
			Architectures: []asmgen.Architecture{asmgen.ArchitectureAMD64},
		},
	}

	for _, name := range tier1HandlerDeclarationNames {
		declarations = append(declarations, goDeclaration{
			Name:          name,
			Parameters:    nil,
			DocTail:       nil,
			Architectures: nil,
		})
	}

	return append(declarations, tier1SubOpDeclarations...)
}

// jumpTableParameters returns the parameter list of the prototypes that fill the jump
// table, a pointer to the fixed-size table.
//
// Returns []*ast.Field which is the one parameter.
func jumpTableParameters() []*ast.Field {
	return []*ast.Field{{
		Names: []*ast.Ident{ast.NewIdent("table")},
		Type: &ast.StarExpr{X: &ast.ArrayType{
			Len: ast.NewIdent("opcodeTableSize"),
			Elt: ast.NewIdent("uintptr"),
		}},
	}}
}

// initSequence returns the ordered package-init steps for the dispatch tables.
//
// Returns []initStep which is the sequence in the order it must execute.
func initSequence() []initStep {
	return []initStep{
		{
			Call: &ast.CallExpr{
				Fun:  ast.NewIdent("initJumpTable"),
				Args: []ast.Expr{&ast.UnaryExpr{Op: token.AND, X: ast.NewIdent("asmJumpTable")}},
			},
			Guard:         nil,
			GuardImport:   "",
			Shared:        false,
			Architectures: nil,
		},
		{
			Call: &ast.CallExpr{
				Fun:  ast.NewIdent("initJumpTableSSE41"),
				Args: []ast.Expr{&ast.UnaryExpr{Op: token.AND, X: ast.NewIdent("asmJumpTable")}},
			},
			Guard: &ast.SelectorExpr{
				X:   &ast.SelectorExpr{X: ast.NewIdent("cpu"), Sel: ast.NewIdent("X86")},
				Sel: ast.NewIdent("HasSSE41"),
			},
			GuardImport:   "golang.org/x/sys/cpu",
			Shared:        false,
			Architectures: []asmgen.Architecture{asmgen.ArchitectureAMD64},
		},
		{Call: &ast.CallExpr{Fun: ast.NewIdent("installTier1Dispatcher"), Args: nil}, Guard: nil, GuardImport: "", Shared: true, Architectures: nil},
		{Call: &ast.CallExpr{Fun: ast.NewIdent("installTier2Dispatcher"), Args: nil}, Guard: nil, GuardImport: "", Shared: true, Architectures: nil},
		{Call: &ast.CallExpr{Fun: ast.NewIdent("installTier3Dispatcher"), Args: nil}, Guard: nil, GuardImport: "", Shared: true, Architectures: nil},
		{Call: &ast.CallExpr{Fun: ast.NewIdent("initSubOpJumpTables"), Args: nil}, Guard: nil, GuardImport: "", Shared: true, Architectures: nil},
		{Call: &ast.CallExpr{Fun: ast.NewIdent("installPerOpDirectExits"), Args: nil}, Guard: nil, GuardImport: "", Shared: true, Architectures: nil},
		{Call: &ast.CallExpr{Fun: ast.NewIdent("installFlatJumpTableASM"), Args: nil}, Guard: nil, GuardImport: "", Shared: true, Architectures: nil},
		{Call: &ast.CallExpr{Fun: ast.NewIdent("initOpNeedsGoFallback"), Args: nil}, Guard: nil, GuardImport: "", Shared: true, Architectures: nil},
	}
}
