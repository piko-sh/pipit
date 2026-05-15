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
	"fmt"
	"go/ast"
	"go/types"
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab/descriptor"
	"pipit.sh/pipit/internal/symtab/typemodel"

	"pipit.sh/pipit/internal/symtab"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/link"
)

// tryCompileLinkedCall routes a generic call through its sibling.
//
// The non-generic sibling function is loaded into the call register and the instantiated
// type arguments, resolved via types.Info.Instances, are attached to the call site for
// the VM to prepend at dispatch time.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector naming the generic
// function.
// Takes expression (*ast.CallExpr) which is the enclosing call expression (its Fun has
// already been unwrapped of any [T] / [T1, T2] instantiation markers).
//
// Returns the call result location, a bool indicating that the linked path handled the
// call, and any compilation error encountered.
func (c *Compiler) tryCompileLinkedCall(
	ctx context.Context,
	selectorExpression *ast.SelectorExpr,
	expression *ast.CallExpr,
) (program.VarLocation, bool, error) {
	if c.symbols == nil {
		return program.VarLocation{}, false, nil
	}
	typeObject, ok := c.Info.Uses[selectorExpression.Sel]
	if !ok || typeObject.Pkg() == nil {
		return program.VarLocation{}, false, nil
	}
	value, found := c.symbols.Lookup(typeObject.Pkg().Path(), typeObject.Name())
	if !found || !value.IsValid() || value.Type() != typemodel.LinkedFunctionReflectType {
		return program.VarLocation{}, false, nil
	}
	linked, ok := typeAssertValue[link.LinkedFunction](value)
	if !ok {
		return program.VarLocation{}, false, nil
	}

	typeArgs, err := c.resolveLinkedTypeArgs(ctx, selectorExpression, linked.TypeArgCount)
	if err != nil {
		return program.VarLocation{}, false, err
	}

	fnRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	constIndex, err := program.AddGeneralConstant(c.Function, linked.Target, descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantPackageSymbol,
		PackagePath: typeObject.Pkg().Path(),
		SymbolName:  typeObject.Name(), TypeDescriptor: descriptor.TypeDescriptor{}})
	if err != nil {
		return program.VarLocation{}, false, err
	}
	program.EmitWide(c.Function, isa.OpLoadGeneralConst, fnRegister, constIndex)

	location, callErr := c.compileLinkedNativeCall(ctx, expression,
		program.VarLocation{Register: fnRegister, Kind: isa.RegisterGeneral}, typeArgs, nil)
	return location, true, callErr
}

// resolveLinkedTypeArgs extracts the concrete type arguments from types.Info.Instances
// for an instantiated generic call and converts each to a reflect.Type. It fails if
// go/types did not record an instantiation for the selector (usually means the source is
// invalid or the expression was not a generic instantiation).
//
// Takes selectorExpression (*ast.SelectorExpr) which names the generic.
// Takes expectedCount (int) which is the TypeArgCount declared on the LinkedFunction. A
// mismatch indicates a codegen bug rather than user error, so it is reported as a
// compilation failure.
//
// Returns the resolved []reflect.Type and any conversion error.
func (c *Compiler) resolveLinkedTypeArgs(
	ctx context.Context,
	selectorExpression *ast.SelectorExpr,
	expectedCount int,
) ([]reflect.Type, error) {
	if expectedCount < 0 || expectedCount > symtab.MaxLinkedTypeArgCount {
		return nil, fmt.Errorf("%w: %s declares %d (limit %d) at %s",
			fault.ErrLinkedCallTooManyTypeArgs, selectorExpression.Sel.Name,
			expectedCount, symtab.MaxLinkedTypeArgCount,
			c.positionString(selectorExpression.Pos()))
	}
	instance, found := c.Info.Instances[selectorExpression.Sel]
	if !found {
		return nil, fmt.Errorf("%w: %s at %s",
			fault.ErrLinkedCallNoInstance, selectorExpression.Sel.Name,
			c.positionString(selectorExpression.Pos()))
	}
	typeArgs := instance.TypeArgs
	if typeArgs == nil || typeArgs.Len() != expectedCount {
		return nil, fmt.Errorf("%w: %s expected %d, got %d at %s",
			fault.ErrLinkedCallArityMismatch, selectorExpression.Sel.Name,
			expectedCount, typeArgsLen(typeArgs),
			c.positionString(selectorExpression.Pos()))
	}
	reflected := make([]reflect.Type, expectedCount)
	for position := range expectedCount {
		reflectType := c.TypeToReflect(ctx, typeArgs.At(position))
		if reflectType == nil {
			return nil, fmt.Errorf("%w: %s argument %d (%s) at %s",
				fault.ErrLinkedCallTypeArgUnresolvable, selectorExpression.Sel.Name,
				position, typeArgs.At(position),
				c.positionString(selectorExpression.Pos()))
		}
		reflected[position] = reflectType
	}
	return reflected, nil
}

