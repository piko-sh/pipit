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
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

func newBoundsCheckFrame(t *testing.T) *CallFrame {
	t.Helper()
	compiledFunction := &program.CompiledFunction{
		Name: "TestFunc",
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpNop, 0, 0, 0),
			isa.NewInstruction(isa.OpNop, 0, 0, 0),
			isa.NewInstruction(isa.OpNop, 0, 0, 0),
			isa.NewInstruction(isa.OpNop, 0, 0, 0),
		},
	}
	return &CallFrame{
		Function:       compiledFunction,
		ProgramCounter: 2,
	}
}

func TestVMBoundsErr_Error(t *testing.T) {
	t.Parallel()
	err := &vMBoundsErr{TableName: "intConstants", Index: 7, TableSize: 3}
	require.Equal(t, "intConstants index out of range", err.Error())
}

func TestVMBoundsErr_DiagnosticDetail(t *testing.T) {
	t.Parallel()
	err := &vMBoundsErr{
		TableName:    "stringConstants",
		Index:        12,
		TableSize:    4,
		Pc:           99,
		FunctionName: "calc",
	}
	got := err.DiagnosticDetail()
	require.Contains(t, got, "index=12")
	require.Contains(t, got, "tableSize=4")
	require.Contains(t, got, "pc=99")
	require.Contains(t, got, "funcName=calc")
}

func TestVMBoundsError_PopulatesVMEvalError(t *testing.T) {
	t.Parallel()
	vm := newTestVM(t)
	frame := newBoundsCheckFrame(t)

	vMBoundsError(vm, frame, "boolConstants", 5, 2)

	require.Error(t, vm.evalError)
	var boundsErr *vMBoundsErr
	ok := errors.As(vm.evalError, &boundsErr)
	require.True(t, ok, "expected *vmBoundsErr, got %T", vm.evalError)
	require.Equal(t, "boolConstants", boundsErr.TableName)
	require.Equal(t, 5, boundsErr.Index)
	require.Equal(t, 2, boundsErr.TableSize)
	require.Equal(t, frame.ProgramCounter, boundsErr.Pc)
	require.Equal(t, frame.Function.Name, boundsErr.FunctionName)
}

func TestVMDiagnosticContext_RendersBytecodeAndRegisters(t *testing.T) {
	t.Parallel()
	frame := newBoundsCheckFrame(t)
	registers := &Registers{
		General: make([]reflect.Value, 8),
	}
	registers.General[3] = reflect.ValueOf("hello")

	got := vMDiagnosticContext(frame, registers, 3)

	require.Contains(t, got, "bytecode around pc:")
	require.Contains(t, got, "nearby registers:")
	require.Contains(t, got, "general[3]: string")
}

func TestVMDiagnosticContext_ZeroValueRegistersAnnotated(t *testing.T) {
	t.Parallel()
	frame := newBoundsCheckFrame(t)
	registers := &Registers{General: make([]reflect.Value, 4)}

	got := vMDiagnosticContext(frame, registers, 2)
	require.Contains(t, got, "general[")
	require.Contains(t, got, "<zero>")
}

func TestVMCallSiteDiagnostic_RendersArgsAndReturns(t *testing.T) {
	t.Parallel()
	frame := newBoundsCheckFrame(t)
	site := &program.CallSite{
		Arguments: []program.VarLocation{
			{Register: 1, Kind: isa.RegisterInt},
			{Register: 2, Kind: isa.RegisterString},
		},
		Returns: []program.VarLocation{
			{Register: 3, Kind: isa.RegisterGeneral},
		},
	}
	got := vMCallSiteDiagnostic(frame, site)
	require.NotEmpty(t, got)
}

func TestVMCallSiteDiagnostic_NoArgsCompact(t *testing.T) {
	t.Parallel()
	frame := newBoundsCheckFrame(t)
	site := &program.CallSite{}
	got := vMCallSiteDiagnostic(frame, site)
	_ = got
}

func TestVMPanicInvalidRegister_PanicsWithDetail(t *testing.T) {
	t.Parallel()
	frame := newBoundsCheckFrame(t)
	registers := &Registers{General: make([]reflect.Value, 4)}
	inst := isa.NewInstruction(isa.OpNop, 0, 0, 0)

	defer func() {
		recovered := recover()
		require.NotNil(t, recovered)
		invariant, ok := recovered.(*invariantError)
		require.True(t, ok, "expected *invariantError panic, got %T", recovered)
		require.ErrorIs(t, invariant, fault.ErrInterpreterInvariant)
		message := invariant.Error()
		require.True(t, strings.Contains(message, "handler") || strings.Contains(message, "register"),
			"panic message should mention handler/register, got %q", message)
	}()

	vMPanicInvalidRegister("testHandler", "receiver", 9, inst, frame, registers)
}

func TestVMPanicNotStruct_PanicsWithDetail(t *testing.T) {
	t.Parallel()
	frame := newBoundsCheckFrame(t)
	registers := &Registers{General: make([]reflect.Value, 4)}
	inst := isa.NewInstruction(isa.OpNop, 0, 0, 0)

	defer func() {
		recovered := recover()
		require.NotNil(t, recovered)
	}()

	vMPanicNotStruct("testHandler", 1, reflect.Int, inst, frame, registers)
}

func TestVMPanicFieldIndex_PanicsWithDetail(t *testing.T) {
	t.Parallel()
	frame := newBoundsCheckFrame(t)
	registers := &Registers{General: make([]reflect.Value, 4)}
	inst := isa.NewInstruction(isa.OpNop, 0, 0, 0)

	structType := reflect.TypeOf(struct{ A int }{})

	defer func() {
		recovered := recover()
		require.NotNil(t, recovered)
	}()

	vMPanicFieldIndex("testHandler", structType, 99, inst, frame, registers)
}

func TestVMPanicTypeMismatch_PanicsWithDetail(t *testing.T) {
	t.Parallel()
	frame := newBoundsCheckFrame(t)
	registers := &Registers{General: make([]reflect.Value, 4)}
	inst := isa.NewInstruction(isa.OpNop, 0, 0, 0)

	defer func() {
		recovered := recover()
		require.NotNil(t, recovered)
	}()

	vMPanicTypeMismatch(
		"testHandler",
		reflect.TypeFor[string](),
		reflect.TypeFor[int](),
		inst,
		frame,
		registers,
	)
}
