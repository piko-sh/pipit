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
	"context"
	"fmt"
	"go/types"
	"path"
	"reflect"
	"strings"

	"pipit.sh/pipit/internal/link"
	"pipit.sh/pipit/internal/symtab/descriptor"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

const (
	// maximumNamedDescriptorBytes bounds the name and the package path of a named composite
	// descriptor.
	maximumNamedDescriptorBytes = 1 << 12
)

// errorReflectType is the predeclared error interface, which a descriptor identifies by
// its name alone.
var errorReflectType = reflect.TypeFor[error]()

// reflectTypeMemo is one reconstruction's memory.
//
// It caches the reflect.Type already reconstructed for a nested descriptor, keyed by the
// descriptor pointer. Without it, shared sub-descriptors would be rebuilt at every
// parent, turning reconstruction into an exponential walk. It also keeps the order
// entries were added, so a group of self-referential types whose build lost a race can
// drop exactly the entries derived from its discarded types, and the groups of
// self-referential types found by one walk of the top-level descriptor.
type reflectTypeMemo struct {
	// types maps each reconstructed descriptor to its runtime type.
	types map[*descriptor.TypeDescriptor]reflect.Type

	// root is the top-level descriptor the reconstruction started from.
	root *descriptor.TypeDescriptor

	// cycles is the walk of root that finds its groups, made on first use.
	cycles *cycleWalk

	// added lists the descriptors entered in types, in order.
	added []*descriptor.TypeDescriptor
}

// store enters node's runtime type.
//
// Takes node (*descriptor.TypeDescriptor) which is the descriptor.
// Takes resolved (reflect.Type) which is its runtime type.
func (memo *reflectTypeMemo) store(node *descriptor.TypeDescriptor, resolved reflect.Type) {
	if _, known := memo.types[node]; !known {
		memo.added = append(memo.added, node)
	}
	memo.types[node] = resolved
}

// rollback drops every entry added since mark.
//
// Takes mark (int) which is len(memo.added) when the entries to keep were complete.
func (memo *reflectTypeMemo) rollback(mark int) {
	for _, node := range memo.added[mark:] {
		delete(memo.types, node)
	}
	memo.added = memo.added[:mark]
}

// newReflectTypeMemo returns an empty memo for a reconstruction from root.
//
// Takes root (*descriptor.TypeDescriptor) which is the top-level descriptor.
//
// Returns *reflectTypeMemo which holds nothing yet.
func newReflectTypeMemo(root *descriptor.TypeDescriptor) *reflectTypeMemo {
	return &reflectTypeMemo{types: make(map[*descriptor.TypeDescriptor]reflect.Type), root: root, cycles: nil, added: nil}
}

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
func ReflectTypeFor(ctx context.Context, typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry) (reflect.Type, error) {
	return descriptorToReflectTypeAtDepth(ctx, typeDescriptor, registry, 0, newReflectTypeMemo(&typeDescriptor))
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
func ReconstructConstant(ctx context.Context, constantDescriptor descriptor.GeneralConstantDescriptor, registry *SymbolRegistry) (reflect.Value, error) {
	switch constantDescriptor.Kind {
	case descriptor.GeneralConstantPackageSymbol:
		value, ok := registry.Lookup(constantDescriptor.PackagePath, constantDescriptor.SymbolName)
		if !ok {
			return reflect.Value{}, fmt.Errorf("symbol %s.%s not found in registry", constantDescriptor.PackagePath, constantDescriptor.SymbolName)
		}
		return linkedSiblingOf(value), nil

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
		reflectType, err := ReflectTypeFor(ctx, constantDescriptor.TypeDescriptor, registry)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("reconstructing composite type: %w", err)
		}
		if reflectType == nil {
			return reflect.Value{}, fmt.Errorf("%w: composite zero of the nil type", errCorruptTypeDescriptor)
		}
		return ZeroConstant(reflectType), nil

	default:
		return reflect.Value{}, fmt.Errorf("unknown general constant kind: %d", constantDescriptor.Kind)
	}
}

// linkedSiblingOf returns the sibling a generic function or method registered as a linked
// function or method calls through, which is the value the compiler put in the constant.
// Any other symbol value is returned unchanged.
//
// Takes value (reflect.Value) which is the registered symbol value.
//
// Returns reflect.Value which is the constant's value.
func linkedSiblingOf(value reflect.Value) reflect.Value {
	if linked, ok := reflect.TypeAssert[link.LinkedFunction](value); ok {
		return linked.Target
	}
	if linked, ok := reflect.TypeAssert[link.LinkedMethod](value); ok {
		return linked.Target
	}
	return value
}

