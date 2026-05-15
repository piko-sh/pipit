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

func TestRegisterToReflectValueBoxesEveryBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		seed  func(registers *Registers)
		check func(t *testing.T, boxed reflect.Value)
		name  string
		kind  isa.RegisterKind
	}{
		{name: "the integer bank", kind: isa.RegisterInt,
			seed:  func(registers *Registers) { registers.Ints[0] = 42 },
			check: func(t *testing.T, boxed reflect.Value) { require.Equal(t, int64(42), boxed.Int()) }},
		{name: "the float bank", kind: isa.RegisterFloat,
			seed:  func(registers *Registers) { registers.Floats[0] = 1.5 },
			check: func(t *testing.T, boxed reflect.Value) { require.InDelta(t, 1.5, boxed.Float(), 0) }},
		{name: "the string bank", kind: isa.RegisterString,
			seed:  func(registers *Registers) { registers.Strings[0] = "pipit" },
			check: func(t *testing.T, boxed reflect.Value) { require.Equal(t, "pipit", boxed.String()) }},
		{name: "the boolean bank", kind: isa.RegisterBool,
			seed:  func(registers *Registers) { registers.Bools[0] = true },
			check: func(t *testing.T, boxed reflect.Value) { require.True(t, boxed.Bool()) }},
		{name: "the unsigned bank", kind: isa.RegisterUint,
			seed:  func(registers *Registers) { registers.Uints[0] = 42 },
			check: func(t *testing.T, boxed reflect.Value) { require.Equal(t, uint64(42), boxed.Uint()) }},
		{name: "the complex bank", kind: isa.RegisterComplex,
			seed:  func(registers *Registers) { registers.Complex[0] = 1 + 2i },
			check: func(t *testing.T, boxed reflect.Value) { require.Equal(t, 1+2i, boxed.Complex()) }},
		{name: "the general bank passes through untouched", kind: isa.RegisterGeneral,
			seed:  func(registers *Registers) { registers.General[0] = reflect.ValueOf("boxed") },
			check: func(t *testing.T, boxed reflect.Value) { require.Equal(t, "boxed", boxed.String()) }},
		{name: "the integer slice bank", kind: isa.RegisterSliceInt,
			seed:  func(registers *Registers) { registers.SlicesInt[0] = []int64{1, 2} },
			check: func(t *testing.T, boxed reflect.Value) { require.Equal(t, 2, boxed.Len()) }},
		{name: "the float slice bank", kind: isa.RegisterSliceFloat,
			seed:  func(registers *Registers) { registers.slicesFloat[0] = []float64{1} },
			check: func(t *testing.T, boxed reflect.Value) { require.Equal(t, 1, boxed.Len()) }},
		{name: "the string slice bank", kind: isa.RegisterSliceString,
			seed:  func(registers *Registers) { registers.slicesString[0] = []string{"a"} },
			check: func(t *testing.T, boxed reflect.Value) { require.Equal(t, 1, boxed.Len()) }},
		{name: "the boolean slice bank", kind: isa.RegisterSliceBool,
			seed:  func(registers *Registers) { registers.slicesBool[0] = []bool{true} },
			check: func(t *testing.T, boxed reflect.Value) { require.Equal(t, 1, boxed.Len()) }},
		{name: "the unsigned slice bank", kind: isa.RegisterSliceUint,
			seed:  func(registers *Registers) { registers.slicesUint[0] = []uint64{7} },
			check: func(t *testing.T, boxed reflect.Value) { require.Equal(t, 1, boxed.Len()) }},
		{name: "the byte slice bank", kind: isa.RegisterSliceByte,
			seed:  func(registers *Registers) { registers.slicesByte[0] = []byte("hi") },
			check: func(t *testing.T, boxed reflect.Value) { require.Equal(t, 2, boxed.Len()) }},
	}

	for _, tt := range tests {
		t.Run(tt.name+" with an arena", func(t *testing.T) {
			t.Parallel()
			arena := newTestArena(t)
			registers := standardRegisters()
			tt.seed(&registers)

			tt.check(t, registerToReflectValue(arena, &registers, tt.kind, 0))
		})

		t.Run(tt.name+" without an arena", func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			tt.seed(&registers)

			tt.check(t, registerToReflectValue(nil, &registers, tt.kind, 0))
		})
	}

	t.Run("an unknown bank yields an invalid value", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()

		require.False(t, registerToReflectValue(nil, &registers, isa.RegisterKind(200), 0).IsValid())
	})
}

