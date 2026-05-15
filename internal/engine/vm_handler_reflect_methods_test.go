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

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func syntheticScriptType(t *testing.T, name string) reflect.Type {
	t.Helper()

	return reflect.StructOf([]reflect.StructField{
		{Name: "X", Type: reflect.TypeFor[int]()},
		{Name: pipitIDFieldPrefix + name, Type: reflect.TypeFor[struct{}](), PkgPath: "main"},
	})
}

func TestPipitTypeStringerHandsBackItsText(t *testing.T) {
	t.Parallel()

	require.Equal(t, "main.Score", pipitTypeStringer("main.Score").String())
	require.Empty(t, pipitTypeStringer("").String())
}

func TestPipitNamedTypeRendersTheSourceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		named      pipitNamedType
		wantName   string
		wantString string
	}{
		{
			name:     "a qualified type",
			named:    pipitNamedType{Type: reflect.TypeFor[int64](), sourceName: "Score", sourcePackage: "main"},
			wantName: "Score", wantString: "main.Score",
		},
		{
			name:     "a type with no package qualifier",
			named:    pipitNamedType{Type: reflect.TypeFor[int64](), sourceName: "Score"},
			wantName: "Score", wantString: "Score",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.wantName, tt.named.Name())
			require.Equal(t, tt.wantString, tt.named.String())
		})
	}
}

func TestPipitReflectTypeNameFindsTheScriptName(t *testing.T) {
	t.Parallel()

	script := syntheticScriptType(t, "Point")

	tests := []struct {
		name        string
		reflectType reflect.Type
		wantName    string
		wantPointer bool
	}{
		{
			name:        "a wrapped named type reports its source name",
			reflectType: pipitNamedType{Type: reflect.TypeFor[int64](), sourceName: "Score"},
			wantName:    "Score",
		},
		{name: "a synthesised struct reports its sentinel name", reflectType: script, wantName: "Point"},
		{
			name: "a pointer to a synthesised struct reports the pointer too", reflectType: reflect.PointerTo(script),
			wantName: "Point", wantPointer: true,
		},
		{name: "an ordinary type has no script name", reflectType: reflect.TypeFor[int](), wantName: ""},
		{
			name: "a pointer to an ordinary type still reports the pointer", reflectType: reflect.TypeFor[*int](),
			wantName: "", wantPointer: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newTestVM(t)
			name, pointer := pipitReflectTypeName(vm, tt.reflectType)
			require.Equal(t, tt.wantName, name)
			require.Equal(t, tt.wantPointer, pointer)
		})
	}
}

func TestPipitMethodSetIsEmptyWithoutARootFunction(t *testing.T) {
	t.Parallel()

	require.Nil(t, pipitMethodSet(nil, "Point", false), "no VM means no method table to consult")
	require.Nil(t, pipitMethodSet(newTestVM(t), "Point", false), "a VM with no root function has no methods")
}

func TestPipitMethodInOutTypesFallsBackToTheErasedShape(t *testing.T) {
	t.Parallel()

	t.Run("a recorded signature wins", func(t *testing.T) {
		t.Parallel()

		callee := program.NewNamedFunction("Method")
		callee.SignatureReflectType = reflect.TypeFor[func(int) string]()

		in, out := pipitMethodInOutTypes(callee)
		require.Equal(t, []reflect.Type{reflect.TypeFor[int]()}, in)
		require.Equal(t, []reflect.Type{reflect.TypeFor[string]()}, out)
	})

	t.Run("the erased shape drops the receiver and defaults every parameter", func(t *testing.T) {
		t.Parallel()

		callee := program.NewNamedFunction("Method")
		callee.ParameterKinds = []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterInt, isa.RegisterString}
		callee.ResultKinds = []isa.RegisterKind{isa.RegisterInt}

		in, out := pipitMethodInOutTypes(callee)
		require.Len(t, in, 2, "the receiver occupies the first parameter slot")
		require.Equal(t, reflect.TypeFor[any](), in[0])
		require.Equal(t, []reflect.Type{program.KindDefaultReflectType(isa.RegisterInt)}, out)
	})

	t.Run("a variadic erased shape ends in a slice", func(t *testing.T) {
		t.Parallel()

		callee := program.NewNamedFunction("Method")
		callee.ParameterKinds = []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterGeneral}
		callee.IsVariadic = true

		in, _ := pipitMethodInOutTypes(callee)
		require.Equal(t, []reflect.Type{reflect.TypeFor[[]any]()}, in)
	})

	t.Run("a method with no parameters at all yields nothing", func(t *testing.T) {
		t.Parallel()

		in, out := pipitMethodInOutTypes(program.NewNamedFunction("Method"))
		require.Empty(t, in)
		require.Empty(t, out)
	})
}

