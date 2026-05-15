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

func newMapOkFrame(t *testing.T, okRegister uint8) (*VM, *CallFrame, *Registers) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.Emit(isa.OpMapIndexOkIntInt, 0, 1, 2)
	builder.body = append(builder.body, isa.NewInstruction(isa.OpExt, okRegister, 0, 0))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	vm, frame, registers := newFramedVM(t, builder.build())
	frame.ProgramCounter = 1
	return vm, frame, registers
}

func TestTypedMapReadsReturnTheZeroValueOnAMiss(t *testing.T) {
	t.Parallel()

	tests := []struct {
		build   func() reflect.Value
		seedKey func(registers *Registers)
		check   func(t *testing.T, registers *Registers, hit bool)
		handler opcodeHandler
		name    string
	}{
		{
			name:    "an integer-keyed integer map",
			handler: handleMapGetIntInt,
			build:   func() reflect.Value { return reflect.ValueOf(map[int]int{7: 42}) },
			seedKey: func(registers *Registers) { registers.Ints[2] = 7 },
			check: func(t *testing.T, registers *Registers, hit bool) {
				require.Equal(t, map[bool]int64{true: 42, false: 0}[hit], registers.Ints[0])
			},
		},
		{
			name:    "a named integer key on the reflect path",
			handler: handleMapGetIntInt,
			build:   func() reflect.Value { return reflect.ValueOf(map[namedMapKey]int64{7: 42}) },
			seedKey: func(registers *Registers) { registers.Ints[2] = 7 },
			check: func(t *testing.T, registers *Registers, hit bool) {
				require.Equal(t, map[bool]int64{true: 42, false: 0}[hit], registers.Ints[0])
			},
		},
		{
			name:    "a string-keyed integer map",
			handler: handleMapGetStringInt,
			build:   func() reflect.Value { return reflect.ValueOf(map[string]int{"k": 42}) },
			seedKey: func(registers *Registers) { registers.Strings[2] = "k" },
			check: func(t *testing.T, registers *Registers, hit bool) {
				require.Equal(t, map[bool]int64{true: 42, false: 0}[hit], registers.Ints[0])
			},
		},
		{
			name:    "a string-keyed string map",
			handler: handleMapGetStringString,
			build:   func() reflect.Value { return reflect.ValueOf(map[string]string{"k": "v"}) },
			seedKey: func(registers *Registers) { registers.Strings[2] = "k" },
			check: func(t *testing.T, registers *Registers, hit bool) {
				require.Equal(t, map[bool]string{true: "v", false: ""}[hit], registers.Strings[0])
			},
		},
		{
			name:    "an integer-keyed string map",
			handler: handleMapGetIntString,
			build:   func() reflect.Value { return reflect.ValueOf(map[int]string{7: "v"}) },
			seedKey: func(registers *Registers) { registers.Ints[2] = 7 },
			check: func(t *testing.T, registers *Registers, hit bool) {
				require.Equal(t, map[bool]string{true: "v", false: ""}[hit], registers.Strings[0])
			},
		},
		{
			name:    "an integer-keyed general map",
			handler: handleMapGetIntGeneral,
			build:   func() reflect.Value { return reflect.ValueOf(map[int][]string{7: {"v"}}) },
			seedKey: func(registers *Registers) { registers.Ints[2] = 7 },
			check: func(t *testing.T, registers *Registers, hit bool) {
				require.True(t, registers.General[0].IsValid())
				require.Equal(t, map[bool]int{true: 1, false: 0}[hit], registers.General[0].Len())
			},
		},
		{
			name:    "a string-keyed general map",
			handler: handleMapGetStringGeneral,
			build:   func() reflect.Value { return reflect.ValueOf(map[string][]string{"k": {"v"}}) },
			seedKey: func(registers *Registers) { registers.Strings[2] = "k" },
			check: func(t *testing.T, registers *Registers, hit bool) {
				require.True(t, registers.General[0].IsValid())
				require.Equal(t, map[bool]int{true: 1, false: 0}[hit], registers.General[0].Len())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" on a hit", func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newStandardVM(t)
			registers.General[1] = tt.build()
			tt.seedKey(registers)

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			tt.check(t, registers, true)
		})

		t.Run(tt.name+" on a miss", func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newStandardVM(t)
			registers.General[1] = tt.build()
			registers.Ints[2] = 999
			registers.Strings[2] = "absent"

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			tt.check(t, registers, false)
		})
	}
}

