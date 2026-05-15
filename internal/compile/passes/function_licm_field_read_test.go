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
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func tier1Jump(offset int16) isa.Instruction {
	lo, hi := jumpOffset(offset)
	return mk(isa.OpDrillTier1, uint8(isa.SubOpJump), lo, hi)
}

func TestLicmHoistsLoopInvariantRead(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		tier1Jump(-3),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = HoistLoopInvariantStructFieldReads(context.Background(), compiledFunction)
	require.Equal(t, 4, len(compiledFunction.Body), "the body keeps its length: nothing is left at the original site")
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[1].Op, "read stays at the loop header, now the pre-header")
	require.Equal(t, uint8(5), compiledFunction.Body[1].A)
	require.Equal(t, uint8(4), compiledFunction.Body[1].B)
	require.Equal(t, uint8(2), compiledFunction.Body[1].C)
	target, isJump := program.JumpTargetAt(compiledFunction.Body, 3)
	require.True(t, isJump)
	require.Equal(t, 2, target, "back-edge skips the hoisted read")
	require.Equal(t, program.PeepholeAnnotation{Kind: peepholeRewriteLicmHoist, Origin: 1, OriginFunction: -1}, compiledFunction.PeepholeProvenance[1])
}

func TestLicmRefusesWhenReceiverWrittenInLoop(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpMoveGeneral, 4, 7, 0),
		tier1Jump(-3),
	}
	originalLen := len(body)
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = HoistLoopInvariantStructFieldReads(context.Background(), compiledFunction)
	require.Equal(t, originalLen, len(compiledFunction.Body), "no hoist when receiver mutated in loop")
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[1].Op, "read unchanged")
}

func TestLicmRefusesWhenDestRewrittenInLoop(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpMoveGeneral, 5, 7, 0),
		tier1Jump(-3),
	}
	originalLen := len(body)
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = HoistLoopInvariantStructFieldReads(context.Background(), compiledFunction)
	require.Equal(t, originalLen, len(compiledFunction.Body), "no hoist when dest reg clobbered in loop")
}

func TestLicmRefusesWhenCallInLoop(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
		tier1Jump(-3),
	}
	originalLen := len(body)
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = HoistLoopInvariantStructFieldReads(context.Background(), compiledFunction)
	require.Equal(t, originalLen, len(compiledFunction.Body), "no hoist when call in loop")
}

func TestLicmRefusesWhenSetSameFieldInLoop(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpSetStructFieldGeneral, 4, 9, 2),
		tier1Jump(-3),
	}
	originalLen := len(body)
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = HoistLoopInvariantStructFieldReads(context.Background(), compiledFunction)
	require.Equal(t, originalLen, len(compiledFunction.Body), "no hoist when same struct field written in loop")
}

func TestLicmIgnoresForwardJumpsAsNonLoops(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		tier1Jump(3),
		mk(isa.OpGetStructFieldGeneral, 6, 4, 2),
	}
	originalLen := len(body)
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = HoistLoopInvariantStructFieldReads(context.Background(), compiledFunction)
	require.Equal(t, originalLen, len(compiledFunction.Body), "forward jump is not a loop; no hoist")
}

func TestLicmEntryJumpLandsOnHoistedRead(t *testing.T) {
	t.Parallel()

	body := []isa.Instruction{
		mk(isa.OpJumpIfFalse, 0, 0, 0),
		mk(isa.OpLoadIntConst, 7, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		tier1Jump(-3),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = HoistLoopInvariantStructFieldReads(context.Background(), compiledFunction)
	require.Equal(t, 4, len(compiledFunction.Body))
	require.Equal(t, isa.OpJumpIfFalse, compiledFunction.Body[0].Op)
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[1].Op, "hoisted read at the old header")
	require.Equal(t, isa.OpLoadIntConst, compiledFunction.Body[2].Op, "the word that led the loop follows the read")
	target, isJump := program.JumpTargetAt(compiledFunction.Body, 0)
	require.True(t, isJump)
	require.Equal(t, 1, target, "entry jump keeps landing on the hoisted read")
	backEdge, isJump := program.JumpTargetAt(compiledFunction.Body, 3)
	require.True(t, isJump)
	require.Equal(t, 2, backEdge, "back-edge skips the hoisted read")
}

func TestLicmHoistsDominatingReadAfterConditional(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(1)
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpJumpIfTrue, 0, lo, hi),
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		tier1Jump(-4),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = HoistLoopInvariantStructFieldReads(context.Background(), compiledFunction)
	require.Equal(t, 5, len(compiledFunction.Body))
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[1].Op, "dominating read moved to the pre-header (the old loop header)")
	require.Equal(t, isa.OpJumpIfTrue, compiledFunction.Body[2].Op)
	skip, isJump := program.JumpTargetAt(compiledFunction.Body, 2)
	require.True(t, isJump)
	require.Equal(t, 4, skip, "a jump that landed on the read lands on the read's successor")
	backEdge, isJump := program.JumpTargetAt(compiledFunction.Body, 4)
	require.True(t, isJump)
	require.Equal(t, 2, backEdge, "back-edge skips the hoisted read")
}

