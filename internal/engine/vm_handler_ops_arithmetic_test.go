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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestNumericConversionsPreserveOrReinterpretTheBits(t *testing.T) {
	t.Parallel()

	t.Run("an integer widens to a float exactly below the mantissa limit", func(t *testing.T) {
		t.Parallel()
		for _, value := range []int64{0, 1, -1, 1 << 52, -(1 << 52)} {
			registers := standardRegisters()
			registers.Ints[1] = value
			require.Equal(t, opContinue, handleIntToFloat(nil, nil, &registers, op(0, 1, 0)))
			require.InDelta(t, float64(value), registers.Floats[0], 0)
		}
	})

	t.Run("a float truncates towards zero when narrowed to an integer", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name  string
			start float64
			want  int64
		}{
			{name: "a positive fraction truncates down", start: 2.9, want: 2},
			{name: "a negative fraction truncates up", start: -2.9, want: -2},
			{name: "an exact value is unchanged", start: 42, want: 42},
			{name: "zero is unchanged", start: 0, want: 0},
		}
		for _, tt := range tests {
			registers := standardRegisters()
			registers.Floats[1] = tt.start
			require.Equal(t, opContinue, handleFloatToInt(nil, nil, &registers, op(0, 1, 0)))
			require.Equal(t, tt.want, registers.Ints[0], tt.name)
		}
	})

	t.Run("an unsigned value widens to a float", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.Uints[1] = 1 << 40
		require.Equal(t, opContinue, handleUintToFloat(nil, nil, &registers, op(0, 1, 0)))
		require.InDelta(t, float64(uint64(1)<<40), registers.Floats[0], 0)
	})

	t.Run("a float narrows to an unsigned value", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.Floats[1] = 42.9
		require.Equal(t, opContinue, handleFloatToUint(nil, nil, &registers, op(0, 1, 0)))
		require.Equal(t, uint64(42), registers.Uints[0])
	})

	t.Run("signed and unsigned conversions reinterpret rather than clamp", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.Ints[1] = -1
		require.Equal(t, opContinue, handleIntToUint(nil, nil, &registers, op(0, 1, 0)))
		require.Equal(t, uint64(math.MaxUint64), registers.Uints[0], "minus one reinterprets as the unsigned ceiling")

		registers.Uints[1] = math.MaxUint64
		require.Equal(t, opContinue, handleUintToInt(nil, nil, &registers, op(0, 1, 0)))
		require.Equal(t, int64(-1), registers.Ints[0], "the unsigned ceiling reinterprets as minus one")
	})

	t.Run("a boolean converts to one or zero and back", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name    string
			start   bool
			wantInt int64
		}{
			{name: "true is one", start: true, wantInt: 1},
			{name: "false is zero", start: false, wantInt: 0},
		}
		for _, tt := range tests {
			registers := standardRegisters()
			registers.Bools[1] = tt.start
			require.Equal(t, opContinue, handleBoolToInt(nil, nil, &registers, op(0, 1, 0)))
			require.Equal(t, tt.wantInt, registers.Ints[0], tt.name)
		}
	})

	t.Run("any non-zero integer converts to true", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name  string
			start int64
			want  bool
		}{
			{name: "zero is false", start: 0, want: false},
			{name: "one is true", start: 1, want: true},
			{name: "a negative value is true", start: -1, want: true},
			{name: "a large value is true", start: math.MaxInt64, want: true},
		}
		for _, tt := range tests {
			registers := standardRegisters()
			registers.Ints[1] = tt.start
			require.Equal(t, opContinue, handleIntToBool(nil, nil, &registers, op(0, 1, 0)))
			require.Equal(t, tt.want, registers.Bools[0], tt.name)
		}
	})

	t.Run("a boolean move copies the source register", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.Bools[1] = true
		require.Equal(t, opContinue, handleMoveBool(nil, nil, &registers, op(0, 1, 0)))
		require.True(t, registers.Bools[0])
	})
}

func TestNarrowRoundingDiscardsPrecisionBeyondTheTargetWidth(t *testing.T) {
	t.Parallel()

	t.Run("rounding to single precision loses the extra mantissa bits", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.Floats[1] = 0.1
		require.Equal(t, opContinue, handleRoundFloat32(nil, nil, &registers, op(0, 1, 0)))
		require.InDelta(t, float64(float32(0.1)), registers.Floats[0], 0)
		require.NotEqual(t, 0.1, registers.Floats[0], "single-precision rounding must actually lose precision")
	})

	t.Run("rounding a complex value narrows both components", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.Complex[1] = complex(0.1, 0.2)
		require.Equal(t, opContinue, handleRoundComplex64(nil, nil, &registers, op(0, 1, 0)))
		require.Equal(t, complex128(complex64(complex(0.1, 0.2))), registers.Complex[0])
	})

	t.Run("infinity and not a number survive single-precision rounding", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.Floats[1] = math.Inf(1)
		require.Equal(t, opContinue, handleRoundFloat32(nil, nil, &registers, op(0, 1, 0)))
		require.True(t, math.IsInf(registers.Floats[0], 1))

		registers.Floats[1] = math.NaN()
		require.Equal(t, opContinue, handleRoundFloat32(nil, nil, &registers, op(0, 1, 0)))
		require.True(t, math.IsNaN(registers.Floats[0]))
	})
}

