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

package passes

import (
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// elideRepeatedGlobalSliceFieldRead reuses a byte-slice field loaded from an unchanged
// global receiver within a basic block. Byte element stores preserve the header.
//
// Takes compiledFunction (*program.CompiledFunction) which receives rewrite provenance.
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes first (int) which is the global load preceding the field read.
// Takes targets (map[int]bool) which marks branch destinations.
func elideRepeatedGlobalSliceFieldRead(compiledFunction *program.CompiledFunction, body []isa.Instruction, first int, targets map[int]bool) {
	if first+2 >= len(body) {
		return
	}
	global, read, layout := body[first], body[first+1], body[first+2]
	if global.Op != isa.OpGetGlobal || isa.RegisterKind(global.C) != isa.RegisterGeneral ||
		!isa.InstrIsTier1SubOp(read, isa.SubOpGetStructFieldSliceByte) || read.C != global.A || layout.Op != isa.OpExt || targets[first+1] || targets[first+2] {
		return
	}
	bank := isa.RoleForKind(isa.RegisterSliceByte)
	for pc := first + 3; pc < min(len(body), first+4*maxCseFieldReadScanWindow); pc++ {
		inst := body[pc]
		if targets[pc] {
			return
		}
		if inst == global && pc+2 < len(body) && body[pc+1] == read && body[pc+2] == layout && !targets[pc+1] && !targets[pc+2] {
			for offset := range 3 {
				body[pc+offset] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
			}
			body[pc] = isa.NewTier1Instruction(isa.SubOpJump, 2, 0)
			RecordPeepholeRewrite(compiledFunction, pc, peepholeRewriteCseGlobalSlice, first)
			pc += 2
			continue
		}
		if invalidatesCachedFieldReads(compiledFunction, inst) && !isa.InstrIsTier1SubOp(inst, isa.SubOpSliceSetByteDirect) {
			return
		}
		if !instructionShapeAllowsCseScan(inst) || instructionWritesRegisterInBank(inst, isa.RoleRegGeneral, global.A) || instructionWritesRegisterInBank(inst, bank, read.B) {
			return
		}
	}
}
