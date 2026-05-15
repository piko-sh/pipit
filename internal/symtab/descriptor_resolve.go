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

package symtab

import (
	"fmt"
	"go/types"
	"reflect"

	"pipit.sh/pipit/internal/symtab/descriptor"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

// reflectTypeMemo caches the reflect.Type already reconstructed for a nested descriptor,
// keyed by the descriptor pointer. Without it, shared sub-descriptors would be rebuilt at
// every parent, turning reconstruction into an exponential walk.
type reflectTypeMemo map[*descriptor.TypeDescriptor]reflect.Type

// ReflectTypeFor reconstructs a reflect.Type from a TypeDescriptor using the
// SymbolRegistry to resolve named types.
//
// Takes typeDescriptor (TypeDescriptor) which describes the type to reconstruct.
// Takes registry (*SymbolRegistry) which provides named type lookups.
//
// Returns reflect.Type which is the reconstructed runtime type, or nil for the nil kind a
// type switch's nil clause records.
// Returns error when a named type cannot be found in the registry, or the nil kind sits
// inside a composite type.
func ReflectTypeFor(typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry) (reflect.Type, error) {
	return descriptorToReflectTypeAtDepth(typeDescriptor, registry, 0, reflectTypeMemo{})
}

// ReconstructConstant rebuilds a reflect.Value from its serialised descriptor using the
// SymbolRegistry.
//
// Takes constantDescriptor (GeneralConstantDescriptor) which describes the constant to
// reconstruct.
// Takes registry (*SymbolRegistry) which provides named type and symbol lookups.
//
// Returns reflect.Value which is the reconstructed runtime value.
// Returns error when the referenced symbol or type cannot be found.
func ReconstructConstant(constantDescriptor descriptor.GeneralConstantDescriptor, registry *SymbolRegistry) (reflect.Value, error) {
	switch constantDescriptor.Kind {
	case descriptor.GeneralConstantPackageSymbol:
		value, ok := registry.Lookup(constantDescriptor.PackagePath, constantDescriptor.SymbolName)
		if !ok {
			return reflect.Value{}, fmt.Errorf("symbol %s.%s not found in registry", constantDescriptor.PackagePath, constantDescriptor.SymbolName)
		}
		return value, nil

	case descriptor.GeneralConstantPackageSymbolAddress:
		value, ok := registry.Lookup(constantDescriptor.PackagePath, constantDescriptor.SymbolName)
		if !ok {
			return reflect.Value{}, fmt.Errorf("symbol %s.%s not found in registry", constantDescriptor.PackagePath, constantDescriptor.SymbolName)
		}
		if !value.CanAddr() {
			return reflect.Value{}, fmt.Errorf("symbol %s.%s is not a settable variable", constantDescriptor.PackagePath, constantDescriptor.SymbolName)
		}
		return value.Addr(), nil
	case descriptor.GeneralConstantNamedTypeZero:
		zeroValue, ok := registry.ZeroValueForType(constantDescriptor.PackagePath, constantDescriptor.SymbolName)
		if !ok {
			return reflect.Value{}, fmt.Errorf("named type %s.%s not found in registry", constantDescriptor.PackagePath, constantDescriptor.SymbolName)
		}
		return zeroValue, nil

	case descriptor.GeneralConstantCompositeZero:
		reflectType, err := ReflectTypeFor(constantDescriptor.TypeDescriptor, registry)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("reconstructing composite type: %w", err)
		}
		if reflectType == nil {
			return reflect.Value{}, fmt.Errorf("%w: composite zero of the nil type", errCorruptTypeDescriptor)
		}
		return reflect.New(reflectType).Elem(), nil

	default:
		return reflect.Value{}, fmt.Errorf("unknown general constant kind: %d", constantDescriptor.Kind)
	}
}

