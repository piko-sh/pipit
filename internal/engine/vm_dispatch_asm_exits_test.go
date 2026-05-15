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

//go:build !safe && !(js && wasm) && (amd64 || arm64)

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type dispatchExitFunc func(*VM, *CallFrame, *Registers, *dispatchContext) (any, dispatchAction, error)

func newDispatchExitFrame(t *testing.T, instruction isa.Instruction) (*VM, *CallFrame, *Registers, *dispatchContext) {
	t.Helper()

	structType := reflect.TypeFor[wideFields]()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.body = append(builder.body, instruction)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	layouts := make([]program.StructFieldLayout, 0, structType.NumField())
	typeTable := make([]reflect.Type, 0, structType.NumField()+1)
	typeTable = append(typeTable, structType)
	for index := range structType.NumField() {
		layouts = append(layouts, wideFieldLayout(t, index))
		typeTable = append(typeTable, structType.Field(index).Type)
	}
	builder.typeTable = typeTable

	compiledFunction := builder.build()
	compiledFunction.StructLayoutTable = layouts

	vm, frame, registers := newFramedVM(t, compiledFunction)
	context := &dispatchContext{}
	vm.buildDispatchContext(context, nil)
	return vm, frame, registers, context
}

func TestDispatchExitHandlersRunTheirOwnInstruction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		exit        dispatchExitFunc
		instruction isa.Instruction
		load        func(*Registers, *recordHolder)
		inspect     func(*testing.T, *Registers, *recordHolder)
	}{
		{
			name: "a general field read", exit: (*VM).processExitGetField,
			instruction: isa.NewInstruction(isa.OpGetField, 0, 1, 1),
			load:        func(r *Registers, h *recordHolder) { h.Label = "read"; r.General[1] = reflect.ValueOf(h) },
			inspect:     func(t *testing.T, r *Registers, _ *recordHolder) { require.Equal(t, "read", r.General[0].Interface()) },
		},
		{
			name: "a general field write", exit: (*VM).processExitSetField,
			instruction: isa.NewInstruction(isa.OpSetField, 0, 1, 2),
			load: func(r *Registers, h *recordHolder) {
				r.General[0] = reflect.ValueOf(h)
				r.General[2] = reflect.ValueOf("written")
			},
			inspect: func(t *testing.T, _ *Registers, h *recordHolder) { require.Equal(t, "written", h.Label) },
		},
		{
			name: "a map index", exit: (*VM).processExitMapIndex,
			instruction: isa.NewInstruction(isa.OpMapIndex, 0, 1, 2),
			load: func(r *Registers, _ *recordHolder) {
				r.General[1] = reflect.ValueOf(map[string]int{"a": 5})
				r.General[2] = reflect.ValueOf("a")
			},
			inspect: func(t *testing.T, r *Registers, _ *recordHolder) { require.Equal(t, 5, r.General[0].Interface()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers, context := newDispatchExitFrame(t, tt.instruction)
			holder := &recordHolder{}
			tt.load(registers, holder)

			_, action, err := tt.exit(vm, frame, registers, context)
			require.NoError(t, err)
			require.Equal(t, loopContinue, action,
				"a clean instruction leaves the counter advanced and hands control back to the assembly loop")
			tt.inspect(t, registers, holder)
		})
	}
}

