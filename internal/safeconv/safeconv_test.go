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

package safeconv_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/safeconv"
)

type convCase[In, Out comparable] struct {
	name  string
	input In
	want  Out
}

func runConvCases[In, Out comparable](t *testing.T, fn func(In) Out, cases []convCase[In, Out]) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, fn(tc.input))
		})
	}
}

func TestIntToUint64(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.IntToUint64, []convCase[int, uint64]{
		{name: "zero", input: 0, want: 0},
		{name: "positive", input: 42, want: 42},
		{name: "negative", input: -1, want: 0},
		{name: "max", input: math.MaxInt, want: uint64(math.MaxInt)},
		{name: "min", input: math.MinInt, want: 0},
	})
}

func TestIntToUint32(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.IntToUint32, []convCase[int, uint32]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxUint32, want: math.MaxUint32},
		{name: "overflow clamps", input: math.MaxInt, want: math.MaxUint32},
		{name: "negative", input: -5, want: 0},
		{name: "min", input: math.MinInt, want: 0},
	})
}

func TestIntToUint16(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.IntToUint16, []convCase[int, uint16]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxUint16, want: math.MaxUint16},
		{name: "one past max clamps", input: math.MaxUint16 + 1, want: math.MaxUint16},
		{name: "overflow clamps", input: math.MaxInt, want: math.MaxUint16},
		{name: "negative", input: -1, want: 0},
	})
}

func TestIntToUint8(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.IntToUint8, []convCase[int, uint8]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxUint8, want: math.MaxUint8},
		{name: "one past max clamps", input: math.MaxUint8 + 1, want: math.MaxUint8},
		{name: "overflow clamps", input: math.MaxInt, want: math.MaxUint8},
		{name: "negative", input: -1, want: 0},
	})
}

func TestIntToInt32(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.IntToInt32, []convCase[int, int32]{
		{name: "zero", input: 0, want: 0},
		{name: "negative fits", input: -7, want: -7},
		{name: "max fits", input: math.MaxInt32, want: math.MaxInt32},
		{name: "min fits", input: math.MinInt32, want: math.MinInt32},
		{name: "overflow clamps", input: math.MaxInt, want: math.MaxInt32},
		{name: "underflow clamps", input: math.MinInt, want: math.MinInt32},
	})
}

func TestIntToInt16(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.IntToInt16, []convCase[int, int16]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxInt16, want: math.MaxInt16},
		{name: "min fits", input: math.MinInt16, want: math.MinInt16},
		{name: "one past max clamps", input: math.MaxInt16 + 1, want: math.MaxInt16},
		{name: "one below min clamps", input: math.MinInt16 - 1, want: math.MinInt16},
		{name: "overflow clamps", input: math.MaxInt, want: math.MaxInt16},
		{name: "underflow clamps", input: math.MinInt, want: math.MinInt16},
	})
}

func TestInt64ToInt16(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Int64ToInt16, []convCase[int64, int16]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxInt16, want: math.MaxInt16},
		{name: "min fits", input: math.MinInt16, want: math.MinInt16},
		{name: "overflow clamps", input: math.MaxInt64, want: math.MaxInt16},
		{name: "underflow clamps", input: math.MinInt64, want: math.MinInt16},
	})
}

func TestIntToInt8(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.IntToInt8, []convCase[int, int8]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxInt8, want: math.MaxInt8},
		{name: "min fits", input: math.MinInt8, want: math.MinInt8},
		{name: "one past max clamps", input: math.MaxInt8 + 1, want: math.MaxInt8},
		{name: "one below min clamps", input: math.MinInt8 - 1, want: math.MinInt8},
	})
}

func TestInt64ToUint32(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Int64ToUint32, []convCase[int64, uint32]{
		{name: "zero", input: 0, want: 0},
		{name: "negative", input: -1, want: 0},
		{name: "min", input: math.MinInt64, want: 0},
		{name: "max fits", input: math.MaxUint32, want: math.MaxUint32},
		{name: "one past max clamps", input: math.MaxUint32 + 1, want: math.MaxUint32},
		{name: "overflow clamps", input: math.MaxInt64, want: math.MaxUint32},
	})
}

func TestInt64ToInt32(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Int64ToInt32, []convCase[int64, int32]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxInt32, want: math.MaxInt32},
		{name: "min fits", input: math.MinInt32, want: math.MinInt32},
		{name: "overflow clamps", input: math.MaxInt64, want: math.MaxInt32},
		{name: "underflow clamps", input: math.MinInt64, want: math.MinInt32},
	})
}

