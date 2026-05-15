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
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func scriptPointType(t *testing.T) reflect.Type {
	t.Helper()

	return reflect.StructOf([]reflect.StructField{
		{Name: "X", Type: reflect.TypeFor[int]()},
		{Name: "Y", Type: reflect.TypeFor[string]()},
		{Name: pipitIDFieldPrefix + "Point", Type: reflect.TypeFor[struct{}](), PkgPath: "main"},
	})
}

func scriptPointValue(t *testing.T, x int, y string) reflect.Value {
	t.Helper()

	value := reflect.New(scriptPointType(t)).Elem()
	value.Field(0).SetInt(int64(x))
	value.Field(1).SetString(y)
	return value
}

func TestFmtRendererHidesTheIdentityField(t *testing.T) {
	t.Parallel()

	point := scriptPointValue(t, 1, "a")

	tests := []struct {
		name   string
		format string
		want   string
	}{
		{name: "the default verb prints the field values", format: "%v", want: "{1 a}"},
		{name: "the plus flag names each field", format: "%+v", want: "{X:1 Y:a}"},
		{name: "the string verb prints like the default verb", format: "%s", want: "{1 a}"},
		{name: "the go-syntax verb qualifies the type", format: "%#v", want: `main.Point{X:1, Y:"a"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, fmt.Sprintf(tt.format, pipitFmtValue{underlying: point}))
		})
	}
}

func TestFmtRendererFollowsPointersAndNils(t *testing.T) {
	t.Parallel()

	point := scriptPointValue(t, 1, "a")
	pointer := reflect.New(scriptPointType(t))
	pointer.Elem().Set(point)

	tests := []struct {
		name  string
		value reflect.Value
		want  string
	}{
		{name: "a pointer is marked with an ampersand", value: pointer, want: "&{1 a}"},
		{name: "a nil pointer prints as nil", value: reflect.Zero(reflect.PointerTo(scriptPointType(t))), want: "<nil>"},
		{name: "a value that is not a struct falls through", value: reflect.ValueOf(3), want: "3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, fmt.Sprintf("%v", pipitFmtValue{underlying: tt.value}))
		})
	}
}

func TestFmtRendererWalksContainersOfScriptTypes(t *testing.T) {
	t.Parallel()

	pointType := scriptPointType(t)

	t.Run("a slice prints each element", func(t *testing.T) {
		t.Parallel()

		slice := reflect.MakeSlice(reflect.SliceOf(pointType), 2, 2)
		slice.Index(0).Set(scriptPointValue(t, 1, "a"))
		slice.Index(1).Set(scriptPointValue(t, 2, "b"))

		require.Equal(t, "[{1 a} {2 b}]", fmt.Sprintf("%v", pipitFmtValue{underlying: slice}))
	})

	t.Run("an array prints each element", func(t *testing.T) {
		t.Parallel()

		array := reflect.New(reflect.ArrayOf(1, pointType)).Elem()
		array.Index(0).Set(scriptPointValue(t, 3, "c"))

		require.Equal(t, "[{3 c}]", fmt.Sprintf("%v", pipitFmtValue{underlying: array}))
	})

	t.Run("a map prints its keys in order", func(t *testing.T) {
		t.Parallel()

		mapValue := reflect.MakeMap(reflect.MapOf(reflect.TypeFor[string](), pointType))
		mapValue.SetMapIndex(reflect.ValueOf("b"), scriptPointValue(t, 2, "y"))
		mapValue.SetMapIndex(reflect.ValueOf("a"), scriptPointValue(t, 1, "x"))

		require.Equal(t, "map[a:{1 x} b:{2 y}]", fmt.Sprintf("%v", pipitFmtValue{underlying: mapValue}),
			"fmt orders map keys, so the renderer must too")
	})

	t.Run("a container of ordinary types is left to fmt", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, "[1 2]", fmt.Sprintf("%v", pipitFmtValue{underlying: reflect.ValueOf([]int{1, 2})}))
	})

	t.Run("a nested script struct is walked too", func(t *testing.T) {
		t.Parallel()

		outerType := reflect.StructOf([]reflect.StructField{
			{Name: "Inner", Type: pointType},
			{Name: pipitIDFieldPrefix + "Outer", Type: reflect.TypeFor[struct{}](), PkgPath: "main"},
		})
		outer := reflect.New(outerType).Elem()
		outer.Field(0).Set(scriptPointValue(t, 4, "d"))

		require.Equal(t, "{{4 d}}", fmt.Sprintf("%v", pipitFmtValue{underlying: outer}))
		require.Equal(t, `main.Outer{Inner:main.Point{X:4, Y:"d"}}`, fmt.Sprintf("%#v", pipitFmtValue{underlying: outer}))
	})
}

func TestFmtRendererFallsBackForUnfamiliarVerbs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
		value  reflect.Value
		want   string
	}{
		{name: "a decimal verb prints the value itself", format: "%d", value: reflect.ValueOf(42), want: "42"},
		{name: "a width is honoured", format: "%5d", value: reflect.ValueOf(42), want: "   42"},
		{name: "a precision is honoured", format: "%.2f", value: reflect.ValueOf(1.5), want: "1.50"},
		{name: "a left-justified width is honoured", format: "%-5d|", value: reflect.ValueOf(42), want: "42   |"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, fmt.Sprintf(tt.format, pipitFmtValue{underlying: tt.value}))
		})
	}

	t.Run("an empty wrapper prints as a nil operand", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, "<nil>", fmt.Sprintf("%v", pipitFmtValue{}))
	})
}

func TestGoSyntaxRendererQualifiesPointersAndNonStructs(t *testing.T) {
	t.Parallel()

	pointType := scriptPointType(t)
	pointer := reflect.New(pointType)
	pointer.Elem().Set(scriptPointValue(t, 1, "a"))

	tests := []struct {
		name  string
		value reflect.Value
		want  string
	}{
		{name: "a pointer is marked with an ampersand", value: pointer, want: `&main.Point{X:1, Y:"a"}`},
		{name: "a nil pointer prints as a parenthesised nil", value: reflect.Zero(reflect.PointerTo(pointType)), want: "(nil)"},
		{name: "a value that is not a struct falls through", value: reflect.ValueOf(3), want: "3"},
		{name: "a struct with no sentinel keeps reflect's name", value: reflect.ValueOf(identityNone{}), want: `engine.identityNone{A:0, B:""}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, fmt.Sprintf("%#v", pipitFmtValue{underlying: tt.value}))
		})
	}
}