func TestTypedMapCommaOkWritesThePresenceBit(t *testing.T) {
	t.Parallel()

	const okRegister uint8 = 5

	tests := []struct {
		build   func() reflect.Value
		seedKey func(registers *Registers, present bool)
		handler opcodeHandler
		name    string
	}{
		{
			name:    "an integer-keyed integer map",
			handler: handleMapIndexOkIntInt,
			build:   func() reflect.Value { return reflect.ValueOf(map[int]int{7: 42}) },
			seedKey: func(registers *Registers, present bool) {
				registers.Ints[2] = map[bool]int64{true: 7, false: 999}[present]
			},
		},
		{
			name:    "an int64-keyed int64 map",
			handler: handleMapIndexOkIntInt,
			build:   func() reflect.Value { return reflect.ValueOf(map[int64]int64{7: 42}) },
			seedKey: func(registers *Registers, present bool) {
				registers.Ints[2] = map[bool]int64{true: 7, false: 999}[present]
			},
		},
		{
			name:    "a named integer key on the reflect path",
			handler: handleMapIndexOkIntInt,
			build:   func() reflect.Value { return reflect.ValueOf(map[namedMapKey]int64{7: 42}) },
			seedKey: func(registers *Registers, present bool) {
				registers.Ints[2] = map[bool]int64{true: 7, false: 999}[present]
			},
		},
		{
			name:    "a string-keyed integer map",
			handler: handleMapIndexOkStringInt,
			build:   func() reflect.Value { return reflect.ValueOf(map[string]int{"k": 42}) },
			seedKey: func(registers *Registers, present bool) {
				registers.Strings[2] = map[bool]string{true: "k", false: "absent"}[present]
			},
		},
		{
			name:    "a string-keyed string map",
			handler: handleMapIndexOkStringString,
			build:   func() reflect.Value { return reflect.ValueOf(map[string]string{"k": "v"}) },
			seedKey: func(registers *Registers, present bool) {
				registers.Strings[2] = map[bool]string{true: "k", false: "absent"}[present]
			},
		},
		{
			name:    "an integer-keyed string map",
			handler: handleMapIndexOkIntString,
			build:   func() reflect.Value { return reflect.ValueOf(map[int]string{7: "v"}) },
			seedKey: func(registers *Registers, present bool) {
				registers.Ints[2] = map[bool]int64{true: 7, false: 999}[present]
			},
		},
		{
			name:    "an integer-keyed general map",
			handler: handleMapIndexOkIntGeneral,
			build:   func() reflect.Value { return reflect.ValueOf(map[int][]string{7: {"v"}}) },
			seedKey: func(registers *Registers, present bool) {
				registers.Ints[2] = map[bool]int64{true: 7, false: 999}[present]
			},
		},
		{
			name:    "a string-keyed general map",
			handler: handleMapIndexOkStringGeneral,
			build:   func() reflect.Value { return reflect.ValueOf(map[string][]string{"k": {"v"}}) },
			seedKey: func(registers *Registers, present bool) {
				registers.Strings[2] = map[bool]string{true: "k", false: "absent"}[present]
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" reports a present key", func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newMapOkFrame(t, okRegister)
			registers.General[1] = tt.build()
			tt.seedKey(registers, true)

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			require.Equal(t, int64(1), registers.Ints[okRegister])
			require.Equal(t, 2, frame.ProgramCounter, "the extension word is consumed exactly once")
		})

		t.Run(tt.name+" reports an absent key", func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newMapOkFrame(t, okRegister)
			registers.General[1] = tt.build()
			registers.Ints[okRegister] = 1
			tt.seedKey(registers, false)

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			require.Equal(t, int64(0), registers.Ints[okRegister])
		})
	}
}