func TestInt64ToUint16(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Int64ToUint16, []convCase[int64, uint16]{
		{name: "zero", input: 0, want: 0},
		{name: "negative", input: -1, want: 0},
		{name: "max fits", input: math.MaxUint16, want: math.MaxUint16},
		{name: "one past max clamps", input: math.MaxUint16 + 1, want: math.MaxUint16},
		{name: "overflow clamps", input: math.MaxInt64, want: math.MaxUint16},
	})
}

func TestUint64ToUint32(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Uint64ToUint32, []convCase[uint64, uint32]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxUint32, want: math.MaxUint32},
		{name: "one past max clamps", input: math.MaxUint32 + 1, want: math.MaxUint32},
		{name: "overflow clamps", input: math.MaxUint64, want: math.MaxUint32},
	})
}

func TestUint64ToUint16(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Uint64ToUint16, []convCase[uint64, uint16]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxUint16, want: math.MaxUint16},
		{name: "one past max clamps", input: math.MaxUint16 + 1, want: math.MaxUint16},
		{name: "overflow clamps", input: math.MaxUint64, want: math.MaxUint16},
	})
}

func TestUint64ToUint8(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Uint64ToUint8, []convCase[uint64, uint8]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxUint8, want: math.MaxUint8},
		{name: "one past max clamps", input: math.MaxUint8 + 1, want: math.MaxUint8},
		{name: "overflow clamps", input: math.MaxUint64, want: math.MaxUint8},
	})
}

func TestUint64ToInt64(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Uint64ToInt64, []convCase[uint64, int64]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxInt64, want: math.MaxInt64},
		{name: "one past max clamps", input: math.MaxInt64 + 1, want: math.MaxInt64},
		{name: "overflow clamps", input: math.MaxUint64, want: math.MaxInt64},
	})
}

func TestInt64ToUint64(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Int64ToUint64, []convCase[int64, uint64]{
		{name: "zero", input: 0, want: 0},
		{name: "positive", input: 9, want: 9},
		{name: "negative", input: -1, want: 0},
		{name: "min", input: math.MinInt64, want: 0},
		{name: "max", input: math.MaxInt64, want: math.MaxInt64},
	})
}

func TestInt64ToInt(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Int64ToInt, []convCase[int64, int]{
		{name: "zero", input: 0, want: 0},
		{name: "negative", input: -3, want: -3},
		{name: "max", input: math.MaxInt64, want: math.MaxInt},
		{name: "min", input: math.MinInt64, want: math.MinInt},
	})
}

func TestUint64ToInt(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Uint64ToInt, []convCase[uint64, int]{
		{name: "zero", input: 0, want: 0},
		{name: "positive", input: 42, want: 42},
		{name: "max fits", input: uint64(math.MaxInt), want: math.MaxInt},
		{name: "overflow clamps", input: math.MaxUint64, want: math.MaxInt},
	})
}

func TestIntToUintptr(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.IntToUintptr, []convCase[int, uintptr]{
		{name: "zero", input: 0, want: 0},
		{name: "positive", input: 42, want: 42},
		{name: "negative", input: -1, want: 0},
		{name: "min", input: math.MinInt, want: 0},
		{name: "max", input: math.MaxInt, want: uintptr(math.MaxInt)},
	})
}

func TestMustConversionsAcceptInRangeValues(t *testing.T) {
	t.Parallel()
	require.Equal(t, uint8(0), safeconv.MustIntToUint8(0))
	require.Equal(t, uint8(math.MaxUint8), safeconv.MustIntToUint8(math.MaxUint8))
	require.Equal(t, uint16(0), safeconv.MustIntToUint16(0))
	require.Equal(t, uint16(math.MaxUint16), safeconv.MustIntToUint16(math.MaxUint16))
	require.Equal(t, int16(math.MinInt16), safeconv.MustIntToInt16(math.MinInt16))
	require.Equal(t, int16(math.MaxInt16), safeconv.MustIntToInt16(math.MaxInt16))
	require.Equal(t, uint8(math.MaxUint8), safeconv.MustUintToUint8(math.MaxUint8))
	require.Equal(t, int8(math.MaxInt8), safeconv.MustUint8ToInt8(math.MaxInt8))
	require.Equal(t, uint8(0), safeconv.MustInt8ToUint8(0))
	require.Equal(t, uint8(math.MaxInt8), safeconv.MustInt8ToUint8(math.MaxInt8))
}

func TestMustConversionsPanicOutOfRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		call      func()
		wantPanic string
	}{
		{name: "MustIntToUint8 negative", call: func() { safeconv.MustIntToUint8(-1) }, wantPanic: "safeconv: int value -1 overflows uint8"},
		{name: "MustIntToUint8 overflow", call: func() { safeconv.MustIntToUint8(math.MaxUint8 + 1) }, wantPanic: "safeconv: int value 256 overflows uint8"},
		{name: "MustIntToUint16 negative", call: func() { safeconv.MustIntToUint16(-1) }, wantPanic: "safeconv: int value -1 overflows uint16"},
		{name: "MustIntToUint16 overflow", call: func() { safeconv.MustIntToUint16(math.MaxUint16 + 1) }, wantPanic: "safeconv: int value 65536 overflows uint16"},
		{name: "MustIntToInt16 underflow", call: func() { safeconv.MustIntToInt16(math.MinInt16 - 1) }, wantPanic: "safeconv: int value -32769 overflows int16"},
		{name: "MustIntToInt16 overflow", call: func() { safeconv.MustIntToInt16(math.MaxInt16 + 1) }, wantPanic: "safeconv: int value 32768 overflows int16"},
		{name: "MustUintToUint8 overflow", call: func() { safeconv.MustUintToUint8(math.MaxUint8 + 1) }, wantPanic: "safeconv: uint value 256 overflows uint8"},
		{name: "MustUint8ToInt8 overflow", call: func() { safeconv.MustUint8ToInt8(math.MaxInt8 + 1) }, wantPanic: "safeconv: uint8 value 128 overflows int8"},
		{name: "MustInt8ToUint8 negative", call: func() { safeconv.MustInt8ToUint8(-1) }, wantPanic: "safeconv: int8 value -1 overflows uint8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.PanicsWithValue(t, tc.wantPanic, tc.call)
		})
	}
}

type namedInt int

func TestToUint64AcrossIntegerTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  uint64
		want uint64
	}{
		{name: "int negative", got: safeconv.ToUint64(int(-1)), want: 0},
		{name: "int max", got: safeconv.ToUint64(int(math.MaxInt)), want: uint64(math.MaxInt)},
		{name: "int8 min", got: safeconv.ToUint64(int8(math.MinInt8)), want: 0},
		{name: "int8 max", got: safeconv.ToUint64(int8(math.MaxInt8)), want: math.MaxInt8},
		{name: "int16 min", got: safeconv.ToUint64(int16(math.MinInt16)), want: 0},
		{name: "int16 max", got: safeconv.ToUint64(int16(math.MaxInt16)), want: math.MaxInt16},
		{name: "int32 min", got: safeconv.ToUint64(int32(math.MinInt32)), want: 0},
		{name: "int32 max", got: safeconv.ToUint64(int32(math.MaxInt32)), want: math.MaxInt32},
		{name: "int64 min", got: safeconv.ToUint64(int64(math.MinInt64)), want: 0},
		{name: "int64 max", got: safeconv.ToUint64(int64(math.MaxInt64)), want: math.MaxInt64},
		{name: "uint", got: safeconv.ToUint64(uint(7)), want: 7},
		{name: "uint8 max", got: safeconv.ToUint64(uint8(math.MaxUint8)), want: math.MaxUint8},
		{name: "uint16 max", got: safeconv.ToUint64(uint16(math.MaxUint16)), want: math.MaxUint16},
		{name: "uint32 max", got: safeconv.ToUint64(uint32(math.MaxUint32)), want: math.MaxUint32},
		{name: "uint64 max", got: safeconv.ToUint64(uint64(math.MaxUint64)), want: math.MaxUint64},
		{name: "uintptr", got: safeconv.ToUint64(uintptr(11)), want: 11},
		{name: "named int negative", got: safeconv.ToUint64(namedInt(-9)), want: 0},
		{name: "named int positive", got: safeconv.ToUint64(namedInt(9)), want: 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.got)
		})
	}
}

func TestUint16ToInt16(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Uint16ToInt16, []convCase[uint16, int16]{
		{name: "zero", input: 0, want: 0},
		{name: "max positive", input: math.MaxInt16, want: math.MaxInt16},
		{name: "sign bit", input: 0x8000, want: math.MinInt16},
		{name: "all ones", input: math.MaxUint16, want: -1},
	})
}