func TestTier0StructFieldDispatchExitsReadAndWriteTheirBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		exit        dispatchExitFunc
		instruction isa.Instruction
		load        func(*Registers, *wideFields)
		inspect     func(*testing.T, *Registers, *wideFields)
	}{
		{
			name: "an int read", exit: (*VM).processExitGetStructFieldIntT0,
			instruction: isa.NewInstruction(isa.OpGetStructFieldIntT0, 0, 1, 0),
			load:        func(r *Registers, w *wideFields) { w.Signed = 42; r.General[1] = reflect.ValueOf(w) },
			inspect:     func(t *testing.T, r *Registers, _ *wideFields) { require.Equal(t, int64(42), r.Ints[0]) },
		},
		{
			name: "a uint read", exit: (*VM).processExitGetStructFieldUintT0,
			instruction: isa.NewInstruction(isa.OpGetStructFieldUint, 0, 1, 1),
			load:        func(r *Registers, w *wideFields) { w.Unsigned = 7; r.General[1] = reflect.ValueOf(w) },
			inspect:     func(t *testing.T, r *Registers, _ *wideFields) { require.Equal(t, uint64(7), r.Uints[0]) },
		},
		{
			name: "a float read", exit: (*VM).processExitGetStructFieldFloatT0,
			instruction: isa.NewInstruction(isa.OpGetStructFieldFloat, 0, 1, 2),
			load:        func(r *Registers, w *wideFields) { w.Ratio = 1.5; r.General[1] = reflect.ValueOf(w) },
			inspect:     func(t *testing.T, r *Registers, _ *wideFields) { require.InDelta(t, 1.5, r.Floats[0], 0) },
		},
		{
			name: "a bool read", exit: (*VM).processExitGetStructFieldBoolT0,
			instruction: isa.NewInstruction(isa.OpGetStructFieldBool, 0, 1, 3),
			load:        func(r *Registers, w *wideFields) { w.Flag = true; r.General[1] = reflect.ValueOf(w) },
			inspect:     func(t *testing.T, r *Registers, _ *wideFields) { require.True(t, r.Bools[0]) },
		},
		{
			name: "a general read", exit: (*VM).processExitGetStructFieldGeneralT0,
			instruction: isa.NewInstruction(isa.OpGetStructFieldGeneral, 0, 1, 4),
			load:        func(r *Registers, w *wideFields) { w.Label = "read"; r.General[1] = reflect.ValueOf(w) },
			inspect: func(t *testing.T, r *Registers, _ *wideFields) {
				require.Equal(t, "read", r.General[0].Interface())
			},
		},
		{
			name: "an int write", exit: (*VM).processExitSetStructFieldIntT0,
			instruction: isa.NewInstruction(isa.OpSetStructFieldIntT0, 1, 2, 0),
			load:        func(r *Registers, w *wideFields) { r.General[1] = reflect.ValueOf(w); r.Ints[2] = 11 },
			inspect:     func(t *testing.T, _ *Registers, w *wideFields) { require.Equal(t, int64(11), w.Signed) },
		},
		{
			name: "a uint write", exit: (*VM).processExitSetStructFieldUintT0,
			instruction: isa.NewInstruction(isa.OpSetStructFieldUint, 1, 2, 1),
			load:        func(r *Registers, w *wideFields) { r.General[1] = reflect.ValueOf(w); r.Uints[2] = 12 },
			inspect:     func(t *testing.T, _ *Registers, w *wideFields) { require.Equal(t, uint64(12), w.Unsigned) },
		},
		{
			name: "a float write", exit: (*VM).processExitSetStructFieldFloatT0,
			instruction: isa.NewInstruction(isa.OpSetStructFieldFloat, 1, 2, 2),
			load:        func(r *Registers, w *wideFields) { r.General[1] = reflect.ValueOf(w); r.Floats[2] = 2.5 },
			inspect:     func(t *testing.T, _ *Registers, w *wideFields) { require.InDelta(t, 2.5, w.Ratio, 0) },
		},
		{
			name: "a bool write", exit: (*VM).processExitSetStructFieldBoolT0,
			instruction: isa.NewInstruction(isa.OpSetStructFieldBool, 1, 2, 3),
			load:        func(r *Registers, w *wideFields) { r.General[1] = reflect.ValueOf(w); r.Bools[2] = true },
			inspect:     func(t *testing.T, _ *Registers, w *wideFields) { require.True(t, w.Flag) },
		},
		{
			name: "a general write", exit: (*VM).processExitSetStructFieldGeneralT0,
			instruction: isa.NewInstruction(isa.OpSetStructFieldGeneral, 1, 2, 4),
			load: func(r *Registers, w *wideFields) {
				r.General[1] = reflect.ValueOf(w)
				r.General[2] = reflect.ValueOf("written")
			},
			inspect: func(t *testing.T, _ *Registers, w *wideFields) { require.Equal(t, "written", w.Label) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers, context := newDispatchExitFrame(t, tt.instruction)
			receiver := &wideFields{}
			tt.load(registers, receiver)

			_, action, err := tt.exit(vm, frame, registers, context)
			require.NoError(t, err)
			require.Equal(t, loopContinue, action)
			tt.inspect(t, registers, receiver)
		})
	}
}

func TestAppendDispatchExitsGrowTheirSlice(t *testing.T) {
	t.Parallel()

	t.Run("the general append exit", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers, context := newDispatchExitFrame(t, isa.NewInstruction(isa.OpAppend, 0, 1, 2))
		registers.General[1] = reflect.ValueOf([]string{"a"})
		registers.General[2] = reflect.ValueOf("b")

		_, action, err := vm.processExitAppend(frame, registers, context)
		require.NoError(t, err)
		require.Equal(t, loopContinue, action)
		require.Equal(t, []string{"a", "b"}, registers.General[0].Interface())
	})

	t.Run("the byte append exit", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers, context := newDispatchExitFrame(t, isa.NewInstruction(isa.OpAppendByteFast, 0, 1, 2))
		registers.General[1] = reflect.ValueOf([]byte{1})
		registers.Uints[2] = 2

		_, action, err := vm.processExitAppendByteFast(frame, registers, context)
		require.NoError(t, err)
		require.Equal(t, loopContinue, action)
		require.Equal(t, []byte{1, 2}, registers.General[0].Interface())
	})
}

func TestNilTestDispatchExitsTakeTheirBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		exit   dispatchExitFunc
		opcode isa.Opcode
		value  reflect.Value
	}{
		{name: "jump when the value is not nil", exit: (*VM).processExitTestNilJumpFalse, opcode: isa.OpTestNilJumpFalse, value: reflect.ValueOf(3)},
		{name: "jump when the value is nil", exit: (*VM).processExitTestNilJumpTrue, opcode: isa.OpTestNilJumpTrue, value: reflect.Value{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers, context := newDispatchExitFrame(t, isa.NewInstruction(tt.opcode, 1, 0, 0))
			registers.General[1] = tt.value

			_, _, err := tt.exit(vm, frame, registers, context)
			require.NoError(t, err, "a nil test never faults, whichever way it branches")
		})
	}
}

func TestProcessEndOfCodeFinishesAtTheBaseFrame(t *testing.T) {
	t.Parallel()

	vm, frame, _, _ := newDispatchExitFrame(t, isa.NewInstruction(isa.OpGetField, 0, 1, 0))

	_, action, err := vm.processEndOfCode(frame, vm.baseFramePointer)
	require.NoError(t, err)
	require.Equal(t, loopReturn, action, "running off the end of the base frame's body ends the run")
}

func TestDispatchCheckpointRebuildsTheContextPointers(t *testing.T) {
	t.Parallel()

	vm, _, registers, context := newDispatchExitFrame(t, isa.NewInstruction(isa.OpGetField, 0, 1, 0))
	require.False(t, vm.dispatchCheckpointDue(), "a fresh arena has no checkpoint pending")

	frame, rebuilt := vm.runDispatchCheckpoint(context)
	require.Equal(t, registers, rebuilt, "the checkpoint hands back the live register file")
	require.Equal(t, vm.CallStack[vm.FramePointer].Function, frame.Function)
}
