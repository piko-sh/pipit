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

package program

import (
	"go/types"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestReflectFuncTypeDropsTheReceiver(t *testing.T) {
	t.Parallel()

	t.Run("a function with no parameters has no method type", func(t *testing.T) {
		t.Parallel()

		_, ok := NewNamedFunction("f").ReflectFuncType()
		require.False(t, ok, "a method type needs at least the receiver slot")
	})

	t.Run("a recorded signature is used as given", func(t *testing.T) {
		t.Parallel()

		function := NewNamedFunction("f")
		function.ParameterKinds = []isa.RegisterKind{isa.RegisterGeneral}
		function.SignatureReflectType = reflect.TypeFor[func(int) string]()

		got, ok := function.ReflectFuncType()
		require.True(t, ok)
		require.Equal(t, reflect.TypeFor[func(int) string](), got)
	})

	t.Run("the erased shape drops the receiver and uses the bank types", func(t *testing.T) {
		t.Parallel()

		function := NewNamedFunction("f")
		function.ParameterKinds = []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterInt}
		function.ResultKinds = []isa.RegisterKind{isa.RegisterString}

		got, ok := function.ReflectFuncType()
		require.True(t, ok)
		require.Equal(t, 1, got.NumIn())
		require.Equal(t, KindDefaultReflectType(isa.RegisterInt), got.In(0))
		require.Equal(t, KindDefaultReflectType(isa.RegisterString), got.Out(0))
	})

	t.Run("a recorded result type wins over the bank's", func(t *testing.T) {
		t.Parallel()

		function := NewNamedFunction("f")
		function.ParameterKinds = []isa.RegisterKind{isa.RegisterGeneral}
		function.ResultKinds = []isa.RegisterKind{isa.RegisterGeneral}
		function.ResultReflectTypes = []reflect.Type{reflect.TypeFor[error]()}

		got, ok := function.ReflectFuncType()
		require.True(t, ok)
		require.Equal(t, reflect.TypeFor[error](), got.Out(0))
	})
}

func TestVariadicSafeInTypesEndsInASlice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		variadic bool
		slice    reflect.Type
		inTypes  []reflect.Type
		want     reflect.Type
	}{
		{
			name: "a non-variadic list is handed back", inTypes: []reflect.Type{reflect.TypeFor[any]()},
			want: reflect.TypeFor[any](),
		},
		{
			name: "a variadic list already ending in a slice is handed back", variadic: true,
			inTypes: []reflect.Type{reflect.TypeFor[[]int]()}, want: reflect.TypeFor[[]int](),
		},
		{
			name: "a recorded slice type replaces the erased last entry", variadic: true,
			slice: reflect.TypeFor[[]string](), inTypes: []reflect.Type{reflect.TypeFor[any]()},
			want: reflect.TypeFor[[]string](),
		},
		{
			name: "no recorded slice type falls back to an any slice", variadic: true,
			inTypes: []reflect.Type{reflect.TypeFor[any]()}, want: reflect.TypeFor[[]any](),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			function := NewNamedFunction("f")
			function.IsVariadic = tt.variadic
			function.VariadicSliceType = tt.slice

			got := function.VariadicSafeInTypes(tt.inTypes)
			require.Equal(t, tt.want, got[len(got)-1])
		})
	}

	t.Run("an empty list is handed back", func(t *testing.T) {
		t.Parallel()

		function := NewNamedFunction("f")
		function.IsVariadic = true

		require.Empty(t, function.VariadicSafeInTypes(nil))
	})
}

func TestReflectMethodExprTypeKeepsTheReceiver(t *testing.T) {
	t.Parallel()

	t.Run("a function with no parameters has no expression type", func(t *testing.T) {
		t.Parallel()

		_, ok := NewNamedFunction("f").ReflectMethodExprType()
		require.False(t, ok)
	})

	t.Run("the erased shape widens general parameters to the empty interface", func(t *testing.T) {
		t.Parallel()

		function := NewNamedFunction("f")
		function.ParameterKinds = []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterInt}

		got, ok := function.ReflectMethodExprType()
		require.True(t, ok)
		require.Equal(t, 2, got.NumIn())
		require.Equal(t, reflect.TypeFor[any](), got.In(0))
		require.Equal(t, KindDefaultReflectType(isa.RegisterInt), got.In(1))
	})

	t.Run("a recorded receiver and signature give the precise type", func(t *testing.T) {
		t.Parallel()

		function := NewNamedFunction("f")
		function.ParameterKinds = []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterInt}
		function.SignatureReflectType = reflect.TypeFor[func(int) string]()
		function.MethodReceiverReflectType = reflect.TypeFor[*int]()

		got, ok := function.ReflectMethodExprType()
		require.True(t, ok)
		require.Equal(t, reflect.TypeFor[func(*int, int) string](), got)
	})

	t.Run("a recorded signature with no receiver falls back to the erased shape", func(t *testing.T) {
		t.Parallel()

		function := NewNamedFunction("f")
		function.ParameterKinds = []isa.RegisterKind{isa.RegisterGeneral}
		function.SignatureReflectType = reflect.TypeFor[func(int) string]()

		got, ok := function.ReflectMethodExprType()
		require.True(t, ok)
		require.Equal(t, reflect.TypeFor[any](), got.In(0))
	})
}

func TestNativeParamCacheIsEmptyUntilPublished(t *testing.T) {
	t.Parallel()

	site := &CallSite{}
	require.Nil(t, site.NativeParamCacheLoad())
	require.Nil(t, site.NativeParamTypes())

	cache := &NativeParamCache{Types: []reflect.Type{reflect.TypeFor[int]()}, IsVariadic: true}
	site.NativeParamCacheStore(cache)

	require.Same(t, cache, site.NativeParamCacheLoad())
	require.Equal(t, []reflect.Type{reflect.TypeFor[int]()}, site.NativeParamTypes())
}

