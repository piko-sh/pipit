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

package typemodel

import (
	"iter"
	"reflect"
	"strconv"
	"strings"
)

// Type is pipit's full-identity reflect.Type representation, carrying source-level type
// identity that Go's reflect cannot represent. Equality is by QualifiedName when set,
// otherwise by nativeRType identity.
type Type struct {
	// nativeRType is the underlying Go reflect.Type handed to native code at the boundary,
	// always non-nil (for named interfaces this is the lossy reflect.TypeFor[any]()
	// collapse).
	nativeRType reflect.Type

	// elem is the element type for Pointer/Slice/Array/Chan/Map (map elem is the value
	// type). nil for non-composite types.
	elem *Type

	// key is the map key type, nil for non-maps.
	key *Type

	// QualifiedName is the rendered source-level type name including any composite
	// decorations (such as "*main.myiface" and "[]main.myiface"). Empty for anonymous types
	// whose identity is purely structural.
	QualifiedName string

	// sourceName is the bare named-type identifier when the type is a named type, otherwise
	// empty.
	sourceName string

	// sourcePackage is the short package name (final segment of pkgPath).
	sourcePackage string

	// pkgPath is the full Go import path of the defining package, or empty for
	// unnamed/predeclared types.
	pkgPath string

	// methodNames lists the sorted method names of an interface type (or the methods of a
	// struct type's method set when populated). Used for Implements and AssignableTo checks.
	methodNames []string

	// inTypes are the parameter types of a function signature.
	inTypes []*Type

	// outTypes are the result types of a function signature.
	outTypes []*Type

	// arrayLen is the array length, 0 for non-arrays.
	arrayLen int

	// kind is the underlying reflect.Kind (Pointer, Interface, Struct, Slice, Map, Array,
	// Chan, Func, basic kinds, etc.). Always set.
	kind reflect.Kind

	// chanDir is the channel direction, 0 for non-channels.
	chanDir reflect.ChanDir

	// variadic reports whether the final inType is a "..." parameter.
	variadic bool
}

// NewNamedInterfaceType builds a Type for a user-declared named interface where Go's
// reflect cannot preserve identity. The nativeRType is the lossy reflect.TypeFor[any]()
// since reflect.InterfaceOf does not exist.
//
// Takes pkgPath (string) which is the defining package's import path.
// Takes sourceName (string) which is the bare type name.
// Takes methodNames ([]string) which lists the interface methods. Must be sorted by the
// caller to keep a canonical iteration order.
//
// Returns the Type.
func NewNamedInterfaceType(pkgPath, sourceName string, methodNames []string) *Type {
	short := shortPackageName(pkgPath)
	qualified := sourceName
	if short != "" {
		qualified = short + "." + sourceName
	}
	return &Type{kind: reflect.Interface,
		QualifiedName: qualified,
		sourceName:    sourceName,
		sourcePackage: short,
		pkgPath:       pkgPath,
		methodNames:   methodNames,
		nativeRType:   reflect.TypeFor[any](), elem: nil, key: nil, inTypes: nil, outTypes: nil, arrayLen: 0, chanDir: 0, variadic: false}
}

// newTypeFromReflect builds a Type from a native reflect.Type.
//
// Used at the native-to-pipit boundary when no registry entry exists for the incoming
// type. Structural fields (kind, elem, key) are populated from the reflect.Type;
// named-type metadata is left empty.
//
// Takes rt (reflect.Type) which is the native reflect.Type to wrap.
//
// Returns *Type with structural identity only. QualifiedName is empty for non-named types
// or set to rt.String() for named ones.
func newTypeFromReflect(rt reflect.Type) *Type {
	if rt == nil {
		return nil
	}
	t := &Type{kind: rt.Kind(),
		nativeRType:   rt,
		elem:          nil,
		key:           nil,
		QualifiedName: "",
		sourceName:    "",
		sourcePackage: "",
		pkgPath:       "",
		methodNames:   nil,
		inTypes:       nil,
		outTypes:      nil,
		arrayLen:      0,
		chanDir:       0,
		variadic:      false}
	populateNamedTypeIdentity(t, rt)
	populateCompositeShape(t, rt)
	return t
}

// String returns the package-qualified source-level type name.
//
// Includes any pointer, slice, array, map, channel, or func decorations. Matches
// reflect.Type.String().
//
// Returns string which is the rendered type name.
func (t *Type) String() string {
	if t == nil {
		return ""
	}
	if t.QualifiedName != "" {
		return t.QualifiedName
	}
	return t.nativeRType.String()
}