// descriptorToReflectTypeAtDepth reconstructs a reflect.Type from a descriptor at a given
// recursion depth. Refuses trees deeper than MaxTypeDescriptorDepth.
//
// Takes typeDescriptor (TypeDescriptor) which is the type to reconstruct.
// Takes registry (*SymbolRegistry) which provides named type lookups.
// Takes depth (int) which is the current nesting depth.
// Takes memo (reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the reconstructed type.
// Returns error when a sub-descriptor is corrupt or the nesting depth limit is exceeded.
func descriptorToReflectTypeAtDepth(typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo reflectTypeMemo) (reflect.Type, error) {
	if depth > descriptor.MaxTypeDescriptorDepth {
		return nil, fmt.Errorf("%w: type nesting exceeds %d levels", errCorruptTypeDescriptor, descriptor.MaxTypeDescriptorDepth)
	}

	switch typeDescriptor.Kind {
	case descriptor.KindBasic:
		return descriptor.BasicKindToReflect(reflect.Kind(typeDescriptor.BasicKind)), nil
	case descriptor.KindNamed:
		return resolveNamedType(typeDescriptor, registry)
	case descriptor.KindPtr:
		return resolveElementContainer(typeDescriptor, registry, reflect.PointerTo, depth, memo)
	case descriptor.KindSlice:
		return resolveElementContainer(typeDescriptor, registry, reflect.SliceOf, depth, memo)
	case descriptor.KindArray:
		return resolveArrayType(typeDescriptor, registry, depth, memo)
	case descriptor.KindMap:
		return resolveMapType(typeDescriptor, registry, depth, memo)
	case descriptor.KindChan:
		return resolveChannelType(typeDescriptor, registry, depth, memo)
	case descriptor.KindFunc:
		return reconstructFunctionType(typeDescriptor, registry, depth, memo)
	case descriptor.KindStruct:
		return reconstructStructType(typeDescriptor, registry, depth, memo)
	case descriptor.KindNil:
		if depth > 0 {
			return nil, fmt.Errorf("%w: nil type nested inside a composite type", errCorruptTypeDescriptor)
		}
		return nil, nil
	default:
		return reflect.TypeFor[any](), nil
	}
}

// resolveSubDescriptor reconstructs a nested type descriptor. A nil sub-descriptor
// becomes a wrapped error instead of a nil-pointer panic into the host.
//
// Takes sub (*TypeDescriptor) which is the nested descriptor pointer, possibly nil.
// Takes registry (*SymbolRegistry) which provides named type lookups.
// Takes role (string) which names the sub-descriptor for the error message.
// Takes depth (int) which is the enclosing descriptor's recursion depth.
// Takes memo (reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the reconstructed sub-type.
// Returns error when the sub-descriptor is absent or cannot be resolved.
func resolveSubDescriptor(sub *descriptor.TypeDescriptor, registry *SymbolRegistry, role string, depth int, memo reflectTypeMemo) (reflect.Type, error) {
	if sub == nil {
		return nil, fmt.Errorf("%w: missing %s sub-descriptor", errCorruptTypeDescriptor, role)
	}
	if cached, ok := memo[sub]; ok {
		return cached, nil
	}
	resolved, err := descriptorToReflectTypeAtDepth(*sub, registry, depth+1, memo)
	if err != nil {
		return nil, err
	}
	memo[sub] = resolved
	return resolved, nil
}

// resolveElementContainer resolves the element type of a descriptor and wraps it with the
// given constructor.
//
// Takes typeDescriptor (TypeDescriptor) which describes the container type to resolve.
// Takes registry (*SymbolRegistry) which provides named type lookups.
// Takes wrap (func(reflect.Type) reflect.Type) which builds the outer type from the
// resolved element.
// Takes depth (int) which is the current nesting depth.
// Takes memo (reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the wrapped container type.
// Returns error when the element type cannot be resolved.
func resolveElementContainer(
	typeDescriptor descriptor.TypeDescriptor,
	registry *SymbolRegistry,
	wrap func(reflect.Type) reflect.Type,
	depth int,
	memo reflectTypeMemo,
) (reflect.Type, error) {
	elementType, err := resolveSubDescriptor(typeDescriptor.Element, registry, "element", depth, memo)
	if err != nil {
		return nil, err
	}
	return wrap(elementType), nil
}

