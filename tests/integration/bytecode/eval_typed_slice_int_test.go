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

func TestTypedSliceIntDirectOps(t *testing.T) {
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
				lengthConstantIndex := builder.addIntConst(7)
				builder.intRegisters(2).sliceIntRegisters(1).returnInt()
				builder.Emit(isa.OpLoadIntConst, 1, lengthConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMakeSliceInt), 0, 1)
				builder.Emit(isa.OpExt, 1, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpLenSliceIntDirect), 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
			expect: 7,
		},
		{
			name: "set_then_get_round_trip",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				lengthConstantIndex := builder.addIntConst(3)
				indexConstantIndex := builder.addIntConst(1)
				valueConstantIndex := builder.addIntConst(99)
				builder.intRegisters(4).sliceIntRegisters(1).returnInt()
				builder.Emit(isa.OpLoadIntConst, 1, lengthConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMakeSliceInt), 0, 1)
				builder.Emit(isa.OpExt, 1, 0, 0)
				builder.Emit(isa.OpLoadIntConst, 2, indexConstantIndex, 0)
				builder.Emit(isa.OpLoadIntConst, 3, valueConstantIndex, 0)
				builder.Emit(isa.OpSliceSetIntDirect, 0, 2, 3)
				builder.Emit(isa.OpSliceGetIntDirect, 0, 0, 2)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
			expect: 99,
		},
		{
			name: "sum_three_elements",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				lengthConstantIndex := builder.addIntConst(3)
				zeroConstantIndex := builder.addIntConst(0)
				oneConstantIndex := builder.addIntConst(1)
				twoConstantIndex := builder.addIntConst(2)
				tenConstantIndex := builder.addIntConst(10)
				twentyConstantIndex := builder.addIntConst(20)
				thirtyConstantIndex := builder.addIntConst(30)
				builder.intRegisters(6).sliceIntRegisters(1).returnInt()
				builder.Emit(isa.OpLoadIntConst, 1, lengthConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMakeSliceInt), 0, 1)
				builder.Emit(isa.OpExt, 1, 0, 0)
				builder.Emit(isa.OpLoadIntConst, 2, zeroConstantIndex, 0)
				builder.Emit(isa.OpLoadIntConst, 3, tenConstantIndex, 0)
				builder.Emit(isa.OpSliceSetIntDirect, 0, 2, 3)
				builder.Emit(isa.OpLoadIntConst, 2, oneConstantIndex, 0)
				builder.Emit(isa.OpLoadIntConst, 3, twentyConstantIndex, 0)
				builder.Emit(isa.OpSliceSetIntDirect, 0, 2, 3)
				builder.Emit(isa.OpLoadIntConst, 2, twoConstantIndex, 0)
				builder.Emit(isa.OpLoadIntConst, 3, thirtyConstantIndex, 0)
				builder.Emit(isa.OpSliceSetIntDirect, 0, 2, 3)
				builder.Emit(isa.OpLoadIntConst, 0, zeroConstantIndex, 0)
				builder.Emit(isa.OpLoadIntConst, 4, zeroConstantIndex, 0)
				builder.Emit(isa.OpSliceGetIntDirect, 5, 0, 4)
				builder.Emit(isa.OpAddInt, 0, 0, 5)
				builder.Emit(isa.OpLoadIntConst, 4, oneConstantIndex, 0)
				builder.Emit(isa.OpSliceGetIntDirect, 5, 0, 4)
				builder.Emit(isa.OpAddInt, 0, 0, 5)
				builder.Emit(isa.OpLoadIntConst, 4, twoConstantIndex, 0)
				builder.Emit(isa.OpSliceGetIntDirect, 5, 0, 4)
				builder.Emit(isa.OpAddInt, 0, 0, 5)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
			expect: 60,
		},
		{
			name: "negative_index_panics",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				lengthConstantIndex := builder.addIntConst(2)
				negativeConstantIndex := builder.addIntConst(-1)
				builder.intRegisters(3).sliceIntRegisters(1).returnInt()
				builder.Emit(isa.OpLoadIntConst, 0, lengthConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMakeSliceInt), 0, 0)
				builder.Emit(isa.OpExt, 0, 0, 0)
				builder.Emit(isa.OpLoadIntConst, 1, negativeConstantIndex, 0)
				builder.Emit(isa.OpSliceGetIntDirect, 2, 0, 1)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
			expect: fault.ErrIndexOutOfRange,
		},
		{
			name: "out_of_range_index_panics",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				lengthConstantIndex := builder.addIntConst(2)
				outOfRangeConstantIndex := builder.addIntConst(2)
				builder.intRegisters(3).sliceIntRegisters(1).returnInt()
				builder.Emit(isa.OpLoadIntConst, 0, lengthConstantIndex, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMakeSliceInt), 0, 0)
				builder.Emit(isa.OpExt, 0, 0, 0)
				builder.Emit(isa.OpLoadIntConst, 1, outOfRangeConstantIndex, 0)
				builder.Emit(isa.OpSliceGetIntDirect, 2, 0, 1)
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
