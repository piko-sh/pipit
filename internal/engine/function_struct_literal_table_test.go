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

type literalPlainScalars struct {
	A int64
	B float64
}

type literalWithPointer struct {
	A int64
	B *int64
}

type literalWithString struct {
	A string
}

type literalWithSlice struct {
	A []int
}

type literalNested struct {
	Inner literalPlainScalars
	C     uint32
}

type literalOverAligned struct {
	V complex128
}

func TestStructLiteralQualifiesForArenaRequiresAPointerFreeStruct(t *testing.T) {
	t.Parallel()

	tests := []struct {
		literalType reflect.Type
		name        string
		want        bool
	}{
		{name: "a struct of plain scalars qualifies", literalType: reflect.TypeFor[literalPlainScalars](), want: true},
		{name: "a nested pointer-free struct qualifies", literalType: reflect.TypeFor[literalNested](), want: true},
		{name: "a struct holding a pointer is refused", literalType: reflect.TypeFor[literalWithPointer](), want: false},
		{name: "a struct holding a string is refused", literalType: reflect.TypeFor[literalWithString](), want: false},
		{name: "a struct holding a slice is refused", literalType: reflect.TypeFor[literalWithSlice](), want: false},
		{name: "a zero-sized struct is refused", literalType: reflect.TypeFor[struct{}](), want: false},
		{name: "a nil type is refused", literalType: nil, want: false},
		{name: "a non-struct type is refused", literalType: reflect.TypeFor[int64](), want: false},
		{name: "a slice type is refused", literalType: reflect.TypeFor[[]int](), want: false},
		{name: "a map type is refused", literalType: reflect.TypeFor[map[string]int](), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, StructLiteralQualifiesForArena(tt.literalType))
		})
	}

	t.Run("a struct whose alignment the byte slab cannot guarantee is refused", func(t *testing.T) {
		t.Parallel()
		literalType := reflect.TypeFor[literalOverAligned]()
		if uintptr(literalType.Align()) <= arenaMaxAlignment {
			require.True(t, StructLiteralQualifiesForArena(literalType),
				"this platform aligns the type within the slab guarantee, so it qualifies")
			return
		}
		require.False(t, StructLiteralQualifiesForArena(literalType))
	})
}

func TestStructLiteralAllocationSubOpRoutesQualifyingTypesToTheArena(t *testing.T) {
	t.Parallel()

	tests := []struct {
		literalType reflect.Type
		name        string
		want        isa.SubOpcodeTier2
	}{
		{name: "a qualifying struct allocates from the arena", literalType: reflect.TypeFor[literalPlainScalars](), want: isa.SubOpTier2AllocStructLiteral},
		{name: "a pointer-bearing struct falls back to the map path", literalType: reflect.TypeFor[literalWithPointer](), want: isa.SubOpTier2MakeMap},
		{name: "a nil type falls back to the map path", literalType: nil, want: isa.SubOpTier2MakeMap},
		{name: "a non-struct falls back to the map path", literalType: reflect.TypeFor[int](), want: isa.SubOpTier2MakeMap},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, StructLiteralAllocationSubOp(tt.literalType))
		})
	}
}

func TestEnsureStructLiteralTableLeavesNonQualifyingSlotsZero(t *testing.T) {
	t.Parallel()

	t.Run("qualifying slots are filled and others left zero", func(t *testing.T) {
		t.Parallel()
		compiledFunction := program.NewNamedFunction("literals")
		compiledFunction.TypeTable = []reflect.Type{
			reflect.TypeFor[literalPlainScalars](),
			reflect.TypeFor[literalWithPointer](),
			reflect.TypeFor[literalNested](),
		}

		EnsureStructLiteralTable(compiledFunction)

		require.Len(t, compiledFunction.StructLiteralTable, 3)
		require.NotZero(t, compiledFunction.StructLiteralTable[0].Size, "a qualifying type carries its size")
		require.Zero(t, compiledFunction.StructLiteralTable[1].Size, "a refused type leaves a zero entry")
		require.NotZero(t, compiledFunction.StructLiteralTable[2].Size)
		require.Equal(t, int64(3), compiledFunction.StructLiteralTableLength,
			"a non-empty table publishes its length in both build lanes")
	})

	t.Run("the alignment mask is one less than the alignment", func(t *testing.T) {
		t.Parallel()
		compiledFunction := program.NewNamedFunction("literals")
		compiledFunction.TypeTable = []reflect.Type{reflect.TypeFor[literalPlainScalars]()}

		EnsureStructLiteralTable(compiledFunction)

		literalType := reflect.TypeFor[literalPlainScalars]()
		require.Equal(t, uintptr(literalType.Align())-1, compiledFunction.StructLiteralTable[0].AlignMask)
		require.Equal(t, literalType.Size(), compiledFunction.StructLiteralTable[0].Size)
	})

	t.Run("an empty type table publishes no base", func(t *testing.T) {
		t.Parallel()
		compiledFunction := program.NewNamedFunction("literals")

		EnsureStructLiteralTable(compiledFunction)

		require.Empty(t, compiledFunction.StructLiteralTable)
		require.Zero(t, compiledFunction.StructLiteralTableLength)
		require.Zero(t, compiledFunction.StructLiteralTableBase,
			"an empty table must not publish a base pointer the assembly would read")
	})

	t.Run("a table of only refused types still publishes its length", func(t *testing.T) {
		t.Parallel()
		compiledFunction := program.NewNamedFunction("literals")
		compiledFunction.TypeTable = []reflect.Type{reflect.TypeFor[literalWithPointer]()}

		EnsureStructLiteralTable(compiledFunction)

		require.Len(t, compiledFunction.StructLiteralTable, 1)
		require.Zero(t, compiledFunction.StructLiteralTable[0].Size)
	})

	t.Run("the table is built only once per function", func(t *testing.T) {
		t.Parallel()
		compiledFunction := program.NewNamedFunction("literals")
		compiledFunction.TypeTable = []reflect.Type{reflect.TypeFor[literalPlainScalars]()}

		EnsureStructLiteralTable(compiledFunction)
		first := compiledFunction.StructLiteralTable

		compiledFunction.TypeTable = append(compiledFunction.TypeTable, reflect.TypeFor[literalNested]())
		EnsureStructLiteralTable(compiledFunction)

		require.Len(t, compiledFunction.StructLiteralTable, 1, "the once-guard must keep the first table")
		require.Equal(t, &first[0], &compiledFunction.StructLiteralTable[0])
	})
}
