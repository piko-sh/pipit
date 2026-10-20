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

func TestRangeNextStringDecodesRunes(t *testing.T) {
	t.Parallel()

	registers := NewRegisters(testRegCounts())
	iterator := &rangeIterator{
		IsString:     true,
		StringSource: "héllo",
	}
	context := rangeNextContext{
		DoneDestination: 0,
		HasKey:          true,
		KeyInstruction:  isa.Instruction{B: 1, C: uint8(isa.RegisterInt)},
		HasValue:        true,
		ValInstruction:  isa.Instruction{B: 2, C: uint8(isa.RegisterInt)},
	}

	expected := []struct {
		index    int64
		runeCode int64
	}{
		{index: 0, runeCode: 'h'},
		{index: 1, runeCode: 'é'},
		{index: 3, runeCode: 'l'},
		{index: 4, runeCode: 'l'},
		{index: 5, runeCode: 'o'},
	}

	for step, want := range expected {
		rangeNextString(&registers, iterator, context)
		require.Equalf(t, int64(1), registers.Ints[0],
			"step %d: doneDestination should be 1 while iteration continues", step)
		require.Equalf(t, want.index, registers.Ints[1],
			"step %d: byte index of rune", step)
		require.Equalf(t, want.runeCode, registers.Ints[2],
			"step %d: decoded rune value", step)
	}

	rangeNextString(&registers, iterator, context)
	require.Equal(t, int64(0), registers.Ints[0],
		"after the last rune, doneDestination should be 0 to terminate the loop")
}

func TestRangeNextStringEmptyString(t *testing.T) {
	t.Parallel()

	registers := NewRegisters(testRegCounts())
	iterator := &rangeIterator{
		IsString:     true,
		StringSource: "",
	}
	context := rangeNextContext{
		DoneDestination: 0,
		HasKey:          true,
		KeyInstruction:  isa.Instruction{B: 1, C: uint8(isa.RegisterInt)},
		HasValue:        true,
		ValInstruction:  isa.Instruction{B: 2, C: uint8(isa.RegisterInt)},
	}

	rangeNextString(&registers, iterator, context)
	require.Equal(t, int64(0), registers.Ints[0],
		"empty string should immediately terminate iteration")
}

func TestRangeNextStringValueOnlyNoKey(t *testing.T) {
	t.Parallel()

	registers := NewRegisters(testRegCounts())
	iterator := &rangeIterator{
		IsString:     true,
		StringSource: "ab",
	}
	context := rangeNextContext{
		DoneDestination: 0,
		HasKey:          false,
		HasValue:        true,
		ValInstruction:  isa.Instruction{B: 1, C: uint8(isa.RegisterInt)},
	}

	rangeNextString(&registers, iterator, context)
	require.Equal(t, int64(1), registers.Ints[0])
	require.Equal(t, int64('a'), registers.Ints[1])

	rangeNextString(&registers, iterator, context)
	require.Equal(t, int64(1), registers.Ints[0])
	require.Equal(t, int64('b'), registers.Ints[1])
}

func TestRangeNextStringInvalidUTF8YieldsRuneError(t *testing.T) {
	t.Parallel()

	registers := NewRegisters(testRegCounts())
	iterator := &rangeIterator{
		IsString:     true,
		StringSource: "\xff",
	}
	context := rangeNextContext{
		DoneDestination: 0,
		HasKey:          true,
		KeyInstruction:  isa.Instruction{B: 1, C: uint8(isa.RegisterInt)},
		HasValue:        true,
		ValInstruction:  isa.Instruction{B: 2, C: uint8(isa.RegisterInt)},
	}

	rangeNextString(&registers, iterator, context)
	require.Equal(t, int64(1), registers.Ints[0])
	require.Equal(t, int64(0xFFFD), registers.Ints[2],
		"invalid UTF-8 sequences should decode to utf8.RuneError (U+FFFD) and consume one byte")

	rangeNextString(&registers, iterator, context)
	require.Equal(t, int64(0), registers.Ints[0],
		"after consuming the lone invalid byte, the iteration terminates")
}

func TestTypeAssertionsSeeTheScriptValue(t *testing.T) {
	t.Parallel()

	vm := probeVM(t, "Site", probeOpen)

	t.Run("an adapter for a named script value yields the value", func(t *testing.T) {
		t.Parallel()

		named := reflect.ValueOf(identityTrailing{})
		adapter := reflect.ValueOf(&pipitFSAdapter{AdapterCore: &AdapterCore{underlying: named}})
		require.Equal(t, named.Interface(), typeAssertOperand(vm, adapter).Interface())
	})

	t.Run("an adapter for an unnamed value stays", func(t *testing.T) {
		t.Parallel()

		core := &AdapterCore{
			vm:         vm,
			methods:    map[string]adapterMethodRef{"Open": {root: nil, index: 0}, "Stat": {root: nil, index: 0}},
			typeName:   "",
			underlying: reflect.ValueOf(map[string]int{}),
		}
		adapter := reflect.ValueOf(&pipitFSAdapter{AdapterCore: core})
		require.Equal(t, adapter.Interface(), typeAssertOperand(vm, adapter).Interface())
		require.True(t, sourceImplementsAllMethods(vm, adapter, []string{"Open", "Stat"}))
		require.False(t, sourceImplementsAllMethods(vm, adapter, []string{"Open", "ReadDir"}),
			"the adapter's Go type has ReadDir, but its value does not")
	})

	t.Run("an interface yields its dynamic value", func(t *testing.T) {
		t.Parallel()

		var boxed any = 3
		require.Equal(t, 3, typeAssertOperand(vm, reflect.ValueOf(&boxed).Elem()).Interface())
	})
}
