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

package typemap

import (
	"context"
	"go/types"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/symtab"
)

func convertOne(t *testing.T, source types.Type) reflect.Type {
	t.Helper()
	return TypeToReflectCached(context.Background(), source, symtab.NewSymbolRegistry(nil), map[types.Type]reflect.Type{}, engine.NewGlobalStore())
}

func testPackage() *types.Package {
	return types.NewPackage("example.com/main", "main")
}

func namedStruct(pkg *types.Package, name string) *types.Named {
	return types.NewNamed(types.NewTypeName(0, pkg, name, nil), nil, nil)
}

func TestConvertingAnUnnamedTypeGivesTheMatchingReflectType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build func(*testing.T) types.Type
		want  reflect.Type
	}{
		{name: "an int", build: func(*testing.T) types.Type { return types.Typ[types.Int] }, want: reflect.TypeFor[int]()},
		{name: "an int8", build: func(*testing.T) types.Type { return types.Typ[types.Int8] }, want: reflect.TypeFor[int8]()},
		{name: "a uint16", build: func(*testing.T) types.Type { return types.Typ[types.Uint16] }, want: reflect.TypeFor[uint16]()},
		{name: "a float32", build: func(*testing.T) types.Type { return types.Typ[types.Float32] }, want: reflect.TypeFor[float32]()},
		{name: "a complex128", build: func(*testing.T) types.Type { return types.Typ[types.Complex128] }, want: reflect.TypeFor[complex128]()},
		{name: "a string", build: func(*testing.T) types.Type { return types.Typ[types.String] }, want: reflect.TypeFor[string]()},
		{name: "a bool", build: func(*testing.T) types.Type { return types.Typ[types.Bool] }, want: reflect.TypeFor[bool]()},
		{name: "a slice of int", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Int]) }, want: reflect.TypeFor[[]int]()},
		{name: "a slice of a slice of string", build: func(*testing.T) types.Type { return types.NewSlice(types.NewSlice(types.Typ[types.String])) }, want: reflect.TypeFor[[][]string]()},
		{name: "a map of string to int", build: func(*testing.T) types.Type { return types.NewMap(types.Typ[types.String], types.Typ[types.Int]) }, want: reflect.TypeFor[map[string]int]()},
		{name: "a pointer to int", build: func(*testing.T) types.Type { return types.NewPointer(types.Typ[types.Int]) }, want: reflect.TypeFor[*int]()},
		{name: "an array of four ints", build: func(*testing.T) types.Type { return types.NewArray(types.Typ[types.Int], 4) }, want: reflect.TypeFor[[4]int]()},
		{name: "a bidirectional channel", build: func(*testing.T) types.Type { return types.NewChan(types.SendRecv, types.Typ[types.Int]) }, want: reflect.TypeFor[chan int]()},
		{name: "a send-only channel", build: func(*testing.T) types.Type { return types.NewChan(types.SendOnly, types.Typ[types.Int]) }, want: reflect.TypeFor[chan<- int]()},
		{name: "a receive-only channel", build: func(*testing.T) types.Type { return types.NewChan(types.RecvOnly, types.Typ[types.Int]) }, want: reflect.TypeFor[<-chan int]()},
		{name: "an empty interface", build: func(*testing.T) types.Type { return types.NewInterfaceType(nil, nil) }, want: reflect.TypeFor[any]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, convertOne(t, tt.build(t)))
		})
	}
}

func TestConvertingASignatureDropsItsNamesAndKeepsItsShape(t *testing.T) {
	t.Parallel()

	pkg := testPackage()

	tests := []struct {
		name         string
		build        func(*testing.T) types.Type
		wantIn       []reflect.Type
		wantOut      []reflect.Type
		wantVariadic bool
	}{
		{
			name: "a function of one argument returning one value",
			build: func(*testing.T) types.Type {
				params := types.NewTuple(types.NewParam(0, pkg, "value", types.Typ[types.Int]))
				results := types.NewTuple(types.NewParam(0, pkg, "", types.Typ[types.Bool]))
				return types.NewSignatureType(nil, nil, nil, params, results, false)
			},
			wantIn:  []reflect.Type{reflect.TypeFor[int]()},
			wantOut: []reflect.Type{reflect.TypeFor[bool]()},
		},
		{
			name: "a function with no arguments and no results",
			build: func(*testing.T) types.Type {
				return types.NewSignatureType(nil, nil, nil, nil, nil, false)
			},
			wantIn:  nil,
			wantOut: nil,
		},
		{
			name: "a function returning two values",
			build: func(*testing.T) types.Type {
				results := types.NewTuple(
					types.NewParam(0, pkg, "", types.Typ[types.Int]),
					types.NewParam(0, pkg, "", types.Typ[types.String]),
				)
				return types.NewSignatureType(nil, nil, nil, nil, results, false)
			},
			wantIn:  nil,
			wantOut: []reflect.Type{reflect.TypeFor[int](), reflect.TypeFor[string]()},
		},
		{
			name: "a variadic function",
			build: func(*testing.T) types.Type {
				params := types.NewTuple(types.NewParam(0, pkg, "values", types.NewSlice(types.Typ[types.Int])))
				return types.NewSignatureType(nil, nil, nil, params, nil, true)
			},
			wantIn:       []reflect.Type{reflect.TypeFor[[]int]()},
			wantVariadic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := convertOne(t, tt.build(t))

			require.Equal(t, reflect.Func, got.Kind())
			require.Equal(t, len(tt.wantIn), got.NumIn())
			for i, want := range tt.wantIn {
				require.Equal(t, want, got.In(i))
			}
			require.Equal(t, len(tt.wantOut), got.NumOut())
			for i, want := range tt.wantOut {
				require.Equal(t, want, got.Out(i))
			}
			require.Equal(t, tt.wantVariadic, got.IsVariadic())
		})
	}
}

