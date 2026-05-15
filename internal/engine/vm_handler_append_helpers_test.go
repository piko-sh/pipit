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
	"unsafe"

	"github.com/stretchr/testify/require"
)

type appendNamedByte byte

func TestSliceDataPointersAddressTheFirstElement(t *testing.T) {
	t.Parallel()

	t.Run("a populated slice yields its first element address", func(t *testing.T) {
		t.Parallel()
		strings := []string{"a", "b"}
		require.Equal(t, unsafe.Pointer(&strings[0]), sliceDataPtrString(strings))

		ints := []int64{1, 2}
		require.Equal(t, unsafe.Pointer(&ints[0]), sliceDataPtrInt64(ints))

		floats := []float64{1, 2}
		require.Equal(t, unsafe.Pointer(&floats[0]), sliceDataPtrFloat64(floats))

		bools := []bool{true, false}
		require.Equal(t, unsafe.Pointer(&bools[0]), sliceDataPtrBool(bools))

		uints := []uint64{1, 2}
		require.Equal(t, unsafe.Pointer(&uints[0]), sliceDataPtrUint64(uints))

		plainInts := []int{1, 2}
		require.Equal(t, unsafe.Pointer(&plainInts[0]), sliceDataPtrInt(plainInts))

		plainUints := []uint{1, 2}
		require.Equal(t, unsafe.Pointer(&plainUints[0]), sliceDataPtrUint(plainUints))

		words := []uint32{1, 2}
		require.Equal(t, unsafe.Pointer(&words[0]), sliceDataPtrUint32(words))

		halves := []uint16{1, 2}
		require.Equal(t, unsafe.Pointer(&halves[0]), sliceDataPtrUint16(halves))

		pointers := []uintptr{1, 2}
		require.Equal(t, unsafe.Pointer(&pointers[0]), sliceDataPtrUintptr(pointers))
	})

	t.Run("a nil slice yields the nil pointer", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, sliceDataPtrString(nil))
		require.Nil(t, sliceDataPtrInt64(nil))
		require.Nil(t, sliceDataPtrFloat64(nil))
		require.Nil(t, sliceDataPtrBool(nil))
		require.Nil(t, sliceDataPtrUint64(nil))
	})
}

func TestAppendFastPathHandlesEveryTypedElementBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		slice   any
		element any
		check   func(t *testing.T, got reflect.Value)
		name    string
	}{
		{
			name: "an int64 slice", slice: []int64{1, 2}, element: int64(3),
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 3, got.Len())
				require.Equal(t, int64(3), got.Index(2).Int())
			},
		},
		{
			name: "a plain int slice", slice: []int{1, 2}, element: 3,
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 3, got.Len())
				require.Equal(t, int64(3), got.Index(2).Int())
			},
		},
		{
			name: "a float slice", slice: []float64{1}, element: 2.5,
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 2, got.Len())
				require.InDelta(t, 2.5, got.Index(1).Float(), 0)
			},
		},
		{
			name: "a string slice", slice: []string{"a"}, element: "b",
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 2, got.Len())
				require.Equal(t, "b", got.Index(1).String())
			},
		},
		{
			name: "a bool slice", slice: []bool{true}, element: false,
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 2, got.Len())
				require.False(t, got.Index(1).Bool())
			},
		},
		{
			name: "a byte slice", slice: []byte{1}, element: byte(2),
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 2, got.Len())
				require.Equal(t, uint64(2), got.Index(1).Uint())
			},
		},
		{
			name: "an empty slice grows to one element", slice: []int64{}, element: int64(1),
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, 1, got.Len()) },
		},
		{
			name: "a nil slice grows to one element", slice: []int64(nil), element: int64(1),
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, 1, got.Len()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			arena := newTestArena(t)

			got, ok := appendFastPath(arena, reflect.ValueOf(tt.slice), reflect.ValueOf(tt.element))

			require.True(t, ok, "a typed bank slice must take the fast path")
			tt.check(t, got)
		})
	}

	t.Run("a slice the fast path does not know is declined", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)

		_, ok := appendFastPath(arena, reflect.ValueOf([]struct{ X int }{}), reflect.ValueOf(struct{ X int }{}))

		require.False(t, ok, "an unknown element type must fall through to the general path")
	})
}

