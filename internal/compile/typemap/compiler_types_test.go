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
	"go/token"
	"go/types"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/symtab"
)

func makeTypeParam(name string) *types.TypeParam {
	tname := types.NewTypeName(token.NoPos, nil, name, nil)
	anyType := types.Universe.Lookup("any").Type()
	constraint, ok := anyType.Underlying().(*types.Interface)
	if !ok {
		panic("any's underlying type is not an interface, so a go/types invariant is broken")
	}
	return types.NewTypeParam(tname, constraint)
}

func TestBasicToReflectCoversEveryNumericKind(t *testing.T) {
	tests := []struct {
		name string
		kind types.BasicKind
		want string
	}{
		{name: "bool", kind: types.Bool, want: "bool"},
		{name: "int", kind: types.Int, want: "int"},
		{name: "uint8", kind: types.Uint8, want: "uint8"},
		{name: "float32", kind: types.Float32, want: "float32"},
		{name: "float64", kind: types.Float64, want: "float64"},
		{name: "complex64", kind: types.Complex64, want: "complex64"},
		{name: "complex128", kind: types.Complex128, want: "complex128"},
		{name: "untyped complex", kind: types.UntypedComplex, want: "complex128"},
		{name: "string", kind: types.String, want: "string"},
		{name: "invalid falls back to any", kind: types.Invalid, want: "interface {}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, basicToReflect(tt.kind).String())
		})
	}
}

func TestGenericInstantiationsPrintTheirTypeArgumentsAsTheRuntimeDoes(t *testing.T) {
	t.Parallel()

	typesPackage := types.NewPackage("example.com/generic", "generic")
	parameterK := types.NewTypeParam(types.NewTypeName(0, typesPackage, "K", nil), types.NewInterfaceType(nil, nil))
	parameterV := types.NewTypeParam(types.NewTypeName(0, typesPackage, "V", nil), types.NewInterfaceType(nil, nil))
	pair := types.NewNamed(types.NewTypeName(0, typesPackage, "Pair", nil), nil, nil)
	pair.SetTypeParams([]*types.TypeParam{parameterK, parameterV})
	pair.SetUnderlying(types.NewStruct([]*types.Var{
		types.NewField(0, typesPackage, "Key", parameterK, false),
		types.NewField(0, typesPackage, "Value", parameterV, false),
	}, nil))
	instance, err := types.Instantiate(nil, pair, []types.Type{types.Typ[types.String], types.NewMap(types.Typ[types.Int], types.Typ[types.Bool])}, true)
	require.NoError(t, err)

	require.Equal(t, "generic.Pair[string,map[int]bool]", RuntimeTypeString(instance))
	require.Equal(t, "a[b,c]", compactTypeArgumentLists("a[b, c]"))
	require.Equal(t, "f(a, b)", compactTypeArgumentLists("f(a, b)"), "a parameter list keeps its spaces")
	converted, err := TypeToReflectCached(t.Context(), instance, symtab.NewSymbolRegistry(nil), NewTypeCache(), engine.NewGlobalStore())
	require.NoError(t, err)
	sentinel := converted.Field(converted.NumField() - 1)
	require.Contains(t, string(sentinel.Tag), "string", "the sentinel records the type arguments")
	require.True(t, wordBoundary("a.b", -1))
	require.False(t, wordBoundary("ab", 1))
}

func TestStructFieldsThatLeadBackKeepTheirContainers(t *testing.T) {
	t.Parallel()

	typesPackage := types.NewPackage("example.com/tree", "tree")
	node := types.NewNamed(types.NewTypeName(0, typesPackage, "Node", nil), nil, nil)
	pointer := types.NewPointer(node)
	node.SetUnderlying(types.NewStruct([]*types.Var{
		types.NewField(0, typesPackage, "Children", types.NewSlice(pointer), false),
		types.NewField(0, typesPackage, "Index", types.NewMap(types.Typ[types.String], types.NewSlice(pointer)), false),
		types.NewField(0, typesPackage, "Pair", types.NewArray(pointer, 2), false),
		types.NewField(0, typesPackage, "Queue", types.NewChan(types.RecvOnly, pointer), false),
		types.NewField(0, typesPackage, "Visit", types.NewSignatureType(nil, nil, nil, types.NewTuple(types.NewVar(0, typesPackage, "", pointer)), nil, false), false),
		types.NewField(0, typesPackage, "Name", types.Typ[types.String], false),
	}, nil))

	converted := convertOne(t, node)
	require.Equal(t, reflect.Slice, converted.Field(0).Type.Kind())
	require.Equal(t, reflect.Map, converted.Field(1).Type.Kind())
	require.Equal(t, reflect.Array, converted.Field(2).Type.Kind())
	require.Equal(t, 2, converted.Field(2).Type.Len())
	require.Equal(t, reflect.Chan, converted.Field(3).Type.Kind())
	require.Equal(t, reflect.RecvDir, converted.Field(3).Type.ChanDir())
	require.Equal(t, reflect.TypeFor[string](), converted.Field(5).Type)
}