// descriptorToReflectTypeAtDepth reconstructs a reflect.Type from a descriptor at a given
// recursion depth. Refuses trees deeper than MaxTypeDescriptorDepth.
//
// Takes typeDescriptor (TypeDescriptor) which is the type to reconstruct.
// Takes registry (*SymbolRegistry) which provides named type lookups.
// Takes depth (int) which is the current nesting depth.
// Takes memo (*reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the reconstructed type.
// Returns error when a sub-descriptor is corrupt or the nesting depth limit is exceeded.
func descriptorToReflectTypeAtDepth(ctx context.Context, typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo *reflectTypeMemo) (reflect.Type, error) {
	if depth > descriptor.MaxTypeDescriptorDepth {
		return nil, fmt.Errorf("%w: type nesting exceeds %d levels", errCorruptTypeDescriptor, descriptor.MaxTypeDescriptorDepth)
	}

	if hostType, ok := resolveHostNamedDescriptor(typeDescriptor, registry); ok {
		return hostType, nil
	}

	switch typeDescriptor.Kind {
	case descriptor.KindBasic:
		return descriptor.BasicKindToReflect(reflect.Kind(typeDescriptor.BasicKind)), nil
	case descriptor.KindNamed:
		if typeDescriptor.BackReference {
			return nil, fmt.Errorf("%w: back-reference to %s.%s outside its description", errCorruptTypeDescriptor, typeDescriptor.PackagePath, typeDescriptor.Name)
		}
		if typeDescriptor.Element != nil {
			return resolveNamedCompositeType(ctx, typeDescriptor, registry, depth, memo)
		}
		return resolveNamedType(ctx, typeDescriptor, registry)
	case descriptor.KindPtr:
		return resolveElementContainer(ctx, typeDescriptor, registry, reflect.PointerTo, depth, memo)
	case descriptor.KindSlice:
		return resolveElementContainer(ctx, typeDescriptor, registry, reflect.SliceOf, depth, memo)
	case descriptor.KindArray:
		return resolveArrayType(ctx, typeDescriptor, registry, depth, memo)
	case descriptor.KindMap:
		return resolveMapType(ctx, typeDescriptor, registry, depth, memo)
	case descriptor.KindChan:
		return resolveChannelType(ctx, typeDescriptor, registry, depth, memo)
	case descriptor.KindFunc:
		return reconstructFunctionType(ctx, typeDescriptor, registry, depth, memo)
	case descriptor.KindStruct:
		return reconstructStructType(ctx, typeDescriptor, registry, depth, memo)
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
// Takes memo (*reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the reconstructed sub-type.
// Returns error when the sub-descriptor is absent or cannot be resolved.
func resolveSubDescriptor(ctx context.Context, sub *descriptor.TypeDescriptor, registry *SymbolRegistry, role string, depth int, memo *reflectTypeMemo) (reflect.Type, error) {
	if sub == nil {
		return nil, fmt.Errorf("%w: missing %s sub-descriptor", errCorruptTypeDescriptor, role)
	}
	if cached, ok := memo.types[sub]; ok {
		return cached, nil
	}
	resolved, err := descriptorToReflectTypeAtDepth(ctx, *sub, registry, depth+1, memo)
	if err != nil {
		return nil, err
	}
	memo.store(sub, resolved)
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
// Takes memo (*reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the wrapped container type.
// Returns error when the element type cannot be resolved.
func resolveElementContainer(
	ctx context.Context,
	typeDescriptor descriptor.TypeDescriptor,
	registry *SymbolRegistry,
	wrap func(reflect.Type) reflect.Type,
	depth int,
	memo *reflectTypeMemo,
) (reflect.Type, error) {
	elementType, err := resolveSubDescriptor(ctx, typeDescriptor.Element, registry, "element", depth, memo)
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
// Takes memo (*reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the reconstructed array type.
// Returns error when the element type cannot be resolved.
func resolveArrayType(ctx context.Context, typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo *reflectTypeMemo) (reflect.Type, error) {
	elementType, err := resolveSubDescriptor(ctx, typeDescriptor.Element, registry, "array element", depth, memo)
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
// Takes memo (*reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the reconstructed map type.
// Returns error when the key or value type cannot be resolved.
func resolveMapType(ctx context.Context, typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo *reflectTypeMemo) (reflect.Type, error) {
	keyType, err := resolveSubDescriptor(ctx, typeDescriptor.Key, registry, "map key", depth, memo)
	if err != nil {
		return nil, err
	}
	valueType, err := resolveSubDescriptor(ctx, typeDescriptor.Value, registry, "map value", depth, memo)
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
// Takes memo (*reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the reconstructed channel type.
// Returns error when the element type cannot be resolved.
func resolveChannelType(ctx context.Context, typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo *reflectTypeMemo) (reflect.Type, error) {
	elementType, err := resolveSubDescriptor(ctx, typeDescriptor.Element, registry, "channel element", depth, memo)
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
// Takes memo (*reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the reconstructed function type.
// Returns error when any parameter or result type cannot be resolved.
func reconstructFunctionType(ctx context.Context, typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo *reflectTypeMemo) (reflect.Type, error) {
	parameterTypes := make([]reflect.Type, len(typeDescriptor.Params))
	for i := range typeDescriptor.Params {
		parameterType, err := resolveSubDescriptor(ctx, &typeDescriptor.Params[i], registry, "parameter", depth, memo)
		if err != nil {
			return nil, err
		}
		parameterTypes[i] = parameterType
	}
	resultTypes := make([]reflect.Type, len(typeDescriptor.Results))
	for i := range typeDescriptor.Results {
		resultType, err := resolveSubDescriptor(ctx, &typeDescriptor.Results[i], registry, "result", depth, memo)
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
// Takes memo (*reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the reconstructed struct type.
// Returns error when any field type cannot be resolved.
func reconstructStructType(ctx context.Context, typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo *reflectTypeMemo) (reflect.Type, error) {
	structFields := make([]reflect.StructField, len(typeDescriptor.Fields))
	for i := range typeDescriptor.Fields {
		fieldType, err := resolveSubDescriptor(ctx, &typeDescriptor.Fields[i].Typ, registry, "field", depth, memo)
		if err != nil {
			return nil, err
		}
		structFields[i] = reflect.StructField{
			Name:      typeDescriptor.Fields[i].Name,
			Type:      fieldType,
			Tag:       reflect.StructTag(typeDescriptor.Fields[i].Tag),
			PkgPath:   typeDescriptor.Fields[i].PackagePath,
			Anonymous: typeDescriptor.Fields[i].Embedded,
		}
	}
	if err := checkReconstructedStructSize(structFields); err != nil {
		return nil, err
	}
	return safeReflectComposite(func() reflect.Type { return reflect.StructOf(structFields) })
}

// resolveHostNamedDescriptor resolves a descriptor that carries a host type's identity to
// the registered host type itself, so the type keeps its methods across serialisation.
//
// Takes typeDescriptor (TypeDescriptor) which is the descriptor to resolve.
// Takes registry (*SymbolRegistry) which provides the host types. May be nil.
//
// Returns reflect.Type which is the registered host type on a hit.
// Returns bool which is true when the descriptor resolved to a host type.
func resolveHostNamedDescriptor(typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry) (reflect.Type, bool) {
	if typeDescriptor.Kind == descriptor.KindNamed || typeDescriptor.Name == "" {
		return nil, false
	}
	if typeDescriptor.PackagePath == "" {
		if typeDescriptor.Kind == descriptor.KindInterface && typeDescriptor.Name == errorReflectType.Name() {
			return errorReflectType, true
		}
		return nil, false
	}
	if registry == nil {
		return nil, false
	}
	if erased, ok := nativeBackedInstantiation(typeDescriptor, registry); ok {
		return erased, true
	}
	hostType, ok := registry.HostNamedType(typeDescriptor.PackagePath, typeDescriptor.Name)
	if !ok || !descriptorShapeMatches(typeDescriptor, hostType) {
		return nil, false
	}
	return hostType, true
}

// nativeBackedInstantiation resolves a descriptor of the erased instantiation of a
// native-backed generic type (sync/atomic.Pointer[struct {}]), which every instantiation
// of the type runs on, to that type, whose methods are native.
//
// Takes typeDescriptor (TypeDescriptor) which names the type with its type arguments.
// Takes registry (*SymbolRegistry) which holds the native-backed generic types.
//
// Returns the erased instantiation and true, or false for any other descriptor.
func nativeBackedInstantiation(typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry) (reflect.Type, bool) {
	base, _, instantiated := strings.Cut(typeDescriptor.Name, "[")
	if !instantiated {
		return nil, false
	}
	erased, ok := registry.NativeBackedReflectType(typeDescriptor.PackagePath, base)
	if !ok || erased.Name() != typeDescriptor.Name || !descriptorShapeMatches(typeDescriptor, erased) {
		return nil, false
	}
	return erased, true
}

// descriptorShapeMatches reports whether reflectType has the shape typeDescriptor
// describes, as far as the descriptor's own fields say without resolving its children.
//
// Takes typeDescriptor (TypeDescriptor) which is the structural descriptor.
// Takes reflectType (reflect.Type) which is the candidate host type.
//
// Returns bool which is true when the kinds, and any length, direction or arity the
// descriptor records, agree.
func descriptorShapeMatches(typeDescriptor descriptor.TypeDescriptor, reflectType reflect.Type) bool {
	switch typeDescriptor.Kind {
	case descriptor.KindBasic:
		return reflectType.Kind() == reflect.Kind(typeDescriptor.BasicKind)
	case descriptor.KindPtr:
		return reflectType.Kind() == reflect.Pointer
	case descriptor.KindSlice:
		return reflectType.Kind() == reflect.Slice
	case descriptor.KindArray:
		return reflectType.Kind() == reflect.Array && reflectType.Len() == typeDescriptor.Length
	case descriptor.KindMap:
		return reflectType.Kind() == reflect.Map
	case descriptor.KindChan:
		return reflectType.Kind() == reflect.Chan && int(reflectType.ChanDir()) == typeDescriptor.Dir
	case descriptor.KindFunc:
		return reflectType.Kind() == reflect.Func &&
			reflectType.NumIn() == len(typeDescriptor.Params) &&
			reflectType.NumOut() == len(typeDescriptor.Results) &&
			reflectType.IsVariadic() == typeDescriptor.IsVariadic
	case descriptor.KindStruct:
		return reflectType.Kind() == reflect.Struct
	case descriptor.KindInterface:
		return reflectType.Kind() == reflect.Interface
	default:
		return false
	}
}

// resolveNamedCompositeType rebuilds a named array, slice, map, channel or function type
// from its name and the descriptor of its underlying type. The registry returns the type
// the compile built when it ran in this process, so values keep one runtime type.
//
// Takes typeDescriptor (TypeDescriptor) which names the type and carries its underlying
// type as Element.
// Takes registry (*SymbolRegistry) which provides named type lookups for the underlying
// type.
// Takes depth (int) which is the current nesting depth.
// Takes memo (*reflectTypeMemo) which caches already reconstructed sub-descriptors.
//
// Returns reflect.Type which is the named type.
// Returns error when the underlying type cannot be resolved or the type cannot be built.
func resolveNamedCompositeType(ctx context.Context, typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo *reflectTypeMemo) (reflect.Type, error) {
	if err := checkNamedCompositeDescriptor(typeDescriptor, registry); err != nil {
		return nil, err
	}
	cycle, err := descriptorCycleOf(&typeDescriptor, memo)
	if err != nil {
		return nil, err
	}
	if cycle != nil {
		return resolveNamedCompositeCycle(ctx, typeDescriptor, cycle, registry, depth, memo)
	}
	underlying, err := resolveSubDescriptor(ctx, typeDescriptor.Element, registry, "underlying", depth, memo)
	if err != nil {
		return nil, err
	}
	bareName, _, _ := strings.Cut(typeDescriptor.Name, "[")
	identity := typemodel.NamedCompositeIdentity{
		PkgPath:  typeDescriptor.PackagePath,
		PkgName:  path.Base(typeDescriptor.PackagePath),
		Name:     typeDescriptor.Name,
		BareName: bareName,
	}
	named, err := registry.NamedCompositeQuota().TypeFor(identity, underlying)
	if err != nil {
		return nil, fmt.Errorf("named type %s.%s: %w", typeDescriptor.PackagePath, typeDescriptor.Name, err)
	}
	return named, nil
}

// checkNamedCompositeDescriptor refuses a named composite descriptor the compiler could
// never have written.
//
// Takes typeDescriptor (descriptor.TypeDescriptor) which is a KindNamed descriptor with
// Element.
// Takes registry (*SymbolRegistry) which supplies the host packages. May be nil.
//
// Returns error which wraps errCorruptTypeDescriptor.
func checkNamedCompositeDescriptor(typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry) error {
	switch {
	case typeDescriptor.Name == "":
		return fmt.Errorf("%w: named type without a name", errCorruptTypeDescriptor)
	case len(typeDescriptor.Name) > maximumNamedDescriptorBytes, len(typeDescriptor.PackagePath) > maximumNamedDescriptorBytes:
		return fmt.Errorf("%w: named type name or package path passes %d bytes", errCorruptTypeDescriptor, maximumNamedDescriptorBytes)
	case registry != nil && registry.HasHostPackage(typeDescriptor.PackagePath):
		return fmt.Errorf("%w: named type %s.%s claims a host package", errCorruptTypeDescriptor, typeDescriptor.PackagePath, typeDescriptor.Name)
	}
	return nil
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
func resolveNamedType(ctx context.Context, typeDescriptor descriptor.TypeDescriptor, registry *SymbolRegistry) (reflect.Type, error) {
	value, ok := registry.Lookup(typeDescriptor.PackagePath, typeDescriptor.Name)
	if !ok {
		if basic, isScalar := basicKindForReflectKind(reflect.Kind(typeDescriptor.BasicKind)); isScalar {
			if poolType := typemodel.NamedScalarPoolTypeForPath(ctx, typeDescriptor.PackagePath, typeDescriptor.Name, basic); poolType != nil {
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
