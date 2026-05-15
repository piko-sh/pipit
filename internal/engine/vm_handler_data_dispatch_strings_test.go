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

func TestStringIndexReadsOneByteAndGuardsBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		index   int64
		want    uint64
		wantMsg string
	}{
		{name: "the first byte of an ascii string", source: "abc", index: 0, want: 'a'},
		{name: "the last byte of an ascii string", source: "abc", index: 2, want: 'c'},
		{name: "a byte inside a multi-byte rune", source: "é", index: 1, want: 0xA9},
		{name: "one past the end is refused", source: "abc", index: 3, wantMsg: "index out of range [3] with length 3"},
		{name: "a negative index is refused", source: "abc", index: -1, wantMsg: "index out of range [-1] with length 3"},
		{name: "any index into an empty string is refused", source: "", index: 0, wantMsg: "index out of range [0] with length 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newStandardVM(t)
			registers.Strings[1] = tt.source
			registers.Ints[2] = tt.index

			got := handleStringIndex(vm, frame, registers, op(0, 1, 2))
			if tt.wantMsg != "" {
				requireRuntimePanic(t, vm, got, tt.wantMsg)
				return
			}
			require.Equal(t, opContinue, got, "an in-range byte read never leaves the dispatch loop")
			require.Equal(t, tt.want, registers.Uints[0], "the byte lands in the uint bank")
		})
	}
}

func TestStringIndexToIntFusesTheWidening(t *testing.T) {
	t.Parallel()

	t.Run("an in-range byte lands in the int bank", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.Strings[1] = "xyz"
		registers.Ints[2] = 1

		got := handleStringIndexToInt(vm, frame, registers, op(0, 1, 2))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64('y'), registers.Ints[0], "the fused form skips the uint bank entirely")
	})

	t.Run("an out-of-range index raises the same panic as the unfused form", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.Strings[1] = "xyz"
		registers.Ints[2] = 9

		got := handleStringIndexToInt(vm, frame, registers, op(0, 1, 2))
		requireRuntimePanic(t, vm, got, "index out of range [9] with length 3")
	})
}

func TestStringConversionHandlersCrossBanks(t *testing.T) {
	t.Parallel()

	t.Run("a string becomes a byte slice in the general bank", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.Strings[1] = "hi"

		got := handleStringToBytes(vm, frame, registers, op(0, 1, 0))
		require.Equal(t, opContinue, got)
		require.Equal(t, []byte("hi"), registers.General[0].Interface())
	})

	t.Run("an empty string becomes an empty byte slice", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)

		got := handleStringToBytes(vm, frame, registers, op(0, 1, 0))
		require.Equal(t, opContinue, got)
		require.Empty(t, registers.General[0].Interface())
	})

	tests := []struct {
		name string
		rune int64
		want string
	}{
		{name: "an ascii code point", rune: 'A', want: "A"},
		{name: "a multi-byte code point", rune: '€', want: "€"},
		{name: "a negative code point becomes the replacement rune", rune: -1, want: "�"},
		{name: "a code point past the unicode ceiling becomes the replacement rune", rune: 0x110000, want: "�"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newStandardVM(t)
			registers.Ints[1] = tt.rune

			got := handleRuneToString(vm, frame, registers, op(0, 1, 0))
			require.Equal(t, opContinue, got)
			require.Equal(t, tt.want, registers.Strings[0])
		})
	}
}

func TestSliceStringAppliesOnlyTheSuppliedBounds(t *testing.T) {
	t.Parallel()

	const (
		lowBound  = 1
		highBound = 2
	)

	tests := []struct {
		name    string
		source  string
		flags   uint8
		low     int64
		high    int64
		want    string
		wantMsg string
	}{
		{name: "no bounds copies the whole string", source: "abcdef", want: "abcdef"},
		{name: "a low bound alone runs to the end", source: "abcdef", flags: lowBound, low: 2, want: "cdef"},
		{name: "a high bound alone starts at zero", source: "abcdef", flags: highBound, high: 3, want: "abc"},
		{name: "both bounds cut from each end", source: "abcdef", flags: lowBound | highBound, low: 1, high: 4, want: "bcd"},
		{name: "an empty window is allowed", source: "abcdef", flags: lowBound | highBound, low: 3, high: 3, want: ""},
		{
			name: "a negative low bound is refused", source: "abcdef", flags: lowBound, low: -1,
			wantMsg: "slice bounds out of range [-1:]",
		},
		{
			name: "a crossed pair is refused", source: "abcdef", flags: lowBound | highBound, low: 4, high: 2,
			wantMsg: "slice bounds out of range [4:2]",
		},
		{
			name: "a high bound past the end is refused", source: "abcdef", flags: highBound, high: 9,
			wantMsg: "slice bounds out of range [:9] with length 6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newExtWordFrame(t, op(4, 5, 0))
			registers.Strings[1] = tt.source
			registers.Ints[4] = tt.low
			registers.Ints[5] = tt.high

			got := handleSliceString(vm, frame, registers, op(0, 1, tt.flags))
			require.Equal(t, 2, frame.ProgramCounter, "the bounds word must be stepped over whatever the outcome")
			if tt.wantMsg != "" {
				requireRuntimePanic(t, vm, got, tt.wantMsg)
				return
			}
			require.Equal(t, opContinue, got)
			require.Equal(t, tt.want, registers.Strings[0])
		})
	}
}