func TestCopySameKindArgMovesBetweenRegisterFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		seed  func(registers *Registers)
		check func(t *testing.T, destination *Registers)
		name  string
		kind  isa.RegisterKind
	}{
		{name: "the integer bank", kind: isa.RegisterInt,
			seed:  func(registers *Registers) { registers.Ints[1] = 42 },
			check: func(t *testing.T, d *Registers) { require.Equal(t, int64(42), d.Ints[0]) }},
		{name: "the float bank", kind: isa.RegisterFloat,
			seed:  func(registers *Registers) { registers.Floats[1] = 1.5 },
			check: func(t *testing.T, d *Registers) { require.InDelta(t, 1.5, d.Floats[0], 0) }},
		{name: "the string bank", kind: isa.RegisterString,
			seed:  func(registers *Registers) { registers.Strings[1] = "pipit" },
			check: func(t *testing.T, d *Registers) { require.Equal(t, "pipit", d.Strings[0]) }},
		{name: "the boolean bank", kind: isa.RegisterBool,
			seed:  func(registers *Registers) { registers.Bools[1] = true },
			check: func(t *testing.T, d *Registers) { require.True(t, d.Bools[0]) }},
		{name: "the unsigned bank", kind: isa.RegisterUint,
			seed:  func(registers *Registers) { registers.Uints[1] = 42 },
			check: func(t *testing.T, d *Registers) { require.Equal(t, uint64(42), d.Uints[0]) }},
		{name: "the complex bank", kind: isa.RegisterComplex,
			seed:  func(registers *Registers) { registers.Complex[1] = 1 + 2i },
			check: func(t *testing.T, d *Registers) { require.Equal(t, 1+2i, d.Complex[0]) }},
		{name: "the general bank", kind: isa.RegisterGeneral,
			seed:  func(registers *Registers) { registers.General[1] = reflect.ValueOf(7) },
			check: func(t *testing.T, d *Registers) { require.Equal(t, int64(7), d.General[0].Int()) }},
		{name: "the integer slice bank", kind: isa.RegisterSliceInt,
			seed:  func(registers *Registers) { registers.SlicesInt[1] = []int64{1} },
			check: func(t *testing.T, d *Registers) { require.Equal(t, []int64{1}, d.SlicesInt[0]) }},
		{name: "the byte slice bank", kind: isa.RegisterSliceByte,
			seed:  func(registers *Registers) { registers.slicesByte[1] = []byte("hi") },
			check: func(t *testing.T, d *Registers) { require.Equal(t, []byte("hi"), d.slicesByte[0]) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := standardRegisters()
			destination := standardRegisters()
			tt.seed(&source)

			copySameKindArg(&destination, &source, tt.kind, 0, 1)

			tt.check(t, &destination)
		})
	}

	t.Run("an unknown bank copies nothing", func(t *testing.T) {
		t.Parallel()
		source := standardRegisters()
		destination := standardRegisters()
		source.Ints[1] = 42

		copySameKindArg(&destination, &source, isa.RegisterKind(200), 0, 1)

		require.Equal(t, int64(0), destination.Ints[0], "an unrecognised bank must leave the destination alone")
	})
}

