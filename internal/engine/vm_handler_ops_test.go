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

func TestIntegerBankBinaryOpsWriteOnlyTheDestination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		name    string
		left    int64
		right   int64
		want    int64
	}{
		{name: "addition of two small values", handler: handleAddInt64, left: 10, right: 32, want: 42},
		{name: "addition of a negative operand subtracts", handler: handleAddInt64, left: 50, right: -8, want: 42},
		{name: "addition wraps past the signed ceiling", handler: handleAddInt64, left: math.MaxInt64, right: 1, want: math.MinInt64},
		{name: "subtraction of two small values", handler: handleSubtractInt64, left: 50, right: 8, want: 42},
		{name: "subtraction wraps past the signed floor", handler: handleSubtractInt64, left: math.MinInt64, right: 1, want: math.MaxInt64},
		{name: "multiplication of two small values", handler: handleMultiplyInt64, left: 6, right: 7, want: 42},
		{name: "multiplication by a negative operand flips the sign", handler: handleMultiplyInt64, left: -6, right: 7, want: -42},
		{name: "multiplication by zero is zero", handler: handleMultiplyInt64, left: math.MaxInt64, right: 0, want: 0},
		{name: "bitwise and keeps only the shared bits", handler: handleBitAndInt64, left: 0b1100, right: 0b1010, want: 0b1000},
		{name: "bitwise and with minus one is the identity", handler: handleBitAndInt64, left: 42, right: -1, want: 42},
		{name: "bitwise or merges both operands", handler: handleBitOrInt64, left: 0b1100, right: 0b1010, want: 0b1110},
		{name: "bitwise exclusive or drops the shared bits", handler: handleBitXorInt64, left: 0b1100, right: 0b1010, want: 0b0110},
		{name: "bitwise exclusive or with itself is zero", handler: handleBitXorInt64, left: -12345, right: -12345, want: 0},
		{name: "bitwise and not clears the masked bits", handler: handleBitAndNotInt64, left: 0b1100, right: 0b1010, want: 0b0100},
		{name: "left shift by one doubles the value", handler: handleShiftLeftInt64, left: 21, right: 1, want: 42},
		{name: "left shift by the full width discards every bit", handler: handleShiftLeftInt64, left: -1, right: 64, want: 0},
		{name: "right shift by one halves the value", handler: handleShiftRightInt64, left: 84, right: 1, want: 42},
		{name: "right shift sign-extends a negative value", handler: handleShiftRightInt64, left: -2, right: 1, want: -1},
		{name: "right shift of a negative value by the full width saturates", handler: handleShiftRightInt64, left: -1, right: 63, want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Ints[1] = tt.left
			registers.Ints[2] = tt.right

			got := tt.handler(nil, nil, &registers, op(0, 1, 2))

			require.Equal(t, opContinue, got, "a bank-local integer op never leaves the dispatch loop")
			require.Equal(t, tt.want, registers.Ints[0])
			require.Equal(t, tt.left, registers.Ints[1], "the left operand register is read-only")
			require.Equal(t, tt.right, registers.Ints[2], "the right operand register is read-only")
		})
	}
}

func TestIntegerBankUnaryOpsNegateAndComplement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		name    string
		start   int64
		want    int64
	}{
		{name: "negation flips a positive value", handler: handleNegateInt64Body, start: 42, want: -42},
		{name: "negation flips a negative value", handler: handleNegateInt64Body, start: -42, want: 42},
		{name: "negation of zero stays zero", handler: handleNegateInt64Body, start: 0, want: 0},
		{name: "negation at the signed floor wraps to itself", handler: handleNegateInt64Body, start: math.MinInt64, want: math.MinInt64},
		{name: "complement of zero is minus one", handler: handleBitNotInt64Body, start: 0, want: -1},
		{name: "complement of minus one is zero", handler: handleBitNotInt64Body, start: -1, want: 0},
		{name: "complement of a positive value is its negative successor", handler: handleBitNotInt64Body, start: 41, want: -42},
		{name: "move copies the source register", handler: handleMoveInt64Body, start: 42, want: 42},
		{name: "move copies the signed floor", handler: handleMoveInt64Body, start: math.MinInt64, want: math.MinInt64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Ints[1] = tt.start

			got := tt.handler(nil, nil, &registers, op(0, 1, 0))

			require.Equal(t, opContinue, got)
			require.Equal(t, tt.want, registers.Ints[0])
		})
	}
}

func TestIntegerComparisonsWriteOneOrZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		name    string
		left    int64
		right   int64
		want    int64
	}{
		{name: "equality holds for identical values", handler: handleEqualInt64, left: 42, right: 42, want: 1},
		{name: "equality fails for different values", handler: handleEqualInt64, left: 42, right: -42, want: 0},
		{name: "inequality holds for different values", handler: handleNotEqualInt64, left: 42, right: -42, want: 1},
		{name: "inequality fails for identical values", handler: handleNotEqualInt64, left: 42, right: 42, want: 0},
		{name: "less than holds below", handler: handleLessThanInt64, left: -1, right: 0, want: 1},
		{name: "less than fails when equal", handler: handleLessThanInt64, left: 0, right: 0, want: 0},
		{name: "less than orders the signed floor first", handler: handleLessThanInt64, left: math.MinInt64, right: math.MaxInt64, want: 1},
		{name: "less or equal holds when equal", handler: handleLessOrEqualInt64, left: 7, right: 7, want: 1},
		{name: "less or equal fails above", handler: handleLessOrEqualInt64, left: 8, right: 7, want: 0},
		{name: "greater than holds above", handler: handleGreaterThanInt64, left: 8, right: 7, want: 1},
		{name: "greater than fails when equal", handler: handleGreaterThanInt64, left: 7, right: 7, want: 0},
		{name: "greater or equal holds when equal", handler: handleGreaterOrEqualInt64, left: 7, right: 7, want: 1},
		{name: "greater or equal fails below", handler: handleGreaterOrEqualInt64, left: 6, right: 7, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Ints[1] = tt.left
			registers.Ints[2] = tt.right
			registers.Ints[0] = -1

			got := tt.handler(nil, nil, &registers, op(0, 1, 2))

			require.Equal(t, opContinue, got)
			require.Equal(t, tt.want, registers.Ints[0], "a comparison writes its verdict to the integer bank")
		})
	}
}

func TestFloatBankOpsFollowIEEESemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		name    string
		left    float64
		right   float64
		want    float64
		wantNaN bool
	}{
		{name: "addition of two finite values", handler: handleAddFloat64, left: 1.5, right: 2.25, want: 3.75},
		{name: "addition of positive and negative infinity is not a number", handler: handleAddFloat64, left: math.Inf(1), right: math.Inf(-1), wantNaN: true},
		{name: "subtraction of two finite values", handler: handleSubtractFloat64, left: 3.75, right: 2.25, want: 1.5},
		{name: "multiplication of two finite values", handler: handleMultiplyFloat64, left: 1.5, right: 4, want: 6},
		{name: "multiplication of zero by infinity is not a number", handler: handleMultiplyFloat64, left: 0, right: math.Inf(1), wantNaN: true},
		{name: "division of two finite values", handler: handleDivideFloat64, left: 9, right: 2, want: 4.5},
		{name: "division by zero yields positive infinity", handler: handleDivideFloat64, left: 1, right: 0, want: math.Inf(1)},
		{name: "division of a negative value by zero yields negative infinity", handler: handleDivideFloat64, left: -1, right: 0, want: math.Inf(-1)},
		{name: "division of zero by zero is not a number", handler: handleDivideFloat64, left: 0, right: 0, wantNaN: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Floats[1] = tt.left
			registers.Floats[2] = tt.right

			got := tt.handler(nil, nil, &registers, op(0, 1, 2))

			require.Equal(t, opContinue, got)
			if tt.wantNaN {
				require.True(t, math.IsNaN(registers.Floats[0]), "the float bank must carry the IEEE not-a-number result")
				return
			}
			require.InDelta(t, tt.want, registers.Floats[0], 0)
		})
	}
}

func TestFloatNegationAndMoveCarrySignedZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler  opcodeHandler
		name     string
		start    float64
		want     float64
		wantSign bool
	}{
		{name: "negation flips a positive value", handler: handleNegateFloat64Body, start: 2.5, want: -2.5},
		{name: "negation flips a negative value", handler: handleNegateFloat64Body, start: -2.5, want: 2.5},
		{name: "negation of zero produces negative zero", handler: handleNegateFloat64Body, start: 0, want: 0, wantSign: true},
		{name: "move copies a finite value", handler: handleMoveFloat64Body, start: 2.5, want: 2.5},
		{name: "move copies positive infinity", handler: handleMoveFloat64Body, start: math.Inf(1), want: math.Inf(1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Floats[1] = tt.start

			got := tt.handler(nil, nil, &registers, op(0, 1, 0))

			require.Equal(t, opContinue, got)
			require.InDelta(t, tt.want, registers.Floats[0], 0)
			if tt.wantSign {
				require.True(t, math.Signbit(registers.Floats[0]), "negating zero must set the sign bit")
			}
		})
	}
}

func TestFloatComparisonsTreatNotANumberAsUnordered(t *testing.T) {
	t.Parallel()

	nan := math.NaN()

	tests := []struct {
		handler opcodeHandler
		name    string
		left    float64
		right   float64
		want    int64
	}{
		{name: "equality holds for identical values", handler: handleEqualFloat64, left: 1.5, right: 1.5, want: 1},
		{name: "equality fails against not a number", handler: handleEqualFloat64, left: nan, right: nan, want: 0},
		{name: "less than holds below", handler: handleLessThanFloat64, left: 1, right: 2, want: 1},
		{name: "less than fails against not a number", handler: handleLessThanFloat64, left: 1, right: nan, want: 0},
		{name: "less than orders negative infinity first", handler: handleLessThanFloat64, left: math.Inf(-1), right: 0, want: 1},
		{name: "less or equal holds when equal", handler: handleLessOrEqualFloat64, left: 2, right: 2, want: 1},
		{name: "less or equal fails against not a number", handler: handleLessOrEqualFloat64, left: nan, right: 2, want: 0},
		{name: "not equal holds for different values", handler: handleNotEqualFloat64, left: 1, right: 2, want: 1},
		{name: "not equal holds against not a number", handler: handleNotEqualFloat64, left: nan, right: nan, want: 1},
		{name: "greater than holds above", handler: handleGreaterThanFloat64, left: 2, right: 1, want: 1},
		{name: "greater or equal holds when equal", handler: handleGreaterOrEqualFloat64, left: 2, right: 2, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Floats[1] = tt.left
			registers.Floats[2] = tt.right
			registers.Ints[0] = -1

			got := tt.handler(nil, nil, &registers, op(0, 1, 2))

			require.Equal(t, opContinue, got)
			require.Equal(t, tt.want, registers.Ints[0])
		})
	}
}

func TestStringBankOpsCompareLexicographically(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		name    string
		left    string
		right   string
		want    int64
	}{
		{name: "equality holds for identical strings", handler: handleEqualString, left: "pipit", right: "pipit", want: 1},
		{name: "equality fails on a case difference", handler: handleEqualString, left: "pipit", right: "Pipit", want: 0},
		{name: "equality holds for two empty strings", handler: handleEqualString, left: "", right: "", want: 1},
		{name: "inequality holds for different strings", handler: handleNotEqualString, left: "a", right: "b", want: 1},
		{name: "less than orders alphabetically", handler: handleLessThanString, left: "abc", right: "abd", want: 1},
		{name: "less than orders a prefix first", handler: handleLessThanString, left: "ab", right: "abc", want: 1},
		{name: "less than fails when equal", handler: handleLessThanString, left: "ab", right: "ab", want: 0},
		{name: "less or equal holds when equal", handler: handleLessOrEqualString, left: "ab", right: "ab", want: 1},
		{name: "greater than orders the longer string later", handler: handleGreaterThanString, left: "abc", right: "ab", want: 1},
		{name: "greater or equal holds when equal", handler: handleGreaterOrEqualString, left: "ab", right: "ab", want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Strings[1] = tt.left
			registers.Strings[2] = tt.right
			registers.Ints[0] = -1

			got := tt.handler(nil, nil, &registers, op(0, 1, 2))

			require.Equal(t, opContinue, got)
			require.Equal(t, tt.want, registers.Ints[0])
		})
	}
}

func TestStringLengthCountsBytesNotRunes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  int64
	}{
		{name: "an empty string has no bytes", value: "", want: 0},
		{name: "an ascii string counts one byte per character", value: "pipit", want: 5},
		{name: "a multi-byte rune counts its encoded bytes", value: "é", want: 2},
		{name: "an emoji counts four bytes", value: "🐦", want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Strings[1] = tt.value

			got := handleLengthStringBody(nil, nil, &registers, op(0, 1, 0))

			require.Equal(t, opContinue, got)
			require.Equal(t, tt.want, registers.Ints[0], "string length is a byte count")
		})
	}
}