// resolveArrayType rebuilds an array reflect.Type from a descriptor.
//
// Takes typeDescriptor (TypeDescriptor) which describes the array type including its
// length and element type.
// Takes registry (*SymbolRegistry) which provides named type lookups.
// Takes depth (int) which is the current nesting depth.
// Takes memo (reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the reconstructed array type.
// Returns error when the element type cannot be resolved.
func resolveArrayType(typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo reflectTypeMemo) (reflect.Type, error) {
	elementType, err := resolveSubDescriptor(typeDescriptor.Element, registry, "array element", depth, memo)
	if err != nil {
		return nil, err
	}
	if typeDescriptor.Length < 0 {
		return nil, fmt.Errorf("%w: negative array length %d", errCorruptTypeDescriptor, typeDescriptor.Length)
	}
	if err := checkReconstructedArraySize(typeDescriptor.Length, elementType); err != nil {
		return nil, err
	}
	return safeReflectComposite(func() reflect.Type { return reflect.ArrayOf(typeDescriptor.Length, elementType) })
}

// safeReflectComposite runs a reflect composite-type constructor, converting panics from
// malformed input into errCorruptTypeDescriptor errors.
//
// Takes build (func() reflect.Type) which constructs the composite type.
//
// Returns the constructed type, or errCorruptTypeDescriptor when construction panics.
func safeReflectComposite(build func() reflect.Type) (result reflect.Type, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("%w: %v", errCorruptTypeDescriptor, recovered)
		}
	}()
	return build(), nil
}

// resolveMapType rebuilds a map reflect.Type from a descriptor.
//
// Takes typeDescriptor (TypeDescriptor) which describes the map type including its key
// and value types.
// Takes registry (*SymbolRegistry) which provides named type lookups.
// Takes depth (int) which is the current nesting depth.
// Takes memo (reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the reconstructed map type.
// Returns error when the key or value type cannot be resolved.
func resolveMapType(typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo reflectTypeMemo) (reflect.Type, error) {
	keyType, err := resolveSubDescriptor(typeDescriptor.Key, registry, "map key", depth, memo)
	if err != nil {
		return nil, err
	}
	valueType, err := resolveSubDescriptor(typeDescriptor.Value, registry, "map value", depth, memo)
	if err != nil {
		return nil, err
	}
	return safeReflectComposite(func() reflect.Type { return reflect.MapOf(keyType, valueType) })
}

// resolveChannelType rebuilds a channel reflect.Type from a descriptor.
//
// Takes typeDescriptor (TypeDescriptor) which describes the channel type including its
// direction and element type.
// Takes registry (*SymbolRegistry) which provides named type lookups.
// Takes depth (int) which is the current nesting depth.
// Takes memo (reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the reconstructed channel type.
// Returns error when the element type cannot be resolved.
func resolveChannelType(typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo reflectTypeMemo) (reflect.Type, error) {
	elementType, err := resolveSubDescriptor(typeDescriptor.Element, registry, "channel element", depth, memo)
	if err != nil {
		return nil, err
	}
	return safeReflectComposite(func() reflect.Type {
		return reflect.ChanOf(reflect.ChanDir(typeDescriptor.Dir), elementType)
	})
}

// reconstructFunctionType rebuilds a function reflect.Type from a descriptor.
//
// Takes typeDescriptor (TypeDescriptor) which describes the function type including its
// parameter and result types.
// Takes registry (*SymbolRegistry) which provides named type lookups.
// Takes depth (int) which is the current nesting depth.
// Takes memo (reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the reconstructed function type.
// Returns error when any parameter or result type cannot be resolved.
func reconstructFunctionType(typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo reflectTypeMemo) (reflect.Type, error) {
	parameterTypes := make([]reflect.Type, len(typeDescriptor.Params))
	for i := range typeDescriptor.Params {
		parameterType, err := resolveSubDescriptor(&typeDescriptor.Params[i], registry, "parameter", depth, memo)
		if err != nil {
			return nil, err
		}
		parameterTypes[i] = parameterType
	}
	resultTypes := make([]reflect.Type, len(typeDescriptor.Results))
	for i := range typeDescriptor.Results {
		resultType, err := resolveSubDescriptor(&typeDescriptor.Results[i], registry, "result", depth, memo)
		if err != nil {
			return nil, err
		}
		resultTypes[i] = resultType
	}
	return safeReflectComposite(func() reflect.Type {
		return reflect.FuncOf(parameterTypes, resultTypes, typeDescriptor.IsVariadic)
	})
}

