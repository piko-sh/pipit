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
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestSubOpInlineGoCallAMD64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want  any
		build func() *program.CompiledFunction
		name  string
	}{
		{
			name: "MathSin_via_ASM",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				srcConst := builder.addFloatConst(0.5)
				builder.intRegisters(2).floatRegisters(2).returnFloat()
				builder.Emit(isa.OpLoadFloat64Constant, 1, srcConst, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMathSin), 0, 1)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
				return builder.build()
			},
			want: math.Sin(0.5),
		},
		{
			name: "MathCos_via_ASM",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				srcConst := builder.addFloatConst(1.0)
				builder.intRegisters(2).floatRegisters(2).returnFloat()
				builder.Emit(isa.OpLoadFloat64Constant, 1, srcConst, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMathCos), 0, 1)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
				return builder.build()
			},
			want: math.Cos(1.0),
		},
		{
			name: "MathMod_via_ASM",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				src1Const := builder.addFloatConst(7.5)
				src2Const := builder.addFloatConst(2.0)
				builder.intRegisters(2).floatRegisters(3).returnFloat()
				builder.Emit(isa.OpLoadFloat64Constant, 1, src1Const, 0)
				builder.Emit(isa.OpLoadFloat64Constant, 2, src2Const, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMathMod), 0, 1)
				builder.Emit(isa.OpExtension, 2, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
				return builder.build()
			},
			want: math.Mod(7.5, 2.0),
		},
		{
			name: "StrconvItoa_via_ASM",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				srcConst := builder.addIntConst(42)
				builder.intRegisters(2).stringRegisters(1).returnString()
				builder.Emit(isa.OpLoadInt64Constant, 1, srcConst, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpStrconvItoa), 0, 1)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
				return builder.build()
			},
			want: "42",
		},
		{
			name: "StrconvFormatBool_via_ASM",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				oneConst := builder.addIntConst(1)
				builder.intRegisters(2).boolRegisters(2).stringRegisters(1).returnString()
				builder.Emit(isa.OpLoadInt64Constant, 1, oneConst, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpConvertInt64ToBool), 1, 1)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpStrconvFormatBool), 0, 1)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
				return builder.build()
			},
			want: "true",
		},
		{
			name: "StrconvFormatInt_via_ASM",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				valConst := builder.addIntConst(255)
				baseConst := builder.addIntConst(16)
				builder.intRegisters(3).stringRegisters(1).returnString()
				builder.Emit(isa.OpLoadInt64Constant, 1, valConst, 0)
				builder.Emit(isa.OpLoadInt64Constant, 2, baseConst, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpStrconvFormatInt), 0, 1)
				builder.Emit(isa.OpExtension, 2, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
				return builder.build()
			},
			want: "ff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			compiled := tt.build()
			result, err := service.Execute(t.Context(), compiled)
			require.NoError(t, err)
			if expected, ok := tt.want.(float64); ok {
				require.InDelta(t, expected, result, 1e-10)
			} else {
				require.Equal(t, tt.want, result)
			}
		})
	}
}