func TestMoveStringAndTypedSliceBanksAliasTheSource(t *testing.T) {
	t.Parallel()

	t.Run("a string move copies the value", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.Strings[1] = "pipit"
		require.Equal(t, opContinue, handleMoveStringBody(nil, nil, &registers, op(0, 1, 0)))
		require.Equal(t, "pipit", registers.Strings[0])
	})

	t.Run("an integer slice move shares the backing array", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.SlicesInt[1] = []int64{1, 2, 3}
		require.Equal(t, opContinue, handleMoveSliceInt64Body(nil, nil, &registers, op(0, 1, 0)))
		registers.SlicesInt[0][0] = 99
		require.Equal(t, int64(99), registers.SlicesInt[1][0], "a slice move copies the header, not the elements")
	})

	t.Run("a float slice move shares the backing array", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.slicesFloat[1] = []float64{1.5}
		require.Equal(t, opContinue, handleMoveSliceFloat64Body(nil, nil, &registers, op(0, 1, 0)))
		require.InDelta(t, 1.5, registers.slicesFloat[0][0], 0)
	})

	t.Run("a string slice move shares the backing array", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.slicesString[1] = []string{"a"}
		require.Equal(t, opContinue, handleMoveSliceStringBody(nil, nil, &registers, op(0, 1, 0)))
		require.Equal(t, []string{"a"}, registers.slicesString[0])
	})

	t.Run("a bool slice move shares the backing array", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.slicesBool[1] = []bool{true}
		require.Equal(t, opContinue, handleMoveSliceBoolBody(nil, nil, &registers, op(0, 1, 0)))
		require.Equal(t, []bool{true}, registers.slicesBool[0])
	})

	t.Run("a uint slice move shares the backing array", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.slicesUint[1] = []uint64{7}
		require.Equal(t, opContinue, handleMoveSliceUint64Body(nil, nil, &registers, op(0, 1, 0)))
		require.Equal(t, []uint64{7}, registers.slicesUint[0])
	})

	t.Run("a byte slice move shares the backing array", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.slicesByte[1] = []byte("hi")
		require.Equal(t, opContinue, handleMoveSliceByteBody(nil, nil, &registers, op(0, 1, 0)))
		require.Equal(t, []byte("hi"), registers.slicesByte[0])
	})
}

