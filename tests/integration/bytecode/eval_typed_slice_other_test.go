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
	"context"
	"errors"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func TestTypedSliceFloatDirectOps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect any
		build  func() *program.CompiledFunction
		name   string
	}{
		{
			name: "make_then_len",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				lengthConstantIndex := builder.addIntConst(5)
				builder.intRegisters(2).sliceFloatRegisters(1).returnInt()
				builder.Emit(isa.OpLoadIntConst, 1, lengthConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMakeSliceFloat), 0, 1)
				builder.Emit(isa.OpExt, 1, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpLenSliceFloatDirect), 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
			expect: 5,
		},
		{
			name: "set_then_get_round_trip",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				lengthConstantIndex := builder.addIntConst(3)
				indexConstantIndex := builder.addIntConst(2)
				valueConstantIndex := builder.addFloatConst(3.14159)
				builder.intRegisters(3).floatRegisters(2).sliceFloatRegisters(1).returnFloat()
				builder.Emit(isa.OpLoadIntConst, 1, lengthConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMakeSliceFloat), 0, 1)
				builder.Emit(isa.OpExt, 1, 0, 0)
				builder.Emit(isa.OpLoadIntConst, 2, indexConstantIndex, 0)
				builder.Emit(isa.OpLoadFloatConst, 1, valueConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpSliceSetFloatDirect), 0, 2)
				builder.Emit(isa.OpExt, 1, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpSliceGetFloatDirect), 0, 0)
				builder.Emit(isa.OpExt, 2, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
			expect: 3.14159,
		},
		{
			name: "out_of_range_panics",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				lengthConstantIndex := builder.addIntConst(2)
				outOfRangeConstantIndex := builder.addIntConst(2)
				builder.intRegisters(3).floatRegisters(1).sliceFloatRegisters(1).returnFloat()
				builder.Emit(isa.OpLoadIntConst, 1, lengthConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMakeSliceFloat), 0, 1)
				builder.Emit(isa.OpExt, 1, 0, 0)
				builder.Emit(isa.OpLoadIntConst, 2, outOfRangeConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpSliceGetFloatDirect), 0, 0)
				builder.Emit(isa.OpExt, 2, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
			expect: fault.ErrIndexOutOfRange,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			compiled := testCase.build()
			result, err := service.Execute(context.Background(), compiled)
			if expectedError, ok := testCase.expect.(error); ok {
				require.Error(t, err)
				require.True(t, errors.Is(err, expectedError),
					"expected error wrapping %v, got %v", expectedError, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.expect, result)
		})
	}
}

func TestTypedSliceStringDirectOps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect any
		build  func() *program.CompiledFunction
		name   string
	}{
		{
			name: "make_then_len",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				lengthConstantIndex := builder.addIntConst(4)
				builder.intRegisters(2).sliceStringRegisters(1).returnInt()
				builder.Emit(isa.OpLoadIntConst, 1, lengthConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMakeSliceString), 0, 1)
				builder.Emit(isa.OpExt, 1, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpLenSliceStringDirect), 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
			expect: 4,
		},
		{
			name: "set_then_get_round_trip",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				lengthConstantIndex := builder.addIntConst(3)
				indexConstantIndex := builder.addIntConst(1)
				valueConstantIndex := builder.addStringConst("hello")
				builder.intRegisters(3).stringRegisters(2).sliceStringRegisters(1).returnString()
				builder.Emit(isa.OpLoadIntConst, 1, lengthConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMakeSliceString), 0, 1)
				builder.Emit(isa.OpExt, 1, 0, 0)
				builder.Emit(isa.OpLoadIntConst, 2, indexConstantIndex, 0)
				builder.Emit(isa.OpLoadStringConst, 1, valueConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpSliceSetStringDirect), 0, 2)
				builder.Emit(isa.OpExt, 1, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpSliceGetStringDirect), 0, 0)
				builder.Emit(isa.OpExt, 2, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
			expect: "hello",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			compiled := testCase.build()
			result, err := service.Execute(context.Background(), compiled)
			require.NoError(t, err)
			require.Equal(t, testCase.expect, result)
		})
	}
}

func TestTypedSliceBoolDirectOps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect any
		build  func() *program.CompiledFunction
		name   string
	}{
		{
			name: "make_then_len",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				lengthConstantIndex := builder.addIntConst(6)
				builder.intRegisters(2).sliceBoolRegisters(1).returnInt()
				builder.Emit(isa.OpLoadIntConst, 1, lengthConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMakeSliceBool), 0, 1)
				builder.Emit(isa.OpExt, 1, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpLenSliceBoolDirect), 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
			expect: 6,
		},
		{
			name: "set_then_get_round_trip",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				lengthConstantIndex := builder.addIntConst(2)
				indexConstantIndex := builder.addIntConst(0)
				trueConstantIndex := builder.addBoolConst(true)
				builder.intRegisters(3).boolRegisters(2).sliceBoolRegisters(1).returnBool()
				builder.Emit(isa.OpLoadIntConst, 1, lengthConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMakeSliceBool), 0, 1)
				builder.Emit(isa.OpExt, 1, 0, 0)
				builder.Emit(isa.OpLoadIntConst, 2, indexConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolConst), 1, trueConstantIndex)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpSliceSetBoolDirect), 0, 2)
				builder.Emit(isa.OpExt, 1, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpSliceGetBoolDirect), 0, 0)
				builder.Emit(isa.OpExt, 2, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
			expect: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			compiled := testCase.build()
			result, err := service.Execute(context.Background(), compiled)
			require.NoError(t, err)
			require.Equal(t, testCase.expect, result)
		})
	}
}

func TestTypedSliceUintDirectOps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect any
		build  func() *program.CompiledFunction
		name   string
	}{
		{
			name: "make_then_len",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				lengthConstantIndex := builder.addIntConst(8)
				builder.intRegisters(2).sliceUintRegisters(1).returnInt()
				builder.Emit(isa.OpLoadIntConst, 1, lengthConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMakeSliceUint), 0, 1)
				builder.Emit(isa.OpExt, 1, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpLenSliceUintDirect), 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
			expect: 8,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			compiled := testCase.build()
			result, err := service.Execute(context.Background(), compiled)
			require.NoError(t, err)
			require.Equal(t, testCase.expect, result)
		})
	}
}
