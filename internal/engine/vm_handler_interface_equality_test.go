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

type equalityPoint struct {
	X int
	Y string
}

type equalityWithSlice struct {
	Values []int
}

func TestReflectInterfaceEqualComparesDynamicTypesBeforeValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		left             reflect.Value
		right            reflect.Value
		name             string
		wantEqual        bool
		wantUncomparable bool
	}{
		{name: "two invalid values are equal", left: reflect.Value{}, right: reflect.Value{}, wantEqual: true},
		{name: "identical integers are equal", left: reflect.ValueOf(42), right: reflect.ValueOf(42), wantEqual: true},
		{name: "different integers are unequal", left: reflect.ValueOf(42), right: reflect.ValueOf(43)},
		{name: "an int and an int64 with the same bits are unequal", left: reflect.ValueOf(42), right: reflect.ValueOf(int64(42))},
		{name: "identical strings are equal", left: reflect.ValueOf("pipit"), right: reflect.ValueOf("pipit"), wantEqual: true},
		{name: "a string and an integer are unequal", left: reflect.ValueOf("42"), right: reflect.ValueOf(42)},
		{name: "identical booleans are equal", left: reflect.ValueOf(true), right: reflect.ValueOf(true), wantEqual: true},
		{name: "identical floats are equal", left: reflect.ValueOf(1.5), right: reflect.ValueOf(1.5), wantEqual: true},
		{name: "an invalid value equals a nil slice under lenient nil", left: reflect.Value{}, right: reflect.ValueOf([]int(nil)), wantEqual: true},
		{name: "an invalid value is unequal to an empty slice", left: reflect.Value{}, right: reflect.ValueOf([]int{})},
		{name: "an invalid value is unequal to a boxed integer", left: reflect.Value{}, right: reflect.ValueOf(0)},
		{name: "equal structs compare field by field", left: reflect.ValueOf(equalityPoint{X: 1, Y: "a"}), right: reflect.ValueOf(equalityPoint{X: 1, Y: "a"}), wantEqual: true},
		{name: "structs differing in one field are unequal", left: reflect.ValueOf(equalityPoint{X: 1, Y: "a"}), right: reflect.ValueOf(equalityPoint{X: 1, Y: "b"})},
		{name: "equal arrays compare element by element", left: reflect.ValueOf([2]int{1, 2}), right: reflect.ValueOf([2]int{1, 2}), wantEqual: true},
		{name: "arrays differing in one element are unequal", left: reflect.ValueOf([2]int{1, 2}), right: reflect.ValueOf([2]int{1, 3})},
		{name: "two slices are uncomparable", left: reflect.ValueOf([]int{1}), right: reflect.ValueOf([]int{1}), wantUncomparable: true},
		{name: "two maps are uncomparable", left: reflect.ValueOf(map[string]int{}), right: reflect.ValueOf(map[string]int{}), wantUncomparable: true},
		{name: "a struct containing a slice is uncomparable", left: reflect.ValueOf(equalityWithSlice{Values: []int{1}}), right: reflect.ValueOf(equalityWithSlice{Values: []int{1}}), wantUncomparable: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			equal, uncomparable := ReflectInterfaceEqual(tt.left, tt.right)

			require.Equal(t, tt.wantUncomparable, uncomparable,
				"an uncomparable operand must be reported, never deep-compared")
			if tt.wantUncomparable {
				return
			}
			require.Equal(t, tt.wantEqual, equal)
		})
	}
}