// PkgPath returns the full import path of the defining package.
//
// Empty for unnamed types. Matches reflect.Type.PkgPath().
//
// Returns string which is the import path or "".
func (t *Type) PkgPath() string {
	if t == nil {
		return ""
	}
	return t.pkgPath
}

// Kind returns the underlying reflect.Kind. Matches reflect.Type.Kind().
//
// Returns reflect.Kind which is the kind of the type.
func (t *Type) Kind() reflect.Kind {
	if t == nil {
		return reflect.Invalid
	}

	return t.kind
}

// Elem returns the element type for Pointer/Slice/Array/Chan/Map.
//
// For Map, this is the value type; use Key for the key type.
//
// Returns *Type which is the element type, or nil for invalid kinds.
func (t *Type) Elem() *Type {
	if t == nil {
		return nil
	}
	return t.elem
}

// Key returns the key type of a map.
//
// Returns *Type which is the map key type, or nil for non-maps.
func (t *Type) Key() *Type {
	if t == nil {
		return nil
	}
	return t.key
}

// Len returns the array length for Array kinds. Matches reflect.Type.Len.
//
// Returns int which is the array length, or 0 for non-arrays.
func (t *Type) Len() int {
	if t == nil {
		return 0
	}
	return t.arrayLen
}

// ChanDir returns the channel direction. Matches reflect.Type.ChanDir.
//
// Returns reflect.ChanDir which is the channel direction.
func (t *Type) ChanDir() reflect.ChanDir {
	if t == nil {
		return reflect.BothDir
	}
	return t.chanDir
}

// Ins returns an iterator over function parameter types.
//
// Returns iter.Seq[*Type] which yields each parameter in order.
func (t *Type) Ins() iter.Seq[*Type] {
	return func(yield func(*Type) bool) {
		if t == nil {
			return
		}
		for _, p := range t.inTypes {
			if !yield(p) {
				return
			}
		}
	}
}

// Outs returns an iterator over function result types.
//
// Returns iter.Seq[*Type] which yields each result in order.
func (t *Type) Outs() iter.Seq[*Type] {
	return func(yield func(*Type) bool) {
		if t == nil {
			return
		}
		for _, p := range t.outTypes {
			if !yield(p) {
				return
			}
		}
	}
}

// Fields returns an iterator over each struct field.
//
// Returns iter.Seq[reflect.StructField] which yields each field in order.
func (t *Type) Fields() iter.Seq[reflect.StructField] {
	return func(yield func(reflect.StructField) bool) {
		if t == nil || t.Kind() != reflect.Struct {
			return
		}
		n := t.nativeRType.NumField()
		for i := range n {
			if !yield(t.nativeRType.Field(i)) {
				return
			}
		}
	}
}

// FieldByIndex returns the nested struct field for the index sequence.
//
// Takes index ([]int) which is the index path through embedded fields.
//
// Returns reflect.StructField which is the resolved field descriptor.
func (t *Type) FieldByIndex(index []int) reflect.StructField {
	if t == nil || t.Kind() != reflect.Struct {
		return reflect.StructField{}
	}
	return t.nativeRType.FieldByIndex(index)
}

// FieldByName looks up a struct field by name.
//
// Takes name (string) which is the field name.
//
// Returns reflect.StructField which is the field descriptor when found.
// Returns bool which reports whether a matching field was found.
func (t *Type) FieldByName(name string) (reflect.StructField, bool) {
	if t == nil || t.Kind() != reflect.Struct {
		return reflect.StructField{}, false
	}
	return t.nativeRType.FieldByName(name)
}

// FieldByNameFunc looks up a struct field whose name satisfies match.
//
// Takes match (func(string) bool) which tests each field name.
//
// Returns reflect.StructField which is the field descriptor when found.
// Returns bool which reports whether a matching field was found.
func (t *Type) FieldByNameFunc(match func(string) bool) (reflect.StructField, bool) {
	if t == nil || t.Kind() != reflect.Struct {
		return reflect.StructField{}, false
	}
	return t.nativeRType.FieldByNameFunc(match)
}

// Size returns the in-memory size in bytes. Delegates to nativeRType.
//
// Returns uintptr which is the size in bytes.
func (t *Type) Size() uintptr {
	if t == nil {
		return 0
	}
	return t.nativeRType.Size()
}

