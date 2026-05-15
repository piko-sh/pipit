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

//go:build !safe

package engine

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

func stringDataAddress(s string) uintptr {
	return uintptr(unsafe.Pointer(unsafe.StringData(s)))
}

func newHandlerTestVM(t *testing.T) (*VM, *Registers) {
	t.Helper()
	vm := newTestVM(t)
	vm.Arena = NewRegisterArena()
	registers := &Registers{
		Ints:    make([]int64, 4),
		Floats:  make([]float64, 4),
		Strings: make([]string, 4),
		General: make([]reflect.Value, 4),
		Bools:   make([]bool, 4),
		Uints:   make([]uint64, 4),
	}
	return vm, registers
}

func TestPolymorphicCallBoxesLargeIntWithoutAllocating(t *testing.T) {
	arena := NewRegisterArena()
	source := &Registers{Ints: []int64{1 << 40}}
	destination := &Registers{General: make([]reflect.Value, 1)}

	allocs := testing.AllocsPerRun(100, func() {
		copyOneCallArgument(destination, source, isa.RegisterGeneral, isa.RegisterInt, 0, 0, arena)
	})
	require.Zero(t, allocs)
	require.EqualValues(t, 1<<40, destination.General[0].Int())
}

func TestTypeAssertStructDoesNotAllocate(t *testing.T) {
	type pair struct {
		A, B int64
	}
	vm, registers := newHandlerTestVM(t)
	function := &program.CompiledFunction{
		Body:      []isa.Instruction{isa.NewInstruction(0, 0, 0, 0)},
		TypeTable: []reflect.Type{reflect.TypeFor[pair]()},
	}
	frame := &CallFrame{Function: function}
	registers.General[1] = reflect.ValueOf(pair{A: 7, B: 9})
	assertion := isa.NewInstruction(isa.OpTypeAssert, 0, 1, 2)

	allocs := testing.AllocsPerRun(100, func() {
		frame.ProgramCounter = 0
		require.Equal(t, opContinue, HandleTypeAssert(vm, frame, registers, assertion))
	})
	require.Zero(t, allocs)
	require.EqualValues(t, 1, registers.Ints[2])
	require.EqualValues(t, 9, registers.General[0].Field(1).Int())
	require.True(t, vm.Arena.ownsBytePointer(ReflectValuePtr(registers.General[0])), "the asserted copy lives in the arena")
}

func TestChannelBufferedSendReceiveAllocs(t *testing.T) {
	vm, registers := newHandlerTestVM(t)
	registers.General[0] = reflect.ValueOf(make(chan int, 1))
	sendFrame := &CallFrame{Function: &program.CompiledFunction{}}
	receiveFunction := &program.CompiledFunction{Body: []isa.Instruction{isa.NewInstruction(0, 1, uint8(isa.RegisterInt), 0)}}
	receiveFrame := &CallFrame{Function: receiveFunction}
	send := isa.NewInstruction(isa.OpChannelSend, 0, 2, uint8(isa.RegisterInt))
	receive := isa.Instruction{A: 0, B: 3}

	registers.Ints[2] = 41
	require.Equal(t, opContinue, HandleChannelSend(vm, sendFrame, registers, send))
	receiveFrame.ProgramCounter = 0
	require.Equal(t, opContinue, handleChannelReceive(vm, receiveFrame, registers, receive))
	require.EqualValues(t, 41, registers.Ints[1])
	require.EqualValues(t, 1, registers.Ints[3])

	allocs := testing.AllocsPerRun(200, func() {
		registers.Ints[2]++
		HandleChannelSend(vm, sendFrame, registers, send)
		receiveFrame.ProgramCounter = 0
		handleChannelReceive(vm, receiveFrame, registers, receive)
	})
	require.LessOrEqual(t, allocs, 1.0)
	require.Equal(t, registers.Ints[2], registers.Ints[1])
}