func TestAppendByteSliceFastPathClampsIntegerElements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		element any
		name    string
		want    uint64
		ok      bool
	}{
		{name: "a byte element", element: byte(7), want: 7, ok: true},
		{name: "an in-range signed element is unchanged", element: 7, want: 7, ok: true},
		{name: "a signed element above the byte range clamps to the ceiling", element: 263, want: 255, ok: true},
		{name: "a negative signed element clamps to zero rather than wrapping", element: -1, want: 0, ok: true},
		{name: "an unsigned element above the byte range clamps to the ceiling", element: uint32(263), want: 255, ok: true},
		{name: "an in-range unsigned element is unchanged", element: uint32(7), want: 7, ok: true},
		{name: "a named byte element narrows through its kind", element: appendNamedByte(7), want: 7, ok: true},
		{name: "a string element is declined", element: "x", ok: false},
		{name: "a float element is declined", element: 1.5, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			arena := newTestArena(t)

			got, ok := appendByteSliceFastPath(arena, []byte{1}, reflect.ValueOf(tt.element))

			require.Equal(t, tt.ok, ok)
			if !tt.ok {
				return
			}
			require.Equal(t, 2, got.Len())
			require.Equal(t, tt.want, got.Index(1).Uint())
		})
	}
}

func TestConformSpreadSourceConvertsOnlyWhenTheTypesDiffer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source          reflect.Value
		destinationType reflect.Type
		check           func(t *testing.T, got reflect.Value)
		name            string
	}{
		{
			name:   "an identical type passes through untouched",
			source: reflect.ValueOf([]int{1, 2}), destinationType: reflect.TypeFor[[]int](),
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, reflect.TypeFor[[]int](), got.Type()) },
		},
		{
			name:   "a convertible element type is converted",
			source: reflect.ValueOf([]int{1, 2}), destinationType: reflect.TypeFor[[]int32](),
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, reflect.TypeFor[[]int32](), got.Type())
				require.Equal(t, int64(2), got.Index(1).Int())
			},
		},
		{
			name:   "an unconvertible element type passes through untouched",
			source: reflect.ValueOf([]int{1}), destinationType: reflect.TypeFor[[]struct{ X int }](),
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, reflect.TypeFor[[]int](), got.Type()) },
		},
		{
			name:   "an empty slice converts to an empty slice",
			source: reflect.ValueOf([]int{}), destinationType: reflect.TypeFor[[]int32](),
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, 0, got.Len()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t, conformSpreadSource(tt.source, tt.destinationType))
		})
	}
}

func TestArenaByteAppendGrowsInPlaceUntilTheCapacityRuns(t *testing.T) {
	t.Parallel()

	t.Run("a spread within capacity extends the existing backing", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		destination := make([]byte, 2, 8)
		destination[0], destination[1] = 1, 2

		grown := arenaAppendByteSpread(arena, destination, []byte{3, 4})

		require.Equal(t, []byte{1, 2, 3, 4}, grown)
		require.Equal(t, 8, cap(grown), "an append within capacity must not reallocate")
	})

	t.Run("a spread past capacity allocates fresh backing", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		destination := []byte{1, 2}

		grown := arenaAppendByteSpread(arena, destination, []byte{3, 4, 5})

		require.Equal(t, []byte{1, 2, 3, 4, 5}, grown)
		require.GreaterOrEqual(t, cap(grown), 5)
	})

	t.Run("a string spread appends its bytes", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)

		grown := arenaAppendByteFromString(arena, []byte("ab"), "cd")

		require.Equal(t, []byte("abcd"), grown)
	})

	t.Run("a string spread within capacity extends the existing backing", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		destination := make([]byte, 2, 16)
		copy(destination, "ab")

		grown := arenaAppendByteFromString(arena, destination, "cd")

		require.Equal(t, []byte("abcd"), grown)
		require.Equal(t, 16, cap(grown))
	})

	t.Run("an empty spread leaves the destination unchanged", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)

		require.Equal(t, []byte("ab"), arenaAppendByteSpread(arena, []byte("ab"), nil))
		require.Equal(t, []byte("ab"), arenaAppendByteFromString(arena, []byte("ab"), ""))
	})
}

