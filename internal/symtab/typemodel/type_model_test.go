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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypeNamedInterface(t *testing.T) {
	t.Parallel()
	pt := NewNamedInterfaceType("github.com/foo/bar", "MyIface", []string{"Foo", "Bar"})
	require.Equal(t, "MyIface", pt.name())
	require.Equal(t, "bar.MyIface", pt.String())
	require.Equal(t, "github.com/foo/bar", pt.PkgPath())
	require.Equal(t, reflect.Interface, pt.Kind())
	require.Equal(t, 2, pt.numMethod())
	require.Equal(t, "Foo", pt.method(0).Name)
	require.Equal(t, "Bar", pt.method(1).Name)
	method, ok := pt.methodByName("Foo")
	require.True(t, ok)
	require.Equal(t, "Foo", method.Name)
	_, ok = pt.methodByName("Missing")
	require.False(t, ok)
}

func TestTypeNamedInterfaceNoPackage(t *testing.T) {
	t.Parallel()
	pt := NewNamedInterfaceType("", "error", []string{"Error"})
	require.Equal(t, "error", pt.name())
	require.Equal(t, "error", pt.String())
}

func TestTypePointerToInterface(t *testing.T) {
	t.Parallel()
	inner := NewNamedInterfaceType("main", "myiface", []string{"Foo"})
	ptr := TypeOfPointer(inner)
	require.Equal(t, "*main.myiface", ptr.String())
	require.Equal(t, "", ptr.name(), "pointer types have empty Name (Go convention)")
	require.Equal(t, reflect.Pointer, ptr.Kind())
	require.Equal(t, "main.myiface", ptr.Elem().String())
	require.Equal(t, reflect.Interface, ptr.Elem().Kind())
}

func TestTypeSliceOfInterface(t *testing.T) {
	t.Parallel()
	inner := NewNamedInterfaceType("main", "myiface", []string{"Foo"})
	slice := pipitTypeOfSlice(inner)
	require.Equal(t, "[]main.myiface", slice.String())
	require.Equal(t, reflect.Slice, slice.Kind())
	require.Equal(t, "main.myiface", slice.Elem().String())
}

func TestTypeMapOfInterface(t *testing.T) {
	t.Parallel()
	key := newTypeFromReflect(reflect.TypeFor[string]())
	value := NewNamedInterfaceType("main", "myiface", []string{"Foo"})
	m := pipitTypeOfMap(key, value)
	require.Equal(t, "map[string]main.myiface", m.String())
	require.Equal(t, reflect.Map, m.Kind())
	require.Equal(t, "string", m.Key().String())
	require.Equal(t, "main.myiface", m.Elem().String())
}

func TestTypeArrayOfInterface(t *testing.T) {
	t.Parallel()
	inner := NewNamedInterfaceType("main", "myiface", []string{"Foo"})
	arr := pipitTypeOfArray(5, inner)
	require.Equal(t, "[5]main.myiface", arr.String())
	require.Equal(t, reflect.Array, arr.Kind())
	require.Equal(t, 5, arr.Len())
}

func TestTypeChanOfInterface(t *testing.T) {
	t.Parallel()
	inner := NewNamedInterfaceType("main", "myiface", []string{"Foo"})
	bidi := pipitTypeOfChan(reflect.BothDir, inner)
	require.Equal(t, "chan main.myiface", bidi.String())
	require.Equal(t, reflect.BothDir, bidi.ChanDir())

	receive := pipitTypeOfChan(reflect.RecvDir, inner)
	require.Equal(t, "<-chan main.myiface", receive.String())
	require.Equal(t, reflect.RecvDir, receive.ChanDir())

	send := pipitTypeOfChan(reflect.SendDir, inner)
	require.Equal(t, "chan<- main.myiface", send.String())
	require.Equal(t, reflect.SendDir, send.ChanDir())
}

func TestTypeImplementsEmptyInterface(t *testing.T) {
	t.Parallel()
	user := NewNamedInterfaceType("main", "myiface", []string{"Foo"})
	empty := NewNamedInterfaceType("", "", nil)
	empty.setKind(reflect.Interface)
	require.True(t, user.implements(empty), "every type implements the empty interface")
}

func TestTypeImplementsSubsetMethodSet(t *testing.T) {
	t.Parallel()
	bigger := NewNamedInterfaceType("main", "Big", []string{"A", "B", "C"})
	smaller := NewNamedInterfaceType("main", "Small", []string{"A", "B"})
	require.True(t, bigger.implements(smaller), "Big has A, B, C and Small wants A, B")
	require.False(t, smaller.implements(bigger), "Small lacks C")
}

