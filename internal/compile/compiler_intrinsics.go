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

package compile

import (
	"context"
	"go/ast"
	"go/types"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// intrinsicDefinition describes a single Compiler intrinsic substitution. When a call
// expression matches an entry in intrinsicTable, the Compiler emits a single opcode
// instead of a full native call sequence.
type intrinsicDefinition struct {
	// opcode is the opcode to Emit for this intrinsic. When useUmbrella is true, the actual
	// Emit uses opDrillTier1 with subOp encoded in operand A and the opcode field is unused.
	opcode isa.Opcode

	// subOp identifies the sub-opcode to encode in operand A when useUmbrella is true.
	// Cold-path opcodes (math intrinsics, strconv conversions, real/imag, BytesToString,
	// MakeMethodExpr, Cap) folded under opDrillTier1 populate subOp; conventional intrinsics
	// leave it zero.
	subOp isa.SubOpcode

	// useUmbrella selects between direct opcode emission and umbrella dispatch. True for
	// cold-path ops folded under opDrillTier1; false for hot-path intrinsics that retain
	// dedicated opcode slots.
	useUmbrella bool

	// returnKind is the register bank for the return value.
	returnKind isa.RegisterKind

	// argumentKinds holds the expected register kind for each argument.
	argumentKinds [2]isa.RegisterKind

	// argumentCount is the number of arguments the intrinsic accepts.
	argumentCount uint8
}

var (
	// intrinsicTable maps "pkg.FuncName" keys to their intrinsicDefinition entries. Entries
	// are matched against call expressions during compilation.
	//
	//nolint:revive // self-documenting keys
	intrinsicTable = map[string]intrinsicDefinition{
		"strings.ContainsRune": {opcode: isa.OpStrContainsRune, returnKind: isa.RegisterBool, argumentKinds: [2]isa.RegisterKind{isa.RegisterString, isa.RegisterInt}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"strings.Contains":     {opcode: isa.OpStrContains, returnKind: isa.RegisterBool, argumentKinds: [2]isa.RegisterKind{isa.RegisterString, isa.RegisterString}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"strings.HasPrefix":    {opcode: isa.OpStrHasPrefix, returnKind: isa.RegisterBool, argumentKinds: [2]isa.RegisterKind{isa.RegisterString, isa.RegisterString}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"strings.HasSuffix":    {opcode: isa.OpStrHasSuffix, returnKind: isa.RegisterBool, argumentKinds: [2]isa.RegisterKind{isa.RegisterString, isa.RegisterString}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"strings.EqualFold":    {opcode: isa.OpStrEqualFold, returnKind: isa.RegisterBool, argumentKinds: [2]isa.RegisterKind{isa.RegisterString, isa.RegisterString}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"strings.Index":        {opcode: isa.OpStrIndex, returnKind: isa.RegisterInt, argumentKinds: [2]isa.RegisterKind{isa.RegisterString, isa.RegisterString}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"strings.Count":        {opcode: isa.OpStrCount, returnKind: isa.RegisterInt, argumentKinds: [2]isa.RegisterKind{isa.RegisterString, isa.RegisterString}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"strings.IndexRune":    {opcode: isa.OpStrIndexRune, returnKind: isa.RegisterInt, argumentKinds: [2]isa.RegisterKind{isa.RegisterString, isa.RegisterInt}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"strings.ToUpper":      {useUmbrella: true, subOp: isa.SubOpStrToUpper, returnKind: isa.RegisterString, argumentKinds: [2]isa.RegisterKind{isa.RegisterString}, argumentCount: 1, opcode: 0},
		"strings.ToLower":      {useUmbrella: true, subOp: isa.SubOpStrToLower, returnKind: isa.RegisterString, argumentKinds: [2]isa.RegisterKind{isa.RegisterString}, argumentCount: 1, opcode: 0},
		"strings.TrimSpace":    {useUmbrella: true, subOp: isa.SubOpStrTrimSpace, returnKind: isa.RegisterString, argumentKinds: [2]isa.RegisterKind{isa.RegisterString}, argumentCount: 1, opcode: 0},
		"strings.TrimPrefix":   {opcode: isa.OpStrTrimPrefix, returnKind: isa.RegisterString, argumentKinds: [2]isa.RegisterKind{isa.RegisterString, isa.RegisterString}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"strings.TrimSuffix":   {opcode: isa.OpStrTrimSuffix, returnKind: isa.RegisterString, argumentKinds: [2]isa.RegisterKind{isa.RegisterString, isa.RegisterString}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"strings.Trim":         {opcode: isa.OpStrTrim, returnKind: isa.RegisterString, argumentKinds: [2]isa.RegisterKind{isa.RegisterString, isa.RegisterString}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"strings.Repeat":       {opcode: isa.OpStrRepeat, returnKind: isa.RegisterString, argumentKinds: [2]isa.RegisterKind{isa.RegisterString, isa.RegisterInt}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"strings.LastIndex":    {opcode: isa.OpStrLastIndex, returnKind: isa.RegisterInt, argumentKinds: [2]isa.RegisterKind{isa.RegisterString, isa.RegisterString}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"strings.Join":         {opcode: isa.OpStrJoin, returnKind: isa.RegisterString, argumentKinds: [2]isa.RegisterKind{isa.RegisterGeneral, isa.RegisterString}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"strings.Split":        {opcode: isa.OpStrSplit, returnKind: isa.RegisterGeneral, argumentKinds: [2]isa.RegisterKind{isa.RegisterString, isa.RegisterString}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"math.Abs":             {useUmbrella: true, subOp: isa.SubOpMathAbs, returnKind: isa.RegisterFloat, argumentKinds: [2]isa.RegisterKind{isa.RegisterFloat}, argumentCount: 1, opcode: 0},
		"math.Sqrt":            {useUmbrella: true, subOp: isa.SubOpMathSqrt, returnKind: isa.RegisterFloat, argumentKinds: [2]isa.RegisterKind{isa.RegisterFloat}, argumentCount: 1, opcode: 0},
		"math.Floor":           {useUmbrella: true, subOp: isa.SubOpMathFloor, returnKind: isa.RegisterFloat, argumentKinds: [2]isa.RegisterKind{isa.RegisterFloat}, argumentCount: 1, opcode: 0},
		"math.Ceil":            {useUmbrella: true, subOp: isa.SubOpMathCeil, returnKind: isa.RegisterFloat, argumentKinds: [2]isa.RegisterKind{isa.RegisterFloat}, argumentCount: 1, opcode: 0},
		"math.Round":           {useUmbrella: true, subOp: isa.SubOpMathRound, returnKind: isa.RegisterFloat, argumentKinds: [2]isa.RegisterKind{isa.RegisterFloat}, argumentCount: 1, opcode: 0},
		"math.Pow":             {opcode: isa.OpMathPow, returnKind: isa.RegisterFloat, argumentKinds: [2]isa.RegisterKind{isa.RegisterFloat, isa.RegisterFloat}, argumentCount: 2, subOp: 0, useUmbrella: false},
		"math.Exp":             {useUmbrella: true, subOp: isa.SubOpMathExp, returnKind: isa.RegisterFloat, argumentKinds: [2]isa.RegisterKind{isa.RegisterFloat}, argumentCount: 1, opcode: 0},
		"math.Sin":             {useUmbrella: true, subOp: isa.SubOpMathSin, returnKind: isa.RegisterFloat, argumentKinds: [2]isa.RegisterKind{isa.RegisterFloat}, argumentCount: 1, opcode: 0},
		"math.Cos":             {useUmbrella: true, subOp: isa.SubOpMathCos, returnKind: isa.RegisterFloat, argumentKinds: [2]isa.RegisterKind{isa.RegisterFloat}, argumentCount: 1, opcode: 0},
		"math.Tan":             {useUmbrella: true, subOp: isa.SubOpMathTan, returnKind: isa.RegisterFloat, argumentKinds: [2]isa.RegisterKind{isa.RegisterFloat}, argumentCount: 1, opcode: 0},
		"math.Mod":             {useUmbrella: true, subOp: isa.SubOpMathMod, returnKind: isa.RegisterFloat, argumentKinds: [2]isa.RegisterKind{isa.RegisterFloat, isa.RegisterFloat}, argumentCount: 2, opcode: 0},
		"math.Trunc":           {useUmbrella: true, subOp: isa.SubOpMathTrunc, returnKind: isa.RegisterFloat, argumentKinds: [2]isa.RegisterKind{isa.RegisterFloat}, argumentCount: 1, opcode: 0},
		"strconv.Itoa":         {useUmbrella: true, subOp: isa.SubOpStrconvItoa, returnKind: isa.RegisterString, argumentKinds: [2]isa.RegisterKind{isa.RegisterInt}, argumentCount: 1, opcode: 0},
		"strconv.FormatBool":   {useUmbrella: true, subOp: isa.SubOpStrconvFormatBool, returnKind: isa.RegisterString, argumentKinds: [2]isa.RegisterKind{isa.RegisterBool}, argumentCount: 1, opcode: 0},
		"strconv.FormatInt":    {useUmbrella: true, subOp: isa.SubOpStrconvFormatInt, returnKind: isa.RegisterString, argumentKinds: [2]isa.RegisterKind{isa.RegisterInt, isa.RegisterInt}, argumentCount: 2, opcode: 0},
	}
)

// tryCompileIntrinsic attempts to lower a qualified function call to a single opcode via
// intrinsicTable.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the qualified call selector.
// Takes expression (*ast.CallExpr) which is the full call expression.
//
// Returns the VarLocation of the result, true if an intrinsic was matched, and an error
// if compilation failed.
func (c *Compiler) tryCompileIntrinsic(ctx context.Context, selectorExpression *ast.SelectorExpr, expression *ast.CallExpr) (program.VarLocation, bool, error) {
	key, ok := c.intrinsicKey(selectorExpression)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	if location, ok, err := c.tryCompileReplaceAll(ctx, key, expression); ok || err != nil {
		return location, ok, err
	}
	definition, found := intrinsicTable[key]
	if !found {
		return program.VarLocation{}, false, nil
	}
	if !c.intrinsicArgumentsMatch(expression, definition) {
		return program.VarLocation{}, false, nil
	}
	argumentRegisters, err := c.compileIntrinsicArgs(ctx, expression, definition)
	if err != nil {
		return program.VarLocation{}, false, err
	}
	dest := c.Scopes.Alloc.Alloc(definition.returnKind)
	c.emitIntrinsicCall(definition, dest, argumentRegisters)
	return program.VarLocation{Register: dest, Kind: definition.returnKind}, true, nil
}

// intrinsicKey resolves a selector expression to its intrinsic-table key
// (`pkgPath.FuncName`) when the selector targets a registered native symbol that has an
// intrinsic mapping. Returns the empty string and false when the selector does not match.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the call selector (e.g.
// `strings.ReplaceAll`).
//
// Returns the intrinsic-table key and a bool indicating whether the selector resolves to
// an intrinsic candidate.
func (c *Compiler) intrinsicKey(selectorExpression *ast.SelectorExpr) (string, bool) {
	typeObject, ok := c.Info.Uses[selectorExpression.Sel]
	if !ok {
		return "", false
	}
	typeFunction, isFunction := typeObject.(*types.Func)
	if !isFunction || typeFunction.Pkg() == nil || c.symbols == nil {
		return "", false
	}
	packagePath := typeFunction.Pkg().Path()
	if _, registered := c.symbols.Lookup(packagePath, typeFunction.Name()); !registered {
		return "", false
	}
	return packagePath + "." + typeFunction.Name(), true
}

// intrinsicArgumentsMatch reports whether the call's argument count and per-argument
// register kinds match the intrinsic definition.
//
// Takes expression (*ast.CallExpr) which is the call site.
// Takes definition (intrinsicDefinition) which carries the expected argument arity and
// kinds.
//
// Returns true when the call matches the intrinsic shape.
func (c *Compiler) intrinsicArgumentsMatch(expression *ast.CallExpr, definition intrinsicDefinition) bool {
	if len(expression.Args) != int(definition.argumentCount) {
		return false
	}
	for i := range int(definition.argumentCount) {
		tv := c.Info.Types[expression.Args[i]]
		if c.kindFor(tv.Type) != definition.argumentKinds[i] {
			return false
		}
	}
	return true
}

// compileIntrinsicArgs compiles each argument expression and packs the resulting register
// indices into a fixed-size array sized for the maximum supported intrinsic arity (2).
//
// Takes expression (*ast.CallExpr) which is the call site.
// Takes definition (intrinsicDefinition) which carries the argument count.
//
// Returns the per-argument register indices and any compilation error.
func (c *Compiler) compileIntrinsicArgs(ctx context.Context, expression *ast.CallExpr, definition intrinsicDefinition) ([2]uint8, error) {
	var argumentRegisters [2]uint8
	for i := range int(definition.argumentCount) {
		location, err := c.compileExpression(ctx, expression.Args[i])
		if err != nil {
			return argumentRegisters, err
		}

		if location.Kind != definition.argumentKinds[i] {
			location = c.coerceToKind(ctx, location, definition.argumentKinds[i])
		}
		argumentRegisters[i] = location.Register
	}
	return argumentRegisters, nil
}

// emitIntrinsicCall emits the bytecode that invokes the intrinsic. Picks between the
// umbrella sub-op encoding (one main opcode plus an optional extension word) and the
// direct opcode form based on the definition.
//
// Takes definition (intrinsicDefinition) which selects the encoding.
// Takes dest (uint8) which is the destination register.
// Takes argumentRegisters ([2]uint8) which are the compiled argument registers.
func (c *Compiler) emitIntrinsicCall(definition intrinsicDefinition, dest uint8, argumentRegisters [2]uint8) {
	if definition.useUmbrella {
		program.Emit(c.Function, isa.OpDrillTier1, uint8(definition.subOp), dest, argumentRegisters[0])
		if definition.argumentCount == 2 {
			program.Emit(c.Function, isa.OpExt, argumentRegisters[1], 0, 0)
		}
		return
	}
	program.Emit(c.Function, definition.opcode, dest, argumentRegisters[0], argumentRegisters[1])
}

// tryCompileReplaceAll handles the strings.ReplaceAll intrinsic, which requires a
// three-argument opcode pair instead of a standard entry.
//
// Takes key (string) which is the "pkg.FuncName" intrinsic key.
// Takes expression (*ast.CallExpr) which is the call expression to lower.
//
// Returns the VarLocation of the result, true if the intrinsic matched, and an error if
// compilation failed.
func (c *Compiler) tryCompileReplaceAll(ctx context.Context, key string, expression *ast.CallExpr) (program.VarLocation, bool, error) {
	if key != "strings.ReplaceAll" || len(expression.Args) != replaceAllArgCount {
		return program.VarLocation{}, false, nil
	}
	for i := range replaceAllArgCount {
		if c.kindFor(c.Info.Types[expression.Args[i]].Type) != isa.RegisterString {
			return program.VarLocation{}, false, nil
		}
	}
	sLocation, err := c.compileExpression(ctx, expression.Args[0])
	if err != nil {
		return program.VarLocation{}, false, err
	}
	oldLocation, err := c.compileExpression(ctx, expression.Args[1])
	if err != nil {
		return program.VarLocation{}, false, err
	}
	newLocation, err := c.compileExpression(ctx, expression.Args[2])
	if err != nil {
		return program.VarLocation{}, false, err
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterString)
	program.Emit(c.Function, isa.OpStrReplaceAll, dest, sLocation.Register, oldLocation.Register)
	program.Emit(c.Function, isa.OpExt, newLocation.Register, 0, 0)
	return program.VarLocation{Register: dest, Kind: isa.RegisterString}, true, nil
}
