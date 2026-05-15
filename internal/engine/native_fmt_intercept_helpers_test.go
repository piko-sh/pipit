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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestReconstructVerbRebuildsTheFormatSpecifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
		value  any
	}{
		{name: "a plain verb", format: "%v", value: 42},
		{name: "a verb with the plus flag", format: "%+v", value: 42},
		{name: "a verb with the hash flag", format: "%#v", value: 42},
		{name: "a verb with the minus flag and a width", format: "%-8v", value: 42},
		{name: "a verb with the space flag", format: "% v", value: 42},
		{name: "a verb with the zero flag and a width", format: "%08v", value: 42},
		{name: "a verb with a width", format: "%10v", value: 42},
		{name: "a verb with a precision", format: "%.3v", value: "abcdef"},
		{name: "a verb with a width and a precision", format: "%10.3v", value: "abcdef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			recorder := &verbRecorder{}

			_ = fmt.Sprintf(tt.format, recorder)

			require.Equal(t, tt.format, recorder.reconstructed,
				"the rebuilt specifier must match the one the caller wrote")
		})
	}
}

type verbRecorder struct {
	reconstructed string
}

func (r *verbRecorder) Format(state fmt.State, verb rune) {
	r.reconstructed = reconstructVerb(state, verb)
}

func TestCountVerbStarArgsCountsOnlyWithinTheFlagRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		start int
		end   int
		want  int
	}{
		{name: "no stars", input: "%08d", start: 0, end: 4, want: 0},
		{name: "one star", input: "%*d", start: 0, end: 3, want: 1},
		{name: "two stars", input: "%*.*f", start: 0, end: 5, want: 2},
		{name: "a star outside the range is not counted", input: "%*d*", start: 0, end: 3, want: 1},
		{name: "an empty range counts nothing", input: "%*d", start: 1, end: 1, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, countVerbStarArgs([]rune(tt.input), tt.start, tt.end))
		})
	}
}

func TestDerefReflectTypeFollowsOnePointerOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input reflect.Type
		want  reflect.Type
		name  string
	}{
		{name: "a pointer is followed", input: reflect.TypeFor[*int](), want: reflect.TypeFor[int]()},
		{name: "a double pointer is followed once", input: reflect.TypeFor[**int](), want: reflect.TypeFor[*int]()},
		{name: "a non-pointer passes through", input: reflect.TypeFor[int](), want: reflect.TypeFor[int]()},
		{name: "a slice passes through", input: reflect.TypeFor[[]int](), want: reflect.TypeFor[[]int]()},
		{name: "a nil type passes through", input: nil, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, derefReflectType(tt.input))
		})
	}
}

func TestReflectMapKeyCompareOrdersEveryComparableKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		left  reflect.Value
		right reflect.Value
		name  string
		want  int
	}{
		{name: "a smaller integer sorts first", left: reflect.ValueOf(1), right: reflect.ValueOf(2), want: -1},
		{name: "equal integers tie", left: reflect.ValueOf(2), right: reflect.ValueOf(2), want: 0},
		{name: "a larger integer sorts last", left: reflect.ValueOf(3), right: reflect.ValueOf(2), want: 1},
		{name: "a negative integer sorts before zero", left: reflect.ValueOf(-1), right: reflect.ValueOf(0), want: -1},
		{name: "a smaller unsigned value sorts first", left: reflect.ValueOf(uint(1)), right: reflect.ValueOf(uint(2)), want: -1},
		{name: "a smaller float sorts first", left: reflect.ValueOf(1.5), right: reflect.ValueOf(2.5), want: -1},
		{name: "a lexicographically smaller string sorts first", left: reflect.ValueOf("a"), right: reflect.ValueOf("b"), want: -1},
		{name: "equal strings tie", left: reflect.ValueOf("a"), right: reflect.ValueOf("a"), want: 0},
		{name: "false sorts before true", left: reflect.ValueOf(false), right: reflect.ValueOf(true), want: -1},
		{name: "true sorts after false", left: reflect.ValueOf(true), right: reflect.ValueOf(false), want: 1},
		{name: "equal booleans tie", left: reflect.ValueOf(true), right: reflect.ValueOf(true), want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, reflectMapKeyCompare(tt.left, tt.right))
		})
	}

	t.Run("an uncomparable kind falls back to its rendered form", func(t *testing.T) {
		t.Parallel()
		left := reflect.ValueOf(struct{ X int }{X: 1})
		right := reflect.ValueOf(struct{ X int }{X: 2})

		require.Equal(t, -1, reflectMapKeyCompare(left, right),
			"a kind with no natural order is compared by how it prints")
	})
}

func TestFmtArgFromValueUnwrapsWhatItCan(t *testing.T) {
	t.Parallel()

	t.Run("an accessible value is unwrapped to its interface", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, 42, fmtArgFromValue(reflect.ValueOf(42)))
	})

	t.Run("an inaccessible value is passed through as the reflect value", func(t *testing.T) {
		t.Parallel()
		type hidden struct{ unexported int }
		value := reflect.ValueOf(hidden{unexported: 1}).Field(0)

		require.False(t, value.CanInterface())
		require.Equal(t, value, fmtArgFromValue(value))
	})
}