func TestReflectInterfaceEqualIsSymmetric(t *testing.T) {
	t.Parallel()

	pairs := []struct {
		left  reflect.Value
		right reflect.Value
		name  string
	}{
		{name: "an invalid value against a nil slice", left: reflect.Value{}, right: reflect.ValueOf([]int(nil))},
		{name: "an int against an int64", left: reflect.ValueOf(1), right: reflect.ValueOf(int64(1))},
		{name: "a string against an integer", left: reflect.ValueOf("1"), right: reflect.ValueOf(1)},
		{name: "two equal structs", left: reflect.ValueOf(equalityPoint{X: 1}), right: reflect.ValueOf(equalityPoint{X: 1})},
	}

	for _, tt := range pairs {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			forward, forwardUncomparable := ReflectInterfaceEqual(tt.left, tt.right)
			reverse, reverseUncomparable := ReflectInterfaceEqual(tt.right, tt.left)

			require.Equal(t, forward, reverse, "equality must not depend on operand order")
			require.Equal(t, forwardUncomparable, reverseUncomparable)
		})
	}
}

func TestNilOperandsEqualDistinguishesLenientFromStrict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		left    reflect.Value
		right   reflect.Value
		name    string
		lenient bool
		want    bool
	}{
		{name: "two invalid values are equal in both modes", left: reflect.Value{}, right: reflect.Value{}, lenient: false, want: true},
		{name: "a nil slice against an invalid value is equal only when lenient", left: reflect.Value{}, right: reflect.ValueOf([]int(nil)), lenient: true, want: true},
		{name: "a nil slice against an invalid value is unequal when strict", left: reflect.Value{}, right: reflect.ValueOf([]int(nil)), lenient: false, want: false},
		{name: "a non-nilable value against an invalid value is never equal", left: reflect.Value{}, right: reflect.ValueOf(0), lenient: true, want: false},
		{name: "a non-nil pointer against an invalid value is never equal", left: reflect.Value{}, right: reflect.ValueOf(new(int)), lenient: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, nilOperandsEqual(tt.left, tt.right, tt.lenient))
		})
	}
}

func TestComparableOperandUnwrapsOneInterfaceLayer(t *testing.T) {
	t.Parallel()

	var boxed any = 42
	interfaceValue := reflect.ValueOf(&boxed).Elem()

	t.Run("an interface value is unwrapped to its dynamic value", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, reflect.Int, comparableOperand(interfaceValue).Kind())
	})

	t.Run("a concrete value passes through unchanged", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, reflect.String, comparableOperand(reflect.ValueOf("x")).Kind())
	})

	t.Run("an invalid value stays invalid", func(t *testing.T) {
		t.Parallel()
		require.False(t, comparableOperand(reflect.Value{}).IsValid())
	})
}

func TestUnpackInterfaceWritesTheZeroOfTheTargetBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		seed  func(registers *Registers)
		check func(t *testing.T, registers *Registers)
		name  string
		kind  isa.RegisterKind
	}{
		{name: "the integer bank", kind: isa.RegisterInt,
			seed:  func(registers *Registers) { registers.Ints[0] = 42 },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(0), registers.Ints[0]) }},
		{name: "the float bank", kind: isa.RegisterFloat,
			seed:  func(registers *Registers) { registers.Floats[0] = 42 },
			check: func(t *testing.T, registers *Registers) { require.InDelta(t, 0, registers.Floats[0], 0) }},
		{name: "the string bank", kind: isa.RegisterString,
			seed:  func(registers *Registers) { registers.Strings[0] = "pipit" },
			check: func(t *testing.T, registers *Registers) { require.Empty(t, registers.Strings[0]) }},
		{name: "the boolean bank", kind: isa.RegisterBool,
			seed:  func(registers *Registers) { registers.Bools[0] = true },
			check: func(t *testing.T, registers *Registers) { require.False(t, registers.Bools[0]) }},
		{name: "the unsigned bank", kind: isa.RegisterUint,
			seed:  func(registers *Registers) { registers.Uints[0] = 42 },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, uint64(0), registers.Uints[0]) }},
		{name: "the complex bank", kind: isa.RegisterComplex,
			seed:  func(registers *Registers) { registers.Complex[0] = 1 + 2i },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, complex128(0), registers.Complex[0]) }},
		{name: "the general bank", kind: isa.RegisterGeneral,
			seed:  func(registers *Registers) { registers.General[0] = reflect.ValueOf(1) },
			check: func(t *testing.T, registers *Registers) { require.False(t, registers.General[0].IsValid()) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			tt.seed(&registers)
			registers.General[1] = reflect.Value{}

			got := handleUnpackInterface(nil, nil, &registers, op(0, 1, uint8(tt.kind)))

			require.Equal(t, opContinue, got)
			tt.check(t, &registers)
		})
	}
}

