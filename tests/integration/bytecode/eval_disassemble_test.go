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

//go:build integration

package bytecode_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestDisassembleOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		compiledFunction func() *program.CompiledFunction
		contains         []string
	}{
		{
			name: "int_constant",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(1)
				b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				b.returnInt()
				return b.build()
			},
			contains: []string{"LOAD_INT64_CONSTANT", "ints[0] = 42"},
		},
		{
			name: "float_constant",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(3.14)
				b.floatRegisters(1)
				b.Emit(isa.OpLoadFloat64Constant, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				b.returnFloat()
				return b.build()
			},
			contains: []string{"LOAD_FLOAT64_CONSTANT", "floats[0] = 3.14"},
		},
		{
			name: "string_constant",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addStringConst("hello")
				b.stringRegisters(1)
				b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				b.returnString()
				return b.build()
			},
			contains: []string{"LOAD_STRING_CONSTANT", "strings[0] = \"hello\""},
		},
		{
			name: "bool_constant",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addBoolConst(true)
				b.boolRegisters(1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolConstant), 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				b.returnBool()
				return b.build()
			},
			contains: []string{"LOAD_BOOL_CONSTANT", "bools[0] = true"},
		},
		{
			name: "load_bool_inline",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolImmediateAsInt64), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				b.returnInt()
				return b.build()
			},
			contains: []string{"LOAD_BOOL_IMMEDIATE_AS_INT64", "ints[0] = true"},
		},
		{
			name: "load_bool_false",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolImmediateAsInt64), 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				b.returnInt()
				return b.build()
			},
			contains: []string{"LOAD_BOOL_IMMEDIATE_AS_INT64", "ints[0] = false"},
		},
		{
			name: "load_nil",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.generalRegisters(1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2LoadNil), 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				b.returnGeneral()
				return b.build()
			},
			contains: []string{"LOAD_NIL"},
		},
		{
			name: "load_int_const_small",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), 0, 7)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				b.returnInt()
				return b.build()
			},
			contains: []string{"LOAD_INT64_IMMEDIATE", "ints[0] = 7"},
		},
		{
			name: "jump_comment",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(1)
				b.EmitJump(isa.OpDrillTier1, uint8(isa.SubOpJump), 2)
				b.Emit(isa.OpNoOperation, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				b.returnInt()
				return b.build()
			},
			contains: []string{"JUMP", "goto"},
		},
		{
			name: "return_comment",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 2)
				return b.build()
			},
			contains: []string{"RETURN", "2 values"},
		},
		{
			name: "return_void_comment",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid))
				return b.build()
			},
			contains: []string{"RETURN_VOID"},
		},
		{
			name: "add_int_const_comment",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(5)
				b.intRegisters(2)
				b.Emit(isa.OpAddInt64Constant, 0, 1, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				b.returnInt()
				return b.build()
			},
			contains: []string{"ADD_INT64_CONSTANT", "const = 5"},
		},
		{
			name: "long_string_truncated",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addStringConst("this is a very long string that should be truncated in disassembly output")
				b.stringRegisters(1)
				b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				b.returnString()
				return b.build()
			},
			contains: []string{"...", "LOAD_STRING_CONSTANT"},
		},
		{
			name: "empty_body",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				return b.build()
			},
			contains: nil,
		},
		{
			name: "jump_if_true",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(1)
				b.EmitJump(isa.OpJumpIfTrue, 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				b.returnInt()
				return b.build()
			},
			contains: []string{"JUMP_IF_TRUE", "if ints[0] != 0 goto"},
		},
		{
			name: "jump_if_false",
			compiledFunction: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(1)
				b.EmitJump(isa.OpJumpIfFalse, 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				b.returnInt()
				return b.build()
			},
			contains: []string{"JUMP_IF_FALSE", "if ints[0] == 0 goto"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := tt.compiledFunction()
			output := compiledFunction.Disassemble()
			if tt.contains == nil {
				require.Empty(t, output)
				return
			}
			for _, substr := range tt.contains {
				require.Contains(t, output, substr)
			}
		})
	}
}

func TestDisassembleRange(t *testing.T) {
	t.Parallel()

	b := newBytecodeBuilder()
	b.addIntConst(1)
	b.addIntConst(2)
	b.intRegisters(2)
	b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
	b.Emit(isa.OpLoadInt64Constant, 1, 0, 1)
	b.Emit(isa.OpAddInt64, 0, 0, 1)
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	b.returnInt()
	compiledFunction := b.build()

	tests := []struct {
		name  string
		start int
		end   int
		lines int
	}{
		{name: "full_range", start: 0, end: 4, lines: 4},
		{name: "partial", start: 1, end: 3, lines: 2},
		{name: "single", start: 0, end: 1, lines: 1},
		{name: "clamped_start", start: -5, end: 2, lines: 2},
		{name: "clamped_end", start: 0, end: 100, lines: 4},
		{name: "empty_start_ge_end", start: 3, end: 2, lines: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			output := compiledFunction.DisassembleRange(tt.start, tt.end)
			if tt.lines == 0 {
				require.Empty(t, output)
			} else {
				lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
				require.Len(t, lines, tt.lines)
			}
		})
	}
}

func TestServiceCompileErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
	}{
		{name: "undefined_variable", code: `x`},
		{name: "type_mismatch", code: `var x int = "hello"`},
		{name: "syntax_error", code: `func {`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			_, err := service.Eval(t.Context(), tt.code)
			require.Error(t, err)
		})
	}
}