// Align returns the in-memory alignment in bytes.
//
// Returns int which is the alignment in bytes.
func (t *Type) Align() int {
	if t == nil {
		return 0
	}
	return t.nativeRType.Align()
}

// FieldAlign returns the alignment when used as a struct field.
//
// Returns int which is the field alignment in bytes.
func (t *Type) FieldAlign() int {
	if t == nil {
		return 0
	}
	return t.nativeRType.FieldAlign()
}

// Bits returns the bit width for numeric kinds.
//
// Returns int which is the bit width.
func (t *Type) Bits() int {
	if t == nil {
		return 0
	}
	return t.nativeRType.Bits()
}

// OverflowUint reports whether x cannot be represented in t.
//
// Takes x (uint64) which is the candidate value.
//
// Returns bool which reports whether x overflows t.
func (t *Type) OverflowUint(x uint64) bool {
	if t == nil {
		return false
	}
	return t.nativeRType.OverflowUint(x)
}

// OverflowFloat reports whether x cannot be represented in t.
//
// Takes x (float64) which is the candidate value.
//
// Returns bool which reports whether x overflows t.
func (t *Type) OverflowFloat(x float64) bool {
	if t == nil {
		return false
	}
	return t.nativeRType.OverflowFloat(x)
}

// OverflowComplex reports whether x cannot be represented in t.
//
// Takes x (complex128) which is the candidate value.
//
// Returns bool which reports whether x overflows t.
func (t *Type) OverflowComplex(x complex128) bool {
	if t == nil {
		return false
	}
	return t.nativeRType.OverflowComplex(x)
}

// name returns the source-level bare type name for named types.
//
// Matches reflect.Type.Name. Empty string for unnamed types.
//
// Returns string which is the bare type name or "".
func (t *Type) name() string {
	if t == nil {
		return ""
	}
	return t.sourceName
}

// setKind overrides the type's reflect kind.
//
// Exposed for tests that need a Type shape the constructors do not produce, such as the
// empty interface.
//
// Takes kind (reflect.Kind) which becomes the type's kind.
func (t *Type) setKind(kind reflect.Kind) { t.kind = kind }

// numMethod returns the count of accessible methods on the type.
//
// Returns int which is the method count.
func (t *Type) numMethod() int {
	if t == nil {
		return 0
	}
	if t.Kind() == reflect.Interface {
		return len(t.methodNames)
	}
	return t.nativeRType.NumMethod()
}

// method returns the i-th method.
//
// For interfaces, synthesises a reflect.Method record from methodNames; for
// non-interfaces delegates to nativeRType.Method.
//
// Takes i (int) which is the method index.
//
// Returns reflect.Method which is the method descriptor.
func (t *Type) method(i int) reflect.Method {
	if t == nil || i < 0 {
		return reflect.Method{}
	}
	if t.Kind() == reflect.Interface {
		if i >= len(t.methodNames) {
			return reflect.Method{}
		}
		return reflect.Method{
			Name:  t.methodNames[i],
			Index: i,
		}
	}
	return t.nativeRType.Method(i)
}

// methodByName looks up a method by its source-level name.
//
// For interfaces, scans methodNames; for non-interfaces delegates to
// nativeRType.methodByName.
//
// Takes name (string) which is the method name.
//
// Returns reflect.Method which is the method descriptor when found.
// Returns bool which reports whether a matching method was found.
func (t *Type) methodByName(name string) (reflect.Method, bool) {
	if t == nil {
		return reflect.Method{}, false
	}
	if t.Kind() == reflect.Interface {
		for i, n := range t.methodNames {
			if n == name {
				return reflect.Method{Name: n, Index: i}, true
			}
		}
		return reflect.Method{}, false
	}
	return t.nativeRType.MethodByName(name)
}

// implements reports whether t satisfies the interface u.
//
// When u is a Type interface the method-set check runs pipit-side; otherwise it delegates
// to nativeRType.
//
// Takes u (*Type) which is the interface type to test against.
//
// Returns bool which reports whether t satisfies u.
func (t *Type) implements(u *Type) bool {
	if t == nil || u == nil {
		return false
	}
	if u.Kind() != reflect.Interface {
		return false
	}
	if len(u.methodNames) == 0 {
		return true
	}
	if t.Kind() == reflect.Interface {
		return methodSetSatisfies(t.methodNames, u.methodNames)
	}
	return t.nativeRType.Implements(u.nativeRType)
}