func TestIndexOutOfRangeOmitsTheLengthWhenNegative(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		index  int
		length int
		want   string
	}{
		{name: "a negative index omits the length", index: -1, length: 4, want: "index out of range [-1]"},
		{name: "an index past the end names the length", index: 7, length: 4, want: "index out of range [7] with length 4"},
		{name: "an index into an empty collection names a zero length", index: 0, length: 0, want: "index out of range [0] with length 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, _, _ := newStandardVM(t)
			got := raiseIndexOutOfRange(vm, tt.index, tt.length)
			requireRuntimePanic(t, vm, got, tt.want)
		})
	}
}

func TestReadRegisterConvertReadsTheNamedBank(t *testing.T) {
	t.Parallel()

	type namedInt int64

	tests := []struct {
		name   string
		kind   isa.RegisterKind
		load   func(*Registers)
		target any
		want   any
	}{
		{name: "the int bank", kind: isa.RegisterInt, load: func(r *Registers) { r.Ints[1] = 5 }, target: int64(0), want: int64(5)},
		{
			name: "the int bank converted to a named type", kind: isa.RegisterInt,
			load: func(r *Registers) { r.Ints[1] = 5 }, target: namedInt(0), want: namedInt(5),
		},
		{name: "the float bank", kind: isa.RegisterFloat, load: func(r *Registers) { r.Floats[1] = 1.5 }, target: float64(0), want: 1.5},
		{name: "the string bank", kind: isa.RegisterString, load: func(r *Registers) { r.Strings[1] = "s" }, target: "", want: "s"},
		{name: "the bool bank", kind: isa.RegisterBool, load: func(r *Registers) { r.Bools[1] = true }, target: false, want: true},
		{name: "the uint bank", kind: isa.RegisterUint, load: func(r *Registers) { r.Uints[1] = 9 }, target: uint64(0), want: uint64(9)},
		{
			name: "the complex bank", kind: isa.RegisterComplex,
			load: func(r *Registers) { r.Complex[1] = 2i }, target: complex128(0), want: 2i,
		},
		{
			name: "the int-slice bank", kind: isa.RegisterSliceInt,
			load: func(r *Registers) { r.SlicesInt[1] = []int64{1, 2} }, target: []int64(nil), want: []int64{1, 2},
		},
		{
			name: "the general bank passes an exact type through", kind: isa.RegisterGeneral,
			load: func(r *Registers) { r.General[1] = reflect.ValueOf("g") }, target: "", want: "g",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := standardRegisters()
			tt.load(&registers)

			got := readRegisterConvert(&registers, 1, tt.kind, reflect.TypeOf(tt.target))
			require.Equal(t, tt.want, got.Interface())
		})
	}
}

func TestReadRegisterConvertLeavesUnconvertibleValuesAlone(t *testing.T) {
	t.Parallel()

	t.Run("an invalid general register stays invalid", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		got := readRegisterConvert(&registers, 1, isa.RegisterGeneral, reflect.TypeFor[string]())
		require.False(t, got.IsValid(), "there is nothing to convert")
	})

	t.Run("a general value of an unrelated type is handed back unconverted", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		registers.General[1] = reflect.ValueOf("text")

		got := readRegisterConvert(&registers, 1, isa.RegisterGeneral, reflect.TypeFor[[]int]())
		require.Equal(t, "text", got.Interface(), "an impossible conversion must not be forced")
	})
}
