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
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type assertStringer struct{}

func (assertStringer) String() string { return "s" }

type assertNamedInt int

func TestMatchTypeAssertionFollowsGoAssertionRules(t *testing.T) {
	t.Parallel()

	var stringerTarget interface{ String() string }
	stringerType := reflect.TypeOf(&stringerTarget).Elem()

	tests := []struct {
		source     reflect.Value
		targetType reflect.Type
		name       string
		want       bool
	}{
		{name: "an identical concrete type matches", source: reflect.ValueOf(42), targetType: reflect.TypeFor[int](), want: true},
		{name: "a different concrete type does not match", source: reflect.ValueOf(42), targetType: reflect.TypeFor[string](), want: false},
		{name: "an int does not match a named int", source: reflect.ValueOf(42), targetType: reflect.TypeFor[assertNamedInt](), want: false},
		{name: "a named int matches itself", source: reflect.ValueOf(assertNamedInt(1)), targetType: reflect.TypeFor[assertNamedInt](), want: true},
		{name: "a type implementing an interface matches it", source: reflect.ValueOf(assertStringer{}), targetType: stringerType, want: true},
		{name: "a type not implementing an interface does not match", source: reflect.ValueOf(42), targetType: stringerType, want: false},
		{name: "a slice matches its own type", source: reflect.ValueOf([]int{1}), targetType: reflect.TypeFor[[]int](), want: true},
		{name: "an invalid source matches a nil target", source: reflect.Value{}, targetType: nil, want: true},
		{name: "a valid source does not match a nil target", source: reflect.ValueOf(1), targetType: nil, want: false},
		{name: "an invalid source does not match a concrete target", source: reflect.Value{}, targetType: reflect.TypeFor[int](), want: false},
		{name: "any type matches the empty interface", source: reflect.ValueOf(42), targetType: reflect.TypeFor[any](), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, ok := matchTypeAssertion(tt.source, tt.targetType)

			require.Equal(t, tt.want, ok)
		})
	}
}

func TestSliceBoundsRuntimeErrorNamesTheOffendingBound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		low          int
		high         int
		size         int
		useCapacity  bool
		wantContains string
	}{
		{name: "a negative low bound", low: -1, high: 0, size: 4, wantContains: "[-1:]"},
		{name: "a high bound below the low bound", low: 3, high: 1, size: 4, wantContains: "[3:1]"},
		{name: "a high bound past the length", low: 0, high: 9, size: 4, wantContains: "with length 4"},
		{name: "a high bound past the capacity", low: 0, high: 9, size: 4, useCapacity: true, wantContains: "with capacity 4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := sliceBoundsRuntimeError(tt.low, tt.high, tt.size, tt.useCapacity)

			require.NotNil(t, err)
			require.Contains(t, err.Error(), tt.wantContains)
			require.Contains(t, err.Error(), "slice bounds out of range")
		})
	}
}

func TestClosureHelpersRefuseValuesThatAreNotClosures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value reflect.Value
		name  string
	}{
		{name: "an invalid value", value: reflect.Value{}},
		{name: "an integer", value: reflect.ValueOf(42)},
		{name: "a plain function", value: reflect.ValueOf(func() {})},
		{name: "a nil closure pointer", value: reflect.ValueOf((*RuntimeClosure)(nil))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Nil(t, closureFromValue(tt.value))
			require.Nil(t, closureSignatureType(tt.value))
		})
	}
}

func TestUncaughtPanicAccessorsUnwrapOnlyTheirOwnError(t *testing.T) {
	t.Parallel()

	t.Run("a goroutine panic carries its value", func(t *testing.T) {
		t.Parallel()
		err := NewGoroutinePanicError("boom")

		value, ok := UncaughtPanicValue(err)

		require.True(t, ok)
		require.Equal(t, "boom", value)
	})

	t.Run("an unrelated error carries no panic value", func(t *testing.T) {
		t.Parallel()
		value, ok := UncaughtPanicValue(errors.New("ordinary"))

		require.False(t, ok)
		require.Nil(t, value)
	})

	t.Run("a nil error carries no panic value", func(t *testing.T) {
		t.Parallel()
		value, ok := UncaughtPanicValue(nil)

		require.False(t, ok)
		require.Nil(t, value)
	})

	t.Run("a wrapped panic is still found", func(t *testing.T) {
		t.Parallel()
		wrapped := errors.Join(errors.New("context"), NewGoroutinePanicError(42))

		value, ok := UncaughtPanicValue(wrapped)

		require.True(t, ok)
		require.Equal(t, 42, value)
	})
}

func TestRuntimeErrorConstructorsFormatTheirMessage(t *testing.T) {
	t.Parallel()

	t.Run("a runtime panic carries its formatted message", func(t *testing.T) {
		t.Parallel()
		err := newRuntimePanicError("index %d out of range for %d", 7, 3)

		require.Contains(t, err.Error(), "index 7 out of range for 3")
	})

	t.Run("an invariant error carries its formatted message", func(t *testing.T) {
		t.Parallel()
		err := newInvariantError("unexpected kind %s", reflect.Slice)

		require.Contains(t, err.Error(), "unexpected kind slice")
	})
}

func TestIsBuiltinScalarTypeNameCoversTheLanguageScalars(t *testing.T) {
	t.Parallel()

	builtins := []string{
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"byte", "rune", "float32", "float64",
		"complex64", "complex128", "bool", "string",
	}

	for _, name := range builtins {
		t.Run(name+" is a builtin scalar", func(t *testing.T) {
			t.Parallel()
			require.True(t, isBuiltinScalarTypeName(name))
		})
	}

	others := []string{"error", "any", "Stringer", "MyInt", "", "Int", "[]int", "map[string]int"}
	for _, name := range others {
		t.Run(name+" is not a builtin scalar", func(t *testing.T) {
			t.Parallel()
			require.False(t, isBuiltinScalarTypeName(name))
		})
	}
}

func TestBareSentinelNameReadsTheIdentityFieldSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sentinel reflect.Type
		name     string
		want     string
	}{
		{name: "a value sentinel", sentinel: reflect.TypeFor[identityTrailing](), want: "at"},
		{name: "a pointer sentinel is followed", sentinel: reflect.TypeFor[*identityTrailing](), want: "at"},
		{name: "a struct with no identity field has no name", sentinel: reflect.TypeFor[identityNone](), want: ""},
		{name: "a non-struct has no name", sentinel: reflect.TypeFor[int](), want: ""},
		{name: "a slice has no name", sentinel: reflect.TypeFor[[]int](), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, bareSentinelName(tt.sentinel))
		})
	}
}