func TestInt16ToUint16(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Int16ToUint16, []convCase[int16, uint16]{
		{name: "zero", input: 0, want: 0},
		{name: "max", input: math.MaxInt16, want: math.MaxInt16},
		{name: "minus one", input: -1, want: math.MaxUint16},
		{name: "min", input: math.MinInt16, want: 0x8000},
	})
}

func TestInt32ToUint32(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Int32ToUint32, []convCase[int32, uint32]{
		{name: "zero", input: 0, want: 0},
		{name: "max", input: math.MaxInt32, want: math.MaxInt32},
		{name: "negative", input: -1, want: 0},
		{name: "min", input: math.MinInt32, want: 0},
	})
}

func TestInt32ToInt64(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Int32ToInt64, []convCase[int32, int64]{
		{name: "zero", input: 0, want: 0},
		{name: "max", input: math.MaxInt32, want: math.MaxInt32},
		{name: "min", input: math.MinInt32, want: math.MinInt32},
	})
}

func TestInt32ToInt(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Int32ToInt, []convCase[int32, int]{
		{name: "zero", input: 0, want: 0},
		{name: "max", input: math.MaxInt32, want: math.MaxInt32},
		{name: "min", input: math.MinInt32, want: math.MinInt32},
	})
}

func TestUint32ToInt64(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Uint32ToInt64, []convCase[uint32, int64]{
		{name: "zero", input: 0, want: 0},
		{name: "max", input: math.MaxUint32, want: math.MaxUint32},
	})
}

func TestInt16ToByte(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Int16ToByte, []convCase[int16, byte]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxUint8, want: math.MaxUint8},
		{name: "one past max clamps", input: math.MaxUint8 + 1, want: math.MaxUint8},
		{name: "overflow clamps", input: math.MaxInt16, want: math.MaxUint8},
		{name: "negative", input: -1, want: 0},
		{name: "min", input: math.MinInt16, want: 0},
	})
}

func TestUint32ToByte(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Uint32ToByte, []convCase[uint32, byte]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxUint8, want: math.MaxUint8},
		{name: "one past max clamps", input: math.MaxUint8 + 1, want: math.MaxUint8},
		{name: "overflow clamps", input: math.MaxUint32, want: math.MaxUint8},
	})
}

func TestRuneToUint16(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.RuneToUint16, []convCase[rune, uint16]{
		{name: "zero", input: 0, want: 0},
		{name: "ascii", input: 'A', want: 'A'},
		{name: "max fits", input: math.MaxUint16, want: math.MaxUint16},
		{name: "one past max clamps", input: math.MaxUint16 + 1, want: math.MaxUint16},
		{name: "overflow clamps", input: math.MaxInt32, want: math.MaxUint16},
		{name: "negative", input: -1, want: 0},
	})
}

func TestRuneToByte(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.RuneToByte, []convCase[rune, byte]{
		{name: "zero", input: 0, want: 0},
		{name: "latin1", input: 'é', want: 0xE9},
		{name: "max fits", input: math.MaxUint8, want: math.MaxUint8},
		{name: "one past max clamps", input: math.MaxUint8 + 1, want: math.MaxUint8},
		{name: "overflow clamps", input: math.MaxInt32, want: math.MaxUint8},
		{name: "negative", input: -1, want: 0},
	})
}

func TestUint32ToInt16(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Uint32ToInt16, []convCase[uint32, int16]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxInt16, want: math.MaxInt16},
		{name: "one past max clamps", input: math.MaxInt16 + 1, want: math.MaxInt16},
		{name: "overflow clamps", input: math.MaxUint32, want: math.MaxInt16},
	})
}

func TestInt64ToUint8(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Int64ToUint8, []convCase[int64, uint8]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxUint8, want: math.MaxUint8},
		{name: "one past max clamps", input: math.MaxUint8 + 1, want: math.MaxUint8},
		{name: "overflow clamps", input: math.MaxInt64, want: math.MaxUint8},
		{name: "negative", input: -1, want: 0},
		{name: "min", input: math.MinInt64, want: 0},
	})
}

