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
	"go/types"
	"reflect"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/policy"
)

type sectionLikeNode struct {
	Name     string
	Children []sectionLikeNode
}

type mutualA struct {
	B     *mutualB
	Label string
}

type mutualB struct {
	A      *mutualA
	Number int
}

func TestSymbolRegistryHasPackage(t *testing.T) {
	t.Parallel()
	reg := NewSymbolRegistry(SymbolExports{
		"fmt":     {"Println": reflect.ValueOf(func(...any) {})},
		"strings": {"Contains": reflect.ValueOf(func(string, string) bool { return false })},
	})
	require.True(t, reg.HasPackage("fmt"))
	require.True(t, reg.HasPackage("strings"))
	require.False(t, reg.HasPackage("os"))
}

func TestSymbolRegistryAllPackages(t *testing.T) {
	t.Parallel()
	reg := NewSymbolRegistry(SymbolExports{
		"fmt":     {"Println": reflect.ValueOf(0)},
		"strings": {"Contains": reflect.ValueOf(0)},
	})
	pkgs := reg.AllPackages()
	sort.Strings(pkgs)
	require.Equal(t, []string{"fmt", "strings"}, pkgs)
}

func TestSymbolRegistryZeroValueForType(t *testing.T) {
	t.Parallel()

	type MyStruct struct {
		X int
	}

	reg := NewSymbolRegistry(SymbolExports{
		"mypkg": {
			"MyStruct": reflect.ValueOf((*MyStruct)(nil)),
		},
	})

	value, ok := reg.ZeroValueForType("mypkg", "MyStruct")
	require.True(t, ok)
	require.Equal(t, reflect.Struct, value.Kind())
	require.Equal(t, 0, int(value.FieldByName("X").Int()))

	_, ok = reg.ZeroValueForType("mypkg", "NoSuch")
	require.False(t, ok)
}

func TestSymbolRegistryZeroValueForTypeKeepsNamedSliceZerosOutOfReach(t *testing.T) {
	t.Parallel()

	type Names []string

	reg := NewSymbolRegistry(SymbolExports{
		"mypkg": {"Names": reflect.ValueOf((*Names)(nil))},
	})

	value, ok := reg.ZeroValueForType("mypkg", "Names")
	require.True(t, ok)
	require.Equal(t, reflect.TypeFor[Names](), value.Type())
	require.True(t, value.IsNil())
	require.False(t, value.CanSet(), "an in-place append must not be able to grow the shared zero constant")
}

func TestZeroConstantIsAddressableOnlyForStructsAndArrays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		typ         reflect.Type
		addressable bool
	}{
		{typ: reflect.TypeFor[struct{ X int }](), addressable: true},
		{typ: reflect.TypeFor[[4]int](), addressable: true},
		{typ: reflect.TypeFor[[]int](), addressable: false},
		{typ: reflect.TypeFor[[]string](), addressable: false},
		{typ: reflect.TypeFor[map[string]int](), addressable: false},
		{typ: reflect.TypeFor[*int](), addressable: false},
		{typ: reflect.TypeFor[string](), addressable: false},
		{typ: reflect.TypeFor[chan int](), addressable: false},
	}
	for _, tt := range tests {
		t.Run(tt.typ.String(), func(t *testing.T) {
			t.Parallel()
			zero := ZeroConstant(tt.typ)
			require.Equal(t, tt.typ, zero.Type())
			require.True(t, zero.IsZero())
			require.Equal(t, tt.addressable, zero.CanSet())
		})
	}
}

func TestSymbolRegistryZeroValueForTypeNonPointer(t *testing.T) {
	t.Parallel()
	reg := NewSymbolRegistry(SymbolExports{
		"pkg": {"Func": reflect.ValueOf(func() {})},
	})
	_, ok := reg.ZeroValueForType("pkg", "Func")
	require.False(t, ok)
}

