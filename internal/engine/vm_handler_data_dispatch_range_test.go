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

const (
	rangeBindsKey   uint8 = 1
	rangeBindsValue uint8 = 2
)

func rangeIteratorFor(t *testing.T, collection any) (*VM, *CallFrame, *Registers, *rangeIterator) {
	t.Helper()

	vm, frame, registers := newStandardVM(t)
	registers.General[2] = reflect.ValueOf(collection)

	got := handleRangeInit(vm, frame, registers, op(0, 1, 2))
	require.Equal(t, opContinue, got, "building an iterator never leaves the dispatch loop")

	iterator, ok := reflect.TypeAssert[*rangeIterator](registers.General[1])
	require.True(t, ok, "the iterator is published into the general bank")
	return vm, frame, registers, iterator
}

func TestRangeInitClassifiesTheCollection(t *testing.T) {
	t.Parallel()

	array := [3]int{1, 2, 3}

	tests := []struct {
		name       string
		collection any
		inspect    func(*testing.T, *rangeIterator)
	}{
		{
			name:       "an int slice takes the typed fast path",
			collection: []int{1, 2},
			inspect: func(t *testing.T, it *rangeIterator) {
				require.Equal(t, []int{1, 2}, it.intSlice)
			},
		},
		{
			name:       "a string slice takes the typed fast path",
			collection: []string{"a"},
			inspect: func(t *testing.T, it *rangeIterator) {
				require.Equal(t, []string{"a"}, it.stringSlice)
			},
		},
		{
			name:       "a float slice takes the typed fast path",
			collection: []float64{1.5},
			inspect: func(t *testing.T, it *rangeIterator) {
				require.Equal(t, []float64{1.5}, it.floatSlice)
			},
		},
		{
			name:       "a bool slice takes the typed fast path",
			collection: []bool{true},
			inspect: func(t *testing.T, it *rangeIterator) {
				require.Equal(t, []bool{true}, it.boolSlice)
			},
		},
		{
			name:       "a slice of an unlisted element type has no fast path",
			collection: []uint8{1},
			inspect: func(t *testing.T, it *rangeIterator) {
				require.Nil(t, it.intSlice)
				require.Nil(t, it.stringSlice)
				require.Nil(t, it.floatSlice)
				require.Nil(t, it.boolSlice)
			},
		},
		{
			name:       "a map gets a reflect map iterator and both scratch cells",
			collection: map[string]int{"a": 1},
			inspect: func(t *testing.T, it *rangeIterator) {
				require.True(t, it.isMap)
				require.NotNil(t, it.mapIterator)
				require.True(t, it.keyScratch.IsValid())
				require.True(t, it.valueScratch.IsValid())
			},
		},
		{
			name:       "a channel is marked for the receiving walk",
			collection: make(chan int),
			inspect: func(t *testing.T, it *rangeIterator) {
				require.True(t, it.isChannel)
			},
		},
		{
			name:       "a string keeps its own source for the rune walk",
			collection: "héllo",
			inspect: func(t *testing.T, it *rangeIterator) {
				require.True(t, it.IsString)
				require.Equal(t, "héllo", it.StringSource)
			},
		},
		{
			name:       "an array is copied so a later write cannot disturb the walk",
			collection: array,
			inspect: func(t *testing.T, it *rangeIterator) {
				require.Equal(t, reflect.Array, it.collection.Kind())
				require.Equal(t, 3, it.collection.Len())
			},
		},
		{
			name:       "a pointer to an array is dereferenced",
			collection: &array,
			inspect: func(t *testing.T, it *rangeIterator) {
				require.Equal(t, reflect.Array, it.collection.Kind())
			},
		},
		{
			name:       "a value that cannot be ranged over yields an inert iterator",
			collection: 7,
			inspect: func(t *testing.T, it *rangeIterator) {
				require.False(t, it.isMap)
				require.False(t, it.isChannel)
				require.False(t, it.IsString)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, _, iterator := rangeIteratorFor(t, tt.collection)
			tt.inspect(t, iterator)
		})
	}
}