func TestNarrowKindPredicatesPartitionTheWidenedElements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		kind     reflect.Kind
		wantInt  bool
		wantUint bool
	}{
		{name: "a plain int widens", kind: reflect.Int, wantInt: true},
		{name: "an int8 widens", kind: reflect.Int8, wantInt: true},
		{name: "an int16 widens", kind: reflect.Int16, wantInt: true},
		{name: "an int32 widens", kind: reflect.Int32, wantInt: true},
		{name: "an int64 is already the bank width", kind: reflect.Int64},
		{name: "a plain uint widens", kind: reflect.Uint, wantUint: true},
		{name: "a uint16 widens", kind: reflect.Uint16, wantUint: true},
		{name: "a uint32 widens", kind: reflect.Uint32, wantUint: true},
		{name: "a uintptr widens", kind: reflect.Uintptr, wantUint: true},
		{name: "a uint64 is already the bank width", kind: reflect.Uint64},
		{name: "a uint8 is carried by the byte bank instead", kind: reflect.Uint8},
		{name: "a float is neither", kind: reflect.Float64},
		{name: "a string is neither", kind: reflect.String},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.wantInt, matchesNarrowIntKind(tt.kind))
			require.Equal(t, tt.wantUint, matchesNarrowUintKind(tt.kind))
		})
	}
}

func TestUnboxToTypedIntSliceWidensOnlyWhatItCan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value reflect.Value
		name  string
		want  []int64
		nil   bool
	}{
		{name: "an int64 slice passes through", value: reflect.ValueOf([]int64{1, 2}), want: []int64{1, 2}},
		{name: "an int slice is reinterpreted", value: reflect.ValueOf([]int{1, 2}), want: []int64{1, 2}},
		{name: "an int8 slice is widened", value: reflect.ValueOf([]int8{1, 2}), want: []int64{1, 2}},
		{name: "an int16 slice is widened", value: reflect.ValueOf([]int16{1, 2}), want: []int64{1, 2}},
		{name: "an int32 slice is widened", value: reflect.ValueOf([]int32{1, 2}), want: []int64{1, 2}},
		{name: "a negative element survives widening", value: reflect.ValueOf([]int8{-5}), want: []int64{-5}},
		{name: "an empty slice widens to an empty slice", value: reflect.ValueOf([]int8{}), want: []int64{}},
		{name: "an unsigned slice is declined", value: reflect.ValueOf([]uint32{1}), nil: true},
		{name: "a string slice is declined", value: reflect.ValueOf([]string{"a"}), nil: true},
		{name: "a non-slice is declined", value: reflect.ValueOf(1), nil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			arena := newTestArena(t)

			got := unboxToTypedIntSlice(tt.value, arena)

			if tt.nil {
				require.Nil(t, got)
				return
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestWidenIntSliceWithArenaCopiesElementByElement(t *testing.T) {
	t.Parallel()

	t.Run("a nil slice widens to nil", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, widenIntSliceWithArena(reflect.ValueOf([]int8(nil)), newTestArena(t)))
	})

	t.Run("elements are copied, not aliased", func(t *testing.T) {
		t.Parallel()
		source := []int32{1, 2, 3}

		widened := widenIntSliceWithArena(reflect.ValueOf(source), newTestArena(t))

		require.Equal(t, []int64{1, 2, 3}, widened)
		source[0] = 99
		require.Equal(t, int64(1), widened[0], "widening must copy rather than reinterpret a narrower element")
	})

	t.Run("a nil arena falls back to the heap", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, []int64{1}, widenIntSliceWithArena(reflect.ValueOf([]int32{1}), nil))
	})
}

func TestReflectValueBufferPoolHandsOutTheRequestedLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		length int
	}{
		{name: "an empty buffer", length: 0},
		{name: "a small buffer", length: 1},
		{name: "a larger buffer", length: 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			buffer := acquireReflectValueBuffer(tt.length)

			require.Len(t, buffer, tt.length)
			for _, slot := range buffer {
				require.False(t, slot.IsValid(), "a leased buffer must be cleared of the previous tenant's values")
			}
			releaseReflectValueBuffer(buffer)
		})
	}

	t.Run("a reused buffer is cleared before it is handed back out", func(t *testing.T) {
		t.Parallel()
		first := acquireReflectValueBuffer(4)
		first[0] = reflect.ValueOf(42)
		releaseReflectValueBuffer(first)

		second := acquireReflectValueBuffer(4)

		require.Len(t, second, 4)
		for _, slot := range second {
			require.False(t, slot.IsValid(), "a pooled buffer must not leak the previous caller's arguments")
		}
		releaseReflectValueBuffer(second)
	})
}
