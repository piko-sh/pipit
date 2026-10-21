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

package escape

import (
	"maps"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestSiteScanAnnotatesAsAWalkPerSite(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(1, 2))
	for trial := range 400 {
		body := randomSiteBody(random, 8+random.IntN(56))
		want := annotateArenaSafeSitesByRescanning(&program.CompiledFunction{Body: body})
		got := &program.CompiledFunction{Body: body}
		annotateArenaSafeAllocs(got)
		require.Equal(t, want, got.ArenaSafeAllocPCs, "trial %d", trial)
	}
}

func TestSiteScanAllocatesPerFunctionNotPerSite(t *testing.T) {
	body := make([]isa.Instruction, 0, 130)
	for register := range 64 {
		body = append(body, isa.NewInstruction(isa.OpAllocateIndirect, uint8(register), 200, 0), isa.NewInstruction(isa.OpLoadInt64Constant, 0, 0, 0))
	}
	body = append(body, isa.NewInstruction(isa.OpJumpIfFalse, 0, 0x7E, 0xFF), isa.NewTier2Instruction(isa.SubOpTier2Return, 0))
	compiledFunction := &program.CompiledFunction{Body: body}
	allocations := testing.AllocsPerRun(10, func() {
		compiledFunction.ArenaSafeAllocPCs = nil
		annotateArenaSafeAllocs(compiledFunction)
	})
	require.Len(t, compiledFunction.ArenaSafeAllocPCs, 64, "test harness: every site is arena-safe")
	require.Less(t, allocations, 32.0, "the jump targets are computed once, and a walk step allocates nothing")
}

func randomSiteBody(random *rand.Rand, length int) []isa.Instruction {
	body := make([]isa.Instruction, length)
	for pc := range body {
		register := uint8(random.IntN(4))
		switch random.IntN(6) {
		case 0:
			body[pc] = isa.NewInstruction(isa.OpAllocateIndirect, register, 200, 0)
		case 1:
			body[pc] = isa.NewInstruction(isa.OpMoveGeneral, register, uint8(random.IntN(4)), 0)
		case 2:
			offset := uint16(int16(random.IntN(length) - pc - 1))
			body[pc] = isa.NewInstruction(isa.OpJumpIfFalse, 0, uint8(offset), uint8(offset>>8))
		case 3:
			body[pc] = isa.NewInstruction(isa.OpSetGlobal, 0, register, uint8(isa.RegisterGeneral))
		case 4:
			body[pc] = isa.NewInstruction(isa.OpLoadInt64Constant, 0, 0, 0)
		default:
			body[pc] = isa.NewTier2Instruction(isa.SubOpTier2Return, 0)
		}
	}
	return body
}

func annotateArenaSafeSitesByRescanning(compiledFunction *program.CompiledFunction) map[int]bool {
	var safe map[int]bool
	for _, structLiterals := range []bool{false, true} {
		for pc := 0; pc < len(compiledFunction.Body); pc = advancePastExtensions(compiledFunction, pc) {
			outputSlot, ok := arenaCandidateSiteAt(compiledFunction, pc)
			if !ok || isStructLiteralSiteAt(compiledFunction, pc) != structLiterals {
				continue
			}
			if outputEscapesFromSiteByRescanning(compiledFunction, outputSlot, pc, advancePastExtensions(compiledFunction, pc)) {
				continue
			}
			if safe == nil {
				safe = make(map[int]bool)
			}
			safe[pc] = true
		}
	}
	return maps.Clone(safe)
}

func outputEscapesFromSiteByRescanning(compiledFunction *program.CompiledFunction, slot uint8, sitePC, afterSite int) bool {
	seed := [isa.GeneralRegisterBankSize]bool{}
	seed[slot] = true
	escapes, ever := analyseGeneralRegisterEscapeSeeded(compiledFunction, &seed, afterSite, len(compiledFunction.Body))
	if escapes {
		return true
	}
	head := -1
	for pc := sitePC; pc < len(compiledFunction.Body); pc++ {
		target, ok := program.JumpTargetAt(compiledFunction.Body, pc)
		if !ok || target > sitePC {
			continue
		}
		if head < 0 || target < head {
			head = target
		}
	}
	if head < 0 {
		return false
	}
	escapes, _ = analyseGeneralRegisterEscapeSeeded(compiledFunction, &ever, head, sitePC)
	return escapes
}
