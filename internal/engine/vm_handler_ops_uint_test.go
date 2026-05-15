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

func TestUintBankBinaryOpsWriteOnlyTheDestination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		name    string
		left    uint64
		right   uint64
		want    uint64
	}{
		{name: "addition of two small values", handler: handleAddUint, left: 10, right: 32, want: 42},
		{name: "addition wraps past the unsigned ceiling", handler: handleAddUint, left: math.MaxUint64, right: 1, want: 0},
		{name: "addition of zero is the identity", handler: handleAddUint, left: 7, right: 0, want: 7},
		{name: "subtraction of two small values", handler: handleSubUint, left: 50, right: 8, want: 42},
		{name: "subtraction below zero wraps to the ceiling", handler: handleSubUint, left: 0, right: 1, want: math.MaxUint64},
		{name: "subtraction of a value from itself is zero", handler: handleSubUint, left: 99, right: 99, want: 0},
		{name: "multiplication of two small values", handler: handleMulUint, left: 6, right: 7, want: 42},
		{name: "multiplication by zero is zero", handler: handleMulUint, left: math.MaxUint64, right: 0, want: 0},
		{name: "multiplication wraps past the unsigned ceiling", handler: handleMulUint, left: math.MaxUint64, right: 2, want: math.MaxUint64 - 1},
		{name: "bitwise and keeps only the shared bits", handler: handleBitAndUint, left: 0b1100, right: 0b1010, want: 0b1000},
		{name: "bitwise and with zero clears every bit", handler: handleBitAndUint, left: math.MaxUint64, right: 0, want: 0},
		{name: "bitwise or merges both operands", handler: handleBitOrUint, left: 0b1100, right: 0b1010, want: 0b1110},
		{name: "bitwise or with the ceiling saturates", handler: handleBitOrUint, left: 0, right: math.MaxUint64, want: math.MaxUint64},
		{name: "bitwise exclusive or drops the shared bits", handler: handleBitXorUint, left: 0b1100, right: 0b1010, want: 0b0110},
		{name: "bitwise exclusive or with itself is zero", handler: handleBitXorUint, left: 0xDEAD, right: 0xDEAD, want: 0},
		{name: "bitwise and not clears the masked bits", handler: handleBitAndNotUint, left: 0b1100, right: 0b1010, want: 0b0100},
		{name: "bitwise and not with zero is the identity", handler: handleBitAndNotUint, left: 0b1111, right: 0, want: 0b1111},
		{name: "left shift by one doubles the value", handler: handleShiftLeftUint, left: 21, right: 1, want: 42},
		{name: "left shift by the full width discards every bit", handler: handleShiftLeftUint, left: math.MaxUint64, right: 64, want: 0},
		{name: "left shift beyond the width still discards every bit", handler: handleShiftLeftUint, left: 1, right: 200, want: 0},
		{name: "right shift by one halves the value", handler: handleShiftRightUint, left: 84, right: 1, want: 42},
		{name: "right shift by the full width discards every bit", handler: handleShiftRightUint, left: math.MaxUint64, right: 64, want: 0},
		{name: "right shift never sign-extends the top bit", handler: handleShiftRightUint, left: 1 << 63, right: 63, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Uints[1] = tt.left
			registers.Uints[2] = tt.right

			got := tt.handler(nil, nil, &registers, op(0, 1, 2))

			require.Equal(t, opContinue, got, "a bank-local uint op never leaves the dispatch loop")
			require.Equal(t, tt.want, registers.Uints[0])
			require.Equal(t, tt.left, registers.Uints[1], "the left operand register is read-only")
			require.Equal(t, tt.right, registers.Uints[2], "the right operand register is read-only")
		})
	}
}

func TestUintBankComparisonsWriteTheIntegerBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		name    string
		left    uint64
		right   uint64
		want    int64
	}{
		{name: "equality holds for identical values", handler: handleEqUint, left: 42, right: 42, want: 1},
		{name: "equality fails for different values", handler: handleEqUint, left: 42, right: 43, want: 0},
		{name: "equality holds at the unsigned ceiling", handler: handleEqUint, left: math.MaxUint64, right: math.MaxUint64, want: 1},
		{name: "inequality fails for identical values", handler: handleNeUint, left: 42, right: 42, want: 0},
		{name: "inequality holds for different values", handler: handleNeUint, left: 42, right: 43, want: 1},
		{name: "less than holds below", handler: handleLtUint, left: 1, right: 2, want: 1},
		{name: "less than fails when equal", handler: handleLtUint, left: 2, right: 2, want: 0},
		{name: "less than fails above", handler: handleLtUint, left: 3, right: 2, want: 0},
		{name: "less than reads the top bit as magnitude, not sign", handler: handleLtUint, left: 1, right: 1 << 63, want: 1},
		{name: "less or equal holds below", handler: handleLeUint, left: 1, right: 2, want: 1},
		{name: "less or equal holds when equal", handler: handleLeUint, left: 2, right: 2, want: 1},
		{name: "less or equal fails above", handler: handleLeUint, left: 3, right: 2, want: 0},
		{name: "greater than holds above", handler: handleGtUint, left: 3, right: 2, want: 1},
		{name: "greater than fails when equal", handler: handleGtUint, left: 2, right: 2, want: 0},
		{name: "greater than at the ceiling beats zero", handler: handleGtUint, left: math.MaxUint64, right: 0, want: 1},
		{name: "greater or equal holds above", handler: handleGeUint, left: 3, right: 2, want: 1},
		{name: "greater or equal holds when equal", handler: handleGeUint, left: 2, right: 2, want: 1},
		{name: "greater or equal fails below", handler: handleGeUint, left: 1, right: 2, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Uints[1] = tt.left
			registers.Uints[2] = tt.right
			registers.Ints[0] = -1

			got := tt.handler(nil, nil, &registers, op(0, 1, 2))

			require.Equal(t, opContinue, got, "a comparison never leaves the dispatch loop")
			require.Equal(t, tt.want, registers.Ints[0], "an unsigned comparison writes its verdict to the integer bank")
		})
	}
}

