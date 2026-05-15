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

type namedMapKey int

type namedStringKey string

const (
	fusedMapValueRegister = 0
	fusedMapMapRegister   = 1
	fusedMapKeyRegister   = 2
	fusedMapOkRegister    = 3
	fusedMapMissOffset    = 7
)

func newFusedMapFrame(t *testing.T) (*VM, *CallFrame, *Registers) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.Emit(isa.OpMapIndexOkIntInt, fusedMapValueRegister, fusedMapMapRegister, fusedMapKeyRegister)
	low, high := isa.SplitOffset(fusedMapMissOffset)
	builder.body = append(builder.body, isa.NewInstruction(isa.OpExt, fusedMapOkRegister, low, high))
	builder.Emit(isa.OpNop, 0, 0, 0)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	vm, frame, registers := newFramedVM(t, builder.build())
	frame.ProgramCounter = 1
	return vm, frame, registers
}

func TestFusedMapLookupWritesTheValueAndTheOkBitOnAHit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		build   func() reflect.Value
		check   func(t *testing.T, registers *Registers)
		handler opcodeHandler
		name    string
		key     any
	}{
		{
			name:    "an integer-keyed integer map on its fast path",
			handler: handleMapIndexOkJumpIfFalseIntInt,
			build:   func() reflect.Value { return reflect.ValueOf(map[int]int{7: 42}) },
			key:     int64(7),
			check:   func(t *testing.T, registers *Registers) { require.Equal(t, int64(42), registers.Ints[0]) },
		},
		{
			name:    "an int64-keyed int64 map on its fast path",
			handler: handleMapIndexOkJumpIfFalseIntInt,
			build:   func() reflect.Value { return reflect.ValueOf(map[int64]int64{7: 42}) },
			key:     int64(7),
			check:   func(t *testing.T, registers *Registers) { require.Equal(t, int64(42), registers.Ints[0]) },
		},
		{
			name:    "a named integer key falls back to reflect",
			handler: handleMapIndexOkJumpIfFalseIntInt,
			build:   func() reflect.Value { return reflect.ValueOf(map[namedMapKey]int64{7: 42}) },
			key:     int64(7),
			check:   func(t *testing.T, registers *Registers) { require.Equal(t, int64(42), registers.Ints[0]) },
		},
		{
			name:    "a string-keyed integer map on its fast path",
			handler: handleMapIndexOkJumpIfFalseStringInt,
			build:   func() reflect.Value { return reflect.ValueOf(map[string]int{"k": 42}) },
			key:     "k",
			check:   func(t *testing.T, registers *Registers) { require.Equal(t, int64(42), registers.Ints[0]) },
		},
		{
			name:    "a named string key falls back to reflect",
			handler: handleMapIndexOkJumpIfFalseStringInt,
			build:   func() reflect.Value { return reflect.ValueOf(map[namedStringKey]int64{"k": 42}) },
			key:     "k",
			check:   func(t *testing.T, registers *Registers) { require.Equal(t, int64(42), registers.Ints[0]) },
		},
		{
			name:    "a string-keyed string map on its fast path",
			handler: handleMapIndexOkJumpIfFalseStringString,
			build:   func() reflect.Value { return reflect.ValueOf(map[string]string{"k": "v"}) },
			key:     "k",
			check:   func(t *testing.T, registers *Registers) { require.Equal(t, "v", registers.Strings[0]) },
		},
		{
			name:    "an integer-keyed string map",
			handler: handleMapIndexOkJumpIfFalseIntString,
			build:   func() reflect.Value { return reflect.ValueOf(map[int]string{7: "v"}) },
			key:     int64(7),
			check:   func(t *testing.T, registers *Registers) { require.Equal(t, "v", registers.Strings[0]) },
		},
		{
			name:    "an integer-keyed general map",
			handler: handleMapIndexOkJumpIfFalseIntGeneral,
			build:   func() reflect.Value { return reflect.ValueOf(map[int][]string{7: {"v"}}) },
			key:     int64(7),
			check: func(t *testing.T, registers *Registers) {
				require.Equal(t, 1, registers.General[0].Len())
				require.Equal(t, "v", registers.General[0].Index(0).String())
			},
		},
		{
			name:    "a string-keyed general map",
			handler: handleMapIndexOkJumpIfFalseStringGeneral,
			build:   func() reflect.Value { return reflect.ValueOf(map[string][]string{"k": {"v"}}) },
			key:     "k",
			check: func(t *testing.T, registers *Registers) {
				require.Equal(t, 1, registers.General[0].Len())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newFusedMapFrame(t)
			registers.General[fusedMapMapRegister] = tt.build()
			switch key := tt.key.(type) {
			case int64:
				registers.Ints[fusedMapKeyRegister] = key
			case string:
				registers.Strings[fusedMapKeyRegister] = key
			}

			got := tt.handler(vm, frame, registers, op(fusedMapValueRegister, fusedMapMapRegister, fusedMapKeyRegister))

			require.Equal(t, opContinue, got)
			tt.check(t, registers)
			require.Equal(t, int64(1), registers.Ints[fusedMapOkRegister], "a hit sets the ok bit")
			require.Equal(t, 2, frame.ProgramCounter, "a hit advances past the extension word only")
		})
	}
}