func TestLicmRefusesHoistWhenPreHeaderUnconditionallyJumps(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		tier1Jump(2),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpLoadIntConst, 1, 0, 0),
		tier1Jump(-2),
	}
	originalLen := len(body)
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = HoistLoopInvariantStructFieldReads(context.Background(), compiledFunction)
	require.Equal(t, originalLen, len(compiledFunction.Body), "no hoist when the slot preceding the loop header unconditionally jumps elsewhere")
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[1].Op)
}

func TestLicmPreservesBackEdgeTargetAfterShift(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpLoadIntConst, 3, 0, 0),
		tier1Jump(-4),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = HoistLoopInvariantStructFieldReads(context.Background(), compiledFunction)
	require.Equal(t, 5, len(compiledFunction.Body))
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[1].Op)
	require.Equal(t, uint8(2), compiledFunction.Body[2].A, "the header's word shifted down by the read's width")
	target, isJump := program.JumpTargetAt(compiledFunction.Body, 4)
	require.True(t, isJump)
	require.Equal(t, 2, target, "back-edge lands on the shifted header word, after the hoisted read")
}

func TestSortLoopRangesAscendingSpanBreaksTiesOnHeader(t *testing.T) {
	tests := []struct {
		name  string
		loops []loopRange
		want  []loopRange
	}{
		{
			name:  "equal spans keep header order",
			loops: []loopRange{{header: 40, latch: 50}, {header: 10, latch: 20}, {header: 25, latch: 35}},
			want:  []loopRange{{header: 10, latch: 20}, {header: 25, latch: 35}, {header: 40, latch: 50}},
		},
		{
			name:  "smaller span first regardless of header",
			loops: []loopRange{{header: 0, latch: 100}, {header: 60, latch: 70}, {header: 10, latch: 30}},
			want:  []loopRange{{header: 60, latch: 70}, {header: 10, latch: 30}, {header: 0, latch: 100}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loops := slices.Clone(tt.loops)
			sortLoopRangesAscendingSpan(loops)
			require.Equal(t, tt.want, loops)
		})
	}
}

func TestLoopHoistPCMap(t *testing.T) {
	t.Parallel()
	pcMap := loopHoistPCMap{HeaderPC: 3, ReadPC: 6, Width: 2}
	tests := []struct {
		name string
		pc   int
		want int
	}{
		{name: "before the header stays", pc: 2, want: 2},
		{name: "header word shifts by the width", pc: 3, want: 5},
		{name: "last word before the read shifts", pc: 5, want: 7},
		{name: "first moved word takes the header", pc: 6, want: 3},
		{name: "second moved word follows it", pc: 7, want: 4},
		{name: "successor of the read stays", pc: 8, want: 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, pcMap.newPC(tt.pc))
		})
	}
	jumps := []struct {
		name   string
		source int
		target int
		want   int
	}{
		{name: "entry from before the loop runs the hoisted words", source: 1, target: 3, want: 3},
		{name: "back-edge skips the hoisted words", source: 12, target: 3, want: 5},
		{name: "in-loop jump to the header skips the hoisted words", source: 9, target: 3, want: 5},
		{name: "jump onto the read lands on its successor", source: 4, target: 6, want: 8},
		{name: "jump onto the read's second word lands on its successor", source: 4, target: 7, want: 8},
		{name: "jump into the shifted prefix follows the shift", source: 4, target: 5, want: 7},
		{name: "jump past the read is unchanged", source: 4, target: 10, want: 10},
	}
	for _, tt := range jumps {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, pcMap.newJumpTarget(tt.source, tt.target))
		})
	}
}

func TestRemapPCKeyedSet(t *testing.T) {
	t.Parallel()
	pcMap := loopHoistPCMap{HeaderPC: 3, ReadPC: 6, Width: 1}
	tests := []struct {
		name string
		set  map[int]bool
		want map[int]bool
	}{
		{name: "nil stays nil", set: nil, want: nil},
		{name: "empty stays empty", set: map[int]bool{}, want: map[int]bool{}},
		{
			name: "keys before, inside and after the rotated segment follow their words",
			set:  map[int]bool{0: true, 3: true, 5: true, 6: true, 9: true},
			want: map[int]bool{0: true, 4: true, 6: true, 3: true, 9: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, remapPCKeyedSet(tt.set, pcMap))
		})
	}
}

func TestLicmRemapsArenaAnnotationsWithTheirInstructions(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpLoadIntConst, 3, 0, 0),
		tier1Jump(-4),
	}

	compiledFunction := &program.CompiledFunction{
		Body:                   body,
		ArenaSafeAllocPCs:      map[int]bool{0: true, 1: true},
		FieldStoreArenaSafePCs: map[int]bool{3: true},
		InPlaceHeaderReusePCs:  map[int]bool{1: true, 4: true},
	}
	_ = HoistLoopInvariantStructFieldReads(context.Background(), compiledFunction)
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[1].Op, "hoisted read sits at the header")
	require.Equal(t, uint8(2), compiledFunction.Body[2].A, "the word that was at PC 1 now sits at PC 2")
	require.Equal(t, map[int]bool{0: true, 2: true}, compiledFunction.ArenaSafeAllocPCs, "the header's key follows its word; the pre-header key stays")
	require.Equal(t, map[int]bool{3: true}, compiledFunction.FieldStoreArenaSafePCs, "keys after the read stay")
	require.Equal(t, map[int]bool{2: true, 4: true}, compiledFunction.InPlaceHeaderReusePCs)
}