func TestLoadZeroClearsTheBankNamedByTheOperand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		seed  func(registers *Registers)
		check func(t *testing.T, registers *Registers)
		name  string
		kind  isa.RegisterKind
	}{
		{name: "the integer bank", kind: isa.RegisterInt64,
			seed: func(registers *Registers) { registers.Ints[0] = 42 },
			check: func(t *testing.T, registers *Registers) {
				t.Helper()
				require.Equal(t, int64(0), registers.Ints[0])
			}},
		{name: "the float bank", kind: isa.RegisterFloat64,
			seed: func(registers *Registers) { registers.Floats[0] = 42 },
			check: func(t *testing.T, registers *Registers) {
				t.Helper()
				require.InDelta(t, 0, registers.Floats[0], 0)
			}},
		{name: "the string bank", kind: isa.RegisterString,
			seed: func(registers *Registers) { registers.Strings[0] = "pipit" },
			check: func(t *testing.T, registers *Registers) {
				t.Helper()
				require.Empty(t, registers.Strings[0])
			}},
		{name: "the general bank", kind: isa.RegisterGeneral,
			seed: func(registers *Registers) { registers.General[0] = reflect.ValueOf(42) },
			check: func(t *testing.T, registers *Registers) {
				t.Helper()
				require.False(t, registers.General[0].IsValid())
			}},
		{name: "the bool bank", kind: isa.RegisterBool,
			seed: func(registers *Registers) { registers.Bools[0] = true },
			check: func(t *testing.T, registers *Registers) {
				t.Helper()
				require.False(t, registers.Bools[0])
			}},
		{name: "the uint bank", kind: isa.RegisterUint64,
			seed: func(registers *Registers) { registers.Uints[0] = 42 },
			check: func(t *testing.T, registers *Registers) {
				t.Helper()
				require.Equal(t, uint64(0), registers.Uints[0])
			}},
		{name: "the complex bank", kind: isa.RegisterComplex128,
			seed: func(registers *Registers) { registers.Complex[0] = 1 + 2i },
			check: func(t *testing.T, registers *Registers) {
				t.Helper()
				require.Equal(t, complex128(0), registers.Complex[0])
			}},
		{name: "the integer slice bank", kind: isa.RegisterSliceInt64,
			seed: func(registers *Registers) { registers.SlicesInt[0] = []int64{1} },
			check: func(t *testing.T, registers *Registers) {
				t.Helper()
				require.Nil(t, registers.SlicesInt[0])
			}},
		{name: "the float slice bank", kind: isa.RegisterSliceFloat64,
			seed: func(registers *Registers) { registers.slicesFloat[0] = []float64{1} },
			check: func(t *testing.T, registers *Registers) {
				t.Helper()
				require.Nil(t, registers.slicesFloat[0])
			}},
		{name: "the string slice bank", kind: isa.RegisterSliceString,
			seed: func(registers *Registers) { registers.slicesString[0] = []string{"a"} },
			check: func(t *testing.T, registers *Registers) {
				t.Helper()
				require.Nil(t, registers.slicesString[0])
			}},
		{name: "the bool slice bank", kind: isa.RegisterSliceBool,
			seed: func(registers *Registers) { registers.slicesBool[0] = []bool{true} },
			check: func(t *testing.T, registers *Registers) {
				t.Helper()
				require.Nil(t, registers.slicesBool[0])
			}},
		{name: "the uint slice bank", kind: isa.RegisterSliceUint64,
			seed: func(registers *Registers) { registers.slicesUint[0] = []uint64{1} },
			check: func(t *testing.T, registers *Registers) {
				t.Helper()
				require.Nil(t, registers.slicesUint[0])
			}},
		{name: "the byte slice bank", kind: isa.RegisterSliceByte,
			seed: func(registers *Registers) { registers.slicesByte[0] = []byte("a") },
			check: func(t *testing.T, registers *Registers) {
				t.Helper()
				require.Nil(t, registers.slicesByte[0])
			}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			tt.seed(&registers)

			got := handleLoadZeroBody(nil, nil, &registers, op(0, uint8(tt.kind), 0))

			require.Equal(t, opContinue, got)
			tt.check(t, &registers)
		})
	}
}

func TestLoadNilInvalidatesTheGeneralRegister(t *testing.T) {
	t.Parallel()

	registers := standardRegisters()
	registers.General[0] = reflect.ValueOf("occupied")

	got := handleLoadNilBody(nil, nil, &registers, op(0, 0, 0))

	require.Equal(t, opContinue, got)
	require.False(t, registers.General[0].IsValid(), "nil is the invalid reflect value, not a typed zero")
}

func TestExtensionWordHandlerIsInert(t *testing.T) {
	t.Parallel()

	registers := standardRegisters()
	registers.Ints[0] = 42

	got := handleExtension(nil, nil, &registers, op(1, 2, 3))

	require.Equal(t, opContinue, got, "an extension word is consumed by its owning handler, never executed")
	require.Equal(t, int64(42), registers.Ints[0], "handling an extension word must touch no register")
}

func TestInterfaceNilComparisonsSeparateInvalidFromTypedNil(t *testing.T) {
	t.Parallel()

	var nilError error
	typedNil := reflect.ValueOf(&nilError).Elem()

	tests := []struct {
		build   func() reflect.Value
		name    string
		wantEq  int64
		wantNeq int64
	}{
		{name: "an invalid value is nil", build: func() reflect.Value { return reflect.Value{} }, wantEq: 1, wantNeq: 0},
		{name: "a nil interface is nil", build: func() reflect.Value { return typedNil }, wantEq: 1, wantNeq: 0},
		{name: "a boxed integer is not nil", build: func() reflect.Value { return reflect.ValueOf(42) }, wantEq: 0, wantNeq: 1},
		{name: "a boxed string is not nil", build: func() reflect.Value { return reflect.ValueOf("x") }, wantEq: 0, wantNeq: 1},
		{name: "a typed nil pointer is not a nil interface", build: func() reflect.Value { return reflect.ValueOf((*int)(nil)) }, wantEq: 0, wantNeq: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.General[2] = tt.build()

			require.Equal(t, opContinue, handleEqualInterfaceNil(nil, nil, &registers, op(0, 1, 2)))
			require.Equal(t, tt.wantEq, registers.Ints[1], "the equality verdict lands in operand B")

			require.Equal(t, opContinue, handleNotEqualInterfaceNil(nil, nil, &registers, op(0, 1, 2)))
			require.Equal(t, tt.wantNeq, registers.Ints[1], "the inequality verdict lands in operand B")
		})
	}
}

