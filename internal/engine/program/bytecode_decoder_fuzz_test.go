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

//go:build fuzz

package program

import (
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func FuzzDecodeInstruction(f *testing.F) {
	f.Add(byte(isa.OpNop), byte(0), byte(0), byte(0))
	f.Add(byte(isa.OpAddInt), byte(1), byte(2), byte(3))
	f.Add(byte(isa.OpLoadIntConst), byte(0), byte(0), byte(0))
	f.Add(byte(isa.OpDrillTier1), byte(isa.SubOpJump), byte(0), byte(0))
	f.Add(byte(isa.OpDrillTier1), byte(isa.SubOpDrillTier2), byte(isa.SubOpTier2Return), byte(1))
	f.Add(byte(isa.OpDrillTier1), byte(isa.SubOpDrillTier2), byte(isa.SubOpTier2DrillTier3), byte(isa.SubOpTier3Nop))
	f.Add(byte(isa.OpExt), byte(0xFF), byte(0xFF), byte(0xFF))

	f.Fuzz(func(t *testing.T, op, a, b, c byte) {
		defer func() {
			if r := recover(); r != nil {
				require.Fail(t, "decoder panicked", "%v", r)
			}
		}()
		instr := isa.Instruction{Op: isa.Opcode(op), A: a, B: b, C: c}
		_ = isa.InstructionDisplayName(instr)
		_ = instr.String()
		_ = instr.SignedOffset()
		_ = instr.WideIndex()
		shape := isa.ShapeForInstruction(instr)
		_ = shape.Flags
		_ = shape.A
		_ = shape.B
		_ = shape.C

		compiledFunction := &CompiledFunction{Name: "fuzz", Body: []isa.Instruction{instr}}
		_ = compiledFunction.Disassemble()
	})
}