func TestUnpackInterfaceMovesTheDynamicValueIntoItsBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source reflect.Value
		check  func(t *testing.T, registers *Registers)
		name   string
		kind   isa.RegisterKind
	}{
		{name: "a boxed integer lands in the integer bank", kind: isa.RegisterInt, source: reflect.ValueOf(42),
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(42), registers.Ints[0]) }},
		{name: "a boxed unsigned value reinterprets into the integer bank", kind: isa.RegisterInt, source: reflect.ValueOf(uint8(200)),
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(200), registers.Ints[0]) }},
		{name: "a boxed boolean lands in the integer bank as one", kind: isa.RegisterInt, source: reflect.ValueOf(true),
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(1), registers.Ints[0]) }},
		{name: "a boxed string lands in the string bank", kind: isa.RegisterString, source: reflect.ValueOf("pipit"),
			check: func(t *testing.T, registers *Registers) { require.Equal(t, "pipit", registers.Strings[0]) }},
		{name: "a boxed float lands in the float bank", kind: isa.RegisterFloat, source: reflect.ValueOf(1.5),
			check: func(t *testing.T, registers *Registers) { require.InDelta(t, 1.5, registers.Floats[0], 0) }},
		{name: "a boxed boolean lands in the boolean bank", kind: isa.RegisterBool, source: reflect.ValueOf(true),
			check: func(t *testing.T, registers *Registers) { require.True(t, registers.Bools[0]) }},
		{name: "a boxed unsigned value lands in the unsigned bank", kind: isa.RegisterUint, source: reflect.ValueOf(uint64(42)),
			check: func(t *testing.T, registers *Registers) { require.Equal(t, uint64(42), registers.Uints[0]) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.General[1] = tt.source

			got := handleUnpackInterface(nil, nil, &registers, op(0, 1, uint8(tt.kind)))

			require.Equal(t, opContinue, got)
			tt.check(t, &registers)
		})
	}
}

func TestUnpackInterfaceIntAcceptsEveryIntegerLikeSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source reflect.Value
		name   string
		want   int64
	}{
		{name: "a signed byte", source: reflect.ValueOf(int8(-5)), want: -5},
		{name: "a signed word", source: reflect.ValueOf(int32(-5)), want: -5},
		{name: "a plain int", source: reflect.ValueOf(-5), want: -5},
		{name: "an unsigned byte", source: reflect.ValueOf(uint8(200)), want: 200},
		{name: "an unsigned word", source: reflect.ValueOf(uint32(4000000000)), want: 4000000000},
		{name: "a true boolean", source: reflect.ValueOf(true), want: 1},
		{name: "a false boolean", source: reflect.ValueOf(false), want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.Ints[0] = -999

			unpackInterfaceInt(&registers, 0, tt.source)

			require.Equal(t, tt.want, registers.Ints[0])
		})
	}

	t.Run("a source that is not integer-like leaves the register alone", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.Ints[0] = -999

		unpackInterfaceInt(&registers, 0, reflect.ValueOf("not a number"))

		require.Equal(t, int64(-999), registers.Ints[0], "an unusable source must not corrupt the destination")
	})
}

