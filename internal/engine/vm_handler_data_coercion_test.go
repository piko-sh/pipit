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
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type coercionTarget struct {
	Unsigned8  uint8
	Unsigned16 uint16
	Unsigned32 uint32
	Unsigned64 uint64
	Signed8    int8
	Signed16   int16
	Signed32   int32
	Signed64   int64
	Text       string
}

type coercionNamedInt int

func TestIsIntegerLikeKindAcceptsEveryWidthAndSign(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind reflect.Kind
		want bool
	}{
		{name: "a plain int", kind: reflect.Int, want: true},
		{name: "an int8", kind: reflect.Int8, want: true},
		{name: "an int16", kind: reflect.Int16, want: true},
		{name: "an int32", kind: reflect.Int32, want: true},
		{name: "an int64", kind: reflect.Int64, want: true},
		{name: "a plain uint", kind: reflect.Uint, want: true},
		{name: "a uint8", kind: reflect.Uint8, want: true},
		{name: "a uint16", kind: reflect.Uint16, want: true},
		{name: "a uint32", kind: reflect.Uint32, want: true},
		{name: "a uint64", kind: reflect.Uint64, want: true},
		{name: "a uintptr", kind: reflect.Uintptr, want: true},
		{name: "a float is not integer-like", kind: reflect.Float64, want: false},
		{name: "a bool is not integer-like", kind: reflect.Bool, want: false},
		{name: "a string is not integer-like", kind: reflect.String, want: false},
		{name: "a complex value is not integer-like", kind: reflect.Complex128, want: false},
		{name: "a slice is not integer-like", kind: reflect.Slice, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isIntegerLikeKind(tt.kind))
		})
	}
}

func TestIntegerSourceReinterpretationCrossesTheSignBoundary(t *testing.T) {
	t.Parallel()

	t.Run("a signed source reinterprets into unsigned bits", func(t *testing.T) {
		t.Parallel()
		source := reflect.ValueOf(int64(-1))
		require.Equal(t, uint64(math.MaxUint64), integerSourceAsUint64(source, reflect.Int64))
	})

	t.Run("an unsigned source passes through unchanged", func(t *testing.T) {
		t.Parallel()
		source := reflect.ValueOf(uint64(42))
		require.Equal(t, uint64(42), integerSourceAsUint64(source, reflect.Uint64))
	})

	t.Run("an unsigned source reinterprets into signed bits", func(t *testing.T) {
		t.Parallel()
		source := reflect.ValueOf(uint64(math.MaxUint64))
		require.Equal(t, int64(-1), integerSourceAsInt64(source, reflect.Uint64))
	})

	t.Run("a signed source passes through unchanged", func(t *testing.T) {
		t.Parallel()
		source := reflect.ValueOf(int64(-42))
		require.Equal(t, int64(-42), integerSourceAsInt64(source, reflect.Int64))
	})
}

func TestWriteIntegerFieldFastCoversEveryDestinationWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		read  func(target *coercionTarget) int64
		name  string
		field string
		value int64
	}{
		{name: "an unsigned byte field", field: "Unsigned8", value: 200, read: func(x *coercionTarget) int64 { return int64(x.Unsigned8) }},
		{name: "an unsigned half-word field", field: "Unsigned16", value: 60000, read: func(x *coercionTarget) int64 { return int64(x.Unsigned16) }},
		{name: "an unsigned word field", field: "Unsigned32", value: 4000000000, read: func(x *coercionTarget) int64 { return int64(x.Unsigned32) }},
		{name: "an unsigned double-word field", field: "Unsigned64", value: 42, read: func(x *coercionTarget) int64 { return int64(x.Unsigned64) }},
		{name: "a signed byte field", field: "Signed8", value: -5, read: func(x *coercionTarget) int64 { return int64(x.Signed8) }},
		{name: "a signed half-word field", field: "Signed16", value: -300, read: func(x *coercionTarget) int64 { return int64(x.Signed16) }},
		{name: "a signed word field", field: "Signed32", value: -70000, read: func(x *coercionTarget) int64 { return int64(x.Signed32) }},
		{name: "a signed double-word field", field: "Signed64", value: -42, read: func(x *coercionTarget) int64 { return x.Signed64 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			target := &coercionTarget{}
			field := reflect.ValueOf(target).Elem().FieldByName(tt.field)

			require.True(t, writeIntegerFieldFast(field, reflect.ValueOf(tt.value)))
			require.Equal(t, tt.value, tt.read(target))
		})
	}

	t.Run("a non-integer destination is refused", func(t *testing.T) {
		t.Parallel()
		target := &coercionTarget{}
		field := reflect.ValueOf(target).Elem().FieldByName("Text")

		require.False(t, writeIntegerFieldFast(field, reflect.ValueOf(int64(1))),
			"the fast path must decline a destination it cannot write")
	})

	t.Run("a non-integer source is refused", func(t *testing.T) {
		t.Parallel()
		target := &coercionTarget{}
		field := reflect.ValueOf(target).Elem().FieldByName("Signed64")

		require.False(t, writeIntegerFieldFast(field, reflect.ValueOf("not a number")))
		require.Equal(t, int64(0), target.Signed64, "a refused write must leave the field alone")
	})

	t.Run("an unsigned source writes into a signed destination", func(t *testing.T) {
		t.Parallel()
		target := &coercionTarget{}
		field := reflect.ValueOf(target).Elem().FieldByName("Signed32")

		require.True(t, writeIntegerFieldFast(field, reflect.ValueOf(uint32(7))))
		require.Equal(t, int32(7), target.Signed32)
	})
}