func TestFusedMapLookupTakesTheJumpAndZeroesTheValueOnAMiss(t *testing.T) {
	t.Parallel()

	tests := []struct {
		build   func() reflect.Value
		check   func(t *testing.T, registers *Registers)
		handler opcodeHandler
		name    string
		key     any
	}{
		{
			name:    "an integer-keyed integer map",
			handler: handleMapIndexOkJumpIfFalseIntInt,
			build:   func() reflect.Value { return reflect.ValueOf(map[int]int{7: 42}) },
			key:     int64(99),
			check:   func(t *testing.T, registers *Registers) { require.Equal(t, int64(0), registers.Ints[0]) },
		},
		{
			name:    "a named integer key on the reflect path",
			handler: handleMapIndexOkJumpIfFalseIntInt,
			build:   func() reflect.Value { return reflect.ValueOf(map[namedMapKey]int64{7: 42}) },
			key:     int64(99),
			check:   func(t *testing.T, registers *Registers) { require.Equal(t, int64(0), registers.Ints[0]) },
		},
		{
			name:    "a string-keyed integer map",
			handler: handleMapIndexOkJumpIfFalseStringInt,
			build:   func() reflect.Value { return reflect.ValueOf(map[string]int{"k": 42}) },
			key:     "absent",
			check:   func(t *testing.T, registers *Registers) { require.Equal(t, int64(0), registers.Ints[0]) },
		},
		{
			name:    "a string-keyed string map",
			handler: handleMapIndexOkJumpIfFalseStringString,
			build:   func() reflect.Value { return reflect.ValueOf(map[string]string{"k": "v"}) },
			key:     "absent",
			check:   func(t *testing.T, registers *Registers) { require.Empty(t, registers.Strings[0]) },
		},
		{
			name:    "an integer-keyed string map",
			handler: handleMapIndexOkJumpIfFalseIntString,
			build:   func() reflect.Value { return reflect.ValueOf(map[int]string{7: "v"}) },
			key:     int64(99),
			check:   func(t *testing.T, registers *Registers) { require.Empty(t, registers.Strings[0]) },
		},
		{
			name:    "an integer-keyed general map yields the element zero value",
			handler: handleMapIndexOkJumpIfFalseIntGeneral,
			build:   func() reflect.Value { return reflect.ValueOf(map[int][]string{7: {"v"}}) },
			key:     int64(99),
			check: func(t *testing.T, registers *Registers) {
				require.True(t, registers.General[0].IsValid(), "a miss still writes a typed zero")
				require.Equal(t, reflect.Slice, registers.General[0].Kind())
			},
		},
		{
			name:    "a string-keyed general map yields the element zero value",
			handler: handleMapIndexOkJumpIfFalseStringGeneral,
			build:   func() reflect.Value { return reflect.ValueOf(map[string][]string{"k": {"v"}}) },
			key:     "absent",
			check: func(t *testing.T, registers *Registers) {
				require.True(t, registers.General[0].IsValid())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newFusedMapFrame(t)
			registers.General[fusedMapMapRegister] = tt.build()
			registers.Ints[fusedMapOkRegister] = 1
			switch key := tt.key.(type) {
			case int64:
				registers.Ints[fusedMapKeyRegister] = key
			case string:
				registers.Strings[fusedMapKeyRegister] = key
			}

			got := tt.handler(vm, frame, registers, op(fusedMapValueRegister, fusedMapMapRegister, fusedMapKeyRegister))

			require.Equal(t, opContinue, got)
			tt.check(t, registers)
			require.Equal(t, int64(0), registers.Ints[fusedMapOkRegister], "a miss clears the ok bit")
			require.Equal(t, 2+fusedMapMissOffset, frame.ProgramCounter,
				"a miss advances past the extension word and then takes the packed jump")
		})
	}
}

func TestFusedMapLookupOnAnEmptyMapAlwaysMisses(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newFusedMapFrame(t)
	registers.General[fusedMapMapRegister] = reflect.ValueOf(map[int]int{})
	registers.Ints[fusedMapKeyRegister] = 0

	got := handleMapIndexOkJumpIfFalseIntInt(vm, frame, registers, op(fusedMapValueRegister, fusedMapMapRegister, fusedMapKeyRegister))

	require.Equal(t, opContinue, got)
	require.Equal(t, int64(0), registers.Ints[fusedMapOkRegister])
	require.Equal(t, 2+fusedMapMissOffset, frame.ProgramCounter)
}

func TestFusedMapLookupRefusesANonMapRegister(t *testing.T) {
	t.Parallel()

	handlers := []struct {
		handler opcodeHandler
		name    string
	}{
		{name: "the integer to integer form", handler: handleMapIndexOkJumpIfFalseIntInt},
		{name: "the string to integer form", handler: handleMapIndexOkJumpIfFalseStringInt},
		{name: "the string to string form", handler: handleMapIndexOkJumpIfFalseStringString},
		{name: "the integer to string form", handler: handleMapIndexOkJumpIfFalseIntString},
		{name: "the integer to general form", handler: handleMapIndexOkJumpIfFalseIntGeneral},
		{name: "the string to general form", handler: handleMapIndexOkJumpIfFalseStringGeneral},
	}

	for _, tt := range handlers {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newFusedMapFrame(t)
			registers.General[fusedMapMapRegister] = reflect.ValueOf("not a map")

			got := tt.handler(vm, frame, registers, op(fusedMapValueRegister, fusedMapMapRegister, fusedMapKeyRegister))

			require.Equal(t, opPanicError, got, "a non-map operand must abandon the fused path")
			require.Error(t, vm.evalError)
			require.Equal(t, 1, frame.ProgramCounter,
				"the refusal happens before the extension word is consumed")
		})
	}
}

func TestFusedMapLookupOffsetPackingRoundTrips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		offset int16
	}{
		{name: "a forward jump", offset: 7},
		{name: "the smallest forward jump", offset: 1},
		{name: "a backward jump", offset: -12},
		{name: "a large forward jump", offset: 30000},
		{name: "a large backward jump", offset: -30000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			low, high := isa.SplitOffset(tt.offset)
			require.Equal(t, tt.offset, isa.JoinOffset(low, high),
				"the fused jump offset must survive the extension-word packing")
		})
	}
}