// emitLinkedCallWithReturns handles the multi-return assignment path (a, b :=
// pkg.Fn[T](...)) when pkg.Fn resolves to an link LinkedFunction. It mirrors
// tryCompileLinkedCall but writes into pre-allocated return locations provided by the
// assignment Compiler instead of allocating its own.
//
// Takes selectorExpression (*ast.SelectorExpr) which names the generic.
// Takes callExpression (*ast.CallExpr) which is the enclosing call (its Fun has already
// been unwrapped of [T] / [T1, T2] by the caller).
// Takes returnLocations ([]VarLocation) which are the pre-allocated return registers
// allocated by the multi-return assignment Compiler.
//
// Returns true when the linked path handled the call, plus any error.
func (c *Compiler) emitLinkedCallWithReturns(
	ctx context.Context,
	selectorExpression *ast.SelectorExpr,
	callExpression *ast.CallExpr,
	returnLocations []program.VarLocation,
) (bool, error) {
	if c.symbols == nil {
		return false, nil
	}
	typeObject, ok := c.Info.Uses[selectorExpression.Sel]
	if !ok || typeObject.Pkg() == nil {
		return false, nil
	}
	value, found := c.symbols.Lookup(typeObject.Pkg().Path(), typeObject.Name())
	if !found || !value.IsValid() || value.Type() != typemodel.LinkedFunctionReflectType {
		return false, nil
	}
	linked, ok := typeAssertValue[link.LinkedFunction](value)
	if !ok {
		return false, nil
	}

	typeArgs, err := c.resolveLinkedTypeArgs(ctx, selectorExpression, linked.TypeArgCount)
	if err != nil {
		return false, err
	}

	fnRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	constIndex, err := program.AddGeneralConstant(c.Function, linked.Target, descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantPackageSymbol,
		PackagePath: typeObject.Pkg().Path(),
		SymbolName:  typeObject.Name(), TypeDescriptor: descriptor.TypeDescriptor{}})
	if err != nil {
		return false, err
	}
	program.EmitWide(c.Function, isa.OpLoadGeneralConst, fnRegister, constIndex)

	argumentLocations, err := c.compileArgumentExpressions(ctx, callExpression)
	if err != nil {
		return false, err
	}

	argumentTypeNames, argumentTypeStrings := c.resolveArgumentStaticTypes(callExpression)
	site := program.CallSite{
		IsNative:                  true,
		NativeRegister:            fnRegister,
		Arguments:                 argumentLocations,
		Returns:                   returnLocations,
		LinkedTypeArgs:            typeArgs,
		ArgumentStaticTypeNames:   argumentTypeNames,
		ArgumentStaticTypeStrings: argumentTypeStrings,
	}
	siteIndex, addErr := program.AddCallSite(c.Function, &site)
	if addErr != nil {
		return false, addErr
	}
	c.markCallPosition()
	program.EmitTier1Wide(c.Function, isa.SubOpCallNative, siteIndex)
	return true, nil
}

// compileLinkedNativeCall compiles the argument list and emits an isa.SubOpCallNative for
// a linked generic. It mirrors compileNativeCallFromLocation but stores the resolved type
// args on the call site so the VM handler prepends them before invoking the sibling.
//
// Takes expression (*ast.CallExpr) which is the call whose args need compiling.
// Takes functionLocation (VarLocation) which points at the register holding the sibling's
// reflect.Value (loaded by tryCompileLinkedCall).
// Takes typeArgs ([]reflect.Type) which are the instantiated type arguments the VM
// prepends at call time.
// Takes receiver (ast.Expr) which is the method receiver to compile ahead of the declared
// arguments, or nil for a package-level function.
//
// Returns the first return register (or zero value when void) and any compilation error.
func (c *Compiler) compileLinkedNativeCall(
	ctx context.Context,
	expression *ast.CallExpr,
	functionLocation program.VarLocation,
	typeArgs []reflect.Type,
	receiver ast.Expr,
) (program.VarLocation, error) {
	callArguments := expression.Args
	if receiver != nil {
		callArguments = append([]ast.Expr{receiver}, expression.Args...)
	}
	argumentLocations := make([]program.VarLocation, len(callArguments))
	for argumentIndex, argument := range callArguments {
		location, err := c.compileExpression(ctx, argument)
		if err != nil {
			return program.VarLocation{}, err
		}
		argumentLocations[argumentIndex] = c.coerceEvalBoolResult(ctx, c.Info, argument, location)
	}

	var returnLocations []program.VarLocation
	var resultLocation program.VarLocation
	typeAndValue := c.Info.Types[expression.Fun]
	if signature, ok := typeAndValue.Type.Underlying().(*types.Signature); ok {
		for resultVariable := range signature.Results().Variables() {
			kind := c.kindFor(resultVariable.Type())
			register := c.Scopes.Alloc.Alloc(kind)
			returnLocations = append(returnLocations, program.VarLocation{Register: register, Kind: kind})
		}
		if len(returnLocations) > 0 {
			resultLocation = returnLocations[0]
		}
	}

	argumentTypeNames, argumentTypeStrings := c.resolveArgumentStaticTypes(expression)
	site := program.CallSite{
		IsNative:                  true,
		NativeRegister:            functionLocation.Register,
		Arguments:                 argumentLocations,
		Returns:                   returnLocations,
		LinkedTypeArgs:            typeArgs,
		ArgumentStaticTypeNames:   argumentTypeNames,
		ArgumentStaticTypeStrings: argumentTypeStrings,
	}
	siteIndex, err := program.AddCallSite(c.Function, &site)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.markCallPosition()
	program.EmitTier1Wide(c.Function, isa.SubOpCallNative, siteIndex)

	return resultLocation, nil
}