func TestInterfaceMethodRequirementRoundTrips(t *testing.T) {
	t.Parallel()

	signature := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(0, nil, "n", types.Typ[types.Int])),
		types.NewTuple(types.NewVar(0, nil, "", types.Typ[types.String])),
		false)

	t.Run("a signature-bearing requirement carries arity and shape", func(t *testing.T) {
		t.Parallel()

		encoded := EncodeInterfaceMethodRequirement("Format", signature)
		decoded := DecodeInterfaceMethodRequirement(encoded)

		require.Equal(t, "Format", decoded.Name)
		require.Equal(t, 1, decoded.Params)
		require.Equal(t, 1, decoded.Results)
		require.Equal(t, SignatureShapeString(signature), decoded.Shape)
	})

	t.Run("an arity-only requirement records no shape", func(t *testing.T) {
		t.Parallel()

		decoded := DecodeInterfaceMethodRequirement(EncodeInterfaceMethodRequirementArity("Format", signature))

		require.Equal(t, "Format", decoded.Name)
		require.Equal(t, 1, decoded.Params)
		require.Empty(t, decoded.Shape)
	})

	t.Run("a bare name is unchecked", func(t *testing.T) {
		t.Parallel()

		decoded := DecodeInterfaceMethodRequirement(EncodeInterfaceMethodRequirement("Format", nil))

		require.Equal(t, "Format", decoded.Name)
		require.Equal(t, -1, decoded.Params)
		require.Equal(t, -1, decoded.Results)
	})

	t.Run("a malformed arity is unchecked", func(t *testing.T) {
		t.Parallel()

		decoded := DecodeInterfaceMethodRequirement("Format/x/1")

		require.Equal(t, "Format", decoded.Name)
		require.Equal(t, -1, decoded.Params)
	})
}

func TestSignatureShapeStringRendersEveryArity(t *testing.T) {
	t.Parallel()

	stringVar := types.NewVar(0, nil, "", types.Typ[types.String])
	intVar := types.NewVar(0, nil, "n", types.Typ[types.Int])

	tests := []struct {
		name      string
		signature *types.Signature
		want      string
	}{
		{name: "a nil signature renders nothing", want: ""},
		{
			name:      "no parameters and no results",
			signature: types.NewSignatureType(nil, nil, nil, nil, nil, false),
			want:      "func()",
		},
		{
			name: "one parameter and one result",
			signature: types.NewSignatureType(nil, nil, nil,
				types.NewTuple(intVar), types.NewTuple(stringVar), false),
			want: "func(int) string",
		},
		{
			name: "two results are parenthesised",
			signature: types.NewSignatureType(nil, nil, nil, nil,
				types.NewTuple(stringVar, intVar), false),
			want: "func() (string, int)",
		},
		{
			name: "a variadic parameter renders with the ellipsis",
			signature: types.NewSignatureType(nil, nil, nil,
				types.NewTuple(types.NewVar(0, nil, "xs", types.NewSlice(types.Typ[types.Int]))), nil, true),
			want: "func(...int)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, SignatureShapeString(tt.signature))
		})
	}
}

func TestSubstituteShapeTypeArgsReplacesEveryPlaceholder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		shape    string
		typeArgs []string
		want     string
	}{
		{name: "a shape with no placeholder is handed back", shape: "func(int) string", want: "func(int) string"},
		{name: "one placeholder", shape: "func($0) $0", typeArgs: []string{"int"}, want: "func(int) int"},
		{
			name: "two placeholders in order", shape: "func($0, $1) $1",
			typeArgs: []string{"int", "string"}, want: "func(int, string) string",
		},
		{
			name:  "a two-digit placeholder is not eaten by a one-digit one",
			shape: "func($1, $10)", typeArgs: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"},
			want: "func(b, k)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, SubstituteShapeTypeArgs(tt.shape, tt.typeArgs))
		})
	}
}

func TestIsInlineableShapeRequiresTheExactSiteShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		site *CallSite
		want bool
	}{
		{
			name: "the candidate shape", want: true,
			site: &CallSite{
				Arguments: []VarLocation{{Kind: isa.RegisterGeneral}, {Kind: isa.RegisterSliceUint}},
				Returns:   []VarLocation{{Kind: isa.RegisterUint}},
			},
		},
		{
			name: "too few arguments",
			site: &CallSite{
				Arguments: []VarLocation{{Kind: isa.RegisterGeneral}},
				Returns:   []VarLocation{{Kind: isa.RegisterUint}},
			},
		},
		{
			name: "too many returns",
			site: &CallSite{
				Arguments: []VarLocation{{Kind: isa.RegisterGeneral}, {Kind: isa.RegisterSliceUint}},
				Returns:   []VarLocation{{Kind: isa.RegisterUint}, {Kind: isa.RegisterUint}},
			},
		},
		{
			name: "a receiver that is not in the general bank",
			site: &CallSite{
				Arguments: []VarLocation{{Kind: isa.RegisterInt}, {Kind: isa.RegisterSliceUint}},
				Returns:   []VarLocation{{Kind: isa.RegisterUint}},
			},
		},
		{
			name: "a return that is not unsigned",
			site: &CallSite{
				Arguments: []VarLocation{{Kind: isa.RegisterGeneral}, {Kind: isa.RegisterSliceUint}},
				Returns:   []VarLocation{{Kind: isa.RegisterInt}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, IsInlineableShape(tt.site))
		})
	}
}