func TestRenderPipitTypeStringNamesScriptTypesTheWayGoDoes(t *testing.T) {
	t.Parallel()

	script := syntheticScriptType(t, "Point")

	tests := []struct {
		name        string
		reflectType reflect.Type
		want        string
	}{
		{name: "a nil type renders nothing", reflectType: nil, want: ""},
		{
			name:        "a wrapped named type renders qualified",
			reflectType: pipitNamedType{Type: reflect.TypeFor[int64](), sourceName: "Score", sourcePackage: "main"},
			want:        "main.Score",
		},
		{
			name:        "a wrapped named type with no package renders bare",
			reflectType: pipitNamedType{Type: reflect.TypeFor[int64](), sourceName: "Score"}, want: "Score",
		},
		{name: "a synthesised struct renders qualified", reflectType: script, want: "main.Point"},
		{name: "an ordinary struct renders nothing", reflectType: reflect.TypeFor[identityNone](), want: ""},
		{name: "a pointer to a script type keeps the star", reflectType: reflect.PointerTo(script), want: "*main.Point"},
		{name: "a slice of a script type keeps the brackets", reflectType: reflect.SliceOf(script), want: "[]main.Point"},
		{name: "an array of a script type carries its length", reflectType: reflect.ArrayOf(3, script), want: "[3]main.Point"},
		{name: "a channel of a script type carries its direction", reflectType: reflect.ChanOf(reflect.BothDir, script), want: "chan main.Point"},
		{name: "a slice of an ordinary type renders nothing", reflectType: reflect.TypeFor[[]int](), want: ""},
		{name: "a plain scalar renders nothing", reflectType: reflect.TypeFor[int](), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, renderPipitTypeString(tt.reflectType))
		})
	}
}

func TestRenderPipitMapTypeStringFillsInTheOrdinaryHalf(t *testing.T) {
	t.Parallel()

	script := syntheticScriptType(t, "Point")

	tests := []struct {
		name        string
		reflectType reflect.Type
		want        string
	}{
		{name: "a script value type", reflectType: reflect.MapOf(reflect.TypeFor[string](), script), want: "map[string]main.Point"},
		{name: "a script key type", reflectType: reflect.MapOf(script, reflect.TypeFor[int]()), want: "map[main.Point]int"},
		{name: "script types on both sides", reflectType: reflect.MapOf(script, script), want: "map[main.Point]main.Point"},
		{name: "no script type at all renders nothing", reflectType: reflect.TypeFor[map[string]int](), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, renderPipitMapTypeString(tt.reflectType))
		})
	}
}

func TestRenderPipitFuncTypeStringOnlyFiresForScriptTypes(t *testing.T) {
	t.Parallel()

	script := syntheticScriptType(t, "Point")

	tests := []struct {
		name        string
		reflectType reflect.Type
		want        string
	}{
		{
			name:        "a script parameter with no result",
			reflectType: reflect.FuncOf([]reflect.Type{script}, nil, false),
			want:        "func(main.Point)",
		},
		{
			name:        "a script parameter and an ordinary result",
			reflectType: reflect.FuncOf([]reflect.Type{script, reflect.TypeFor[int]()}, []reflect.Type{reflect.TypeFor[int]()}, false),
			want:        "func(main.Point, int) int",
		},
		{
			name:        "a script result alone",
			reflectType: reflect.FuncOf([]reflect.Type{reflect.TypeFor[int]()}, []reflect.Type{script}, false),
			want:        "func(int) main.Point",
		},
		{
			name:        "two results are parenthesised",
			reflectType: reflect.FuncOf(nil, []reflect.Type{script, reflect.TypeFor[error]()}, false),
			want:        "func() (main.Point, error)",
		},
		{
			name:        "a variadic script parameter renders with the ellipsis",
			reflectType: reflect.FuncOf([]reflect.Type{reflect.SliceOf(script)}, nil, true),
			want:        "func(...main.Point)",
		},
		{
			name:        "a variadic ordinary parameter alongside a script result",
			reflectType: reflect.FuncOf([]reflect.Type{reflect.TypeFor[[]int]()}, []reflect.Type{script}, true),
			want:        "func(...int) main.Point",
		},
		{
			name:        "a signature with no script type renders nothing",
			reflectType: reflect.TypeFor[func(int) string](),
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, renderPipitFuncTypeString(tt.reflectType))
		})
	}
}

func TestQualifiedNameFromStructSentinelReadsTheTrailingField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		reflectType reflect.Type
		want        string
	}{
		{name: "a trailing sentinel", reflectType: syntheticScriptType(t, "Point"), want: "main.Point"},
		{name: "a struct with no fields", reflectType: reflect.TypeFor[struct{}](), want: ""},
		{name: "a struct whose last field is not a sentinel", reflectType: reflect.TypeFor[identityNone](), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, qualifiedNameFromStructSentinel(tt.reflectType))
		})
	}
}
