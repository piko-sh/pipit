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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func sliceChainLoopFixture(extra ...isa.Instruction) *program.CompiledFunction {
	body := make([]isa.Instruction, 0, len(extra)+10)
	body = append(body,
		mk(isa.OpLoadIntConst, 0, 0, 0),
		mk(isa.OpJumpIfFalse, 1, 0, 0),
		mk(isa.OpGetGlobal, 2, 7, uint8(isa.RegisterGeneral)),
		isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 0, 2),
		mk(isa.OpExt, 0, 0, 0),
		isa.NewTier1Instruction(isa.SubOpSliceSetByteDirect, 0, 0),
		mk(isa.OpExt, 0, 0, 0),
	)
	body = append(body, extra...)
	body = append(body, isa.NewTier2Instruction(isa.SubOpTier2IncInt, 0), tier1Jump(0), isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid))
	program.SetJumpTarget(body, 1, len(body)-1)
	program.SetJumpTarget(body, len(body)-2, 1)
	return &program.CompiledFunction{Body: body}
}

func TestSliceChainHoistKeepsFirstIteration(t *testing.T) {
	t.Parallel()
	cf := sliceChainLoopFixture()
	original := slices.Clone(cf.Body)
	hoistLoopInvariantSliceChains(cf, nil)
	require.Len(t, cf.Body, 15)
	require.Equal(t, original[2:7], cf.Body[2:7])
	for pc, want := range map[int]int{1: 14, 8: 9, 9: 14, 13: 9} {
		got, ok := program.JumpTargetAt(cf.Body, pc)
		require.True(t, ok)
		require.Equal(t, want, got)
	}
	require.Equal(t, original[5:8], cf.Body[10:13])
	for _, inst := range cf.Body[9:] {
		require.NotEqual(t, isa.OpGetGlobal, inst.Op)
		require.False(t, isa.InstrIsTier1SubOp(inst, isa.SubOpGetStructFieldSliceByte))
	}
}

func TestSliceChainHoistRejectsMemoryChanges(t *testing.T) {
	t.Parallel()
	for name, extra := range map[string][]isa.Instruction{
		"global":        {mk(isa.OpSetGlobal, 3, 7, uint8(isa.RegisterGeneral))},
		"callback":      {isa.NewTier1Instruction(isa.SubOpCall, 0, 0)},
		"header":        {isa.NewTier1Instruction(isa.SubOpSetStructFieldSliceByte, 2, 0), mk(isa.OpExt, 0, 0, 0)},
		"append":        {isa.NewTier1Instruction(isa.SubOpStarAppendByteFast, 2, 0)},
		"reslice":       {isa.NewTier1Instruction(isa.SubOpSliceByteSlice, 0, 0), mk(isa.OpExt, 0, 0, 0), mk(isa.OpExt, 0, 0, 0)},
		"destination":   {isa.NewTier1Instruction(isa.SubOpMoveSliceByte, 0, 1)},
		"alias":         {mk(isa.OpIndexSet, 3, 0, 2)},
		"opaque":        {{Op: isa.Opcode(250)}},
		"live receiver": {mk(isa.OpGetStructFieldIntT0, 0, 2, 0)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cf := sliceChainLoopFixture(extra...)
			before := slices.Clone(cf.Body)
			hoistLoopInvariantSliceChains(cf, nil)
			require.Equal(t, before, cf.Body)
		})
	}
}

func TestSliceChainHoistRejectsConditionalReadAndInteriorEntry(t *testing.T) {
	t.Parallel()
	for _, target := range []int{2, 3, 5, 8} {
		t.Run(string(rune('a'+target)), func(t *testing.T) {
			t.Parallel()
			cf := sliceChainLoopFixture()
			if target < 5 {
				cf.Body[0] = mk(isa.OpJumpIfFalse, 0, 0, 0)
				require.True(t, program.SetJumpTarget(cf.Body, 0, target))
			} else {
				require.True(t, program.SetJumpTarget(cf.Body, 1, target))
			}
			before := slices.Clone(cf.Body)
			hoistLoopInvariantSliceChains(cf, nil)
			require.Equal(t, before, cf.Body)
		})
	}
}

func TestSliceChainHoistPreservesDebugAndPCMetadata(t *testing.T) {
	t.Parallel()
	cf := sliceChainLoopFixture()
	cf.DebugSourceMap = &program.SourceMap{Positions: make([]program.SourcePosition, len(cf.Body))}
	for pc := range cf.Body {
		cf.DebugSourceMap.Positions[pc].Line = int32(pc + 1)
	}
	cf.DebugVarTable = &program.DebugVarTable{Entries: []program.DebugVarEntry{{Name: "whole", StartPC: 0}, {Name: "body", StartPC: 5, EndPC: 8}}}
	cf.ArenaSafeAllocPCs = map[int]bool{5: true, 9: true}
	cf.GetMethodReceiverTypeNames = map[uint32]string{9: "named"}
	hoistLoopInvariantSliceChains(cf, nil)
	require.Len(t, cf.DebugSourceMap.Positions, len(cf.Body))
	require.Equal(t, int32(6), cf.DebugSourceMap.Positions[10].Line)
	require.Equal(t, map[int]bool{5: true, 10: true, 14: true}, cf.ArenaSafeAllocPCs)
	require.Equal(t, map[uint32]string{14: "named"}, cf.GetMethodReceiverTypeNames)
	for pc := range cf.Body {
		live := cf.DebugVarTable.LiveVariables(pc)
		names := make([]string, 0, len(live))
		for _, v := range live {
			names = append(names, v.Name)
		}
		require.Contains(t, names, "whole")
		require.Equal(t, (pc >= 5 && pc < 8) || (pc >= 10 && pc < 13), slices.Contains(names, "body"))
	}
}

func TestSliceChainPeelReusesFieldOfInvariantReceiver(t *testing.T) {
	t.Parallel()
	for name, writesReceiver := range map[string]bool{"invariant receiver": false, "receiver written in loop": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tail := isa.NewInstruction(isa.OpNop, 0, 0, 0)
			if writesReceiver {
				tail = mk(isa.OpMoveGeneral, 2, 3, 1)
			}
			body := []isa.Instruction{
				mk(isa.OpGetGlobal, 2, 7, uint8(isa.RegisterGeneral)),
				mk(isa.OpLoadIntConst, 0, 0, 0),
				mk(isa.OpJumpIfFalse, 1, 0, 0),
				isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 0, 2),
				mk(isa.OpExt, 0, 0, 0),
				isa.NewTier1Instruction(isa.SubOpSliceSetByteDirect, 0, 0),
				mk(isa.OpExt, 0, 0, 0),
				tail,
				isa.NewTier2Instruction(isa.SubOpTier2IncInt, 0),
				tier1Jump(0),
				isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
			}
			require.True(t, program.SetJumpTarget(body, 2, 10))
			require.True(t, program.SetJumpTarget(body, 9, 2))
			cf := &program.CompiledFunction{Body: slices.Clone(body)}
			hoistLoopInvariantSliceChains(cf, nil)
			if writesReceiver {
				require.Equal(t, body, cf.Body)
				return
			}
			require.Len(t, cf.Body, len(body)+8-2, "the loop is copied without the field read")
			reads := 0
			for _, inst := range cf.Body {
				if isa.InstrIsTier1SubOp(inst, isa.SubOpGetStructFieldSliceByte) {
					reads++
				}
			}
			require.Equal(t, 1, reads, "only the first traversal reads the field")
		})
	}
}