func TestSymbolRegistryTypeOwnersFacadeAlias(t *testing.T) {
	t.Parallel()

	reg := NewSymbolRegistry(SymbolExports{
		"myfacade": {
			"MyTime": reflect.ValueOf((*time.Time)(nil)),
		},
		"consumer": {
			"Format": reflect.ValueOf(func(t time.Time) string { return t.String() }),
		},
	})

	rt := reflect.TypeFor[time.Time]()
	require.Equal(t, "myfacade", reg.typeOwners[rt])

	reg.SynthesiseAll(t.Context())

	facadePkg, err := reg.Import(t.Context(), "myfacade")
	require.NoError(t, err)
	object := facadePkg.Scope().Lookup("MyTime")
	require.NotNil(t, object, "MyTime should be in facade scope")

	consumerPkg, err := reg.Import(t.Context(), "consumer")
	require.NoError(t, err)
	fmtObj := consumerPkg.Scope().Lookup("Format")
	require.NotNil(t, fmtObj, "Format should be in consumer scope")

	signature, ok := fmtObj.Type().(*types.Signature)
	require.True(t, ok, "expected *types.Signature, got %T", fmtObj.Type())
	paramType := signature.Params().At(0).Type()
	named, ok := paramType.(*types.Named)
	require.True(t, ok, "parameter should be named type, got %T: %v", paramType, paramType)
	require.Equal(t, "MyTime", named.Obj().Name())
	require.Equal(t, "myfacade", named.Obj().Pkg().Path())
}

type linkedListNode struct {
	Next  *linkedListNode
	Value int
}

type treeByKey struct {
	Children map[string]*treeByKey
	Name     string
}

type recursiveDAG struct {
	ID       string
	Children []*recursiveDAG
}

func TestSymbolRegistrySelfReferentialStruct(t *testing.T) {
	t.Parallel()

	reg := NewSymbolRegistry(SymbolExports{
		"tree": {
			"Node": reflect.ValueOf((*sectionLikeNode)(nil)),
		},
	})

	pkg, err := reg.Import(t.Context(), "tree")
	require.NoError(t, err)

	object := pkg.Scope().Lookup("Node")
	require.NotNil(t, object, "Node should be registered")
	named, ok := object.Type().(*types.Named)
	require.True(t, ok, "Node should be a named type, got %T", object.Type())

	underlying, ok := named.Underlying().(*types.Struct)
	require.True(t, ok, "underlying should be a struct, got %T", named.Underlying())

	childrenField := findStructField(t, underlying, "Children")
	slice, ok := childrenField.Type().(*types.Slice)
	require.True(t, ok, "Children should be a slice, got %T", childrenField.Type())

	elem := slice.Elem()
	elemNamed, ok := elem.(*types.Named)
	require.True(t, ok,
		"Children element type should be the named SectionNode-like type, got %T (%v)",
		elem, elem)
	require.Same(t, named, elemNamed,
		"Children element should be the same *types.Named as the parent")
}

func TestSymbolRegistrySelfReferentialPointer(t *testing.T) {
	t.Parallel()

	reg := NewSymbolRegistry(SymbolExports{
		"ll": {
			"Node": reflect.ValueOf((*linkedListNode)(nil)),
		},
	})

	pkg, err := reg.Import(t.Context(), "ll")
	require.NoError(t, err)

	named, ok := pkg.Scope().Lookup("Node").Type().(*types.Named)
	require.True(t, ok)

	underlying, ok := named.Underlying().(*types.Struct)
	require.True(t, ok, "underlying should be a struct, got %T", named.Underlying())

	nextField := findStructField(t, underlying, "Next")
	pointer, ok := nextField.Type().(*types.Pointer)
	require.True(t, ok, "Next should be a pointer, got %T", nextField.Type())
	elem, ok := pointer.Elem().(*types.Named)
	require.True(t, ok, "Next should point to named type, got %T", pointer.Elem())
	require.Same(t, named, elem)
}

func TestSymbolRegistrySelfReferentialMap(t *testing.T) {
	t.Parallel()

	reg := NewSymbolRegistry(SymbolExports{
		"tm": {
			"Tree": reflect.ValueOf((*treeByKey)(nil)),
		},
	})

	pkg, err := reg.Import(t.Context(), "tm")
	require.NoError(t, err)

	named, ok := pkg.Scope().Lookup("Tree").Type().(*types.Named)
	require.True(t, ok)

	underlying, ok := named.Underlying().(*types.Struct)
	require.True(t, ok, "underlying should be a struct, got %T", named.Underlying())

	childrenField := findStructField(t, underlying, "Children")
	mapType, ok := childrenField.Type().(*types.Map)
	require.True(t, ok, "Children should be a map, got %T", childrenField.Type())
	pointer, ok := mapType.Elem().(*types.Pointer)
	require.True(t, ok, "Children value should be pointer, got %T", mapType.Elem())
	elem, ok := pointer.Elem().(*types.Named)
	require.True(t, ok, "map value pointer should reference named type, got %T", pointer.Elem())
	require.Same(t, named, elem)
}