func TestUnwrapErasedInterfaceValueOnlyPeelsAssignableInners(t *testing.T) {
	t.Parallel()

	var holder any = 42
	interfaceValue := reflect.ValueOf(&holder).Elem()

	var empty any
	nilInterface := reflect.ValueOf(&empty).Elem()

	tests := []struct {
		value      reflect.Value
		targetType reflect.Type
		name       string
		want       bool
	}{
		{name: "an interface holding an assignable value is peeled", value: interfaceValue, targetType: reflect.TypeFor[int](), want: true},
		{name: "an interface holding an unassignable value is not peeled", value: interfaceValue, targetType: reflect.TypeFor[string](), want: false},
		{name: "a nil interface is not peeled", value: nilInterface, targetType: reflect.TypeFor[int](), want: false},
		{name: "a concrete value is not peeled", value: reflect.ValueOf(42), targetType: reflect.TypeFor[int](), want: false},
		{name: "an interface target keeps the interface intact", value: interfaceValue, targetType: reflect.TypeFor[any](), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inner, ok := unwrapErasedInterfaceValue(tt.value, tt.targetType)

			require.Equal(t, tt.want, ok)
			if tt.want {
				require.True(t, inner.IsValid())
				require.Equal(t, tt.targetType, inner.Type())
			}
		})
	}
}

func TestElementCoercionPredicatesMatchOnlyCompatibleContainers(t *testing.T) {
	t.Parallel()

	t.Run("slice element coercion", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			from reflect.Type
			to   reflect.Type
			name string
			want bool
		}{
			{name: "a named element assignable to its underlying type", from: reflect.TypeFor[[]coercionNamedInt](), to: reflect.TypeFor[[]coercionNamedInt](), want: true},
			{name: "an element that is not assignable", from: reflect.TypeFor[[]int](), to: reflect.TypeFor[[]string](), want: false},
			{name: "a non-slice source", from: reflect.TypeFor[int](), to: reflect.TypeFor[[]int](), want: false},
			{name: "a non-slice target", from: reflect.TypeFor[[]int](), to: reflect.TypeFor[int](), want: false},
			{name: "a concrete element into an interface element", from: reflect.TypeFor[[]int](), to: reflect.TypeFor[[]any](), want: true},
		}
		for _, tt := range tests {
			require.Equal(t, tt.want, isSliceElementCoercion(tt.from, tt.to), tt.name)
		}
	})

	t.Run("array element coercion", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			from reflect.Type
			to   reflect.Type
			name string
			want bool
		}{
			{name: "matching lengths and assignable elements", from: reflect.TypeFor[[2]int](), to: reflect.TypeFor[[2]any](), want: true},
			{name: "mismatched lengths", from: reflect.TypeFor[[2]int](), to: reflect.TypeFor[[3]any](), want: false},
			{name: "a non-array source", from: reflect.TypeFor[[]int](), to: reflect.TypeFor[[2]any](), want: false},
			{name: "unassignable elements", from: reflect.TypeFor[[2]int](), to: reflect.TypeFor[[2]string](), want: false},
		}
		for _, tt := range tests {
			require.Equal(t, tt.want, isArrayElementCoercion(tt.from, tt.to), tt.name)
		}
	})

	t.Run("map element coercion", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			from reflect.Type
			to   reflect.Type
			name string
			want bool
		}{
			{name: "matching keys and assignable elements", from: reflect.TypeFor[map[string]int](), to: reflect.TypeFor[map[string]any](), want: true},
			{name: "mismatched key types", from: reflect.TypeFor[map[string]int](), to: reflect.TypeFor[map[int]any](), want: false},
			{name: "a non-map source", from: reflect.TypeFor[[]int](), to: reflect.TypeFor[map[string]any](), want: false},
			{name: "unassignable elements", from: reflect.TypeFor[map[string]int](), to: reflect.TypeFor[map[string]string](), want: false},
		}
		for _, tt := range tests {
			require.Equal(t, tt.want, isMapElementCoercion(tt.from, tt.to), tt.name)
		}
	})
}