func TestIsNilableAndNilRecognisesOnlyTheNilableKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value reflect.Value
		name  string
		want  bool
	}{
		{name: "a nil pointer is nilable and nil", value: reflect.ValueOf((*int)(nil)), want: true},
		{name: "a nil slice is nilable and nil", value: reflect.ValueOf([]int(nil)), want: true},
		{name: "a nil map is nilable and nil", value: reflect.ValueOf(map[string]int(nil)), want: true},
		{name: "a nil channel is nilable and nil", value: reflect.ValueOf((chan int)(nil)), want: true},
		{name: "a nil function is nilable and nil", value: reflect.ValueOf((func())(nil)), want: true},
		{name: "a non-nil pointer is nilable but not nil", value: reflect.ValueOf(new(int)), want: false},
		{name: "an empty slice is nilable but not nil", value: reflect.ValueOf([]int{}), want: false},
		{name: "an integer is not nilable", value: reflect.ValueOf(0), want: false},
		{name: "a string is not nilable", value: reflect.ValueOf(""), want: false},
		{name: "a struct is not nilable", value: reflect.ValueOf(struct{}{}), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isNilableAndNil(tt.value))
		})
	}
}

func TestLogicalNotInvertsTruthiness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		start int64
		want  int64
	}{
		{name: "not of zero is one", start: 0, want: 1},
		{name: "not of one is zero", start: 1, want: 0},
		{name: "not of a large value is zero", start: 12345, want: 0},
		{name: "not of a negative value is zero", start: -1, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Ints[1] = tt.start

			got := handleNotInt64Body(nil, nil, &registers, op(0, 1, 0))

			require.Equal(t, opContinue, got)
			require.Equal(t, tt.want, registers.Ints[0])
		})
	}
}

func TestBoxingHelpersRoundTripThroughTheArena(t *testing.T) {
	t.Parallel()

	t.Run("a string boxes to the same contents with and without an arena", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		require.Equal(t, "pipit", BoxStringToGeneral(arena, "pipit").String())
		require.Equal(t, "pipit", BoxStringToGeneral(nil, "pipit").String())
	})

	t.Run("integers box to the same value across the small-value cache boundary", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		for _, value := range []int64{-1000, -1, 0, 1, 255, 1 << 40, math.MaxInt64, math.MinInt64} {
			require.Equal(t, value, boxInt64ToGeneral(arena, value).Int(), "arena boxing must preserve %d", value)
			require.Equal(t, value, boxInt64ToGeneral(nil, value).Int(), "heap boxing must preserve %d", value)
		}
	})

	t.Run("unsigned integers box to the same value", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		for _, value := range []uint64{0, 1, 255, 1 << 40, math.MaxUint64} {
			require.Equal(t, value, boxUint64ToGeneral(arena, value).Uint(), "arena boxing must preserve %d", value)
			require.Equal(t, value, boxUint64ToGeneral(nil, value).Uint(), "heap boxing must preserve %d", value)
		}
	})

	t.Run("floats box to the same value", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		require.InDelta(t, 2.5, boxFloat64ToGeneral(arena, 2.5).Float(), 0)
		require.InDelta(t, 2.5, boxFloat64ToGeneral(nil, 2.5).Float(), 0)
	})

	t.Run("complex values box to the same value", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		require.Equal(t, 1+2i, boxComplex128ToGeneral(arena, 1+2i).Complex())
		require.Equal(t, 1+2i, boxComplex128ToGeneral(nil, 1+2i).Complex())
	})

	t.Run("booleans box from a shared cache", func(t *testing.T) {
		t.Parallel()
		require.True(t, boxBoolToGeneral(true).Bool())
		require.False(t, boxBoolToGeneral(false).Bool())
	})
}