func TestConformSliceWidthConvertsBetweenScalarBanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value      reflect.Value
		targetType reflect.Type
		name       string
		want       bool
	}{
		{name: "a signed widening converts", value: reflect.ValueOf([]int32{1, 2}), targetType: reflect.TypeFor[[]int64](), want: true},
		{name: "a signed narrowing converts", value: reflect.ValueOf([]int64{1, 2}), targetType: reflect.TypeFor[[]int32](), want: true},
		{name: "a sign change converts", value: reflect.ValueOf([]int64{1}), targetType: reflect.TypeFor[[]uint64](), want: true},
		{name: "an integer to float conversion is allowed", value: reflect.ValueOf([]int64{1}), targetType: reflect.TypeFor[[]float64](), want: true},
		{name: "an identical type is declined", value: reflect.ValueOf([]int64{1}), targetType: reflect.TypeFor[[]int64](), want: false},
		{name: "a string element is not a bank scalar", value: reflect.ValueOf([]string{"a"}), targetType: reflect.TypeFor[[]int64](), want: false},
		{name: "a bool element is not a bank scalar", value: reflect.ValueOf([]bool{true}), targetType: reflect.TypeFor[[]int64](), want: false},
		{name: "a non-slice source is declined", value: reflect.ValueOf(1), targetType: reflect.TypeFor[[]int64](), want: false},
		{name: "a non-slice target is declined", value: reflect.ValueOf([]int64{1}), targetType: reflect.TypeFor[int64](), want: false},
		{name: "an invalid value is declined", value: reflect.Value{}, targetType: reflect.TypeFor[[]int64](), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := conformSliceWidth(tt.value, tt.targetType)

			require.Equal(t, tt.want, ok)
			if tt.want {
				require.Equal(t, tt.targetType, got.Type())
			}
		})
	}
}

func TestIsBankScalarKindAcceptsTheNumericKindsOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind reflect.Kind
		want bool
	}{
		{name: "a plain int", kind: reflect.Int, want: true},
		{name: "an int8", kind: reflect.Int8, want: true},
		{name: "an int64", kind: reflect.Int64, want: true},
		{name: "a plain uint", kind: reflect.Uint, want: true},
		{name: "a uint8", kind: reflect.Uint8, want: true},
		{name: "a uintptr", kind: reflect.Uintptr, want: true},
		{name: "a float32", kind: reflect.Float32, want: true},
		{name: "a float64", kind: reflect.Float64, want: true},
		{name: "a bool is not a bank scalar", kind: reflect.Bool, want: false},
		{name: "a string is not a bank scalar", kind: reflect.String, want: false},
		{name: "a complex value is not a bank scalar", kind: reflect.Complex128, want: false},
		{name: "a slice is not a bank scalar", kind: reflect.Slice, want: false},
		{name: "a struct is not a bank scalar", kind: reflect.Struct, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isBankScalarKind(tt.kind))
		})
	}
}

func TestArenaWrapHelpersPreserveLengthAndCapacity(t *testing.T) {
	t.Parallel()

	t.Run("a byte slice wraps with its own length", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)

		wrapped := arenaWrapByteSlice(arena, []byte{1, 2, 3})

		require.Equal(t, 3, wrapped.Len())
		require.Equal(t, reflect.Slice, wrapped.Kind())
	})

	t.Run("a typed slice wraps with the requested length and capacity", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		backing := []int64{1, 2, 3, 4}

		wrapped := arenaWrapTypedSlice(arena, sliceDataPtrInt64(backing), 2, 4, reflect.TypeFor[[]int64]())

		require.Equal(t, 2, wrapped.Len())
		require.Equal(t, 4, wrapped.Cap())
		require.Equal(t, int64(1), wrapped.Index(0).Int())
	})
}