func TestConvertingANamedStructAddsAnIdentitySentinelField(t *testing.T) {
	t.Parallel()

	pkg := testPackage()
	named := namedStruct(pkg, "point")
	named.SetUnderlying(types.NewStruct([]*types.Var{
		types.NewField(0, pkg, "X", types.Typ[types.Int], false),
		types.NewField(0, pkg, "Y", types.Typ[types.Int], false),
	}, nil))

	got := convertOne(t, named)

	require.Equal(t, reflect.Struct, got.Kind())
	require.Equal(t, 3, got.NumField(),
		"the two declared fields are followed by the sentinel that keeps two identical layouts apart")
	require.Equal(t, "X", got.Field(0).Name)
	require.Equal(t, reflect.TypeFor[int](), got.Field(0).Type)
	require.Equal(t, "Y", got.Field(1).Name)

	sentinel := got.Field(2)
	require.Equal(t, "_pipitID_point", sentinel.Name,
		"the sentinel is named after the declaration it identifies")
	require.Equal(t, reflect.TypeFor[struct{}](), sentinel.Type,
		"the sentinel must not widen the layout it marks")
	require.Equal(t, "example.com/main.point", sentinel.PkgPath,
		"an unexported sentinel keeps the type out of another package's identical layout")
}

func TestConvertingANamedStructCarriesFieldDetailsThrough(t *testing.T) {
	t.Parallel()

	pkg := testPackage()

	tests := []struct {
		name      string
		fields    []*types.Var
		tags      []string
		wantName  string
		wantType  reflect.Type
		wantTag   string
		wantPkg   string
		wantAnon  bool
		fieldSlot int
	}{
		{
			name:      "an exported field keeps its name and type",
			fields:    []*types.Var{types.NewField(0, pkg, "Total", types.Typ[types.Float64], false)},
			wantName:  "Total",
			wantType:  reflect.TypeFor[float64](),
			fieldSlot: 0,
		},
		{
			name:      "an unexported field carries its package path",
			fields:    []*types.Var{types.NewField(0, pkg, "hidden", types.Typ[types.Int], false)},
			wantName:  "hidden",
			wantType:  reflect.TypeFor[int](),
			wantPkg:   "example.com/main",
			fieldSlot: 0,
		},
		{
			name:      "a struct tag survives the conversion",
			fields:    []*types.Var{types.NewField(0, pkg, "Name", types.Typ[types.String], false)},
			tags:      []string{`json:"name"`},
			wantName:  "Name",
			wantType:  reflect.TypeFor[string](),
			wantTag:   `json:"name"`,
			fieldSlot: 0,
		},
		{
			name:      "a slice field converts its element",
			fields:    []*types.Var{types.NewField(0, pkg, "Items", types.NewSlice(types.Typ[types.String]), false)},
			wantName:  "Items",
			wantType:  reflect.TypeFor[[]string](),
			fieldSlot: 0,
		},
		{
			name:      "an exported embedded field stays anonymous",
			fields:    []*types.Var{types.NewField(0, pkg, "Reader", types.Typ[types.Int], true)},
			wantName:  "Reader",
			wantType:  reflect.TypeFor[int](),
			wantAnon:  true,
			fieldSlot: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			named := namedStruct(testPackage(), "record")
			named.SetUnderlying(types.NewStruct(tt.fields, tt.tags))

			got := convertOne(t, named)
			field := got.Field(tt.fieldSlot)

			require.Equal(t, tt.wantName, field.Name)
			require.Equal(t, tt.wantType, field.Type)
			require.Equal(t, tt.wantTag, string(field.Tag))
			require.Equal(t, tt.wantPkg, field.PkgPath)
			require.Equal(t, tt.wantAnon, field.Anonymous)
		})
	}
}

func TestConvertingAnUnexportedEmbeddedFieldRenamesItSoReflectAcceptsIt(t *testing.T) {
	t.Parallel()

	pkg := testPackage()
	named := namedStruct(pkg, "wrapper")
	named.SetUnderlying(types.NewStruct([]*types.Var{
		types.NewField(0, pkg, "inner", types.Typ[types.Int], true),
	}, nil))

	got := convertOne(t, named)
	field := got.Field(0)

	require.NotEqual(t, "inner", field.Name,
		"reflect refuses an unexported anonymous field, so the name has to be rewritten")
	require.False(t, field.Anonymous,
		"the rewritten field is no longer the embedded one reflect would promote through")
	require.Equal(t, reflect.TypeFor[int](), field.Type)
}

