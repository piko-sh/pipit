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
)

func TestSetScalarFromValueRefusesAMismatchedSource(t *testing.T) {
	t.Parallel()

	type scalars struct {
		Signed    int32
		Unsigned  uint16
		Ratio     float32
		Text      string
		Flag      bool
		Composite []int
	}

	tests := []struct {
		source reflect.Value
		check  func(t *testing.T, target *scalars)
		name   string
		field  string
		want   bool
	}{
		{
			name: "a signed source into a signed field", field: "Signed", source: reflect.ValueOf(42), want: true,
			check: func(t *testing.T, target *scalars) { require.Equal(t, int32(42), target.Signed) },
		},
		{
			name: "an unsigned source into an unsigned field", field: "Unsigned", source: reflect.ValueOf(uint8(7)), want: true,
			check: func(t *testing.T, target *scalars) { require.Equal(t, uint16(7), target.Unsigned) },
		},
		{
			name: "a float source into a float field", field: "Ratio", source: reflect.ValueOf(1.5), want: true,
			check: func(t *testing.T, target *scalars) { require.InDelta(t, 1.5, float64(target.Ratio), 0.001) },
		},
		{
			name: "a string source into a string field", field: "Text", source: reflect.ValueOf("pipit"), want: true,
			check: func(t *testing.T, target *scalars) { require.Equal(t, "pipit", target.Text) },
		},
		{
			name: "a boolean source into a boolean field", field: "Flag", source: reflect.ValueOf(true), want: true,
			check: func(t *testing.T, target *scalars) { require.True(t, target.Flag) },
		},
		{name: "a string source into a signed field is refused", field: "Signed", source: reflect.ValueOf("x")},
		{name: "a signed source into a string field is refused", field: "Text", source: reflect.ValueOf(1)},
		{name: "a float source into an unsigned field is refused", field: "Unsigned", source: reflect.ValueOf(1.5)},
		{name: "a signed source into a boolean field is refused", field: "Flag", source: reflect.ValueOf(1)},
		{name: "any source into a composite field is refused", field: "Composite", source: reflect.ValueOf([]int{1})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			target := &scalars{}
			field := reflect.ValueOf(target).Elem().FieldByName(tt.field)

			got := setScalarFromValue(field, tt.source)

			require.Equal(t, tt.want, got)
			if tt.want {
				tt.check(t, target)
			}
		})
	}
}

func TestConvertMakeFuncResultsWidensOnlyWhereItCan(t *testing.T) {
	t.Parallel()

	t.Run("a convertible result is converted to the declared type", func(t *testing.T) {
		t.Parallel()
		functionType := reflect.TypeFor[func() int64]()
		results := []reflect.Value{reflect.ValueOf(int32(42))}

		convertMakeFuncResults(functionType, results)

		require.Equal(t, reflect.TypeFor[int64](), results[0].Type())
		require.Equal(t, int64(42), results[0].Int())
	})

	t.Run("a matching result is left alone", func(t *testing.T) {
		t.Parallel()
		functionType := reflect.TypeFor[func() int64]()
		results := []reflect.Value{reflect.ValueOf(int64(42))}

		convertMakeFuncResults(functionType, results)

		require.Equal(t, int64(42), results[0].Int())
	})

	t.Run("an unconvertible result is left alone", func(t *testing.T) {
		t.Parallel()
		functionType := reflect.TypeFor[func() []int]()
		results := []reflect.Value{reflect.ValueOf("not a slice")}

		convertMakeFuncResults(functionType, results)

		require.Equal(t, reflect.TypeFor[string](), results[0].Type())
	})

	t.Run("more results than the signature declares are ignored", func(t *testing.T) {
		t.Parallel()
		functionType := reflect.TypeFor[func() int64]()
		results := []reflect.Value{reflect.ValueOf(int32(1)), reflect.ValueOf(int32(2))}

		require.NotPanics(t, func() { convertMakeFuncResults(functionType, results) })
		require.Equal(t, reflect.TypeFor[int32](), results[1].Type())
	})

	t.Run("an invalid result is skipped", func(t *testing.T) {
		t.Parallel()
		functionType := reflect.TypeFor[func() int64]()
		results := []reflect.Value{{}}

		require.NotPanics(t, func() { convertMakeFuncResults(functionType, results) })
	})
}

func TestUnwrapReflectTypeSliceElementLeavesPlainValuesAlone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		element reflect.Value
		name    string
	}{
		{name: "an integer", element: reflect.ValueOf(42)},
		{name: "a string", element: reflect.ValueOf("a")},
		{name: "a reflect type", element: reflect.ValueOf(reflect.TypeFor[int]())},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.element, unwrapReflectTypeSliceElement(tt.element))
		})
	}
}

func TestPipitTypeOfSourcePackageStripsTheTypePrefixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "a qualified type", input: "pkg.Type", want: "pkg"},
		{name: "a pointer to a qualified type", input: "*pkg.Type", want: "pkg"},
		{name: "a double pointer", input: "**pkg.Type", want: "pkg"},
		{name: "a slice of a qualified type", input: "[]pkg.Type", want: "pkg"},
		{name: "a slice of pointers", input: "[]*pkg.Type", want: "pkg"},
		{name: "an array of a qualified type", input: "[4]pkg.Type", want: "pkg"},
		{name: "an unqualified type has no package", input: "Type", want: ""},
		{name: "a builtin has no package", input: "int", want: ""},
		{name: "an empty string has no package", input: "", want: ""},
		{name: "an unterminated bracket has no package", input: "[pkg.Type", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			site := &program.CallSite{ArgumentStaticTypeStrings: []string{tt.input}}

			require.Equal(t, tt.want, pipitTypeOfSourcePackage(site))
		})
	}

	t.Run("a site with no static type strings has no package", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, pipitTypeOfSourcePackage(&program.CallSite{}))
	})
}

func TestIsPipitSynthesisedStructValueRejectsOrdinaryValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value reflect.Value
		name  string
	}{
		{name: "an integer", value: reflect.ValueOf(42)},
		{name: "a plain struct", value: reflect.ValueOf(leafCounter{})},
		{name: "a pointer to a plain struct", value: reflect.ValueOf(&leafCounter{})},
		{name: "a nil pointer", value: reflect.ValueOf((*leafCounter)(nil))},
		{name: "a slice", value: reflect.ValueOf([]int{1})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.False(t, isPipitSynthesisedStructValue(tt.value),
				"only a struct the compiler synthesised carries the identity marker")
		})
	}

	t.Run("a struct carrying the identity marker is recognised", func(t *testing.T) {
		t.Parallel()
		require.True(t, isPipitSynthesisedStructValue(reflect.ValueOf(identityTrailing{})))
		require.True(t, isPipitSynthesisedStructValue(reflect.ValueOf(&identityTrailing{})))
	})
}