func TestTruncateNarrowMasksThenSignExtendsTheSignedBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		bank  isa.RegisterKind
		width uint8
		start int64
		want  int64
	}{
		{name: "an unsigned byte keeps the low eight bits", bank: isa.RegisterUint, width: 8, start: 0x1FF, want: 0xFF},
		{name: "an unsigned half-word keeps the low sixteen bits", bank: isa.RegisterUint, width: 16, start: 0x1FFFF, want: 0xFFFF},
		{name: "an unsigned word keeps the low thirty-two bits", bank: isa.RegisterUint, width: 32, start: 0x1FFFFFFFF, want: 0xFFFFFFFF},
		{name: "a signed byte sign-extends a negative result", bank: isa.RegisterInt, width: 8, start: 0xFF, want: -1},
		{name: "a signed byte keeps a positive result positive", bank: isa.RegisterInt, width: 8, start: 0x7F, want: 127},
		{name: "a signed byte wraps at the width boundary", bank: isa.RegisterInt, width: 8, start: 128, want: -128},
		{name: "a signed half-word sign-extends a negative result", bank: isa.RegisterInt, width: 16, start: 0xFFFF, want: -1},
		{name: "a signed word sign-extends a negative result", bank: isa.RegisterInt, width: 32, start: 0xFFFFFFFF, want: -1},
		{name: "a signed word keeps a positive result positive", bank: isa.RegisterInt, width: 32, start: 42, want: 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			if tt.bank == isa.RegisterUint {
				registers.Uints[0] = uint64(tt.start)
			} else {
				registers.Ints[0] = tt.start
			}

			got := HandleTruncateNarrow(nil, nil, &registers, op(0, tt.width, uint8(tt.bank)))

			require.Equal(t, opContinue, got)
			if tt.bank == isa.RegisterUint {
				require.Equal(t, uint64(tt.want), registers.Uints[0])
				return
			}
			require.Equal(t, tt.want, registers.Ints[0])
		})
	}
}

func TestComplexBankArithmeticFollowsGoSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		name    string
		left    complex128
		right   complex128
		want    complex128
	}{
		{name: "addition sums both components", handler: handleAddComplex, left: 1 + 2i, right: 3 + 4i, want: 4 + 6i},
		{name: "subtraction differences both components", handler: handleSubComplex, left: 4 + 6i, right: 3 + 4i, want: 1 + 2i},
		{name: "multiplication follows the cross-term rule", handler: handleMulComplex, left: 1 + 2i, right: 3 + 4i, want: -5 + 10i},
		{name: "multiplication by the real unit is the identity", handler: handleMulComplex, left: 1 + 2i, right: 1, want: 1 + 2i},
		{name: "division inverts multiplication", handler: handleDivComplex, left: -5 + 10i, right: 3 + 4i, want: 1 + 2i},
		{name: "division by the real unit is the identity", handler: handleDivComplex, left: 1 + 2i, right: 1, want: 1 + 2i},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Complex[1] = tt.left
			registers.Complex[2] = tt.right

			got := tt.handler(nil, nil, &registers, op(0, 1, 2))

			require.Equal(t, opContinue, got)
			require.InDelta(t, real(tt.want), real(registers.Complex[0]), 1e-9)
			require.InDelta(t, imag(tt.want), imag(registers.Complex[0]), 1e-9)
		})
	}
}

func TestComplexComparisonsAndConstructionUseBothComponents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		left   complex128
		right  complex128
		wantEq int64
	}{
		{name: "identical values are equal", left: 1 + 2i, right: 1 + 2i, wantEq: 1},
		{name: "a different real part is unequal", left: 1 + 2i, right: 9 + 2i, wantEq: 0},
		{name: "a different imaginary part is unequal", left: 1 + 2i, right: 1 + 9i, wantEq: 0},
		{name: "two zero values are equal", left: 0, right: 0, wantEq: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Complex[1] = tt.left
			registers.Complex[2] = tt.right

			require.Equal(t, opContinue, handleEqComplex(nil, nil, &registers, op(0, 1, 2)))
			require.Equal(t, tt.wantEq, registers.Ints[0])

			require.Equal(t, opContinue, handleNeComplex(nil, nil, &registers, op(0, 1, 2)))
			require.Equal(t, 1-tt.wantEq, registers.Ints[0], "inequality is the complement of equality")
		})
	}

	t.Run("construction pairs two float registers", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.Floats[1] = 1.5
		registers.Floats[2] = -2.5

		require.Equal(t, opContinue, handleBuildComplex(nil, nil, &registers, op(0, 1, 2)))
		require.Equal(t, complex(1.5, -2.5), registers.Complex[0])
	})
}

func TestIntegerIncrementAndDecrementWrapAtTheSignedBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		name    string
		start   int64
		want    int64
	}{
		{name: "increment advances by one", handler: handleIncInt, start: 41, want: 42},
		{name: "increment at the signed ceiling wraps to the floor", handler: handleIncInt, start: math.MaxInt64, want: math.MinInt64},
		{name: "decrement retreats by one", handler: handleDecInt, start: 43, want: 42},
		{name: "decrement at the signed floor wraps to the ceiling", handler: handleDecInt, start: math.MinInt64, want: math.MaxInt64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Ints[0] = tt.start

			got := tt.handler(nil, nil, &registers, op(0, 0, 0))

			require.Equal(t, opContinue, got)
			require.Equal(t, tt.want, registers.Ints[0])
		})
	}
}