func TestRangeNextWalksSlicesToExhaustion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		collection any
		valueKind  isa.RegisterKind
		read       func(*Registers) any
		want       []any
	}{
		{
			name: "an int slice", collection: []int{4, 5}, valueKind: isa.RegisterInt,
			read: func(r *Registers) any { return r.Ints[6] }, want: []any{int64(4), int64(5)},
		},
		{
			name: "a string slice", collection: []string{"a", "b"}, valueKind: isa.RegisterString,
			read: func(r *Registers) any { return r.Strings[6] }, want: []any{"a", "b"},
		},
		{
			name: "a float slice", collection: []float64{1.5, 2.5}, valueKind: isa.RegisterFloat,
			read: func(r *Registers) any { return r.Floats[6] }, want: []any{1.5, 2.5},
		},
		{
			name: "a bool slice", collection: []bool{true, false}, valueKind: isa.RegisterBool,
			read: func(r *Registers) any { return r.Bools[6] }, want: []any{true, false},
		},
		{
			name: "a slice with no typed fast path falls back to reflect", collection: []uint8{7, 8}, valueKind: isa.RegisterUint,
			read: func(r *Registers) any { return r.Uints[6] }, want: []any{uint64(7), uint64(8)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers, _ := rangeIteratorFor(t, tt.collection)
			advance := func() OpResult {
				frame.ProgramCounter = 1
				frame.Function.Body = []isa.Instruction{
					{},
					op(rangeBindsKey|rangeBindsValue, 5, uint8(isa.RegisterInt)),
					op(0, 6, uint8(tt.valueKind)),
					{},
				}
				return handleRangeNext(vm, frame, registers, op(0, 1, 4))
			}

			for index, want := range tt.want {
				require.Equal(t, opContinue, advance(), "a slice walk never leaves the dispatch loop")
				require.Equal(t, int64(1), registers.Ints[4], "the done register stays set while elements remain")
				require.Equal(t, int64(index), registers.Ints[5], "the key register carries the index")
				require.Equal(t, want, tt.read(registers))
			}

			require.Equal(t, opContinue, advance())
			require.Equal(t, int64(0), registers.Ints[4], "the done register clears once the slice is exhausted")
		})
	}
}

func TestRangeNextWalksAMapAndAString(t *testing.T) {
	t.Parallel()

	t.Run("a map yields every key and value exactly once", func(t *testing.T) {
		t.Parallel()

		source := map[string]int{"a": 1, "b": 2}
		vm, frame, registers, _ := rangeIteratorFor(t, source)
		frame.Function.Body = []isa.Instruction{
			{},
			op(rangeBindsKey|rangeBindsValue, 5, uint8(isa.RegisterString)),
			op(0, 6, uint8(isa.RegisterInt)),
			{},
		}

		seen := map[string]int64{}
		for range len(source) {
			frame.ProgramCounter = 1
			require.Equal(t, opContinue, handleRangeNext(vm, frame, registers, op(0, 1, 4)))
			require.Equal(t, int64(1), registers.Ints[4])
			seen[registers.Strings[5]] = registers.Ints[6]
		}

		frame.ProgramCounter = 1
		require.Equal(t, opContinue, handleRangeNext(vm, frame, registers, op(0, 1, 4)))
		require.Equal(t, int64(0), registers.Ints[4], "the done register clears once the map is exhausted")
		require.Equal(t, map[string]int64{"a": 1, "b": 2}, seen)
	})

	t.Run("a string yields byte offsets and decoded runes", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers, _ := rangeIteratorFor(t, "aé")
		frame.Function.Body = []isa.Instruction{
			{},
			op(rangeBindsKey|rangeBindsValue, 5, uint8(isa.RegisterInt)),
			op(0, 6, uint8(isa.RegisterInt)),
			{},
		}

		want := []struct {
			offset int64
			rune   int64
		}{{offset: 0, rune: 'a'}, {offset: 1, rune: 'é'}}

		for _, step := range want {
			frame.ProgramCounter = 1
			require.Equal(t, opContinue, handleRangeNext(vm, frame, registers, op(0, 1, 4)))
			require.Equal(t, int64(1), registers.Ints[4])
			require.Equal(t, step.offset, registers.Ints[5], "the key register carries the byte offset, not the rune count")
			require.Equal(t, step.rune, registers.Ints[6])
		}

		frame.ProgramCounter = 1
		require.Equal(t, opContinue, handleRangeNext(vm, frame, registers, op(0, 1, 4)))
		require.Equal(t, int64(0), registers.Ints[4])
	})

	t.Run("invalid utf-8 decodes to the replacement rune one byte at a time", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers, _ := rangeIteratorFor(t, "\xff\xfe")
		frame.Function.Body = []isa.Instruction{
			{},
			op(rangeBindsKey|rangeBindsValue, 5, uint8(isa.RegisterInt)),
			op(0, 6, uint8(isa.RegisterInt)),
			{},
		}

		for offset := range int64(2) {
			frame.ProgramCounter = 1
			require.Equal(t, opContinue, handleRangeNext(vm, frame, registers, op(0, 1, 4)))
			require.Equal(t, offset, registers.Ints[5])
			require.Equal(t, int64(0xFFFD), registers.Ints[6], "an undecodable byte becomes the replacement rune")
		}
	})
}