func TestLeafMethodTextNeedsARegisteredMethod(t *testing.T) {
	t.Parallel()

	point := scriptPointValue(t, 1, "a")

	t.Run("no VM means no method to consult", func(t *testing.T) {
		t.Parallel()

		_, ok := pipitFmtRenderer{}.leafMethodText(point)
		require.False(t, ok)
	})

	t.Run("a type with no registered method declines", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "Other.String", stringReturningMethod(t, "String", "x"))

		_, ok := pipitFmtRenderer{vm: vm}.leafMethodText(point)
		require.False(t, ok)
	})

	t.Run("an ordinary type declines", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "Point.String", stringReturningMethod(t, "String", "x"))

		_, ok := pipitFmtRenderer{vm: vm}.leafMethodText(reflect.ValueOf(3))
		require.False(t, ok)
	})

	t.Run("a registered String method renders the leaf", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "Point.String", stringReturningMethod(t, "String", "rendered"))

		text, ok := pipitFmtRenderer{vm: vm}.leafMethodText(point)
		require.True(t, ok)
		require.Equal(t, "rendered", text)
	})

	t.Run("an Error method wins over a String method", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "Point.Error", stringReturningMethod(t, "Error", "failed"))

		text, ok := pipitFmtRenderer{vm: vm}.leafMethodText(point)
		require.True(t, ok)
		require.Equal(t, "failed", text, "fmt consults Error before String")
	})
}

func TestRestoreNamedTypeForFmtNeedsAQualifiedStaticType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		static string
	}{
		{name: "no static type"},
		{name: "an unqualified name", static: "Kind"},
		{name: "a leading dot", static: ".Kind"},
		{name: "a trailing dot", static: "reflect."},
		{name: "a slice form", static: "reflect.[]Kind"},
		{name: "a pointer form", static: "reflect.*Kind"},
		{name: "a type no registry holds", static: "absent.Kind"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newTestVM(t)
			require.Equal(t, uint64(6), restoreNamedTypeForFmt(vm, uint64(6), tt.static))
		})
	}

	t.Run("a nil argument is handed back", func(t *testing.T) {
		t.Parallel()

		require.Nil(t, restoreNamedTypeForFmt(newTestVM(t), nil, "reflect.Kind"))
	})

	t.Run("no VM hands the argument back", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, uint64(6), restoreNamedTypeForFmt(nil, uint64(6), "reflect.Kind"))
	})
}

func TestFmtRendererForWrapsTheValueWithItsTypeName(t *testing.T) {
	t.Parallel()

	vm := adapterHostVM(t, "Point.String", stringReturningMethod(t, "String", "x"))
	point := scriptPointValue(t, 1, "a")

	renderer := fmtRendererFor(vm, point)
	require.Equal(t, point.Interface(), renderer.underlying.Interface())
	require.Equal(t, "Point", renderer.typeName, "the wrapper carries the name the method table is keyed by")
}