func TestChannelReceiveConsumesReadyValueBeforeCancellation(t *testing.T) {
	t.Parallel()
	vm, registers := newHandlerTestVM(t)
	cancelled, cancel := NewExecutionContext(vm.executionContext())
	cancel(fault.ErrMainReturned)
	vm.setExecutionContext(cancelled)
	channel := make(chan int, 1)
	channel <- 5
	registers.General[0] = reflect.ValueOf(channel)
	receiveFrame := &CallFrame{Function: &program.CompiledFunction{Body: []isa.Instruction{isa.NewInstruction(0, 1, uint8(isa.RegisterInt), 0)}}}

	require.Equal(t, opContinue, handleChannelReceive(vm, receiveFrame, registers, isa.Instruction{A: 0, B: 3}))
	require.EqualValues(t, 5, registers.Ints[1])
	require.EqualValues(t, 1, registers.Ints[3])
}

func TestMapSetStringHitDoesNotAllocate(t *testing.T) {
	vm, registers := newHandlerTestVM(t)
	frame := &CallFrame{Function: &program.CompiledFunction{}}
	counts := map[string]int{}
	registers.General[0] = reflect.ValueOf(counts)
	registers.Strings[1] = ArenaConcatString(vm.Arena, "wo", "rd")
	require.True(t, vm.Arena.OwnsString(registers.Strings[1]))
	registers.Ints[2] = 1
	set := isa.NewInstruction(isa.OpMapSetStringInt, 0, 1, 2)
	add := isa.NewInstruction(isa.OpMapAddStringInt, 0, 1, 2)
	require.Equal(t, opContinue, HandleMapSetStringInt(vm, frame, registers, set))

	allocs := testing.AllocsPerRun(100, func() {
		HandleMapSetStringInt(vm, frame, registers, set)
		HandleMapAddStringInt(vm, frame, registers, add)
	})
	require.Zero(t, allocs)
	require.Equal(t, 2, counts["word"])
	for key := range counts {
		require.False(t, vm.Arena.OwnsString(key), "stored key must stay heap-owned")
	}

	values := map[string]string{}
	registers.General[0] = reflect.ValueOf(values)
	registers.Strings[2] = "heap value"
	setString := isa.NewInstruction(isa.OpMapSetStringString, 0, 1, 2)
	require.Equal(t, opContinue, HandleMapSetStringString(vm, frame, registers, setString))
	allocs = testing.AllocsPerRun(100, func() {
		HandleMapSetStringString(vm, frame, registers, setString)
	})
	require.Zero(t, allocs)
	require.Equal(t, "heap value", values["word"])
}

func TestMapSetStringMissClonesArenaKey(t *testing.T) {
	t.Parallel()
	vm, registers := newHandlerTestVM(t)
	frame := &CallFrame{Function: &program.CompiledFunction{}}
	counts := map[string]int64{}
	registers.General[0] = reflect.ValueOf(counts)
	arenaKey := ArenaConcatString(vm.Arena, "ke", "y1")
	registers.Strings[1] = arenaKey
	registers.Ints[2] = 3
	require.Equal(t, opContinue, HandleMapSetStringInt(vm, frame, registers, isa.NewInstruction(isa.OpMapSetStringInt, 0, 1, 2)))

	var storedKey string
	for key := range counts {
		storedKey = key
	}
	require.Equal(t, "key1", storedKey)
	require.False(t, vm.Arena.OwnsString(storedKey), "a miss must clone the arena key")
	require.NotEqual(t, stringDataAddress(arenaKey), stringDataAddress(storedKey))

	registers.Strings[1] = ArenaConcatString(vm.Arena, "key", "1")
	registers.Ints[2] = 4
	require.Equal(t, opContinue, HandleMapSetStringInt(vm, frame, registers, isa.NewInstruction(isa.OpMapSetStringInt, 0, 1, 2)))
	for key := range counts {
		require.Equal(t, stringDataAddress(storedKey), stringDataAddress(key), "a hit must keep the map's own key")
	}
	require.EqualValues(t, 4, counts["key1"])

	type tagged struct {
		label string
	}
	general := map[string]tagged{}
	registers.General[0] = reflect.ValueOf(general)
	registers.General[2] = reflect.ValueOf(tagged{label: ArenaConcatString(vm.Arena, "arena ", "value")})
	require.Equal(t, opContinue, HandleMapSetStringGeneral(vm, frame, registers, isa.NewInstruction(isa.OpMapSetStringGeneral, 0, 1, 2)))
	for key, value := range general {
		require.False(t, vm.Arena.OwnsString(key))
		require.Equal(t, "arena value", value.label)
		require.False(t, vm.Arena.OwnsString(value.label), "general values must be materialised")
	}
}