// assignableTo reports whether t is assignable to u.
//
// Computed via pipit-side equality when both have qualified names; otherwise falls back
// to native reflect.
//
// Takes u (*Type) which is the destination type.
//
// Returns bool which reports whether the assignment is permitted.
func (t *Type) assignableTo(u *Type) bool {
	if t == nil || u == nil {
		return false
	}
	if t.QualifiedName != "" && t.QualifiedName == u.QualifiedName {
		return true
	}
	if u.Kind() == reflect.Interface {
		return t.implements(u)
	}
	return t.nativeRType.AssignableTo(u.nativeRType)
}

// convertibleTo reports whether t is convertible to u.
//
// Falls back to native reflect for the structural rules.
//
// Takes u (*Type) which is the destination type.
//
// Returns bool which reports whether the conversion is permitted.
func (t *Type) convertibleTo(u *Type) bool {
	if t == nil || u == nil {
		return false
	}
	if t.assignableTo(u) {
		return true
	}
	return t.nativeRType.ConvertibleTo(u.nativeRType)
}

// isComparable reports whether values of t are comparable.
//
// Delegates to native reflect, which is correct for every kind.
//
// Returns bool which reports whether t is comparable.
func (t *Type) isComparable() bool {
	if t == nil {
		return false
	}
	return t.nativeRType.Comparable()
}

// numIn returns the function parameter count.
//
// Returns int which is the number of input parameters.
func (t *Type) numIn() int {
	if t == nil {
		return 0
	}
	return len(t.inTypes)
}

// numOut returns the function result count.
//
// Returns int which is the number of output results.
func (t *Type) numOut() int {
	if t == nil {
		return 0
	}
	return len(t.outTypes)
}

// in returns the i-th function parameter type.
//
// Takes i (int) which is the parameter index.
//
// Returns *Type which is the parameter type, or nil when out of range.
func (t *Type) in(i int) *Type {
	if t == nil || i < 0 || i >= len(t.inTypes) {
		return nil
	}
	return t.inTypes[i]
}

// out returns the i-th function result type.
//
// Takes i (int) which is the result index.
//
// Returns *Type which is the result type, or nil when out of range.
func (t *Type) out(i int) *Type {
	if t == nil || i < 0 || i >= len(t.outTypes) {
		return nil
	}
	return t.outTypes[i]
}

// isVariadic reports whether the final input is a "..." parameter.
//
// Returns bool which reports whether t is a variadic function type.
func (t *Type) isVariadic() bool {
	if t == nil {
		return false
	}
	return t.variadic
}

// numField returns the field count for Struct kinds.
//
// Returns int which is the number of struct fields.
func (t *Type) numField() int {
	if t == nil || t.Kind() != reflect.Struct {
		return 0
	}
	return t.nativeRType.NumField()
}

// field returns the i-th struct field.
//
// Takes i (int) which is the field index.
//
// Returns reflect.StructField which is the field descriptor.
func (t *Type) field(i int) reflect.StructField {
	if t == nil || t.Kind() != reflect.Struct {
		return reflect.StructField{}
	}
	return t.nativeRType.Field(i)
}

// overflowInt reports whether x cannot be represented in t.
//
// Takes x (int64) which is the candidate value.
//
// Returns bool which reports whether x overflows t.
func (t *Type) overflowInt(x int64) bool {
	if t == nil {
		return false
	}
	return t.nativeRType.OverflowInt(x)
}

// TypeOfPointer constructs a pointer-to-elem Type.
//
// The native reflect.Type is built via reflect.PointerTo(elem.nativeRType) so the result
// is a real Go pointer type usable at the native boundary.
//
// Takes elem (*Type) which is the pointee type.
//
// Returns *Type which is the pointer type wrapping elem.
func TypeOfPointer(elem *Type) *Type {
	return &Type{kind: reflect.Pointer,
		QualifiedName: "*" + elem.QualifiedName,
		elem:          elem,
		nativeRType:   reflect.PointerTo(elem.nativeRType),
		key:           nil,
		sourceName:    "",
		sourcePackage: "",
		pkgPath:       "",
		methodNames:   nil,
		inTypes:       nil,
		outTypes:      nil,
		arrayLen:      0,
		chanDir:       0,
		variadic:      false}
}