func TestInt64ToInt8(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Int64ToInt8, []convCase[int64, int8]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxInt8, want: math.MaxInt8},
		{name: "min fits", input: math.MinInt8, want: math.MinInt8},
		{name: "one past max clamps", input: math.MaxInt8 + 1, want: math.MaxInt8},
		{name: "one below min clamps", input: math.MinInt8 - 1, want: math.MinInt8},
		{name: "overflow clamps", input: math.MaxInt64, want: math.MaxInt8},
		{name: "underflow clamps", input: math.MinInt64, want: math.MinInt8},
	})
}

func TestUint16ToUint8(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Uint16ToUint8, []convCase[uint16, uint8]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxUint8, want: math.MaxUint8},
		{name: "one past max clamps", input: math.MaxUint8 + 1, want: math.MaxUint8},
		{name: "overflow clamps", input: math.MaxUint16, want: math.MaxUint8},
	})
}

func TestUintToUint8(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.UintToUint8, []convCase[uint, uint8]{
		{name: "zero", input: 0, want: 0},
		{name: "max fits", input: math.MaxUint8, want: math.MaxUint8},
		{name: "one past max clamps", input: math.MaxUint8 + 1, want: math.MaxUint8},
		{name: "overflow clamps", input: math.MaxUint, want: math.MaxUint8},
	})
}

func TestUintptrToInt(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.UintptrToInt, []convCase[uintptr, int]{
		{name: "zero", input: 0, want: 0},
		{name: "positive", input: 42, want: 42},
		{name: "max fits", input: uintptr(math.MaxInt), want: math.MaxInt},
		{name: "overflow clamps", input: ^uintptr(0), want: math.MaxInt},
	})
}

func TestUintptrToInt64(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.UintptrToInt64, []convCase[uintptr, int64]{
		{name: "zero", input: 0, want: 0},
		{name: "positive", input: 42, want: 42},
		{name: "max int32 fits", input: uintptr(math.MaxInt32), want: math.MaxInt32},
		{name: "overflow clamps", input: ^uintptr(0), want: math.MaxInt64},
	})
}

func TestInt64ToUint64Reinterpret(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Int64ToUint64Reinterpret, []convCase[int64, uint64]{
		{name: "zero", input: 0, want: 0},
		{name: "positive", input: 5, want: 5},
		{name: "minus one", input: -1, want: math.MaxUint64},
		{name: "min", input: math.MinInt64, want: 1 << 63},
		{name: "max", input: math.MaxInt64, want: math.MaxInt64},
	})
}

func TestUint64ToInt64Reinterpret(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Uint64ToInt64Reinterpret, []convCase[uint64, int64]{
		{name: "zero", input: 0, want: 0},
		{name: "positive", input: 5, want: 5},
		{name: "all ones", input: math.MaxUint64, want: -1},
		{name: "sign bit", input: 1 << 63, want: math.MinInt64},
	})
}

func TestReinterpretRoundTrips(t *testing.T) {
	t.Parallel()
	for _, v := range []int64{0, 1, -1, math.MaxInt64, math.MinInt64} {
		require.Equal(t, v, safeconv.Uint64ToInt64Reinterpret(safeconv.Int64ToUint64Reinterpret(v)))
	}
	for _, v := range []int16{0, 1, -1, math.MaxInt16, math.MinInt16} {
		require.Equal(t, v, safeconv.Uint16ToInt16(safeconv.Int16ToUint16(v)))
	}
}

func TestUint64ToByteTruncate(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.Uint64ToByteTruncate, []convCase[uint64, byte]{
		{name: "zero", input: 0, want: 0},
		{name: "fits", input: 7, want: 7},
		{name: "max byte", input: math.MaxUint8, want: math.MaxUint8},
		{name: "wraps to zero", input: math.MaxUint8 + 1, want: 0},
		{name: "keeps low bits", input: 0x1FF, want: 0xFF},
		{name: "max uint64", input: math.MaxUint64, want: math.MaxUint8},
	})
}

func TestIntToUint32Truncate(t *testing.T) {
	t.Parallel()
	runConvCases(t, safeconv.IntToUint32Truncate, []convCase[int, uint32]{
		{name: "zero", input: 0, want: 0},
		{name: "fits", input: 5, want: 5},
		{name: "max uint32", input: math.MaxUint32, want: math.MaxUint32},
		{name: "minus one wraps", input: -1, want: math.MaxUint32},
		{name: "min int32 keeps sign bit", input: math.MinInt32, want: 1 << 31},
	})
}
