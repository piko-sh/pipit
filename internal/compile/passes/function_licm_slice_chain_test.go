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
		mk(isa.OpLoadInt64Constant, 0, 0, 0),
		mk(isa.OpJumpIfFalse, 1, 0, 0),
		mk(isa.OpGetGlobal, 2, 7, uint8(isa.RegisterGeneral)),
		isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 0, 2),
		mk(isa.OpExtension, 0, 0, 0),
		isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, 0, 0),
		mk(isa.OpExtension, 0, 0, 0),
	)
	body = append(body, extra...)
	body = append(body, isa.NewTier2Instruction(isa.SubOpTier2IncrementInt64, 0), tier1Jump(0), isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid))
	program.SetJumpTarget(body, 1, len(body)-1)
	program.SetJumpTarget(body, len(body)-2, 1)
	return &program.CompiledFunction{Body: body}
}

func TestSliceChainHoistKeepsFirstIteration(t *testing.T) {
	t.Parallel()
	compiledFunction := sliceChainLoopFixture()
	original := slices.Clone(compiledFunction.Body)
	hoistLoopInvariantSliceChains(compiledFunction, nil)
	require.Len(t, compiledFunction.Body, 15)
	require.Equal(t, original[2:7], compiledFunction.Body[2:7])
	for pc, want := range map[int]int{1: 14, 8: 9, 9: 14, 13: 9} {
		got, ok := program.JumpTargetAt(compiledFunction.Body, pc)
		require.True(t, ok)
		require.Equal(t, want, got)
	}
	require.Equal(t, original[5:8], compiledFunction.Body[10:13])
	for _, instruction := range compiledFunction.Body[9:] {
		require.NotEqual(t, isa.OpGetGlobal, instruction.Op)
		require.False(t, isa.InstrIsTier1SubOp(instruction, isa.SubOpGetStructFieldSliceByte))
	}
}

func TestSliceChainHoistRejectsMemoryChanges(t *testing.T) {
	t.Parallel()
	for name, extra := range map[string][]isa.Instruction{
		"global":        {mk(isa.OpSetGlobal, 3, 7, uint8(isa.RegisterGeneral))},
		"callback":      {isa.NewTier1Instruction(isa.SubOpCall, 0, 0)},
		"header":        {isa.NewTier1Instruction(isa.SubOpSetStructFieldSliceByte, 2, 0), mk(isa.OpExtension, 0, 0, 0)},
		"append":        {isa.NewTier1Instruction(isa.SubOpAppendByteViaPointer, 2, 0)},
		"reslice":       {isa.NewTier1Instruction(isa.SubOpSliceSliceByte, 0, 0), mk(isa.OpExtension, 0, 0, 0), mk(isa.OpExtension, 0, 0, 0)},
		"destination":   {isa.NewTier1Instruction(isa.SubOpMoveSliceByte, 0, 1)},
		"alias":         {mk(isa.OpSetElement, 3, 0, 2)},
		"opaque":        {{Op: isa.Opcode(250)}},
		"live receiver": {mk(isa.OpGetStructFieldInt64, 0, 2, 0)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := sliceChainLoopFixture(extra...)
			before := slices.Clone(compiledFunction.Body)
			hoistLoopInvariantSliceChains(compiledFunction, nil)
			require.Equal(t, before, compiledFunction.Body)
		})
	}
}

func TestSliceChainHoistRejectsConditionalReadAndInteriorEntry(t *testing.T) {
	t.Parallel()
	for _, target := range []int{2, 3, 5, 8} {
		t.Run(string(rune('a'+target)), func(t *testing.T) {
			t.Parallel()
			compiledFunction := sliceChainLoopFixture()
			if target < 5 {
				compiledFunction.Body[0] = mk(isa.OpJumpIfFalse, 0, 0, 0)
				require.True(t, program.SetJumpTarget(compiledFunction.Body, 0, target))
			} else {
				require.True(t, program.SetJumpTarget(compiledFunction.Body, 1, target))
			}
			before := slices.Clone(compiledFunction.Body)
			hoistLoopInvariantSliceChains(compiledFunction, nil)
			require.Equal(t, before, compiledFunction.Body)
		})
	}
}

