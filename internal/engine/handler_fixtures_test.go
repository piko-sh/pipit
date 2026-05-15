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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func op(a, b, c uint8) isa.Instruction {
	return isa.Instruction{A: a, B: b, C: c}
}

func newFramedVM(t *testing.T, compiledFunction *program.CompiledFunction) (*VM, *CallFrame, *Registers) {
	t.Helper()
	vm := newTestVM(t)
	vm.prepareForExecution(compiledFunction)
	t.Cleanup(vm.ReleaseArena)
	vm.PushFrame(compiledFunction)
	frame := &vm.CallStack[vm.FramePointer]
	registers := &frame.Registers
	clearRegisterBanks(registers)
	return vm, frame, registers
}

func clearRegisterBanks(registers *Registers) {

	clear(registers.Ints)
	clear(registers.Floats)
	clear(registers.Strings)
	clear(registers.General)
	clear(registers.Bools)
	clear(registers.Uints)
	clear(registers.Complex)
	clear(registers.SlicesInt)
	clear(registers.slicesFloat)
	clear(registers.slicesString)
	clear(registers.slicesBool)
	clear(registers.slicesUint)
	clear(registers.slicesByte)
}

func newBankedVM(t *testing.T, counts [isa.NumRegisterKinds]uint32) (*VM, *CallFrame, *Registers) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = counts
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	return newFramedVM(t, builder.build())
}

func newStandardVM(t *testing.T) (*VM, *CallFrame, *Registers) {
	t.Helper()
	return newBankedVM(t, wideRegCounts(8))
}

func newExtWordFrame(t *testing.T, words ...isa.Instruction) (*VM, *CallFrame, *Registers) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.Emit(isa.OpDrillTier1, 0, 0, 0)
	builder.body = append(builder.body, words...)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	vm, frame, registers := newFramedVM(t, builder.build())
	frame.ProgramCounter = 1
	return vm, frame, registers
}

func requireEvalError(t *testing.T, vm *VM, got OpResult, target error) {
	t.Helper()
	require.Equal(t, opPanicError, got, "the handler must route the fault through the dispatch loop")
	require.Error(t, vm.evalError, "opPanicError must leave an error on the VM")
	if target != nil {
		require.ErrorIs(t, vm.evalError, target)
	}
}

func requireRuntimePanic(t *testing.T, vm *VM, got OpResult, message string) {
	t.Helper()
	require.NotEqual(t, opContinue, got, "a runtime fault must abandon the fast path")
	require.Error(t, vm.evalError, "the raised panic must surface as an error")
	require.Contains(t, vm.evalError.Error(), message)
}

func newTestArena(t *testing.T) *RegisterArena {
	t.Helper()
	arena := GetRegisterArena()
	t.Cleanup(func() {
		arena.MaxArenaBytes = 0
		arena.MaxAllocSize = 0
		PutRegisterArena(arena)
	})
	return arena
}

func newBudgetedArena(t *testing.T, maxBytes uint64) *RegisterArena {
	t.Helper()
	arena := newTestArena(t)
	arena.MaxArenaBytes = maxBytes
	return arena
}

func standardRegisters() Registers {
	return NewRegisters(wideRegCounts(8))
}

func wideRegCounts(n uint32) [isa.NumRegisterKinds]uint32 {
	var counts [isa.NumRegisterKinds]uint32
	for kind := range counts {
		counts[kind] = n
	}
	return counts
}
