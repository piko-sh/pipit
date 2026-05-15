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

import "piko.sh/asmgen"

// tier1HandlerDeclarationNames lists the tier-1 assembly handlers that get a Go prototype
// for go vet's asmdecl analyser.
var tier1HandlerDeclarationNames = []string{
	"pathBFallback",
	"handlerLoadIntConst",
	"handlerLoadBool",
	"handlerAddInt",
	"handlerAddUint",
	"handlerSubUint",
	"handlerMulUint",
	"handlerBitAndUint",
	"handlerBitOrUint",
	"handlerBitXorUint",
	"handlerBitAndNotUint",
	"handlerShiftLeftUint",
	"handlerShiftRightUint",
	"handlerEqUint",
	"handlerNeUint",
	"handlerLtUint",
	"handlerLeUint",
	"handlerGtUint",
	"handlerGeUint",
	"handlerSubInt",
	"handlerMulInt",
	"handlerDivInt",
	"handlerRemInt",
	"handlerBitAnd",
	"handlerBitOr",
	"handlerBitXor",
	"handlerBitAndNot",
	"handlerShiftLeft",
	"handlerShiftRight",
	"handlerAddFloat",
	"handlerSubFloat",
	"handlerMulFloat",
	"handlerDivFloat",
	"handlerEqInt",
	"handlerNeInt",
	"handlerLtInt",
	"handlerLeInt",
	"handlerGtInt",
	"handlerGeInt",
	"handlerJump",
	"handlerJumpIfTrue",
	"handlerJumpIfFalse",
	"handlerCallExit",
	"handlerReturnExit",
	"handlerReturnVoidExit",
	"handlerTailCallExit",
	"handlerTailCallInlineSubroutine",
	"handlerCallInline",
	"handlerCallInlineSetupGeneralBank",
	"handlerCallInlineClearGeneralBank",
	"handlerReturnInline",
	"handlerReturnVoidInline",
	"handlerSubIntConst",
	"handlerAddIntConst",
	"handlerLeIntConstJumpFalse",
	"handlerLtIntConstJumpFalse",
	"handlerEqIntConstJumpFalse",
	"handlerEqIntConstJumpTrue",
	"handlerGeIntConstJumpFalse",
	"handlerGtIntConstJumpFalse",
	"handlerLtIntJumpFalse",
	"handlerLeIntJumpFalse",
	"handlerGtIntJumpFalse",
	"handlerGeIntJumpFalse",
	"handlerEqIntJumpFalse",
	"handlerNeIntJumpFalse",
	"handlerMulIntConst",
	"handlerAddIntJump",
	"handlerIncIntJumpLt",
	"handlerLoadIntConstSmall",
	"handlerLoadFloatConst",
	"handlerLoadStringConst",
	"handlerLoadBoolConst",
	"handlerEqFloat",
	"handlerNeFloat",
	"handlerLtFloat",
	"handlerLeFloat",
	"handlerGtFloat",
	"handlerGeFloat",
	"handlerStringIndex",
	"handlerEqString",
	"handlerNeString",
	"handlerSliceString",
	"handlerStringIndexToInt",
	"handlerLenStringLtJumpFalse",
}

// goDeclaration is one Go forward declaration the generator emits for an assembly symbol
// so go vet's asmdecl analyser can check the TEXT block against a signature.
type goDeclaration struct {
	// Name is the assembly symbol, which must match a handler definition's Name.
	Name string

	// Parameters is the Go parameter list without its surrounding parentheses.
	Parameters string

	// DocTail holds doc-comment lines placed after the synopsis, already prefixed with "// "
	// or empty for a blank comment line.
	DocTail []string

	// Architectures, when non-nil, restricts the declaration to the listed architectures. A
	// nil slice puts the declaration in the shared file that every architecture builds.
	Architectures []asmgen.Architecture
}