func TestTypeAssignableToSameName(t *testing.T) {
	t.Parallel()
	a := NewNamedInterfaceType("main", "myiface", []string{"Foo"})
	b := NewNamedInterfaceType("main", "myiface", []string{"Foo"})
	require.True(t, a.assignableTo(b))
	require.True(t, b.assignableTo(a))
}

func TestTypeFromReflectStdlibInterface(t *testing.T) {
	t.Parallel()
	pt := newTypeFromReflect(reflect.TypeFor[error]())
	require.Equal(t, "error", pt.name())
	require.Equal(t, "error", pt.String())
	require.Equal(t, reflect.Interface, pt.Kind())
	require.GreaterOrEqual(t, pt.numMethod(), 1)
}

func TestTypeFromReflectScalar(t *testing.T) {
	t.Parallel()
	pt := newTypeFromReflect(reflect.TypeFor[int]())
	require.Equal(t, "int", pt.name())
	require.Equal(t, reflect.Int, pt.Kind())
}

func TestTypeFromReflectStruct(t *testing.T) {
	t.Parallel()
	type myStruct struct{ X int }
	pt := newTypeFromReflect(reflect.TypeFor[myStruct]())
	require.Equal(t, "myStruct", pt.name())
	require.Equal(t, reflect.Struct, pt.Kind())
	require.Equal(t, 1, pt.numField())
	require.Equal(t, "X", pt.field(0).Name)
}

func TestTypeFuncSignature(t *testing.T) {
	t.Parallel()
	intT := newTypeFromReflect(reflect.TypeFor[int]())
	stringT := newTypeFromReflect(reflect.TypeFor[string]())
	errorT := newTypeFromReflect(reflect.TypeFor[error]())

	fn := pipitTypeOfFunction([]*Type{intT, stringT}, []*Type{errorT}, false)
	require.Equal(t, reflect.Func, fn.Kind())
	require.Equal(t, 2, fn.numIn())
	require.Equal(t, 1, fn.numOut())
	require.Equal(t, "int", fn.in(0).name())
	require.Equal(t, "string", fn.in(1).name())
	require.Equal(t, "error", fn.out(0).name())
	require.False(t, fn.isVariadic())
	require.Equal(t, "func(int, string) error", fn.String())
}

func TestTypeFuncVariadic(t *testing.T) {
	t.Parallel()
	intT := newTypeFromReflect(reflect.TypeFor[int]())
	intsT := pipitTypeOfSlice(intT)
	fn := pipitTypeOfFunction([]*Type{intT, intsT}, nil, true)
	require.Equal(t, "func(int, ...int)", fn.String())
	require.True(t, fn.isVariadic())
}

func TestTypeOverflow(t *testing.T) {
	t.Parallel()
	pt := newTypeFromReflect(reflect.TypeFor[int8]())
	require.True(t, pt.overflowInt(200))
	require.False(t, pt.overflowInt(50))
}

func TestTypeNilSafety(t *testing.T) {
	t.Parallel()
	var pt *Type
	require.Equal(t, "", pt.name())
	require.Equal(t, "", pt.String())
	require.Equal(t, "", pt.PkgPath())
	require.Equal(t, reflect.Invalid, pt.Kind())
	require.Equal(t, 0, pt.numMethod())
	require.Equal(t, 0, pt.numField())
	require.Equal(t, 0, pt.numIn())
	require.Equal(t, 0, pt.numOut())
	require.Nil(t, pt.Elem())
	require.Nil(t, pt.Key())
	require.False(t, pt.isComparable())
}

func TestMethodSetSatisfies(t *testing.T) {
	t.Parallel()
	require.True(t, methodSetSatisfies([]string{"A", "B", "C"}, []string{"A", "C"}))
	require.True(t, methodSetSatisfies([]string{"A", "B"}, nil))
	require.False(t, methodSetSatisfies([]string{"A", "C"}, []string{"A", "B"}))
	require.False(t, methodSetSatisfies(nil, []string{"A"}))
}

func TestShortPackageName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "main", shortPackageName("main"))
	require.Equal(t, "bar", shortPackageName("github.com/foo/bar"))
	require.Equal(t, "", shortPackageName(""))
	require.Equal(t, "v1", shortPackageName("k8s.io/api/core/v1"))
}
