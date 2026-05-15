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
	"fmt"
	"go/ast"
	"go/types"
	"reflect"
	"strings"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

// resolveStructFieldPath resolves the target field path and value expression for a struct
// literal element, handling both keyed and positional forms.
//
// Takes positionalIndex (int) which is the fallback index for an unkeyed element.
// Takes element (ast.Expr) which is the element expression.
// Takes literalType (types.Type) which is the literal's go/types type, or nil when the
// literal has no go/types origin.
// Takes reflectType (reflect.Type) which is the struct's reflect type.
//
// Returns the resolved field path, the value expression, and any error.
func (c *Compiler) resolveStructFieldPath(positionalIndex int, element ast.Expr, literalType types.Type, reflectType reflect.Type) ([]int, ast.Expr, error) {
	kv, ok := element.(*ast.KeyValueExpr)
	if !ok {
		return []int{positionalIndex}, element, nil
	}

	key, ok := kv.Key.(*ast.Ident)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %s", fault.ErrCompileInvalidStructLiteralKey, types.ExprString(kv.Key))
	}

	if path, ok := c.literalFieldPath(literalType, key); ok {
		return path, kv.Value, nil
	}

	for j := range reflectType.NumField() {
		if structFieldNameMatches(reflectType.Field(j), key.Name) {
			return []int{j}, kv.Value, nil
		}
	}
	return nil, nil, fmt.Errorf("unknown field: %s in struct %v (has %d fields)", key.Name, reflectType, reflectType.NumField())
}

// literalFieldPath resolves a struct-literal key to its go/types field index path.
//
// The package for the lookup comes from the field object go/types already resolved rather
// than from a nil qualifier, because a promoted key may name an unexported field
// (Line{name: ...} in the spec's own example) and a nil package would hide it.
//
// Takes literalType (types.Type) which is the literal's type, or nil when unavailable.
// Takes key (*ast.Ident) which is the field key identifier.
//
// Returns the index path and true when go/types resolved the key to a field.
func (c *Compiler) literalFieldPath(literalType types.Type, key *ast.Ident) ([]int, bool) {
	if literalType == nil || c.Info == nil {
		return nil, false
	}
	field, ok := c.Info.Uses[key].(*types.Var)
	if !ok {
		return nil, false
	}
	object, path, _ := types.LookupFieldOrMethod(literalType, false, field.Pkg(), key.Name)
	if object != field || len(path) == 0 {
		return nil, false
	}
	return path, true
}

// needsReflectSameKind reports whether a same-kind conversion still requires the
// reflect-based path because one side involves unsafe.Pointer or the destination is an
// array (Go 1.20+ slice-to-array conversion).
//
// Takes kind (isa.RegisterKind) which is the shared register kind.
// Takes sourceType (types.Type) which is the source Go type.
// Takes destinationType (types.Type) which is the destination Go type.
//
// Returns true when a reflect-based conversion is required.
func needsReflectSameKind(kind isa.RegisterKind, sourceType, destinationType types.Type) bool {
	if kind == isa.RegisterGeneral && isUnsafePointerConversion(sourceType, destinationType) {
		return true
	}
	if kind == isa.RegisterGeneral && isNamedFuncConversion(sourceType, destinationType) {
		return true
	}
	return isSliceToArrayConversion(sourceType, destinationType)
}

// isNamedFuncConversion reports whether a conversion moves a func value between two
// distinct func types (`Op(f)`, `func(int) int(op)`). The register stays the same, but
// the runtime must record or clear the named type on the closure so method dispatch and
// %T follow the conversion.
//
// Takes source (types.Type) which is the operand's static type.
// Takes destination (types.Type) which is the conversion target.
//
// Returns true when both are func types that are not identical.
func isNamedFuncConversion(source, destination types.Type) bool {
	_, sourceIsFunc := source.Underlying().(*types.Signature)
	_, destinationIsFunc := destination.Underlying().(*types.Signature)
	return sourceIsFunc && destinationIsFunc && !types.Identical(source, destination)
}

// isUnsafePointerConversion reports whether either side of a conversion is
// unsafe.Pointer.
//
// Takes source (types.Type) which is the source type.
// Takes destination (types.Type) which is the destination type.
//
// Returns true when either type is unsafe.Pointer.
func isUnsafePointerConversion(source, destination types.Type) bool {
	sourceBasic, sourceOk := source.Underlying().(*types.Basic)
	destinationBasic, destinationOk := destination.Underlying().(*types.Basic)
	return (sourceOk && sourceBasic.Kind() == types.UnsafePointer) ||
		(destinationOk && destinationBasic.Kind() == types.UnsafePointer)
}

// isSliceToArrayConversion reports whether the conversion is from a slice type to an
// array type or to a pointer-to-array type.
//
// Takes source (types.Type) which is the source type.
// Takes destination (types.Type) which is the destination type.
//
// Returns true when source underlies a slice and destination underlies an array or a
// pointer to an array.
func isSliceToArrayConversion(source, destination types.Type) bool {
	if _, sourceSlice := source.Underlying().(*types.Slice); !sourceSlice {
		return false
	}
	switch destinationUnderlying := destination.Underlying().(type) {
	case *types.Array:
		return true
	case *types.Pointer:
		_, pointerToArray := destinationUnderlying.Elem().Underlying().(*types.Array)
		return pointerToArray
	default:
		return false
	}
}

// isSliceOfByte reports whether t's underlying type is []byte.
//
// Takes t (types.Type) which is the type to check.
//
// Returns true when the underlying type is a byte slice.
func isSliceOfByte(t types.Type) bool {
	sliceValue, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	b, ok := sliceValue.Elem().(*types.Basic)
	return ok && b.Kind() == types.Byte
}

// structFieldNameMatches reports whether a reflect.StructField corresponds to the
// source-level field name. Handles the isa.EmbeddedUnexportedPrefix applied by
// buildStructFields to unexported anonymous fields, which reflect.StructOf rejects
// natively.
//
// Takes field (reflect.StructField) which is the reflected field metadata.
// Takes name (string) which is the source-level identifier.
//
// Returns true when the names refer to the same field.
func structFieldNameMatches(field reflect.StructField, name string) bool {
	if field.Name == name {
		return true
	}
	if strings.HasPrefix(field.Name, isa.EmbeddedUnexportedPrefix) && field.Name[len(isa.EmbeddedUnexportedPrefix):] == name {
		return true
	}
	return false
}