func TestReflectSliceToBankConvertsOnlyMatchingElementKinds(t *testing.T) {
	t.Parallel()

	t.Run("signed element slices convert to the integer bank", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			source reflect.Value
			name   string
			want   []int64
			ok     bool
		}{
			{name: "an int64 slice converts", source: reflect.ValueOf([]int64{1, 2}), want: []int64{1, 2}, ok: true},
			{name: "an int slice converts", source: reflect.ValueOf([]int{1, 2}), want: []int64{1, 2}, ok: true},
			{name: "an empty slice converts", source: reflect.ValueOf([]int{}), want: []int64{}, ok: true},
			{name: "a string slice does not convert", source: reflect.ValueOf([]string{"a"}), ok: false},
			{name: "a float slice does not convert", source: reflect.ValueOf([]float64{1}), ok: false},
		}
		for _, tt := range tests {
			got, ok := reflectSliceToBankInt(tt.source)
			require.Equal(t, tt.ok, ok, tt.name)
			if tt.ok {
				require.Equal(t, tt.want, got, tt.name)
			}
		}
	})

	t.Run("unsigned element slices convert to the unsigned bank", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			source reflect.Value
			name   string
			want   []uint64
			ok     bool
		}{
			{name: "a uint64 slice converts", source: reflect.ValueOf([]uint64{1, 2}), want: []uint64{1, 2}, ok: true},
			{name: "a uint slice converts", source: reflect.ValueOf([]uint{1, 2}), want: []uint64{1, 2}, ok: true},
			{name: "a signed slice does not convert", source: reflect.ValueOf([]int{1}), ok: false},
		}
		for _, tt := range tests {
			got, ok := reflectSliceToBankUint(tt.source)
			require.Equal(t, tt.ok, ok, tt.name)
			if tt.ok {
				require.Equal(t, tt.want, got, tt.name)
			}
		}
	})
}

func TestPackTypedSliceHelpersWrapWithoutCopyingElements(t *testing.T) {
	t.Parallel()

	t.Run("a string slice wraps with the cached slice type", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		source := []string{"a", "b"}

		wrapped := PackTypedSliceStringToGeneral(arena, source)

		require.Equal(t, reflect.Slice, wrapped.Kind())
		require.Equal(t, 2, wrapped.Len())
		require.Equal(t, "a", wrapped.Index(0).String())
	})

	t.Run("a nil arena falls back to an ordinary reflect value", func(t *testing.T) {
		t.Parallel()
		wrapped := PackTypedSliceStringToGeneral(nil, []string{"a"})

		require.Equal(t, reflect.Slice, wrapped.Kind())
		require.Equal(t, 1, wrapped.Len())
	})

	t.Run("float, bool and unsigned slices wrap to the matching element kind", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)

		floats := packTypedSliceFloatToGeneral(arena, []float64{1.5, 2.5})
		require.Equal(t, 2, floats.Len())
		require.InDelta(t, 1.5, floats.Index(0).Float(), 0)

		bools := packTypedSliceBoolToGeneral(arena, []bool{true, false})
		require.Equal(t, 2, bools.Len())
		require.True(t, bools.Index(0).Bool())

		uints := packTypedSliceUintToGeneral(arena, []uint64{7, 8})
		require.Equal(t, 2, uints.Len())
		require.Equal(t, uint64(7), uints.Index(0).Uint())
	})
}

func TestCopyIntsAndUintsProduceIndependentTypedSlices(t *testing.T) {
	t.Parallel()

	t.Run("signed values copy into a narrower element type", func(t *testing.T) {
		t.Parallel()
		source := []int64{1, 2, 3}

		copied := copyIntsAs(reflect.TypeFor[[]int32](), source)

		require.Equal(t, 3, copied.Len())
		require.Equal(t, int64(2), copied.Index(1).Int())
		source[1] = 99
		require.Equal(t, int64(2), copied.Index(1).Int(), "the copy must not alias the source bank")
	})

	t.Run("unsigned values copy into a narrower element type", func(t *testing.T) {
		t.Parallel()
		source := []uint64{1, 2, 3}

		copied := copyUintsAs(reflect.TypeFor[[]uint16](), source)

		require.Equal(t, 3, copied.Len())
		require.Equal(t, uint64(2), copied.Index(1).Uint())
		source[1] = 99
		require.Equal(t, uint64(2), copied.Index(1).Uint(), "the copy must not alias the source bank")
	})
}
