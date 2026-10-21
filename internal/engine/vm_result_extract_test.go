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

	"pipit.sh/pipit/internal/isa"
)

type extractTestNamedInt int32

func TestAScalarResultIsBoxedAsItsExactType(t *testing.T) {
	t.Parallel()
	registers := standardRegisters()
	registers.Ints[0] = 7
	registers.Uints[0] = 8
	registers.Floats[0] = 1.5
	registers.Bools[0] = true
	registers.Strings[0] = "text"
	registers.Complex[0] = 2i

	tests := []struct {
		hint reflect.Type

		want any

		kind isa.RegisterKind
	}{
		{hint: reflect.TypeFor[int](), kind: isa.RegisterInt64, want: 7},
		{hint: reflect.TypeFor[int8](), kind: isa.RegisterInt64, want: int8(7)},
		{hint: reflect.TypeFor[int64](), kind: isa.RegisterInt64, want: int64(7)},
		{hint: reflect.TypeFor[uint16](), kind: isa.RegisterUint64, want: uint16(8)},
		{hint: reflect.TypeFor[uintptr](), kind: isa.RegisterUint64, want: uintptr(8)},
		{hint: reflect.TypeFor[float32](), kind: isa.RegisterFloat64, want: float32(1.5)},
		{hint: reflect.TypeFor[float64](), kind: isa.RegisterFloat64, want: 1.5},
		{hint: reflect.TypeFor[bool](), kind: isa.RegisterBool, want: true},
		{hint: reflect.TypeFor[bool](), kind: isa.RegisterInt64, want: true},
		{hint: reflect.TypeFor[string](), kind: isa.RegisterString, want: "text"},
		{hint: reflect.TypeFor[complex64](), kind: isa.RegisterComplex128, want: complex64(2i)},
		{hint: reflect.TypeFor[extractTestNamedInt](), kind: isa.RegisterInt64, want: extractTestNamedInt(7)},
	}
	for _, tt := range tests {
		got, ok := extractScalarRegisterValueAs(nil, &registers, tt.kind, 0, tt.hint)
		require.True(t, ok, "%s", tt.hint)
		require.Equal(t, tt.want, got, "%s", tt.hint)
	}

	_, ok := extractScalarRegisterValueAs(nil, &registers, isa.RegisterFloat64, 0, reflect.TypeFor[int]())
	require.False(t, ok, "a bank the type cannot be read from")
	_, ok = extractScalarRegisterValueAs(nil, &registers, isa.RegisterInt64, len(registers.Ints), reflect.TypeFor[int]())
	require.False(t, ok, "a slot past the bank")
}

func TestAScalarResultIsBoxedOnce(t *testing.T) {
	if raceDetectorOn {
		t.Skip("the race detector allocates")
	}
	registers := standardRegisters()
	registers.Ints[0] = 1 << 40
	allocations := testing.AllocsPerRun(100, func() {
		if _, ok := extractScalarRegisterValueAs(nil, &registers, isa.RegisterInt64, 0, reflect.TypeFor[int64]()); !ok {
			t.Fatal("not extracted")
		}
	})
	require.LessOrEqual(t, allocations, 1.0, "an unnamed scalar is boxed with a plain conversion")
	named := testing.AllocsPerRun(100, func() {
		if _, ok := extractScalarRegisterValueAs(nil, &registers, isa.RegisterInt64, 0, reflect.TypeFor[extractTestNamedInt]()); !ok {
			t.Fatal("not extracted")
		}
	})
	require.LessOrEqual(t, named, 1.0, "a named scalar is boxed once")
}

func TestNarrowIntegerResultsTruncateAsGoConversionsDo(t *testing.T) {
	registers := standardRegisters()
	signed := []int64{300, -300, 1<<40 + 5, -(1 << 40) - 5, 70000, -70000, 1<<33 + 7, -1, -129, 128, math.MaxInt64, math.MinInt64}
	for _, value := range signed {
		registers.Ints[0] = value
		tests := []struct {
			hint reflect.Type

			want any
		}{
			{hint: reflect.TypeFor[int8](), want: int8(value)},
			{hint: reflect.TypeFor[int16](), want: int16(value)},
			{hint: reflect.TypeFor[int32](), want: int32(value)},
			{hint: reflect.TypeFor[extractTestNamedInt](), want: extractTestNamedInt(value)},
		}
		for _, tt := range tests {
			got, ok := extractScalarRegisterValueAs(nil, &registers, isa.RegisterInt64, 0, tt.hint)
			require.True(t, ok, "%s from %d", tt.hint, value)
			require.Equal(t, tt.want, got, "%s from %d", tt.hint, value)
		}
	}
	unsigned := []uint64{300, 70000, 1<<33 + 7, math.MaxUint64, 1 << 63, math.MaxUint32 + 1, 1<<64 - 5}
	for _, value := range unsigned {
		registers.Uints[0] = value
		tests := []struct {
			hint reflect.Type

			want any
		}{
			{hint: reflect.TypeFor[uint8](), want: uint8(value)},
			{hint: reflect.TypeFor[uint16](), want: uint16(value)},
			{hint: reflect.TypeFor[uint32](), want: uint32(value)},
		}
		for _, tt := range tests {
			got, ok := extractScalarRegisterValueAs(nil, &registers, isa.RegisterUint64, 0, tt.hint)
			require.True(t, ok, "%s from %d", tt.hint, value)
			require.Equal(t, tt.want, got, "%s from %d", tt.hint, value)
		}
	}
}