// tier1SubOpDeclarations lists the tier-1 sub-op handlers that get a Go prototype.
var tier1SubOpDeclarations = []goDeclaration{
	{Name: "handlerSubOpEqUintConstJumpFalse", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpLenSliceIntDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpLenSliceFloatDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpLenSliceStringDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpLenSliceBoolDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpLenSliceUintDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpSliceGetFloatDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpSliceSetFloatDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpSliceGetUintDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpSliceSetUintDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpSliceGetBoolDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpSliceSetBoolDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpSliceGetStringDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpRealComplex", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpImagComplex", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMoveComplex", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpNegComplex", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMoveInt", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMoveFloat", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMoveBool", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMoveUint", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMoveString", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMoveSliceInt", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMoveSliceFloat", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMoveSliceString", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMoveSliceBool", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMoveSliceUint", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMoveSliceByte", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpSliceSliceIntDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpSliceSliceFloatDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpSliceSliceStringDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpSliceSliceBoolDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpSliceSliceUintDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpTier2IncInt", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpTier2DecInt", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpNegInt", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpNegFloat", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpBitNot", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpIntToFloat", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpFloatToInt", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpUintToFloat", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpFloatToUint", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMathSqrt", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMathAbs", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMathFloor", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMathCeil", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMathTrunc", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMathRound", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpLenString", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMathSin", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMathCos", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMathExp", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMathTan", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMathMod", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpStrconvFormatBool", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpStrconvItoa", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpStrconvFormatInt", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpCap", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpBytesToString", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpBoxSliceInt", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMakeSliceInt", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMakeSliceFloat", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMakeSliceString", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMakeSliceBool", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMakeSliceUint", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpMakeSliceByte", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpLenSliceByteDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpSliceGetByteDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpSliceSetByteDirect", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpSliceByteSlice", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerRangeNextSliceByte", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpIncStructFieldInt", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpDecStructFieldInt", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpIncStructFieldUint", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpDecStructFieldUint", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpRangeCheckUintJumpFalse", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpTier2IncUint", Parameters: "", DocTail: nil, Architectures: nil},
	{Name: "handlerSubOpTier2DecUint", Parameters: "", DocTail: nil, Architectures: nil},
}

// initStep is one entry in the ordered package-init sequence.
//
// initSubOpJumpTables must follow installTier1Dispatcher, because the wrappers it
// replaces with .abi0 addresses would otherwise overflow NOSPLIT under a tight loop.
type initStep struct {
	// Call is the Go call expression to emit, without a trailing semicolon.
	Call string

	// Guard, when non-empty, is a boolean expression the call is wrapped in.
	Guard string

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
			Name:       "dispatchLoop",
			Parameters: "ctx *dispatchContext",
			DocTail: []string{
				"",
				"Takes ctx (*dispatchContext) which provides the register file and program counter",
				"state for dispatch.",
			},
			Architectures: nil,
		},
		{
			Name:       "initJumpTable",
			Parameters: "table *[opcodeTableSize]uintptr",
			DocTail: []string{
				"",
				"Takes table (*[opcodeTableSize]uintptr) which is the fixed-size array to populate",
				"with handler addresses.",
			},
			Architectures: nil,
		},
		{
			Name:          "initSubOpJumpTables",
			Parameters:    "",
			DocTail:       nil,
			Architectures: nil,
		},
		{
			Name:       "initJumpTableSSE41",
			Parameters: "table *[opcodeTableSize]uintptr",
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
			Parameters:    "",
			DocTail:       nil,
			Architectures: nil,
		})
	}

	return append(declarations, tier1SubOpDeclarations...)
}

// initSequence returns the ordered package-init steps for the dispatch tables.
//
// Returns []initStep which is the sequence in the order it must execute.
func initSequence() []initStep {
	return []initStep{
		{Call: "initJumpTable(&asmJumpTable)", Guard: "", GuardImport: "", Shared: false, Architectures: nil},
		{
			Call:          "initJumpTableSSE41(&asmJumpTable)",
			Guard:         "cpu.X86.HasSSE41",
			GuardImport:   "golang.org/x/sys/cpu",
			Shared:        false,
			Architectures: []asmgen.Architecture{asmgen.ArchitectureAMD64},
		},
		{Call: "installTier1Dispatcher()", Guard: "", GuardImport: "", Shared: true, Architectures: nil},
		{Call: "installTier2Dispatcher()", Guard: "", GuardImport: "", Shared: true, Architectures: nil},
		{Call: "installTier3Dispatcher()", Guard: "", GuardImport: "", Shared: true, Architectures: nil},
		{Call: "initSubOpJumpTables()", Guard: "", GuardImport: "", Shared: true, Architectures: nil},
		{Call: "installPerOpDirectExits()", Guard: "", GuardImport: "", Shared: true, Architectures: nil},
		{Call: "installFlatJumpTableASM()", Guard: "", GuardImport: "", Shared: true, Architectures: nil},
		{Call: "initOpNeedsGoFallback()", Guard: "", GuardImport: "", Shared: true, Architectures: nil},
	}
}