func TestSliceChainHoistPreservesDebugAndPCMetadata(t *testing.T) {
	t.Parallel()
	compiledFunction := sliceChainLoopFixture()
	compiledFunction.DebugSourceMap = &program.SourceMap{Positions: make([]program.SourcePosition, len(compiledFunction.Body))}
	for pc := range compiledFunction.Body {
		compiledFunction.DebugSourceMap.Positions[pc].Line = int32(pc + 1)
	}
	compiledFunction.DebugVarTable = &program.DebugVarTable{Entries: []program.DebugVarEntry{{Name: "whole", StartPC: 0}, {Name: "body", StartPC: 5, EndPC: 8}}}
	compiledFunction.ArenaSafeAllocPCs = map[int]bool{5: true, 9: true}
	compiledFunction.BindMethodByNameReceiverTypeNames = map[uint32]string{9: "named"}
	hoistLoopInvariantSliceChains(compiledFunction, nil)
	require.Len(t, compiledFunction.DebugSourceMap.Positions, len(compiledFunction.Body))
	require.Equal(t, int32(6), compiledFunction.DebugSourceMap.Positions[10].Line)
	require.Equal(t, map[int]bool{5: true, 10: true, 14: true}, compiledFunction.ArenaSafeAllocPCs)
	require.Equal(t, map[uint32]string{14: "named"}, compiledFunction.BindMethodByNameReceiverTypeNames)
	for pc := range compiledFunction.Body {
		live := compiledFunction.DebugVarTable.LiveVariables(pc)
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
			tail := isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
			if writesReceiver {
				tail = mk(isa.OpMoveGeneral, 2, 3, 1)
			}
			body := []isa.Instruction{
				mk(isa.OpGetGlobal, 2, 7, uint8(isa.RegisterGeneral)),
				mk(isa.OpLoadInt64Constant, 0, 0, 0),
				mk(isa.OpJumpIfFalse, 1, 0, 0),
				isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 0, 2),
				mk(isa.OpExtension, 0, 0, 0),
				isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, 0, 0),
				mk(isa.OpExtension, 0, 0, 0),
				tail,
				isa.NewTier2Instruction(isa.SubOpTier2IncrementInt64, 0),
				tier1Jump(0),
				isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
			}
			require.True(t, program.SetJumpTarget(body, 2, 10))
			require.True(t, program.SetJumpTarget(body, 9, 2))
			compiledFunction := &program.CompiledFunction{Body: slices.Clone(body)}
			hoistLoopInvariantSliceChains(compiledFunction, nil)
			if writesReceiver {
				require.Equal(t, body, compiledFunction.Body)
				return
			}
			require.Len(t, compiledFunction.Body, len(body)+8-2, "the loop is copied without the field read")
			reads := 0
			for _, instruction := range compiledFunction.Body {
				if isa.InstrIsTier1SubOp(instruction, isa.SubOpGetStructFieldSliceByte) {
					reads++
				}
			}
			require.Equal(t, 1, reads, "only the first traversal reads the field")
		})
	}
}

func TestSliceChainHoistPeelsALoopTestedAtTheBottom(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadInt64Constant, 0, 0, 0),
		mk(isa.OpJumpIfFalse, 1, 0, 0),
		mk(isa.OpGetGlobal, 2, 7, uint8(isa.RegisterGeneral)),
		isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 0, 2),
		mk(isa.OpExtension, 0, 0, 0),
		isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, 0, 0),
		mk(isa.OpExtension, 0, 0, 0),
		isa.NewTier1Instruction(isa.SubOpIncrementInt64ThenJumpIfLessThan, 0, 1),
		mk(isa.OpExtension, 0, 0, 0),
		returnVoid(),
	}
	require.True(t, program.SetJumpTarget(body, 1, 9))
	require.True(t, program.SetJumpTarget(body, 7, 2))
	compiledFunction := &program.CompiledFunction{Body: body}
	original := slices.Clone(body)
	hoistLoopInvariantSliceChains(compiledFunction, nil)

	require.Len(t, compiledFunction.Body, 15, "the copy drops the three chain words and gains one jump to the exit")
	require.Equal(t, original[2:7], compiledFunction.Body[2:7], "the first traversal keeps its chain")
	require.True(t, isa.InstrIsTier1SubOp(compiledFunction.Body[9], isa.SubOpJump), "a latch that is not taken leaves through the jump laid after it")
	require.Equal(t, original[5:7], compiledFunction.Body[10:12], "the copy starts at the store")
	for pc, want := range map[int]int{1: 14, 7: 10, 9: 14, 12: 10} {
		got, ok := program.JumpTargetAt(compiledFunction.Body, pc)
		require.True(t, ok, "jump at %d", pc)
		require.Equal(t, want, got, "jump at %d", pc)
	}
	for _, instruction := range compiledFunction.Body[10:] {
		require.NotEqual(t, isa.OpGetGlobal, instruction.Op)
		require.False(t, isa.InstrIsTier1SubOp(instruction, isa.SubOpGetStructFieldSliceByte))
	}
}
