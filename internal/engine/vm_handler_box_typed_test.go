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

	"pipit.sh/pipit/internal/isa"
)

type boxNamedInt int

type boxNamedString string

func TestBoxTypedToGeneralProducesTheDeclaredType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		targetType reflect.Type
		seed       func(registers *Registers)
		check      func(t *testing.T, boxed reflect.Value)
		name       string
		sourceKind isa.RegisterKind
	}{
		{
			name: "a plain int", targetType: reflect.TypeFor[int](), sourceKind: isa.RegisterInt,
			seed:  func(r *Registers) { r.Ints[0] = 42 },
			check: func(t *testing.T, b reflect.Value) { require.Equal(t, int64(42), b.Int()) },
		},
		{
			name: "a narrow int", targetType: reflect.TypeFor[int8](), sourceKind: isa.RegisterInt,
			seed:  func(r *Registers) { r.Ints[0] = -5 },
			check: func(t *testing.T, b reflect.Value) { require.Equal(t, int64(-5), b.Int()) },
		},
		{
			name: "a named int keeps its own type", targetType: reflect.TypeFor[boxNamedInt](), sourceKind: isa.RegisterInt,
			seed: func(r *Registers) { r.Ints[0] = 7 },
			check: func(t *testing.T, b reflect.Value) {
				require.Equal(t, reflect.TypeFor[boxNamedInt](), b.Type())
				require.Equal(t, int64(7), b.Int())
			},
		},
		{
			name: "an unsigned value", targetType: reflect.TypeFor[uint32](), sourceKind: isa.RegisterUint,
			seed:  func(r *Registers) { r.Uints[0] = 42 },
			check: func(t *testing.T, b reflect.Value) { require.Equal(t, uint64(42), b.Uint()) },
		},
		{
			name: "a float", targetType: reflect.TypeFor[float32](), sourceKind: isa.RegisterFloat,
			seed:  func(r *Registers) { r.Floats[0] = 1.5 },
			check: func(t *testing.T, b reflect.Value) { require.InDelta(t, 1.5, b.Float(), 0.001) },
		},
		{
			name: "a string", targetType: reflect.TypeFor[string](), sourceKind: isa.RegisterString,
			seed:  func(r *Registers) { r.Strings[0] = "pipit" },
			check: func(t *testing.T, b reflect.Value) { require.Equal(t, "pipit", b.String()) },
		},
		{
			name: "a named string", targetType: reflect.TypeFor[boxNamedString](), sourceKind: isa.RegisterString,
			seed: func(r *Registers) { r.Strings[0] = "pipit" },
			check: func(t *testing.T, b reflect.Value) {
				require.Equal(t, reflect.TypeFor[boxNamedString](), b.Type())
			},
		},
		{
			name: "a boolean", targetType: reflect.TypeFor[bool](), sourceKind: isa.RegisterBool,
			seed:  func(r *Registers) { r.Bools[0] = true },
			check: func(t *testing.T, b reflect.Value) { require.True(t, b.Bool()) },
		},
		{
			name: "a complex value", targetType: reflect.TypeFor[complex128](), sourceKind: isa.RegisterComplex,
			seed:  func(r *Registers) { r.Complex[0] = 1 + 2i },
			check: func(t *testing.T, b reflect.Value) { require.Equal(t, 1+2i, b.Complex()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" with an arena", func(t *testing.T) {
			t.Parallel()
			arena := newTestArena(t)
			registers := standardRegisters()
			tt.seed(&registers)

			boxed := boxTypedToGeneral(arena, tt.targetType, &registers, 0, tt.sourceKind)

			require.Equal(t, tt.targetType, boxed.Type())
			tt.check(t, boxed)
		})

		t.Run(tt.name+" without an arena", func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			tt.seed(&registers)

			boxed := boxTypedToGeneral(nil, tt.targetType, &registers, 0, tt.sourceKind)

			require.Equal(t, tt.targetType, boxed.Type())
			tt.check(t, boxed)
		})
	}
}

func TestBoxTypedToGeneralFallsBackToTheSourceBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		seed       func(registers *Registers)
		check      func(t *testing.T, boxed reflect.Value)
		name       string
		sourceKind isa.RegisterKind
	}{
		{
			name: "an integer source", sourceKind: isa.RegisterInt,
			seed:  func(r *Registers) { r.Ints[0] = 42 },
			check: func(t *testing.T, b reflect.Value) { require.Equal(t, int64(42), b.Int()) },
		},
		{
			name: "a float source", sourceKind: isa.RegisterFloat,
			seed:  func(r *Registers) { r.Floats[0] = 1.5 },
			check: func(t *testing.T, b reflect.Value) { require.InDelta(t, 1.5, b.Float(), 0) },
		},
		{
			name: "a string source", sourceKind: isa.RegisterString,
			seed:  func(r *Registers) { r.Strings[0] = "pipit" },
			check: func(t *testing.T, b reflect.Value) { require.Equal(t, "pipit", b.String()) },
		},
		{
			name: "a boolean source", sourceKind: isa.RegisterBool,
			seed:  func(r *Registers) { r.Bools[0] = true },
			check: func(t *testing.T, b reflect.Value) { require.True(t, b.Bool()) },
		},
		{
			name: "an unsigned source", sourceKind: isa.RegisterUint,
			seed:  func(r *Registers) { r.Uints[0] = 42 },
			check: func(t *testing.T, b reflect.Value) { require.Equal(t, uint64(42), b.Uint()) },
		},
		{
			name: "a general source passes through", sourceKind: isa.RegisterGeneral,
			seed:  func(r *Registers) { r.General[0] = reflect.ValueOf([]int{1}) },
			check: func(t *testing.T, b reflect.Value) { require.Equal(t, 1, b.Len()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" boxes through its own bank when the target is a map", func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			tt.seed(&registers)

			boxed := boxTypedToGeneral(nil, reflect.TypeFor[map[string]int](), &registers, 0, tt.sourceKind)

			tt.check(t, boxed)
		})
	}
}