// reconstructStructType rebuilds a struct reflect.Type from a descriptor.
//
// Takes typeDescriptor (TypeDescriptor) which describes the struct type including its
// field names, types, and tags.
// Takes registry (*SymbolRegistry) which provides named type lookups.
// Takes depth (int) which is the current nesting depth.
// Takes memo (reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the reconstructed struct type.
// Returns error when any field type cannot be resolved.
func reconstructStructType(typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo reflectTypeMemo) (reflect.Type, error) {
	if typeDescriptor.PackagePath != "" && typeDescriptor.Name != "" && registry != nil {
		if value, ok := registry.Lookup(typeDescriptor.PackagePath, typeDescriptor.Name); ok {
			reflectType := value.Type()
			if reflectType.Kind() == reflect.Pointer {
				reflectType = reflectType.Elem()
			}
			if reflectType.Kind() == reflect.Struct {
				return reflectType, nil
			}
		}
	}
	structFields := make([]reflect.StructField, len(typeDescriptor.Fields))
	for i := range typeDescriptor.Fields {
		fieldType, err := resolveSubDescriptor(&typeDescriptor.Fields[i].Typ, registry, "field", depth, memo)
		if err != nil {
			return nil, err
		}
		structFields[i] = reflect.StructField{
			Name:    typeDescriptor.Fields[i].Name,
			Type:    fieldType,
			Tag:     reflect.StructTag(typeDescriptor.Fields[i].Tag),
			PkgPath: typeDescriptor.Fields[i].PackagePath,
		}
	}
	if err := checkReconstructedStructSize(structFields); err != nil {
		return nil, err
	}
	return safeReflectComposite(func() reflect.Type { return reflect.StructOf(structFields) })
}

// resolveNamedType looks up a named type in the SymbolRegistry and extracts its element
// type from the (*T)(nil) registration pattern.
//
// Takes typeDescriptor (TypeDescriptor) which identifies the named type by its package
// path and name.
// Takes registry (*SymbolRegistry) which provides the type lookup.
//
// Returns reflect.Type which is the resolved named type.
// Returns error when the named type is not found in the registry.
func resolveNamedType(typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry) (reflect.Type, error) {
	value, ok := registry.Lookup(typeDescriptor.PackagePath, typeDescriptor.Name)
	if !ok {
		if basic, isScalar := basicKindForReflectKind(reflect.Kind(typeDescriptor.BasicKind)); isScalar {
			if poolType := typemodel.NamedScalarPoolTypeForPath(typeDescriptor.PackagePath, typeDescriptor.Name, basic); poolType != nil {
				return poolType, nil
			}
		}
		return nil, fmt.Errorf("named type %s.%s not found in symbol registry", typeDescriptor.PackagePath, typeDescriptor.Name)
	}
	reflectType := value.Type()
	if reflectType.Kind() == reflect.Pointer {
		return reflectType.Elem(), nil
	}
	return reflectType, nil
}

// basicKindForReflectKind maps a scalar reflect kind to its go/types basic kind.
//
// Takes kind (reflect.Kind) which is the descriptor's basic kind.
//
// Returns the basic kind and true for the scalar kinds the named-scalar pool covers.
func basicKindForReflectKind(kind reflect.Kind) (types.BasicKind, bool) {
	switch kind {
	case reflect.Bool:
		return types.Bool, true
	case reflect.Int:
		return types.Int, true
	case reflect.Int8:
		return types.Int8, true
	case reflect.Int16:
		return types.Int16, true
	case reflect.Int32:
		return types.Int32, true
	case reflect.Int64:
		return types.Int64, true
	case reflect.Uint:
		return types.Uint, true
	case reflect.Uint8:
		return types.Uint8, true
	case reflect.Uint16:
		return types.Uint16, true
	case reflect.Uint32:
		return types.Uint32, true
	case reflect.Uint64:
		return types.Uint64, true
	case reflect.Uintptr:
		return types.Uintptr, true
	case reflect.Float32:
		return types.Float32, true
	case reflect.Float64:
		return types.Float64, true
	case reflect.Complex64:
		return types.Complex64, true
	case reflect.Complex128:
		return types.Complex128, true
	case reflect.String:
		return types.String, true
	default:
		return types.Invalid, false
	}
}
