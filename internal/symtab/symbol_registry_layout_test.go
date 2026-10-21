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
	"go/token"
	"go/types"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
	"weak"

	"github.com/stretchr/testify/require"
)

type layoutEmbedded struct{ Inner int }

type layoutRecord struct {
	layoutEmbedded
	Name   string
	Pairs  [2]int32
	Count  *int
	Bytes  []byte
	Lookup map[string]int
	Events chan int
	Hook   func()
	Value  any
}

type layoutBytes struct {
	A int8
	B int16
}

func layoutField(name string, fieldType types.Type, embedded bool) *types.Var {
	return types.NewField(token.NoPos, nil, name, fieldType, embedded)
}

func layoutStruct(fields ...*types.Var) *types.Struct {
	return types.NewStruct(fields, nil)
}

func layoutRecordFields() []*types.Var {
	return []*types.Var{
		layoutField("layoutEmbedded", layoutStruct(layoutField("Inner", types.Typ[types.Int], false)), true),
		layoutField("Name", types.Typ[types.String], false),
		layoutField("Pairs", types.NewArray(types.Typ[types.Int32], 2), false),
		layoutField("Count", types.NewPointer(types.Typ[types.Int]), false),
		layoutField("Bytes", types.NewSlice(types.Typ[types.Uint8]), false),
		layoutField("Lookup", types.NewMap(types.Typ[types.String], types.Typ[types.Int]), false),
		layoutField("Events", types.NewChan(types.SendRecv, types.Typ[types.Int]), false),
		layoutField("Hook", types.NewSignatureType(nil, nil, nil, nil, nil, false), false),
		layoutField("Value", types.NewInterfaceType(nil, nil), false),
	}
}

func layoutPackage(underlying types.Type) *types.Package {
	pkg := types.NewPackage("layoutpkg", "layoutpkg")
	name := types.NewTypeName(token.NoPos, pkg, "T", nil)
	types.NewNamed(name, underlying, nil)
	pkg.Scope().Insert(name)
	return pkg
}

func TestLayoutDisagreementAcceptsAMatchingLayout(t *testing.T) {
	require.Empty(t, layoutDisagreement(layoutStruct(layoutRecordFields()...), reflect.TypeFor[layoutRecord](), 0))
}

func TestLayoutDisagreementNamesTheFirstDifference(t *testing.T) {
	renamed := layoutRecordFields()
	renamed[1] = layoutField("Label", types.Typ[types.String], false)
	unembedded := layoutRecordFields()
	unembedded[0] = layoutField("layoutEmbedded", unembedded[0].Type(), false)
	shortArray := layoutRecordFields()
	shortArray[2] = layoutField("Pairs", types.NewArray(types.Typ[types.Int32], 3), false)
	wideElements := layoutRecordFields()
	wideElements[2] = layoutField("Pairs", types.NewArray(types.Typ[types.Int64], 2), false)
	pointerToString := layoutRecordFields()
	pointerToString[3] = layoutField("Count", types.NewPointer(types.Typ[types.String]), false)
	sliceOfStrings := layoutRecordFields()
	sliceOfStrings[4] = layoutField("Bytes", types.NewSlice(types.Typ[types.String]), false)

	cases := []struct {
		exported types.Type
		native   reflect.Type
		name     string
		want     string
	}{
		{name: "kind", exported: types.Typ[types.Int], native: reflect.TypeFor[string](), want: "kind int in the export data, string natively"},
		{name: "field count", exported: layoutStruct(layoutRecordFields()[:2]...), native: reflect.TypeFor[layoutRecord](), want: "2 fields in the export data, 9 natively"},
		{name: "field name", exported: layoutStruct(renamed...), native: reflect.TypeFor[layoutRecord](), want: "field 1 is Label in the export data, Name natively"},
		{name: "embedding", exported: layoutStruct(unembedded...), native: reflect.TypeFor[layoutRecord](), want: "field 0 is layoutEmbedded in the export data, embedded layoutEmbedded natively"},
		{name: "offset", exported: layoutStruct(layoutField("A", types.Typ[types.Int8], false), layoutField("B", types.Typ[types.Int8], false)), native: reflect.TypeFor[layoutBytes](),
			want: "field 1 (B) at offset 1 in the export data, 2 natively"},
		{name: "array length", exported: layoutStruct(shortArray...), native: reflect.TypeFor[layoutRecord](), want: "field 2 (Pairs): array length 3 in the export data, 2 natively"},
		{name: "array element", exported: layoutStruct(wideElements...), native: reflect.TypeFor[layoutRecord](), want: "field 2 (Pairs): element: kind int64 in the export data, int32 natively"},
		{name: "pointer element", exported: layoutStruct(pointerToString...), native: reflect.TypeFor[layoutRecord](), want: "field 3 (Count): element kind string in the export data, int natively"},
		{name: "slice element", exported: layoutStruct(sliceOfStrings...), native: reflect.TypeFor[layoutRecord](), want: "field 4 (Bytes): element kind string in the export data, uint8 natively"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, layoutDisagreement(testCase.exported, testCase.native, 0))
		})
	}
}

