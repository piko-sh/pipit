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
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

const (
	wideGlobalRegister = 1
	wideGlobalCount    = 300
)

func globalRouteStore() *GlobalStore {
	globals := NewGlobalStore()
	pointee := 5
	for index := range 3 {
		globals.AllocInt(int64(index) - 100)
		globals.AllocFloat(float64(index) + 0.5)
		globals.AllocString(fmt.Sprint("global-", index))
		globals.AllocBool(index == 1)
		globals.AllocUint(math.MaxUint64 - uint64(index))
		globals.AllocGeneral(reflect.ValueOf([]any{&pointee, index, fmt.Sprint(index)}[index]))
		globals.AllocComplex(complex(float64(index), -1))
	}
	return globals
}

func wideGlobalRouteStore() *GlobalStore {
	globals := globalRouteStore()
	for index := 3; index < wideGlobalCount; index++ {
		globals.AllocInt(int64(index) - 100)
		globals.AllocUint(uint64(index))
		globals.AllocFloat(float64(index) + 0.5)
		globals.AllocBool(index%2 == 1)
	}
	return globals
}

func wideGlobalFrame(t *testing.T, operation isa.SubOpcode, kind isa.RegisterKind, slot int) (*VM, *CallFrame, *Registers, isa.Instruction) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	instruction := isa.NewTier1Instruction(operation, wideGlobalRegister, uint8(kind))
	builder.body = append(builder.body, instruction, isa.NewInstruction(isa.OpExtension, uint8(slot&0xFF), uint8(slot>>8), 0))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	vm, frame, registers := newFramedVM(t, builder.build())
	vm.Globals = wideGlobalRouteStore()
	return vm, frame, registers, instruction
}

func TestGetGlobalWideReadsTheSlotItsExtensionWordNames(t *testing.T) {
	tests := []struct {
		read func(registers *Registers) any
		want any
		name string
		kind isa.RegisterKind
		slot int
	}{
		{
			name: "the last int", kind: isa.RegisterInt64, slot: wideGlobalCount - 1, want: int64(199),
			read: func(registers *Registers) any { return registers.Ints[wideGlobalRegister] },
		},
		{
			name: "an int below 256", kind: isa.RegisterInt64, slot: 2, want: int64(-98),
			read: func(registers *Registers) any { return registers.Ints[wideGlobalRegister] },
		},
		{
			name: "a uint past 255", kind: isa.RegisterUint64, slot: 256, want: uint64(256),
			read: func(registers *Registers) any { return registers.Uints[wideGlobalRegister] },
		},
		{
			name: "a float past 255", kind: isa.RegisterFloat64, slot: 298, want: 298.5,
			read: func(registers *Registers) any { return registers.Floats[wideGlobalRegister] },
		},
		{
			name: "a bool past 255", kind: isa.RegisterBool, slot: 257, want: true,
			read: func(registers *Registers) any { return registers.Bools[wideGlobalRegister] },
		},
		{
			name: "a string", kind: isa.RegisterString, slot: 2, want: "global-2",
			read: func(registers *Registers) any { return registers.Strings[wideGlobalRegister] },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, assembly := range dispatchRoutes() {
				vm, frame, registers, instruction := wideGlobalFrame(t, isa.SubOpGetGlobalWide, tt.kind, tt.slot)

				runOneInstruction(t, vm, frame, registers, instruction, assembly)

				require.NoError(t, vm.evalError, routeName(assembly))
				require.Equal(t, tt.want, tt.read(registers), routeName(assembly))
			}
		})
	}
}

func TestSetGlobalWideStoresIntoTheSlotItsExtensionWordNames(t *testing.T) {
	tests := []struct {
		stored func(globals *GlobalStore) any
		want   any
		name   string
		kind   isa.RegisterKind
		slot   int
	}{
		{
			name: "the last int", kind: isa.RegisterInt64, slot: wideGlobalCount - 1, want: int64(-77),
			stored: func(globals *GlobalStore) any { return globals.ints[wideGlobalCount-1] },
		},
		{
			name: "a uint past 255", kind: isa.RegisterUint64, slot: 256, want: uint64(math.MaxUint64 - 77),
			stored: func(globals *GlobalStore) any { return globals.uints[256] },
		},
		{
			name: "a float", kind: isa.RegisterFloat64, slot: 3, want: -77.5,
			stored: func(globals *GlobalStore) any { return globals.floats[3] },
		},
		{
			name: "a bool that was false", kind: isa.RegisterBool, slot: 4, want: true,
			stored: func(globals *GlobalStore) any { return globals.bools[4] },
		},
		{
			name: "a string", kind: isa.RegisterString, slot: 1, want: "stored",
			stored: func(globals *GlobalStore) any { return globals.Strings[1] },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, assembly := range dispatchRoutes() {
				vm, frame, registers, instruction := wideGlobalFrame(t, isa.SubOpSetGlobalWide, tt.kind, tt.slot)
				registers.Ints[wideGlobalRegister], registers.Uints[wideGlobalRegister] = -77, math.MaxUint64-77
				registers.Floats[wideGlobalRegister], registers.Bools[wideGlobalRegister], registers.Strings[wideGlobalRegister] = -77.5, true, "stored"
				require.NotEqual(t, tt.want, tt.stored(vm.Globals), "the variable holds something else before the store")

				runOneInstruction(t, vm, frame, registers, instruction, assembly)

				require.NoError(t, vm.evalError, routeName(assembly))
				require.Equal(t, tt.want, tt.stored(vm.Globals), routeName(assembly))
			}
		})
	}
}

func TestWideGlobalHandlersRefuseASlotPastTheEndOfItsBank(t *testing.T) {
	handlers := []struct {
		handler   opcodeHandler
		name      string
		operation isa.SubOpcode
	}{
		{name: "a read", operation: isa.SubOpGetGlobalWide, handler: handleGetGlobalWide},
		{name: "a store", operation: isa.SubOpSetGlobalWide, handler: handleSetGlobalWide},
	}
	for _, tt := range handlers {
		t.Run(tt.name, func(t *testing.T) {
			vm, frame, registers, instruction := wideGlobalFrame(t, tt.operation, isa.RegisterInt64, wideGlobalCount)
			registers.Ints[wideGlobalRegister] = -7
			before := fmt.Sprint(vm.Globals.ints)
			frame.ProgramCounter = 1

			result := tt.handler(vm, frame, registers, instruction)

			require.Equal(t, opPanicError, result)
			require.Error(t, vm.evalError)
			require.EqualValues(t, -7, registers.Ints[wideGlobalRegister], "nothing is read")
			require.Equal(t, before, fmt.Sprint(vm.Globals.ints), "nothing is stored")
		})
	}
}