func TestCoerceContainerElementsProducesTheTargetType(t *testing.T) {
	t.Parallel()

	t.Run("assignable slice elements are copied element by element", func(t *testing.T) {
		t.Parallel()
		source := reflect.ValueOf([]int{1, 2, 3})

		coerced := coerceSliceElements(source, reflect.TypeFor[[]any]())

		require.Equal(t, reflect.TypeFor[[]any](), coerced.Type())
		require.Equal(t, 3, coerced.Len())
		require.Equal(t, 1, coerced.Index(0).Interface())
	})

	t.Run("array elements are boxed into the target element type", func(t *testing.T) {
		t.Parallel()
		source := reflect.ValueOf([2]int{1, 2})

		coerced := coerceArrayElements(source, reflect.TypeFor[[2]any]())

		require.Equal(t, reflect.TypeFor[[2]any](), coerced.Type())
		require.Equal(t, 2, coerced.Len())
	})

	t.Run("map elements are boxed into the target element type", func(t *testing.T) {
		t.Parallel()
		source := reflect.ValueOf(map[string]int{"a": 1})

		coerced := coerceMapElements(source, reflect.TypeFor[map[string]any]())

		require.Equal(t, reflect.TypeFor[map[string]any](), coerced.Type())
		require.Equal(t, 1, coerced.Len())
	})
}

func TestCoerceConvertibleSliceElementsNeedsAMatchingElementWidth(t *testing.T) {
	t.Parallel()

	t.Run("a named element of the same width converts", func(t *testing.T) {
		t.Parallel()
		coerced, ok := coerceConvertibleSliceElements(reflect.ValueOf([]int{1, 2}), reflect.TypeFor[[]coercionNamedInt]())

		require.True(t, ok)
		require.Equal(t, reflect.TypeFor[[]coercionNamedInt](), coerced.Type())
		require.Equal(t, int64(1), coerced.Index(0).Int())
	})

	t.Run("a nil slice converts to the typed nil", func(t *testing.T) {
		t.Parallel()
		coerced, ok := coerceConvertibleSliceElements(reflect.ValueOf([]int(nil)), reflect.TypeFor[[]coercionNamedInt]())

		require.True(t, ok)
		require.True(t, coerced.IsNil(), "a nil source must stay nil rather than become an empty slice")
	})

	tests := []struct {
		to   reflect.Type
		from reflect.Value
		name string
	}{
		{name: "an element of a different width is refused", from: reflect.ValueOf([]int32{1}), to: reflect.TypeFor[[]int64]()},
		{name: "an interface element is a different width", from: reflect.ValueOf([]int{1}), to: reflect.TypeFor[[]any]()},
		{name: "an unconvertible element is refused", from: reflect.ValueOf([]string{"a"}), to: reflect.TypeFor[[]int]()},
		{name: "a non-slice source is refused", from: reflect.ValueOf(1), to: reflect.TypeFor[[]int]()},
		{name: "a non-slice target is refused", from: reflect.ValueOf([]int{1}), to: reflect.TypeFor[int]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, ok := coerceConvertibleSliceElements(tt.from, tt.to)
			require.False(t, ok)
		})
	}
}

func TestConvertOrCoerceContainerPrefersADirectConversion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		check      func(t *testing.T, got reflect.Value)
		targetType reflect.Type
		value      reflect.Value
		name       string
	}{
		{
			name:  "a non-zero integer converts to true",
			value: reflect.ValueOf(1), targetType: reflect.TypeFor[bool](),
			check: func(t *testing.T, got reflect.Value) { require.True(t, got.Bool()) },
		},
		{
			name:  "a zero integer converts to false",
			value: reflect.ValueOf(0), targetType: reflect.TypeFor[bool](),
			check: func(t *testing.T, got reflect.Value) { require.False(t, got.Bool()) },
		},
		{
			name:  "a directly convertible scalar takes the conversion",
			value: reflect.ValueOf(65), targetType: reflect.TypeFor[coercionNamedInt](),
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, int64(65), got.Int()) },
		},
		{
			name:  "an assignable slice element takes the element copy",
			value: reflect.ValueOf([]int{1}), targetType: reflect.TypeFor[[]any](),
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, reflect.TypeFor[[]any](), got.Type()) },
		},
		{
			name:  "an assignable map element takes the element copy",
			value: reflect.ValueOf(map[string]int{"a": 1}), targetType: reflect.TypeFor[map[string]any](),
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, reflect.TypeFor[map[string]any](), got.Type()) },
		},
		{
			name:  "a value with no route back is returned unchanged",
			value: reflect.ValueOf([]int{1}), targetType: reflect.TypeFor[map[string]int](),
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, reflect.TypeFor[[]int](), got.Type()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t, convertOrCoerceContainer(tt.value, tt.targetType))
		})
	}
}