func TestTypedMapWritesStoreThroughTheDeclaredKeyType(t *testing.T) {
	t.Parallel()

	t.Run("an integer-keyed integer map", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[int]int{}
		registers.General[0] = reflect.ValueOf(target)
		registers.Ints[1] = 7
		registers.Ints[2] = 42

		require.Equal(t, opContinue, handleMapSetIntInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, 42, target[7])
	})

	t.Run("a named integer key takes the reflect path", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[namedMapKey]int64{}
		registers.General[0] = reflect.ValueOf(target)
		registers.Ints[1] = 7
		registers.Ints[2] = 42

		require.Equal(t, opContinue, handleMapSetIntInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, int64(42), target[7])
	})

	t.Run("a string-keyed integer map", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[string]int{}
		registers.General[0] = reflect.ValueOf(target)
		registers.Strings[1] = "k"
		registers.Ints[2] = 42

		require.Equal(t, opContinue, handleMapSetStringInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, 42, target["k"])
	})

	t.Run("a string-keyed string map", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[string]string{}
		registers.General[0] = reflect.ValueOf(target)
		registers.Strings[1] = "k"
		registers.Strings[2] = "v"

		require.Equal(t, opContinue, handleMapSetStringString(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, "v", target["k"])
	})

	t.Run("an integer-keyed string map", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[int]string{}
		registers.General[0] = reflect.ValueOf(target)
		registers.Ints[1] = 7
		registers.Strings[2] = "v"

		require.Equal(t, opContinue, handleMapSetIntString(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, "v", target[7])
	})

	t.Run("an integer-keyed general map", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[int][]string{}
		registers.General[0] = reflect.ValueOf(target)
		registers.Ints[1] = 7
		registers.General[2] = reflect.ValueOf([]string{"v"})

		require.Equal(t, opContinue, handleMapSetIntGeneral(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, []string{"v"}, target[7])
	})

	t.Run("a string-keyed general map", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[string][]string{}
		registers.General[0] = reflect.ValueOf(target)
		registers.Strings[1] = "k"
		registers.General[2] = reflect.ValueOf([]string{"v"})

		require.Equal(t, opContinue, handleMapSetStringGeneral(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, []string{"v"}, target["k"])
	})

	t.Run("a write replaces an existing entry", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[int]int{7: 1}
		registers.General[0] = reflect.ValueOf(target)
		registers.Ints[1] = 7
		registers.Ints[2] = 42

		require.Equal(t, opContinue, handleMapSetIntInt(vm, frame, registers, op(0, 1, 2)))
		require.Len(t, target, 1)
		require.Equal(t, 42, target[7])
	})
}

func TestTypedMapAccumulationAddsToTheExistingValue(t *testing.T) {
	t.Parallel()

	t.Run("an integer key accumulates onto a present entry", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[int]int{7: 10}
		registers.General[0] = reflect.ValueOf(target)
		registers.Ints[1] = 7
		registers.Ints[2] = 5

		require.Equal(t, opContinue, handleMapAddIntInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, 15, target[7])
	})

	t.Run("an integer key seeds an absent entry from zero", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[int]int{}
		registers.General[0] = reflect.ValueOf(target)
		registers.Ints[1] = 7
		registers.Ints[2] = 5

		require.Equal(t, opContinue, handleMapAddIntInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, 5, target[7], "an absent key accumulates onto the zero value")
	})

	t.Run("a string key accumulates onto a present entry", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[string]int{"k": 10}
		registers.General[0] = reflect.ValueOf(target)
		registers.Strings[1] = "k"
		registers.Ints[2] = 5

		require.Equal(t, opContinue, handleMapAddStringInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, 15, target["k"])
	})

	t.Run("a string key seeds an absent entry from zero", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[string]int{}
		registers.General[0] = reflect.ValueOf(target)
		registers.Strings[1] = "new"
		registers.Ints[2] = 5

		require.Equal(t, opContinue, handleMapAddStringInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, 5, target["new"])
	})

	t.Run("a negative addend subtracts", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[int]int{7: 10}
		registers.General[0] = reflect.ValueOf(target)
		registers.Ints[1] = 7
		registers.Ints[2] = -4

		require.Equal(t, opContinue, handleMapAddIntInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, 6, target[7])
	})
}

func TestTypedMapHandlersRefuseANonMapRegister(t *testing.T) {
	t.Parallel()

	readers := []struct {
		handler opcodeHandler
		name    string
	}{
		{name: "the integer to integer read", handler: handleMapGetIntInt},
		{name: "the string to integer read", handler: handleMapGetStringInt},
		{name: "the string to string read", handler: handleMapGetStringString},
		{name: "the integer to string read", handler: handleMapGetIntString},
		{name: "the integer to general read", handler: handleMapGetIntGeneral},
		{name: "the string to general read", handler: handleMapGetStringGeneral},
	}

	for _, tt := range readers {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newStandardVM(t)
			registers.General[1] = reflect.ValueOf("not a map")

			got := tt.handler(vm, frame, registers, op(0, 1, 2))

			require.Equal(t, opPanicError, got)
			require.Error(t, vm.evalError)
		})
	}

	writers := []struct {
		handler opcodeHandler
		name    string
	}{
		{name: "the integer to integer write", handler: handleMapSetIntInt},
		{name: "the string to integer write", handler: handleMapSetStringInt},
		{name: "the string to string write", handler: handleMapSetStringString},
		{name: "the integer to string write", handler: handleMapSetIntString},
	}

	for _, tt := range writers {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newStandardVM(t)
			registers.General[0] = reflect.ValueOf("not a map")

			got := tt.handler(vm, frame, registers, op(0, 1, 2))

			require.Equal(t, opPanicError, got)
			require.Error(t, vm.evalError)
		})
	}
}