func TestRangeNextWalksAChannelUntilClosed(t *testing.T) {
	t.Parallel()

	channel := make(chan int, 2)
	channel <- 3
	channel <- 4
	close(channel)

	vm, frame, registers, _ := rangeIteratorFor(t, channel)
	frame.Function.Body = []isa.Instruction{
		{},
		op(rangeBindsKey, 5, uint8(isa.RegisterInt)),
		op(0, 6, uint8(isa.RegisterInt)),
		{},
	}

	for _, want := range []int64{3, 4} {
		frame.ProgramCounter = 1
		require.Equal(t, opContinue, handleRangeNext(vm, frame, registers, op(0, 1, 4)))
		require.Equal(t, int64(1), registers.Ints[4])
		require.Equal(t, want, registers.Ints[5], "a channel range binds the received value to the key slot")
	}

	frame.ProgramCounter = 1
	require.Equal(t, opContinue, handleRangeNext(vm, frame, registers, op(0, 1, 4)))
	require.Equal(t, int64(0), registers.Ints[4], "a closed channel ends the walk")
}

func TestRangeNextRefusesAForeignIterator(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newExtWordFrame(t, op(0, 5, 0), op(0, 6, 0))
	registers.General[1] = reflect.ValueOf("not an iterator")

	got := handleRangeNext(vm, frame, registers, op(0, 1, 4))
	requireEvalError(t, vm, got, nil)
	require.Contains(t, vm.evalError.Error(), "range iterator is not valid")
}

func TestRangeSliceValueFallsBackWhenTheBankDisagrees(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		collection any
		kind       isa.RegisterKind
		inspect    func(*testing.T, *Registers)
	}{
		{
			name: "an int slice asked for the general bank goes through reflect", collection: []int{9},
			kind: isa.RegisterGeneral,
			inspect: func(t *testing.T, r *Registers) {
				require.Equal(t, 9, r.General[6].Interface())
			},
		},
		{
			name: "a string slice asked for the general bank goes through reflect", collection: []string{"s"},
			kind: isa.RegisterGeneral,
			inspect: func(t *testing.T, r *Registers) {
				require.Equal(t, "s", r.General[6].Interface())
			},
		},
		{
			name: "a float slice asked for the general bank goes through reflect", collection: []float64{2.5},
			kind: isa.RegisterGeneral,
			inspect: func(t *testing.T, r *Registers) {
				require.InDelta(t, 2.5, r.General[6].Interface(), 0)
			},
		},
		{
			name: "a bool slice asked for the general bank goes through reflect", collection: []bool{true},
			kind: isa.RegisterGeneral,
			inspect: func(t *testing.T, r *Registers) {
				require.Equal(t, true, r.General[6].Interface())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, _, registers, iterator := rangeIteratorFor(t, tt.collection)
			rangeSliceValue(vm, registers, iterator, 6, tt.kind)
			tt.inspect(t, registers)
		})
	}
}

func TestRangeNextSkipsUnboundDestinations(t *testing.T) {
	t.Parallel()

	vm, frame, registers, _ := rangeIteratorFor(t, []int{11})
	frame.Function.Body = []isa.Instruction{
		{},
		op(0, 5, uint8(isa.RegisterInt)),
		op(0, 6, uint8(isa.RegisterInt)),
		{},
	}
	frame.ProgramCounter = 1

	require.Equal(t, opContinue, handleRangeNext(vm, frame, registers, op(0, 1, 4)))
	require.Equal(t, int64(1), registers.Ints[4], "the done register is written whether or not anything is bound")
	require.Zero(t, registers.Ints[5], "a `for range xs` loop binds no key")
	require.Zero(t, registers.Ints[6], "a `for range xs` loop binds no value")
}
