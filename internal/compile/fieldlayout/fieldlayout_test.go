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

package fieldlayout

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

type layoutInner struct {
	A int64
	B string
}

type layoutOuter struct {
	Inner layoutInner
	C     uint32
}

type layoutCycleBroken struct {
	Next  *layoutOuter `pipit_cycle_broken:"1"`
	Value int
}

func TestStructFieldLayoutSupportsKindAdmitsTheAddressableKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind reflect.Kind
		want bool
	}{
		{name: "a plain int", kind: reflect.Int, want: true},
		{name: "an int8", kind: reflect.Int8, want: true},
		{name: "a uint64", kind: reflect.Uint64, want: true},
		{name: "a uintptr", kind: reflect.Uintptr, want: true},
		{name: "a float32", kind: reflect.Float32, want: true},
		{name: "a bool", kind: reflect.Bool, want: true},
		{name: "a string", kind: reflect.String, want: true},
		{name: "a pointer", kind: reflect.Pointer, want: true},
		{name: "an interface", kind: reflect.Interface, want: true},
		{name: "a slice", kind: reflect.Slice, want: true},
		{name: "an array", kind: reflect.Array, want: true},
		{name: "a map", kind: reflect.Map, want: true},
		{name: "a channel", kind: reflect.Chan, want: true},
		{name: "a function", kind: reflect.Func, want: true},
		{name: "a struct is not a leaf", kind: reflect.Struct, want: false},
		{name: "a complex value is unsupported", kind: reflect.Complex128, want: false},
		{name: "an unsafe pointer is unsupported", kind: reflect.UnsafePointer, want: false},
		{name: "the invalid kind is unsupported", kind: reflect.Invalid, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, StructFieldLayoutSupportsKind(tt.kind))
		})
	}
}

func TestLeafRegisterKindAdmittedGuardsTheGeneralBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		bank     isa.RegisterKind
		leafKind reflect.Kind
		want     bool
	}{
		{name: "a scalar bank admits any leaf", bank: isa.RegisterInt, leafKind: reflect.Struct, want: true},
		{name: "the float bank admits any leaf", bank: isa.RegisterFloat, leafKind: reflect.Complex128, want: true},
		{name: "the general bank admits a pointer", bank: isa.RegisterGeneral, leafKind: reflect.Pointer, want: true},
		{name: "the general bank admits an interface", bank: isa.RegisterGeneral, leafKind: reflect.Interface, want: true},
		{name: "the general bank admits a slice", bank: isa.RegisterGeneral, leafKind: reflect.Slice, want: true},
		{name: "the general bank admits an array", bank: isa.RegisterGeneral, leafKind: reflect.Array, want: true},
		{name: "the general bank admits a map", bank: isa.RegisterGeneral, leafKind: reflect.Map, want: true},
		{name: "the general bank admits a channel", bank: isa.RegisterGeneral, leafKind: reflect.Chan, want: true},
		{name: "the general bank admits a function", bank: isa.RegisterGeneral, leafKind: reflect.Func, want: true},
		{name: "the general bank refuses a struct leaf", bank: isa.RegisterGeneral, leafKind: reflect.Struct, want: false},
		{name: "the general bank refuses an integer leaf", bank: isa.RegisterGeneral, leafKind: reflect.Int, want: false},
		{name: "the general bank refuses a string leaf", bank: isa.RegisterGeneral, leafKind: reflect.String, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, LeafRegisterKindAdmitted(tt.bank, tt.leafKind))
		})
	}
}

func TestStructFieldLayoutIndexFitsTier0BoundsTheOperandByte(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		index uint16
		want  bool
	}{
		{name: "the first index fits", index: 0, want: true},
		{name: "the last index that fits", index: 255, want: true},
		{name: "the first index past the operand byte", index: 256, want: false},
		{name: "a far larger index does not fit", index: 5000, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, StructFieldLayoutIndexFitsTier0(tt.index))
		})
	}
}