func TestLayoutDisagreementComparesOnlySizesPastItsDepth(t *testing.T) {
	exported := types.NewArray(types.Typ[types.Int8], 2)
	require.Equal(t, "size 2 in the export data, 3 natively", layoutDisagreement(exported, reflect.TypeFor[[3]int8](), maxLayoutDepth))
	require.Equal(t, "array length 2 in the export data, 3 natively", layoutDisagreement(exported, reflect.TypeFor[[3]int8](), 0))
}

func TestLayoutDisagreementReportsNothingForAMalformedType(t *testing.T) {
	require.Empty(t, layoutDisagreement(nil, reflect.TypeFor[int](), 0), "a type it cannot inspect is left to the compiler")
}

func TestLayoutKindOfTypesWithNoNativeKind(t *testing.T) {
	require.Equal(t, reflect.Invalid, layoutKind(types.Typ[types.UntypedInt]))
	require.Equal(t, reflect.Invalid, layoutKind(types.NewTuple()))
}

func TestRegisterTypesPackageChecksTheNativeLayout(t *testing.T) {
	exports := SymbolExports{"layoutpkg": {
		"T":    reflect.ValueOf((*layoutBytes)(nil)),
		"Make": reflect.ValueOf(func() layoutBytes { return layoutBytes{} }),
		"Gone": reflect.ValueOf((*layoutBytes)(nil)),
	}}
	matching := layoutStruct(layoutField("A", types.Typ[types.Int8], false), layoutField("B", types.Typ[types.Int16], false))
	require.NoError(t, NewSymbolRegistry(exports).RegisterTypesPackage("layoutpkg", layoutPackage(matching)))

	stale := layoutStruct(layoutField("A", types.Typ[types.Int8], false), layoutField("C", types.Typ[types.Int16], false))
	err := NewSymbolRegistry(exports).RegisterTypesPackage("layoutpkg", layoutPackage(stale))
	require.ErrorIs(t, err, ErrTypesPackageLayout)
	require.ErrorContains(t, err, "layoutpkg.T: field 1 is C in the export data, B natively")

	require.NoError(t, NewSymbolRegistry(nil).RegisterTypesPackage("layoutpkg", layoutPackage(stale)), "a package with no native exports has nothing to compare")
	require.NoError(t, NewSymbolRegistry(exports).RegisterTypesPackage("layoutpkg", nil), "no types package")
}