// tryCompileLinkedMethodCall routes a call to a Go 1.27 generic method on a host type
// through its registered sibling.
//
// reflect has no representation for an uninstantiated generic method, so a host package
// that exposes one registers a link.LinkedMethod under "ReceiverType.Method" instead. The
// receiver compiles as the sibling's first ordinary argument, behind the type arguments
// resolved from types.Info.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector naming the method.
// Takes expression (*ast.CallExpr) which is the enclosing call expression.
//
// Returns the call result location, a bool indicating that the linked path handled the
// call, and any compilation error encountered.
func (c *Compiler) tryCompileLinkedMethodCall(
	ctx context.Context,
	selectorExpression *ast.SelectorExpr,
	expression *ast.CallExpr,
) (program.VarLocation, bool, error) {
	if c.symbols == nil || c.Info == nil {
		return program.VarLocation{}, false, nil
	}
	packagePath, receiverTypeName, ok := c.linkedMethodReceiver(selectorExpression)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	symbolName := link.MethodSymbolKey(receiverTypeName, selectorExpression.Sel.Name)
	value, found := c.symbols.Lookup(packagePath, symbolName)
	if !found || !value.IsValid() || value.Type() != typemodel.LinkedMethodReflectType {
		return program.VarLocation{}, false, nil
	}
	linked, ok := typeAssertValue[link.LinkedMethod](value)
	if !ok {
		return program.VarLocation{}, false, nil
	}

	typeArgs, err := c.resolveLinkedTypeArgs(ctx, selectorExpression, linked.TypeArgCount)
	if err != nil {
		return program.VarLocation{}, false, err
	}

	fnRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	constIndex, err := program.AddGeneralConstant(c.Function, linked.Target, descriptor.GeneralConstantDescriptor{
		Kind:           descriptor.GeneralConstantPackageSymbol,
		PackagePath:    packagePath,
		SymbolName:     symbolName,
		TypeDescriptor: descriptor.TypeDescriptor{},
	})
	if err != nil {
		return program.VarLocation{}, false, err
	}
	program.EmitWide(c.Function, isa.OpLoadGeneralConst, fnRegister, constIndex)

	location, callErr := c.compileLinkedNativeCall(ctx, expression,
		program.VarLocation{Register: fnRegister, Kind: isa.RegisterGeneral}, typeArgs, selectorExpression.X)
	return location, true, callErr
}

// linkedMethodReceiver resolves the package path and bare type name of a method
// selector's receiver.
//
// Only a method value on a named type can name a linked method: the registry keys those
// entries by the receiver's declared name, and a pointer receiver resolves to the same
// name as a value one.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector to inspect.
//
// Returns the receiver's package path, its bare type name, and false when the selector is
// not a method call on a named type.
func (c *Compiler) linkedMethodReceiver(selectorExpression *ast.SelectorExpr) (packagePath, typeName string, ok bool) {
	selection, ok := c.Info.Selections[selectorExpression]
	if !ok || selection.Kind() != types.MethodVal {
		return "", "", false
	}
	receiverType := selection.Recv()
	if pointer, isPointer := types.Unalias(receiverType).(*types.Pointer); isPointer {
		receiverType = pointer.Elem()
	}
	named, isNamed := types.Unalias(receiverType).(*types.Named)
	if !isNamed || named.Obj() == nil || named.Obj().Pkg() == nil {
		return "", "", false
	}
	return named.Obj().Pkg().Path(), named.Obj().Name(), true
}

// typeArgsLen reports the length of a *types.TypeList, treating a nil list as zero rather
// than panicking.
//
// Takes list (*types.TypeList) which may be nil.
//
// Returns the number of entries in the list, or 0 when nil.
func typeArgsLen(list *types.TypeList) int {
	if list == nil {
		return 0
	}
	return list.Len()
}

// typeAssertValue performs a type assertion on value.Interface() instead of using
// reflect.TypeAssert.
//
// The self-hosting lane compiles under the interpreter, which cannot instantiate a
// generic standard-library function, so a plain assertion is used instead.
//
// Takes value (reflect.Value) which may be invalid or hold an unexported field.
//
// Returns T and true when value holds a T.
func typeAssertValue[T any](value reflect.Value) (T, bool) {
	if !value.IsValid() || !value.CanInterface() {
		var zero T
		return zero, false
	}
	typed, ok := value.Interface().(T)
	return typed, ok
}