func TestSymbolRegistryMutuallyRecursiveStructs(t *testing.T) {
	t.Parallel()

	reg := NewSymbolRegistry(SymbolExports{
		"pair": {
			"A": reflect.ValueOf((*mutualA)(nil)),
			"B": reflect.ValueOf((*mutualB)(nil)),
		},
	})

	pkg, err := reg.Import(t.Context(), "pair")
	require.NoError(t, err)

	aNamed, ok := pkg.Scope().Lookup("A").Type().(*types.Named)
	require.True(t, ok)
	bNamed, ok := pkg.Scope().Lookup("B").Type().(*types.Named)
	require.True(t, ok)

	aStruct, ok := aNamed.Underlying().(*types.Struct)
	require.True(t, ok, "A underlying should be a struct, got %T", aNamed.Underlying())
	bStruct, ok := bNamed.Underlying().(*types.Struct)
	require.True(t, ok, "B underlying should be a struct, got %T", bNamed.Underlying())

	assertFieldPointsToNamed(t, aStruct, "B", bNamed)
	assertFieldPointsToNamed(t, bStruct, "A", aNamed)
}

func TestSymbolRegistryRecursiveDAG(t *testing.T) {
	t.Parallel()

	reg := NewSymbolRegistry(SymbolExports{
		"ast": {
			"Node": reflect.ValueOf((*recursiveDAG)(nil)),
		},
	})

	pkg, err := reg.Import(t.Context(), "ast")
	require.NoError(t, err)

	named, ok := pkg.Scope().Lookup("Node").Type().(*types.Named)
	require.True(t, ok, "Node should be a named type")
	underlying, ok := named.Underlying().(*types.Struct)
	require.True(t, ok, "underlying should be a struct, got %T", named.Underlying())

	childrenField := findStructField(t, underlying, "Children")
	slice, ok := childrenField.Type().(*types.Slice)
	require.True(t, ok, "Children should be a slice, got %T", childrenField.Type())
	pointer, ok := slice.Elem().(*types.Pointer)
	require.True(t, ok, "slice element should be pointer, got %T", slice.Elem())
	elem, ok := pointer.Elem().(*types.Named)
	require.True(t, ok, "pointer element should be named, got %T", pointer.Elem())
	require.Same(t, named, elem)
}

func findStructField(t *testing.T, st *types.Struct, name string) *types.Var {
	t.Helper()
	for field := range st.Fields() {
		if field.Name() == name {
			return field
		}
	}
	require.Failf(t, "field not found", "field %s not found in struct", name)
	return nil
}

func assertFieldPointsToNamed(t *testing.T, st *types.Struct, name string, expected *types.Named) {
	t.Helper()
	field := findStructField(t, st, name)
	pointer, ok := field.Type().(*types.Pointer)
	require.True(t, ok, "field %s should be a pointer, got %T", name, field.Type())
	named, ok := pointer.Elem().(*types.Named)
	require.True(t, ok, "field %s should point to named type, got %T", name, pointer.Elem())
	require.Same(t, expected, named, "field %s should reference expected named type", name)
}

func TestRegisteredFunctionsAreKnownByTheIdentityTheyAreRegisteredUnder(t *testing.T) {
	t.Parallel()

	writer := func(name string, data []byte, perm uint32) error { return nil }
	reader := func(name string) ([]byte, error) { return nil, nil }
	remover := func(name string) error { return nil }
	registry := NewSymbolRegistry(SymbolExports{"example.com/gatedwrite": {"WriteFile": reflect.ValueOf(writer)}})
	registry.RegisterPackage("example.com/gatedread", map[string]reflect.Value{"ReadFile": reflect.ValueOf(reader)})
	registry.OverlayPackage("example.com/gatedremove", map[string]reflect.Value{"Remove": reflect.ValueOf(remover)})

	for function, want := range map[reflect.Value]string{
		reflect.ValueOf(writer):  "example.com/gatedwrite.WriteFile",
		reflect.ValueOf(reader):  "example.com/gatedread.ReadFile",
		reflect.ValueOf(remover): "example.com/gatedremove.Remove",
	} {
		identity, ok := policy.NativeSymbolIdentity(function)
		require.True(t, ok, want)
		require.Equal(t, want, identity, "the gate keys on the registered identity, not the closure's runtime name")
		require.NotContains(t, runtime.FuncForPC(function.Pointer()).Name(), want, "the runtime name would mislead the gate")
	}
}