func TestRegisterTypesPackageSkipsGenericTypes(t *testing.T) {
	pkg := types.NewPackage("layoutpkg", "layoutpkg")
	name := types.NewTypeName(token.NoPos, pkg, "T", nil)
	named := types.NewNamed(name, layoutStruct(layoutField("Other", types.Typ[types.Int], false)), nil)
	named.SetTypeParams([]*types.TypeParam{types.NewTypeParam(types.NewTypeName(token.NoPos, pkg, "E", nil), types.NewInterfaceType(nil, nil))})
	pkg.Scope().Insert(name)
	exports := SymbolExports{"layoutpkg": {"T": reflect.ValueOf((*layoutBytes)(nil))}}
	require.NoError(t, NewSymbolRegistry(exports).RegisterTypesPackage("layoutpkg", pkg), "a generic type has no single native layout")
}

func layoutMemoMatchingStruct() *types.Struct {
	return layoutStruct(layoutField("A", types.Typ[types.Int8], false), layoutField("B", types.Typ[types.Int16], false))
}

func layoutMemoDeclaration(t *testing.T, pkg *types.Package) *types.TypeName {
	t.Helper()
	typeName, ok := pkg.Scope().Lookup("T").(*types.TypeName)
	require.True(t, ok)
	return typeName
}

func TestLayoutMemoAnswersAsTheComparisonDoes(t *testing.T) {
	native := reflect.ValueOf((*layoutBytes)(nil))
	stale := layoutStruct(layoutField("A", types.Typ[types.Int8], false), layoutField("C", types.Typ[types.Int16], false))
	for name, underlying := range map[string]*types.Struct{"agreeing": layoutMemoMatchingStruct(), "disagreeing": stale} {
		t.Run(name, func(t *testing.T) {
			pkg := layoutPackage(underlying)
			typeName := layoutMemoDeclaration(t, pkg)
			want := layoutDisagreement(typeName.Type(), native.Type().Elem(), 0)

			require.Equal(t, want, exportLayoutDisagreement(pkg.Scope(), "T", native))
			cached, ok := layoutMemo.lookup(layoutMemoKey{typeName: weak.Make(typeName), native: native.Type().Elem()})
			require.True(t, ok, "the first comparison is remembered")
			require.Equal(t, want, cached)
			require.Equal(t, want, exportLayoutDisagreement(pkg.Scope(), "T", native), "a remembered answer is the comparison's answer")
		})
	}
}

func TestLayoutMemoIsConsultedBeforeComparing(t *testing.T) {
	native := reflect.ValueOf((*layoutBytes)(nil))
	pkg := layoutPackage(layoutMemoMatchingStruct())
	key := layoutMemoKey{typeName: weak.Make(layoutMemoDeclaration(t, pkg)), native: native.Type().Elem()}
	require.True(t, layoutMemo.remember(key, "remembered"))
	require.False(t, layoutMemo.remember(key, "second"), "the first answer stays")

	require.Equal(t, "remembered", exportLayoutDisagreement(pkg.Scope(), "T", native), "the comparison is not repeated")
}

func TestLayoutMemoForgetsADiscardedDeclaration(t *testing.T) {
	native := reflect.ValueOf((*layoutBytes)(nil))
	handle := func() weak.Pointer[types.TypeName] {
		pkg := layoutPackage(layoutMemoMatchingStruct())
		require.Empty(t, exportLayoutDisagreement(pkg.Scope(), "T", native))
		return weak.Make(layoutMemoDeclaration(t, pkg))
	}()
	key := layoutMemoKey{typeName: handle, native: native.Type().Elem()}

	require.Eventually(t, func() bool {
		runtime.GC()
		_, held := layoutMemo.lookup(key)
		return handle.Value() == nil && !held
	}, 10*time.Second, 5*time.Millisecond, "the entry goes with its package")
}

func TestLayoutMemoServesConcurrentRegistries(t *testing.T) {
	exports := SymbolExports{"layoutpkg": {"T": reflect.ValueOf((*layoutBytes)(nil))}}
	pkg := layoutPackage(layoutMemoMatchingStruct())
	var group sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		group.Go(func() {
			errs <- NewSymbolRegistry(exports).RegisterTypesPackage("layoutpkg", pkg)
		})
	}
	group.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}
