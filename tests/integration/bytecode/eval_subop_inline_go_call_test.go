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

//go:build integration && !safe && !(js && wasm) && (amd64 || arm64)

package bytecode_test

import (
	"context"
	"math"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func TestSubOpInlineGoCallAMD64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect any
		build  func() *program.CompiledFunction
		name   string
	}{
		{
			name: "MathSin_via_ASM",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				srcConst := builder.addFloatConst(0.5)
				builder.intRegisters(2).floatRegisters(2).returnFloat()
				builder.Emit(isa.OpLoadFloatConst, 1, srcConst, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMathSin), 0, 1)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
				return builder.build()
			},
			expect: math.Sin(0.5),
		},
		{
			name: "MathCos_via_ASM",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				srcConst := builder.addFloatConst(1.0)
				builder.intRegisters(2).floatRegisters(2).returnFloat()
				builder.Emit(isa.OpLoadFloatConst, 1, srcConst, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMathCos), 0, 1)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
				return builder.build()
			},
			expect: math.Cos(1.0),
		},
		{
			name: "MathMod_via_ASM",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				src1Const := builder.addFloatConst(7.5)
				src2Const := builder.addFloatConst(2.0)
				builder.intRegisters(2).floatRegisters(3).returnFloat()
				builder.Emit(isa.OpLoadFloatConst, 1, src1Const, 0)
				builder.Emit(isa.OpLoadFloatConst, 2, src2Const, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMathMod), 0, 1)
				builder.Emit(isa.OpExt, 2, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
				return builder.build()
			},
			expect: math.Mod(7.5, 2.0),
		},
		{
			name: "StrconvItoa_via_ASM",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				srcConst := builder.addIntConst(42)
				builder.intRegisters(2).stringRegisters(1).returnString()
				builder.Emit(isa.OpLoadIntConst, 1, srcConst, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpStrconvItoa), 0, 1)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
				return builder.build()
			},
			expect: "42",
		},
		{
			name: "StrconvFormatBool_via_ASM",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				oneConst := builder.addIntConst(1)
				builder.intRegisters(2).boolRegisters(2).stringRegisters(1).returnString()
				builder.Emit(isa.OpLoadIntConst, 1, oneConst, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpIntToBool), 1, 1)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpStrconvFormatBool), 0, 1)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
				return builder.build()
			},
			expect: "true",
		},
		{
			name: "StrconvFormatInt_via_ASM",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				valConst := builder.addIntConst(255)
				baseConst := builder.addIntConst(16)
				builder.intRegisters(3).stringRegisters(1).returnString()
				builder.Emit(isa.OpLoadIntConst, 1, valConst, 0)
				builder.Emit(isa.OpLoadIntConst, 2, baseConst, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpStrconvFormatInt), 0, 1)
				builder.Emit(isa.OpExt, 2, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
				return builder.build()
			},
			expect: "ff",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			compiled := testCase.build()
			result, err := service.Execute(context.Background(), compiled)
			require.NoError(t, err)
			if expected, ok := testCase.expect.(float64); ok {
				require.InDelta(t, expected, result, 1e-10)
			} else {
				require.Equal(t, testCase.expect, result)
			}
		})
	}
}