// populateNamedTypeIdentity fills named-type metadata on t from rt.
//
// Sets sourceName, pkgPath, sourcePackage, and QualifiedName when rt is a named type,
// otherwise sets QualifiedName to the reflect.Type's natural rendering.
//
// Takes t (*Type) which receives the populated metadata.
// Takes rt (reflect.Type) which is the source reflect.Type.
func populateNamedTypeIdentity(t *Type, rt reflect.Type) {
	if rt.Name() != "" {
		t.sourceName = rt.Name()
		t.pkgPath = rt.PkgPath()
		if t.pkgPath != "" {
			t.sourcePackage = shortPackageName(t.pkgPath)
			t.QualifiedName = t.sourcePackage + "." + t.sourceName
		} else {
			t.QualifiedName = t.sourceName
		}
		return
	}
	t.QualifiedName = rt.String()
}

// populateCompositeShape fills composite-shape fields on t from rt.
//
// Populates elem, key, arrayLen, chanDir, inTypes, outTypes, variadic, and methodNames
// based on rt.Kind(). Scalar and struct kinds rely on nativeRType for their introspection
// so nothing is populated for them.
//
// Takes t (*Type) which receives the populated metadata.
// Takes rt (reflect.Type) which is the source reflect.Type.
func populateCompositeShape(t *Type, rt reflect.Type) {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice:
		t.elem = newTypeFromReflect(rt.Elem())
	case reflect.Array:
		t.elem = newTypeFromReflect(rt.Elem())
		t.arrayLen = rt.Len()
	case reflect.Chan:
		t.elem = newTypeFromReflect(rt.Elem())
		t.chanDir = rt.ChanDir()
	case reflect.Map:
		t.elem = newTypeFromReflect(rt.Elem())
		t.key = newTypeFromReflect(rt.Key())
	case reflect.Func:
		t.variadic = rt.IsVariadic()
		t.inTypes = make([]*Type, rt.NumIn())
		for i := range t.inTypes {
			t.inTypes[i] = newTypeFromReflect(rt.In(i))
		}
		t.outTypes = make([]*Type, rt.NumOut())
		for i := range t.outTypes {
			t.outTypes[i] = newTypeFromReflect(rt.Out(i))
		}
	case reflect.Interface:
		n := rt.NumMethod()
		t.methodNames = make([]string, n)
		for i := range t.methodNames {
			t.methodNames[i] = rt.Method(i).Name
		}
	default:
	}
}

// shortPackageName returns the final '/' segment of pkgPath, matching Go's
// reflect.Type.String() convention for package-qualified names when the package directory
// and package name agree.
//
// Takes pkgPath (string) which is the Go import path.
//
// Returns the package short name, or "" when pkgPath is empty.
func shortPackageName(pkgPath string) string {
	if pkgPath == "" {
		return ""
	}
	if slash := strings.LastIndexByte(pkgPath, '/'); slash >= 0 {
		return pkgPath[slash+1:]
	}
	return pkgPath
}

// pipitTypeOfSlice constructs a slice-of-elem Type.
//
// Takes elem (*Type) which is the element type.
//
// Returns *Type which is the slice type wrapping elem.
func pipitTypeOfSlice(elem *Type) *Type {
	return &Type{kind: reflect.Slice,
		QualifiedName: "[]" + elem.QualifiedName,
		elem:          elem,
		nativeRType:   reflect.SliceOf(elem.nativeRType),
		key:           nil,
		sourceName:    "",
		sourcePackage: "",
		pkgPath:       "",
		methodNames:   nil,
		inTypes:       nil,
		outTypes:      nil,
		arrayLen:      0,
		chanDir:       0,
		variadic:      false}
}

// pipitTypeOfArray constructs an array-of-elem Type.
//
// Takes length (int) which is the array length.
// Takes elem (*Type) which is the element type.
//
// Returns *Type which is the array type with the given length.
func pipitTypeOfArray(length int, elem *Type) *Type {
	return &Type{kind: reflect.Array,
		QualifiedName: "[" + strconv.Itoa(length) + "]" + elem.QualifiedName,
		elem:          elem,
		arrayLen:      length,
		nativeRType:   reflect.ArrayOf(length, elem.nativeRType), key: nil, sourceName: "", sourcePackage: "", pkgPath: "", methodNames: nil, inTypes: nil, outTypes: nil, chanDir: 0, variadic: false}
}

