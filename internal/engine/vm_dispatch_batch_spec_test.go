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
	"pipit.sh/pipit/internal/isa"
)

func TestEveryAssemblyBodyIsInstalled(t *testing.T) {
	t.Parallel()
	for _, row := range isa.AllSpecs() {
		if row.AsmBody == "" {
			continue
		}
		instruction, ok := instructionForSpecRow(row)
		if !ok {
			continue
		}
		t.Run(row.Name, func(t *testing.T) {
			t.Parallel()
			require.Falsef(t, instructionWouldTrampoline(instruction),
				"%s names assembly body %s but its slot still points at the Go fallback; regenerate the tables (make generate-engine, then make generate-asmgen)", row.Name, row.AsmBody)
		})
	}
}

func instructionForSpecRow(row isa.OpSpec) (isa.Instruction, bool) {
	switch row.Tier {
	case isa.TierMain:
		return isa.NewInstruction(isa.Opcode(row.Code), 0, 0, 0), true
	case isa.TierSub1:
		return isa.NewInstruction(isa.OpDrillTier1, row.Code, 0, 0), true
	case isa.TierSub2:
		return isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), row.Code, 0), true
	case isa.TierSub3:
		return isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), row.Code), true
	default:
		return isa.Instruction{}, false
	}
}

func TestEveryGoOnlyOperationIsBatchable(t *testing.T) {
	t.Parallel()
	for _, row := range isa.AllSpecs() {
		if row.Code == 0 || row.Handler == "" || row.AsmBody != "" || row.Shim != "" {
			continue
		}
		instruction, ok := instructionForSpecRow(row)
		if !ok {
			continue
		}
		t.Run(row.Name, func(t *testing.T) {
			require.True(t, instructionWouldTrampoline(instruction), "Go-only operation must participate in Path B batching")
		})
	}
}

func TestEveryInLoopShimStaysInAssembly(t *testing.T) {
	t.Parallel()
	for _, row := range isa.AllSpecs() {
		if row.Shim == "" || row.ExitStub != "" {
			continue
		}
		instruction, ok := instructionForSpecRow(row)
		if !ok {
			continue
		}
		t.Run(row.Name, func(t *testing.T) {
			require.False(t, instructionWouldTrampoline(instruction), "in-loop shim must not be batched as an unconditional exit")
		})
	}
}

func TestPathBBatchConsumesGoRunAndStopsAtAssembly(t *testing.T) {
	t.Parallel()
	registers := Registers{General: []reflect.Value{reflect.ValueOf(2), reflect.ValueOf(3), {}}}
	body := []isa.Instruction{
		isa.NewInstruction(isa.OpAdd, 2, 0, 1),
		isa.NewInstruction(isa.OpAdd, 2, 2, 1),
		isa.NewInstruction(isa.OpAddInt, 0, 0, 0),
	}
	frame := CallFrame{}
	ctx := dispatchContext{}
	vm := VM{}
	_, action, err := vm.runPathBBatch(&frame, &registers, &ctx, len(body), body, opContinue)
	require.NoError(t, err)
	require.Equal(t, loopContinue, action)
	require.Equal(t, 2, frame.ProgramCounter, "consume both generic additions before returning to assembly")
	require.Equal(t, int64(2), ctx.programCounter)
	require.Equal(t, int64(8), registers.General[2].Int())
}