func TestEncodeFieldPathProducesAStableCommaSeparatedKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path []int
		want string
	}{
		{name: "an empty path", path: nil, want: ""},
		{name: "a single index", path: []int{0}, want: "0,"},
		{name: "several indices", path: []int{1, 2, 3}, want: "1,2,3,"},
		{name: "a multi-digit index", path: []int{12, 345}, want: "12,345,"},
		{name: "a negative index", path: []int{-1}, want: "-1,"},
		{name: "a mix of signs", path: []int{-12, 0, 7}, want: "-12,0,7,"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, EncodeFieldPath(tt.path))
		})
	}

	t.Run("distinct paths encode distinctly", func(t *testing.T) {
		t.Parallel()
		require.NotEqual(t, EncodeFieldPath([]int{1, 23}), EncodeFieldPath([]int{12, 3}),
			"the separator must keep neighbouring indices apart")
	})
}

func TestWalkStructFieldPathResolvesNestedFields(t *testing.T) {
	t.Parallel()

	outerType := reflect.TypeFor[layoutOuter]()

	t.Run("a direct field resolves to its own type and offset", func(t *testing.T) {
		t.Parallel()
		leafType, offset, path, ok := WalkStructFieldPath(outerType, []int{1})

		require.True(t, ok)
		require.Equal(t, reflect.TypeFor[uint32](), leafType)
		require.Equal(t, uint32(outerType.Field(1).Offset), offset)
		require.Equal(t, uint8(1), path[0])
	})

	t.Run("a nested field accumulates the offsets of its path", func(t *testing.T) {
		t.Parallel()
		leafType, offset, _, ok := WalkStructFieldPath(outerType, []int{0, 1})

		require.True(t, ok)
		require.Equal(t, reflect.TypeFor[string](), leafType)
		innerOffset := outerType.Field(0).Offset + reflect.TypeFor[layoutInner]().Field(1).Offset
		require.Equal(t, uint32(innerOffset), offset)
	})

	t.Run("an empty path resolves to the struct itself", func(t *testing.T) {
		t.Parallel()
		leafType, offset, _, ok := WalkStructFieldPath(outerType, nil)

		require.True(t, ok)
		require.Equal(t, outerType, leafType)
		require.Equal(t, uint32(0), offset)
	})

	t.Run("a field index past the struct is refused", func(t *testing.T) {
		t.Parallel()
		_, _, _, ok := WalkStructFieldPath(outerType, []int{9})

		require.False(t, ok)
	})

	t.Run("a path deeper than the encoding allows is refused", func(t *testing.T) {
		t.Parallel()
		_, _, _, ok := WalkStructFieldPath(outerType, []int{0, 0, 0, 0, 0, 0})

		require.False(t, ok, "the path array holds a fixed number of entries")
	})

	t.Run("a non-struct root is refused", func(t *testing.T) {
		t.Parallel()
		_, _, _, ok := WalkStructFieldPath(reflect.TypeFor[int](), []int{0})

		require.False(t, ok)
	})
}

func TestLeafFieldIsCycleBrokenSpotsASelfReference(t *testing.T) {
	t.Parallel()

	t.Run("a plain field is not cycle broken", func(t *testing.T) {
		t.Parallel()
		require.False(t, LeafFieldIsCycleBroken(reflect.TypeFor[layoutOuter](), []int{1}))
	})

	t.Run("a field tagged as cycle broken is reported", func(t *testing.T) {
		t.Parallel()
		taggedType := reflect.TypeFor[layoutCycleBroken]()

		require.True(t, LeafFieldIsCycleBroken(taggedType, []int{0}),
			"the predicate reads the tag the type mapper stamps, not the shape of the type")
		require.False(t, LeafFieldIsCycleBroken(taggedType, []int{1}),
			"an untagged neighbour is unaffected")
	})

	t.Run("an empty path is never cycle broken", func(t *testing.T) {
		t.Parallel()
		require.False(t, LeafFieldIsCycleBroken(reflect.TypeFor[layoutOuter](), nil))
	})

	t.Run("an index past the struct is not cycle broken", func(t *testing.T) {
		t.Parallel()
		require.False(t, LeafFieldIsCycleBroken(reflect.TypeFor[layoutOuter](), []int{9}))
	})
}