func TestUintBankUnaryOpsCopyAndComplement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		name    string
		start   uint64
		want    uint64
	}{
		{name: "move copies the source register", handler: handleMoveUint, start: 42, want: 42},
		{name: "move copies the unsigned ceiling", handler: handleMoveUint, start: math.MaxUint64, want: math.MaxUint64},
		{name: "complement of zero is the ceiling", handler: handleBitNotUint, start: 0, want: math.MaxUint64},
		{name: "complement of the ceiling is zero", handler: handleBitNotUint, start: math.MaxUint64, want: 0},
		{name: "complement flips every bit of a mixed pattern", handler: handleBitNotUint, start: 0x0F0F0F0F0F0F0F0F, want: 0xF0F0F0F0F0F0F0F0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Uints[1] = tt.start

			got := tt.handler(nil, nil, &registers, op(0, 1, 0))

			require.Equal(t, opContinue, got)
			require.Equal(t, tt.want, registers.Uints[0])
		})
	}
}

func TestUintIncrementAndDecrementWrapAtTheBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		name    string
		start   uint64
		want    uint64
	}{
		{name: "increment advances by one", handler: handleIncUint, start: 41, want: 42},
		{name: "increment at the ceiling wraps to zero", handler: handleIncUint, start: math.MaxUint64, want: 0},
		{name: "decrement retreats by one", handler: handleDecUint, start: 43, want: 42},
		{name: "decrement at zero wraps to the ceiling", handler: handleDecUint, start: 0, want: math.MaxUint64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Uints[0] = tt.start

			got := tt.handler(nil, nil, &registers, op(0, 0, 0))

			require.Equal(t, opContinue, got)
			require.Equal(t, tt.want, registers.Uints[0])
		})
	}
}

func TestUintDivisionAndRemainderRejectAZeroDivisor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		name    string
		left    uint64
		right   uint64
		want    uint64
		divides bool
	}{
		{name: "division of two small values", handler: handleDivUint, left: 84, right: 2, want: 42, divides: true},
		{name: "division truncates towards zero", handler: handleDivUint, left: 7, right: 2, want: 3, divides: true},
		{name: "division at the ceiling by one is the identity", handler: handleDivUint, left: math.MaxUint64, right: 1, want: math.MaxUint64, divides: true},
		{name: "division by zero raises an interpreted panic", handler: handleDivUint, left: 1, right: 0},
		{name: "remainder of two small values", handler: handleRemUint, left: 47, right: 5, want: 2, divides: true},
		{name: "remainder is zero when the divisor divides exactly", handler: handleRemUint, left: 40, right: 5, want: 0, divides: true},
		{name: "remainder by zero raises an interpreted panic", handler: handleRemUint, left: 1, right: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, _, registers := newStandardVM(t)
			registers.Uints[1] = tt.left
			registers.Uints[2] = tt.right

			got := tt.handler(vm, nil, registers, op(0, 1, 2))

			if !tt.divides {
				requireRuntimePanic(t, vm, got, "integer divide by zero")
				return
			}
			require.Equal(t, opContinue, got)
			require.Equal(t, tt.want, registers.Uints[0])
		})
	}
}

func TestLoadUintConstRefusesAnIndexPastThePool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		constants []uint64
		index     uint16
		want      uint64
		inPool    bool
	}{
		{name: "the first constant loads", constants: []uint64{42, 7}, index: 0, want: 42, inPool: true},
		{name: "a later constant loads", constants: []uint64{42, 7}, index: 1, want: 7, inPool: true},
		{name: "an index at the pool length is refused", constants: []uint64{42}, index: 1},
		{name: "an index far past the pool is refused", constants: []uint64{42}, index: 500},
		{name: "an empty pool refuses every index", constants: nil, index: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			builder := newBytecodeBuilder()
			builder.numRegisters = wideRegCounts(8)
			builder.uintConstants = tt.constants
			builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
			vm, frame, registers := newFramedVM(t, builder.build())

			low, high := isa.SplitWide(tt.index)
			got := handleLoadUintConst(vm, frame, registers, op(0, low, high))

			if !tt.inPool {
				requireEvalError(t, vm, got, nil)
				return
			}
			require.Equal(t, opContinue, got)
			require.Equal(t, tt.want, registers.Uints[0])
		})
	}
}