func TestConvertingASelfReferentialStructBreaksTheCycle(t *testing.T) {
	t.Parallel()

	pkg := testPackage()
	named := namedStruct(pkg, "node")
	named.SetUnderlying(types.NewStruct([]*types.Var{
		types.NewField(0, pkg, "Value", types.Typ[types.Int], false),
		types.NewField(0, pkg, "Next", types.NewPointer(named), false),
	}, nil))

	got := convertOne(t, named)

	require.Equal(t, reflect.Struct, got.Kind())
	require.Equal(t, "Value", got.Field(0).Name)
	require.Equal(t, "Next", got.Field(1).Name)
	require.NotEqual(t, reflect.TypeFor[int](), got.Field(1).Type,
		"the recursive field cannot be the value type, because reflect cannot build a type that contains itself")
	require.NotEmpty(t, string(got.Field(1).Tag),
		"a field whose cycle was broken is tagged so the engine knows the layout is not the literal one")
}

func TestConvertingAMutuallyRecursivePairTerminates(t *testing.T) {
	t.Parallel()

	pkg := testPackage()
	left := namedStruct(pkg, "left")
	right := namedStruct(pkg, "right")
	left.SetUnderlying(types.NewStruct([]*types.Var{types.NewField(0, pkg, "R", types.NewPointer(right), false)}, nil))
	right.SetUnderlying(types.NewStruct([]*types.Var{types.NewField(0, pkg, "L", types.NewPointer(left), false)}, nil))

	got := convertOne(t, left)

	require.Equal(t, reflect.Struct, got.Kind())
	require.Equal(t, "R", got.Field(0).Name,
		"neither half of the pair may be dropped just because the other one refers back to it")
}

func TestConvertingANamedTypeOverANonStructUsesItsUnderlyingShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		build    func(*testing.T) types.Type
		wantKind reflect.Kind
		wantElem reflect.Type
	}{
		{
			name: "a named slice",
			build: func(*testing.T) types.Type {
				named := namedStruct(testPackage(), "row")
				named.SetUnderlying(types.NewSlice(types.Typ[types.Int]))
				return named
			},
			wantKind: reflect.Slice,
			wantElem: reflect.TypeFor[int](),
		},
		{
			name: "a named map",
			build: func(*testing.T) types.Type {
				named := namedStruct(testPackage(), "index")
				named.SetUnderlying(types.NewMap(types.Typ[types.String], types.Typ[types.Int]))
				return named
			},
			wantKind: reflect.Map,
			wantElem: reflect.TypeFor[int](),
		},
		{
			name: "a named channel",
			build: func(*testing.T) types.Type {
				named := namedStruct(testPackage(), "stream")
				named.SetUnderlying(types.NewChan(types.SendRecv, types.Typ[types.String]))
				return named
			},
			wantKind: reflect.Chan,
			wantElem: reflect.TypeFor[string](),
		},
		{
			name: "a named interface is erased",
			build: func(*testing.T) types.Type {
				named := namedStruct(testPackage(), "shape")
				named.SetUnderlying(types.NewInterfaceType(nil, nil))
				return named
			},
			wantKind: reflect.Interface,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := convertOne(t, tt.build(t))

			require.Equal(t, tt.wantKind, got.Kind())
			if tt.wantElem != nil {
				require.Equal(t, tt.wantElem, got.Elem())
			}
		})
	}
}

func TestConvertingTheSameNamedStructTwiceReusesTheCachedLayout(t *testing.T) {
	t.Parallel()

	pkg := testPackage()
	named := namedStruct(pkg, "cached")
	named.SetUnderlying(types.NewStruct([]*types.Var{types.NewField(0, pkg, "X", types.Typ[types.Int], false)}, nil))

	cache := map[types.Type]reflect.Type{}
	symbols := symtab.NewSymbolRegistry(nil)
	globals := engine.NewGlobalStore()

	first := TypeToReflectCached(context.Background(), named, symbols, cache, globals)
	second := TypeToReflectCached(context.Background(), named, symbols, cache, globals)

	require.Equal(t, first, second,
		"two conversions of one declaration have to agree, or two values of the same type would not be assignable")
	require.NotEmpty(t, cache,
		"the cache the caller supplies is what makes the second conversion cheap")
}

func TestConvertingAnAliasFollowsItToTheTypeItNames(t *testing.T) {
	t.Parallel()

	pkg := testPackage()
	alias := types.NewAlias(types.NewTypeName(0, pkg, "count", nil), types.Typ[types.Int])

	require.Equal(t, reflect.TypeFor[int](), convertOne(t, alias),
		"an alias is another spelling of one type, not a type of its own")
}