func TestBoxTypedSliceAsWrapsTheMatchingBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		targetType reflect.Type
		seed       func(registers *Registers)
		name       string
		sourceKind isa.RegisterKind
		wantLen    int
		wantOK     bool
	}{
		{
			name: "an integer slice", targetType: reflect.TypeFor[[]int64](), sourceKind: isa.RegisterSliceInt,
			seed: func(r *Registers) { r.SlicesInt[0] = []int64{1, 2} }, wantLen: 2, wantOK: true,
		},
		{
			name: "a float slice", targetType: reflect.TypeFor[[]float64](), sourceKind: isa.RegisterSliceFloat,
			seed: func(r *Registers) { r.slicesFloat[0] = []float64{1} }, wantLen: 1, wantOK: true,
		},
		{
			name: "a string slice", targetType: reflect.TypeFor[[]string](), sourceKind: isa.RegisterSliceString,
			seed: func(r *Registers) { r.slicesString[0] = []string{"a"} }, wantLen: 1, wantOK: true,
		},
		{
			name: "a boolean slice", targetType: reflect.TypeFor[[]bool](), sourceKind: isa.RegisterSliceBool,
			seed: func(r *Registers) { r.slicesBool[0] = []bool{true} }, wantLen: 1, wantOK: true,
		},
		{
			name: "an unsigned slice", targetType: reflect.TypeFor[[]uint64](), sourceKind: isa.RegisterSliceUint,
			seed: func(r *Registers) { r.slicesUint[0] = []uint64{1} }, wantLen: 1, wantOK: true,
		},
		{
			name: "a scalar bank is not a typed slice", targetType: reflect.TypeFor[[]int64](), sourceKind: isa.RegisterInt,
			seed: func(r *Registers) { r.Ints[0] = 1 }, wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			arena := newTestArena(t)
			registers := standardRegisters()
			tt.seed(&registers)

			boxed, ok := boxTypedSliceAs(arena, tt.targetType, &registers, 0, tt.sourceKind)

			require.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				require.Equal(t, tt.wantLen, boxed.Len())
			}
		})
	}
}

func TestBoxScalarToArenaBoxHandlesEachWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		targetType reflect.Type
		seed       func(registers *Registers)
		check      func(t *testing.T, boxed reflect.Value)
		name       string
	}{
		{
			name: "a signed byte", targetType: reflect.TypeFor[int8](),
			seed:  func(r *Registers) { r.Ints[0] = -5 },
			check: func(t *testing.T, b reflect.Value) { require.Equal(t, int64(-5), b.Int()) },
		},
		{
			name: "a signed double word", targetType: reflect.TypeFor[int64](),
			seed:  func(r *Registers) { r.Ints[0] = 42 },
			check: func(t *testing.T, b reflect.Value) { require.Equal(t, int64(42), b.Int()) },
		},
		{
			name: "an unsigned byte", targetType: reflect.TypeFor[uint8](),
			seed:  func(r *Registers) { r.Uints[0] = 200 },
			check: func(t *testing.T, b reflect.Value) { require.Equal(t, uint64(200), b.Uint()) },
		},
		{
			name: "a float", targetType: reflect.TypeFor[float64](),
			seed:  func(r *Registers) { r.Floats[0] = 1.5 },
			check: func(t *testing.T, b reflect.Value) { require.InDelta(t, 1.5, b.Float(), 0) },
		},
		{
			name: "a string", targetType: reflect.TypeFor[string](),
			seed:  func(r *Registers) { r.Strings[0] = "pipit" },
			check: func(t *testing.T, b reflect.Value) { require.Equal(t, "pipit", b.String()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			arena := newTestArena(t)
			registers := standardRegisters()
			tt.seed(&registers)

			boxed, ok := boxScalarToArenaBox(arena, tt.targetType, tt.targetType.Kind(), &registers, 0)

			require.True(t, ok, "every scalar width must have an arena box")
			require.Equal(t, tt.targetType, boxed.Type())
			tt.check(t, boxed)
		})
	}

	t.Run("a composite type has no scalar box", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		registers := standardRegisters()

		_, ok := boxScalarToArenaBox(arena, reflect.TypeFor[[]int](), reflect.Slice, &registers, 0)

		require.False(t, ok)
	})
}