// pipitTypeOfMap constructs a map[key]elem Type.
//
// Takes key (*Type) which is the map key type.
// Takes elem (*Type) which is the map value type.
//
// Returns *Type which is the map type.
func pipitTypeOfMap(key, elem *Type) *Type {
	return &Type{kind: reflect.Map,
		QualifiedName: "map[" + key.QualifiedName + "]" + elem.QualifiedName,
		elem:          elem,
		key:           key,
		nativeRType:   reflect.MapOf(key.nativeRType, elem.nativeRType),
		sourceName:    "",
		sourcePackage: "",
		pkgPath:       "",
		methodNames:   nil,
		inTypes:       nil,
		outTypes:      nil,
		arrayLen:      0,
		chanDir:       0,
		variadic:      false}
}

// pipitTypeOfChan constructs a chan-of-elem Type with direction dir.
//
// Takes dir (reflect.ChanDir) which is the channel direction.
// Takes elem (*Type) which is the channel element type.
//
// Returns *Type which is the channel type.
func pipitTypeOfChan(dir reflect.ChanDir, elem *Type) *Type {
	prefix := "chan "
	switch dir {
	case reflect.RecvDir:
		prefix = "<-chan "
	case reflect.SendDir:
		prefix = "chan<- "
	case reflect.BothDir:
	}
	return &Type{kind: reflect.Chan,
		QualifiedName: prefix + elem.QualifiedName,
		elem:          elem,
		chanDir:       dir,
		nativeRType:   reflect.ChanOf(dir, elem.nativeRType), key: nil, sourceName: "", sourcePackage: "", pkgPath: "", methodNames: nil, inTypes: nil, outTypes: nil, arrayLen: 0, variadic: false}
}

// pipitTypeOfFunction constructs a function-signature Type.
//
// Takes in ([]*Type) which is the parameter type list.
// Takes out ([]*Type) which is the result type list.
// Takes variadic (bool) which reports whether the final parameter is a "..." variadic
// parameter.
//
// Returns *Type which is the function-signature type.
func pipitTypeOfFunction(in, out []*Type, variadic bool) *Type {
	inRT := make([]reflect.Type, len(in))
	for i, p := range in {
		inRT[i] = p.nativeRType
	}
	outRT := make([]reflect.Type, len(out))
	for i, p := range out {
		outRT[i] = p.nativeRType
	}
	return &Type{kind: reflect.Func,
		QualifiedName: renderFunctionQualifiedName(in, out, variadic),
		inTypes:       in,
		outTypes:      out,
		variadic:      variadic,
		nativeRType:   reflect.FuncOf(inRT, outRT, variadic), elem: nil, key: nil, sourceName: "", sourcePackage: "", pkgPath: "", methodNames: nil, arrayLen: 0, chanDir: 0}
}

// renderFunctionQualifiedName produces a Go-syntax rendering of a function signature
// suitable for Type.QualifiedName.
//
// Takes in ([]*Type) which is the parameter type list.
// Takes out ([]*Type) which is the result type list.
// Takes variadic (bool) which reports whether the final parameter is variadic.
//
// Returns string which is the Go-syntax rendering.
func renderFunctionQualifiedName(in, out []*Type, variadic bool) string {
	var sb strings.Builder
	sb.WriteString("func(")
	for i, p := range in {
		if i > 0 {
			sb.WriteString(", ")
		}
		if variadic && i == len(in)-1 && p.Kind() == reflect.Slice {
			sb.WriteString("...")
			sb.WriteString(p.elem.QualifiedName)
		} else {
			sb.WriteString(p.QualifiedName)
		}
	}
	sb.WriteByte(')')
	switch len(out) {
	case 0:
	case 1:
		sb.WriteByte(' ')
		sb.WriteString(out[0].QualifiedName)
	default:
		sb.WriteString(" (")
		for i, p := range out {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(p.QualifiedName)
		}
		sb.WriteByte(')')
	}
	return sb.String()
}

// methodSetSatisfies reports whether available contains every required name.
//
// Both slices must be sorted; the check is O(len(available) + len(required)).
//
// Takes available ([]string) which is the sorted method-set of the subject type.
// Takes required ([]string) which is the sorted method-set the subject must satisfy.
//
// Returns bool which reports whether every required name is present.
func methodSetSatisfies(available, required []string) bool {
	if len(required) == 0 {
		return true
	}
	if len(available) < len(required) {
		return false
	}
	i, j := 0, 0
	for i < len(available) && j < len(required) {
		switch {
		case available[i] < required[j]:
			i++
		case available[i] == required[j]:
			i++
			j++
		default:
			return false
		}
	}
	return j == len(required)
}
