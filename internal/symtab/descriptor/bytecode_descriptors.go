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

package descriptor

import (
	"reflect"
	"unsafe"

	"pipit.sh/pipit/internal/safeconv"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

const (
	// MaxTypeDescriptorDepth bounds type-descriptor reconstruction depth to prevent tampered
	// payloads from driving unbounded stack recursion.
	MaxTypeDescriptorDepth = 1024
)

// GeneralConstantKind identifies the source of a general constant, enabling
// reconstruction from a descriptor without the original reflect.Value.
type GeneralConstantKind uint8

const (
	// GeneralConstantPackageSymbol is a package-qualified symbol such as fmt.Println,
	// resolved via SymbolRegistry.Lookup.
	GeneralConstantPackageSymbol GeneralConstantKind = iota

	// GeneralConstantNamedTypeZero is a zero value for a named type from a registered
	// package, resolved via SymbolRegistry.ZeroValueForType.
	GeneralConstantNamedTypeZero

	// GeneralConstantCompositeZero is a zero value for a composite type (array or struct),
	// reconstructed via reflect.New(reconstructedType).Elem().
	GeneralConstantCompositeZero

	// GeneralConstantPackageSymbolAddress is the address of a settable package variable of a
	// registered package (runtime.MemProfileRate), resolved via SymbolRegistry.Lookup
	// followed by Addr, so a program can assign through it.
	GeneralConstantPackageSymbolAddress
)

// GeneralConstantDescriptor records how to reconstruct a general constant from its
// serialised form.
type GeneralConstantDescriptor struct {
	// PackagePath is the import path of the package containing the symbol or type.
	PackagePath string

	// SymbolName is the name of the symbol within its package.
	SymbolName string

	// TypeDescriptor is the type descriptor used for composite zero value reconstruction.
	TypeDescriptor TypeDescriptor

	// Kind identifies which reconstruction strategy to use.
	Kind GeneralConstantKind
}

// TypeDescriptorKind identifies the structural category of a type descriptor.
type TypeDescriptorKind uint8

const (
	// KindBasic is a primitive type such as int, string, or bool.
	KindBasic TypeDescriptorKind = iota

	// KindNamed is a named type from a registered package.
	KindNamed

	// KindPtr is a pointer type.
	KindPtr

	// KindSlice is a slice type.
	KindSlice

	// KindArray is a fixed-length array type.
	KindArray

	// KindMap is a map type with key and value descriptors.
	KindMap

	// KindChan is a channel type with a direction.
	KindChan

	// KindFunc is a function type with parameter and result descriptors.
	KindFunc

	// KindStruct is a struct type with field descriptors.
	KindStruct

	// KindInterface is an interface type, also the fallback for unresolvable types.
	KindInterface

	// KindNil is the absent type. A type switch's nil clause records a nil reflect.Type in
	// the type table; encoding it as KindInterface would turn the clause into `case any:`
	// once the bytecode is loaded from bytes.
	KindNil
)

// TypeDescriptor is a serialisable description of a reflect.Type that can be stored in
// FlatBuffers and later reconstructed via descriptorToReflectType.
//
//exhaustruct:ignore
type TypeDescriptor struct {
	// PackagePath is the import path of the package that owns this named type, empty for
	// unnamed types.
	PackagePath string

	// Name is the symbol name of the type within its package.
	Name string

	// Element is the descriptor for the element type of pointers, slices, arrays, and
	// channels.
	Element *TypeDescriptor

	// Key is the descriptor for the key type of a map.
	Key *TypeDescriptor

	// Value is the descriptor for the value type of a map.
	Value *TypeDescriptor

	// Fields holds the struct field descriptors when the kind is KindStruct.
	Fields []TypeDescriptorField

	// Params holds the parameter type descriptors when the kind is KindFunc.
	Params []TypeDescriptor

	// Results holds the return type descriptors when the kind is KindFunc.
	Results []TypeDescriptor

	// Length is the fixed size of an array type.
	Length int

	// Dir is the channel direction, stored as an int cast from reflect.ChanDir.
	Dir int

	// BasicKind is the reflect.Kind value stored as uint8 for primitive types.
	BasicKind uint8

	// Kind identifies the structural category of the type descriptor.
	Kind TypeDescriptorKind

	// IsVariadic reports whether a function type accepts variadic arguments in its last
	// parameter.
	IsVariadic bool
}

// zeroTypeDescriptor is the all-zero descriptor every constructor below starts from,
// avoiding fourteen-field exhaustruct literals at each site.
var zeroTypeDescriptor TypeDescriptor

// TypeDescriptorField describes a single struct field within a type descriptor.
type TypeDescriptorField struct {
	// Name is the Go identifier of the struct field.
	Name string

	// Tag is the raw struct tag string for the field.
	Tag string

	// PackagePath is the import path of the package that defines the field, empty for
	// exported fields.
	PackagePath string

	// Typ is the type descriptor for the field's type.
	Typ TypeDescriptor
}

// TypeDescriptorData is a serialisation-safe representation of a reflect.Type descriptor.
//
//exhaustruct:ignore
type TypeDescriptorData struct {
	// PackagePath is the import path for named types.
	PackagePath string

	// Name is the type name for named types.
	Name string

	// Elem is the element type for pointers, slices, arrays, and channels.
	Elem *TypeDescriptorData

	// Key is the key type for maps.
	Key *TypeDescriptorData

	// Value is the value type for maps.
	Value *TypeDescriptorData

	// Fields holds struct field descriptors.
	Fields []TypeDescriptorFieldData

	// Params holds function parameter type descriptors.
	Params []TypeDescriptorData

	// Results holds function result type descriptors.
	Results []TypeDescriptorData

	// Length is the array length.
	Length int32

	// Dir is the channel direction.
	Dir int32

	// BasicKind is the reflect.Kind for basic types.
	BasicKind uint8

	// Kind is the TypeDescriptorKind identifying the structural category.
	Kind uint8

	// IsVariadic is true for variadic function types.
	IsVariadic bool
}

// TypeDescriptorFieldData is a serialisation-safe representation of a struct field within
// a type descriptor.
//
//exhaustruct:ignore
type TypeDescriptorFieldData struct {
	// Name is the field name.
	Name string

	// Tag is the struct tag string.
	Tag string

	// PackagePath is the package path for unexported fields.
	PackagePath string

	// Typ is the field's type descriptor.
	Typ TypeDescriptorData
}

// descriptorBuilder recursively converts reflect.Type to TypeDescriptor with cycle
// detection to prevent stack overflow on recursive struct types.
type descriptorBuilder struct {
	// visiting tracks struct types currently being converted to detect and break recursive
	// cycles.
	visiting map[reflect.Type]bool
}

// typeToDescriptor converts a single reflect.Type, tracking visited struct types to break
// cycles.
//
// Takes reflectType (reflect.Type) which is the runtime type to convert.
//
// Returns TypeDescriptor which is the serialisable description of the type.
func (b *descriptorBuilder) typeToDescriptor(reflectType reflect.Type) TypeDescriptor {
	if reflectType == nil {
		return typeDescriptorOf(KindNil)
	}
	switch reflectType.Kind() {
	case reflect.Pointer:
		return b.elementDescriptor(KindPtr, reflectType)

	case reflect.Slice:
		return b.elementDescriptor(KindSlice, reflectType)

	case reflect.Array:
		descriptor := b.elementDescriptor(KindArray, reflectType)
		descriptor.Length = reflectType.Len()
		return descriptor

	case reflect.Map:
		descriptor := typeDescriptorOf(KindMap)
		descriptor.Key = new(b.typeToDescriptor(reflectType.Key()))
		descriptor.Value = new(b.typeToDescriptor(reflectType.Elem()))
		return descriptor

	case reflect.Chan:
		descriptor := b.elementDescriptor(KindChan, reflectType)
		descriptor.Dir = int(reflectType.ChanDir())
		return descriptor

	case reflect.Func:
		return b.functionToDescriptor(reflectType)

	case reflect.Struct:
		return b.structToDescriptor(reflectType)

	case reflect.Interface:
		return typeDescriptorOf(KindInterface)

	default:
		if info, isPool := typemodel.LookupNamedScalarPoolInfo(reflectType); isPool {
			descriptor := typeDescriptorOf(KindNamed)
			descriptor.PackagePath = info.PkgPath
			descriptor.Name = info.BareName
			descriptor.BasicKind = safeconv.MustUintToUint8(uint(reflectType.Kind()))
			return descriptor
		}
		descriptor := typeDescriptorOf(KindBasic)
		descriptor.BasicKind = safeconv.MustUintToUint8(uint(reflectType.Kind()))
		return descriptor
	}
}

// elementDescriptor returns a descriptor for a type whose shape is its element type.
//
// Takes kind (TypeDescriptorKind) which classifies the described type.
// Takes reflectType (reflect.Type) whose Elem supplies the element descriptor.
//
// Returns the descriptor with Kind and Element set.
func (b *descriptorBuilder) elementDescriptor(kind TypeDescriptorKind, reflectType reflect.Type) TypeDescriptor {
	descriptor := typeDescriptorOf(kind)
	descriptor.Element = new(b.typeToDescriptor(reflectType.Elem()))
	return descriptor
}

// functionToDescriptor converts a function reflect.Type to a TypeDescriptor.
//
// Takes reflectType (reflect.Type) which is the function type to convert.
//
// Returns TypeDescriptor which contains the parameter and result type descriptors.
func (b *descriptorBuilder) functionToDescriptor(reflectType reflect.Type) TypeDescriptor {
	parameterDescriptors := make([]TypeDescriptor, 0, reflectType.NumIn())
	for parameterType := range reflectType.Ins() {
		parameterDescriptors = append(parameterDescriptors, b.typeToDescriptor(parameterType))
	}
	resultDescriptors := make([]TypeDescriptor, 0, reflectType.NumOut())
	for resultType := range reflectType.Outs() {
		resultDescriptors = append(resultDescriptors, b.typeToDescriptor(resultType))
	}
	return TypeDescriptor{Kind: KindFunc,
		Params:     parameterDescriptors,
		Results:    resultDescriptors,
		IsVariadic: reflectType.IsVariadic(), PackagePath: "", Name: "", Element: nil, Key: nil, Value: nil, Fields: nil, Length: 0, Dir: 0, BasicKind: 0}
}

// structToDescriptor converts a struct reflect.Type to a TypeDescriptor.
//
// Tracks the struct in the visiting set to detect cycles; if a struct is encountered
// while already being processed, an interface fallback is returned to break the cycle.
//
// Takes reflectType (reflect.Type) which is the struct type to convert.
//
// Returns TypeDescriptor which contains the field descriptors for the struct.
func (b *descriptorBuilder) structToDescriptor(reflectType reflect.Type) TypeDescriptor {
	if b.visiting[reflectType] {
		return TypeDescriptor{
			Kind:        KindInterface,
			PackagePath: "",
			Name:        "",
			Element:     nil,
			Key:         nil,
			Value:       nil,
			Fields:      nil,
			Params:      nil,
			Results:     nil,
			Length:      0,
			Dir:         0,
			BasicKind:   0,
			IsVariadic:  false,
		}
	}
	b.visiting[reflectType] = true
	defer delete(b.visiting, reflectType)

	fieldDescriptors := make([]TypeDescriptorField, 0, reflectType.NumField())
	for field := range reflectType.Fields() {
		fieldDescriptors = append(fieldDescriptors, TypeDescriptorField{
			Name:        field.Name,
			Tag:         string(field.Tag),
			PackagePath: field.PkgPath,
			Typ:         b.typeToDescriptor(field.Type),
		})
	}

	return TypeDescriptor{Kind: KindStruct,
		PackagePath: reflectType.PkgPath(),
		Name:        reflectType.Name(),
		Fields:      fieldDescriptors, Element: nil, Key: nil, Value: nil, Params: nil, Results: nil, Length: 0, Dir: 0, BasicKind: 0, IsVariadic: false}
}

// ReflectTypeToDescriptor converts a reflect.Type into a serialisable TypeDescriptor.
//
// Takes reflectType (reflect.Type) which is the runtime type to describe.
//
// Returns TypeDescriptor which contains enough information to reconstruct the type via
// descriptorToReflectType.
func ReflectTypeToDescriptor(reflectType reflect.Type) TypeDescriptor {
	b := &descriptorBuilder{visiting: make(map[reflect.Type]bool)}
	return b.typeToDescriptor(reflectType)
}

// BasicKindToReflect converts a reflect.Kind for basic types to a reflect.Type.
//
// Takes kind (reflect.Kind) which is the basic type kind to convert.
//
// Returns reflect.Type which is the corresponding runtime type, or any for unsupported
// kinds.
func BasicKindToReflect(kind reflect.Kind) reflect.Type {
	switch kind {
	case reflect.Bool:
		return reflect.TypeFor[bool]()
	case reflect.Int:
		return reflect.TypeFor[int]()
	case reflect.Int8:
		return reflect.TypeFor[int8]()
	case reflect.Int16:
		return reflect.TypeFor[int16]()
	case reflect.Int32:
		return reflect.TypeFor[int32]()
	case reflect.Int64:
		return reflect.TypeFor[int64]()
	case reflect.Uint:
		return reflect.TypeFor[uint]()
	case reflect.Uint8:
		return reflect.TypeFor[uint8]()
	case reflect.Uint16:
		return reflect.TypeFor[uint16]()
	case reflect.Uint32:
		return reflect.TypeFor[uint32]()
	case reflect.Uint64:
		return reflect.TypeFor[uint64]()
	case reflect.Uintptr:
		return reflect.TypeFor[uintptr]()
	case reflect.Float32:
		return reflect.TypeFor[float32]()
	case reflect.Float64:
		return reflect.TypeFor[float64]()
	case reflect.Complex64:
		return reflect.TypeFor[complex64]()
	case reflect.Complex128:
		return reflect.TypeFor[complex128]()
	case reflect.String:
		return reflect.TypeFor[string]()
	case reflect.UnsafePointer:
		return reflect.TypeFor[unsafe.Pointer]()
	default:
		return reflect.TypeFor[any]()
	}
}

// typeDescriptorOf returns a descriptor carrying only its kind.
//
// Takes kind (TypeDescriptorKind) which classifies the described type.
//
// Returns the descriptor with every other field at its zero value.
func typeDescriptorOf(kind TypeDescriptorKind) TypeDescriptor {
	descriptor := zeroTypeDescriptor
	descriptor.Kind = kind
	return descriptor
}