func TestWellKnownNamedInterfaceReflectTypeResolvesTheBuiltinError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pkgPath  string
		typeName string
		want     bool
	}{
		{name: "the builtin error type resolves", pkgPath: "", typeName: "error", want: true},
		{name: "another builtin name does not resolve", pkgPath: "", typeName: "Stringer", want: false},
		{name: "an unknown package does not resolve", pkgPath: "example.com/nope", typeName: "Thing", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := WellKnownNamedInterfaceReflectType(tt.pkgPath, tt.typeName)

			require.Equal(t, tt.want, ok)
			if tt.want {
				require.Equal(t, reflect.Interface, got.Kind())
			}
		})
	}
}

func TestUnwrapAdapterUnderlyingLeavesOrdinaryValuesAlone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value reflect.Value
		name  string
	}{
		{name: "an integer", value: reflect.ValueOf(42)},
		{name: "a string", value: reflect.ValueOf("pipit")},
		{name: "a nil pointer", value: reflect.ValueOf((*int)(nil))},
		{name: "a non-nil pointer to an integer", value: reflect.ValueOf(new(int))},
		{name: "a slice", value: reflect.ValueOf([]int{1})},
		{name: "an invalid value", value: reflect.Value{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.value, unwrapAdapterUnderlying(tt.value),
				"a value that is not an adapter must pass through untouched")
		})
	}
}

func TestEmbeddedFieldNameHelpersRoundTripThePrefix(t *testing.T) {
	t.Parallel()

	renamed := isa.EmbeddedUnexportedPrefix + "inner"

	tests := []struct {
		name        string
		fieldName   string
		wantRenamed bool
		wantSource  string
	}{
		{name: "a renamed embedded field is recognised", fieldName: renamed, wantRenamed: true, wantSource: "inner"},
		{name: "an ordinary field is not renamed", fieldName: "Value", wantRenamed: false, wantSource: "Value"},
		{name: "an empty name is not renamed", fieldName: "", wantRenamed: false, wantSource: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			field := reflect.StructField{Name: tt.fieldName}

			require.Equal(t, tt.wantRenamed, isRenamedEmbeddedField(field))
			require.Equal(t, tt.wantSource, pipitSourceFieldName(tt.fieldName))
		})
	}

	t.Run("the prefix is stripped exactly once", func(t *testing.T) {
		t.Parallel()
		doubled := isa.EmbeddedUnexportedPrefix + isa.EmbeddedUnexportedPrefix + "x"

		require.Equal(t, isa.EmbeddedUnexportedPrefix+"x", pipitSourceFieldName(doubled))
	})
}

type identityTrailing struct {
	A           int
	_pipitID_at struct{}
}

type identityLeading struct {
	_pipitID_at struct{}
	A           int
}

type identityNone struct {
	A int
	B string
}

func TestPipitUserFieldCountExcludesTheSyntheticIdentityField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		structType reflect.Type
		name       string
		want       int
	}{
		{name: "an ordinary struct counts every field", structType: reflect.TypeFor[identityNone](), want: 2},
		{name: "a struct with no fields counts none", structType: reflect.TypeFor[struct{}](), want: 0},
		{name: "a trailing identity field is excluded", structType: reflect.TypeFor[identityTrailing](), want: 1},
		{name: "the prefix is only honoured in the last position", structType: reflect.TypeFor[identityLeading](), want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, pipitUserFieldCount(tt.structType))
		})
	}
}

func TestIsScalarReflectKindPartitionsTheKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind reflect.Kind
		want bool
	}{
		{name: "an integer is scalar", kind: reflect.Int, want: true},
		{name: "an unsigned value is scalar", kind: reflect.Uint64, want: true},
		{name: "a float is scalar", kind: reflect.Float64, want: true},
		{name: "a bool is scalar", kind: reflect.Bool, want: true},
		{name: "a string is scalar", kind: reflect.String, want: true},
		{name: "a slice is not scalar", kind: reflect.Slice, want: false},
		{name: "a struct is not scalar", kind: reflect.Struct, want: false},
		{name: "a map is not scalar", kind: reflect.Map, want: false},
		{name: "a pointer is not scalar", kind: reflect.Pointer, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isScalarReflectKind(tt.kind))
		})
	}
}

func TestStartsWithStarOfQualifiedMatchesThePointerRendering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		qualified string
		want      bool
	}{
		{name: "a pointer rendering matches", input: "*pkg.Type", qualified: "pkg.Type", want: true},
		{name: "a value rendering does not match", input: "pkg.Type", qualified: "pkg.Type", want: false},
		{name: "a different type does not match", input: "*pkg.Other", qualified: "pkg.Type", want: false},
		{name: "an empty input does not match", input: "", qualified: "pkg.Type", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, startsWithStarOfQualified(tt.input, tt.qualified))
		})
	}

	t.Run("the helper agrees with a plain prefix check", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, strings.HasPrefix("*pkg.Type", "*pkg.Type"),
			startsWithStarOfQualified("*pkg.Type", "pkg.Type"))
	})
}
