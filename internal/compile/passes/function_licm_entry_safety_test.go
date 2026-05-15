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

func condJump(op isa.Opcode, cond uint8, offset int16) isa.Instruction {
	lo, hi := jumpOffset(offset)
	return mk(op, cond, lo, hi)
}

func exitTestedLoop(pre isa.Instruction, exitBlock ...isa.Instruction) []isa.Instruction {
	return slices.Concat([]isa.Instruction{
		pre,
		condJump(isa.OpJumpIfFalse, 3, 3),
		mk(isa.OpGetStructFieldIntT0, 1, 4, 0),
		mk(isa.OpAddInt, 2, 2, 1),
		tier1Jump(-4),
	}, exitBlock)
}

func TestLicmEntrySafetyOnExitTestedLoops(t *testing.T) {
	t.Parallel()
	dominatingDeref := mk(isa.OpGetStructFieldIntT0, 6, 4, 0)
	deadOnExit := mk(isa.OpLoadIntConst, 1, 0, 0)
	liveOnExit := mk(isa.OpAddInt, 0, 1, 1)
	tests := []struct {
		name      string
		body      []isa.Instruction
		wantHoist bool
	}{
		{
			name:      "hoists past the exit test when the receiver was dereferenced before the loop and the destination is dead on exit",
			body:      exitTestedLoop(dominatingDeref, deadOnExit),
			wantHoist: true,
		},
		{
			name:      "refuses when the destination is live on the exit",
			body:      exitTestedLoop(dominatingDeref, liveOnExit),
			wantHoist: false,
		},
		{
			name:      "refuses when nothing before the loop dereferences the receiver",
			body:      exitTestedLoop(mk(isa.OpLoadIntConst, 7, 0, 0), deadOnExit),
			wantHoist: false,
		},
		{
			name:      "hoists when the exit runs off the end of the body, which ends the frame",
			body:      exitTestedLoop(dominatingDeref),
			wantHoist: true,
		},
		{
			name: "refuses when the receiver is rewritten after the dominating dereference",
			body: []isa.Instruction{
				dominatingDeref,
				mk(isa.OpMoveGeneral, 4, 8, 0),
				condJump(isa.OpJumpIfFalse, 3, 3),
				mk(isa.OpGetStructFieldIntT0, 1, 4, 0),
				mk(isa.OpAddInt, 2, 2, 1),
				tier1Jump(-4),
				deadOnExit,
			},
			wantHoist: false,
		},
		{
			name: "hoists when the receiver is written once before the dereference",
			body: []isa.Instruction{
				mk(isa.OpMoveGeneral, 4, 8, 0),
				dominatingDeref,
				condJump(isa.OpJumpIfFalse, 3, 3),
				mk(isa.OpGetStructFieldIntT0, 1, 4, 0),
				mk(isa.OpAddInt, 2, 2, 1),
				tier1Jump(-4),
				deadOnExit,
			},
			wantHoist: true,
		},
		{
			name: "refuses when the destination is read before the load inside the loop",
			body: []isa.Instruction{
				dominatingDeref,
				condJump(isa.OpJumpIfFalse, 3, 4),
				mk(isa.OpAddInt, 2, 2, 1),
				mk(isa.OpGetStructFieldIntT0, 1, 4, 0),
				mk(isa.OpLoadIntConst, 7, 0, 0),
				tier1Jump(-5),
				deadOnExit,
			},
			wantHoist: false,
		},
		{
			name: "hoists a read that runs before the exit test without any receiver proof",
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 7, 0, 0),
				mk(isa.OpGetStructFieldIntT0, 1, 4, 0),
				condJump(isa.OpJumpIfFalse, 3, 2),
				mk(isa.OpLoadIntConst, 8, 0, 0),
				tier1Jump(-4),
				liveOnExit,
			},
			wantHoist: true,
		},
		{
			name: "refuses a read invariant only in the narrower of two loops sharing a header",
			body: []isa.Instruction{
				dominatingDeref,
				condJump(isa.OpJumpIfFalse, 3, 6),
				mk(isa.OpGetStructFieldIntT0, 1, 4, 0),
				mk(isa.OpAddInt, 2, 2, 1),
				condJump(isa.OpJumpIfTrue, 3, -4),
				mk(isa.OpMoveGeneral, 4, 8, 0),
				tier1Jump(-6),
				deadOnExit,
			},
			wantHoist: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: tt.body}
			_ = HoistLoopInvariantStructFieldReads(context.Background(), compiledFunction)
			require.Equal(t, tt.wantHoist, len(compiledFunction.PeepholeProvenance) > 0, "hoist recorded in the provenance")
		})
	}
}

func TestLicmZeroTripHoistWritesOnlyDeadRegisters(t *testing.T) {
	t.Parallel()

	body := exitTestedLoop(mk(isa.OpGetStructFieldIntT0, 6, 4, 0), mk(isa.OpLoadIntConst, 1, 0, 0))
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = HoistLoopInvariantStructFieldReads(context.Background(), compiledFunction)
	require.Equal(t, 6, len(compiledFunction.Body))
	require.Equal(t, isa.OpGetStructFieldIntT0, compiledFunction.Body[1].Op)
	require.Equal(t, uint8(1), compiledFunction.Body[1].A)
	require.Equal(t, isa.OpJumpIfFalse, compiledFunction.Body[2].Op)
	target, isJump := program.JumpTargetAt(compiledFunction.Body, 2)
	require.True(t, isJump)
	require.Equal(t, 5, target, "exit test still lands on the exit block")
	require.Equal(t, isa.OpAddInt, compiledFunction.Body[3].Op, "the loop body follows the exit test with no remnant")
	backEdge, isJump := program.JumpTargetAt(compiledFunction.Body, 4)
	require.True(t, isJump)
	require.Equal(t, 2, backEdge, "back-edge skips the hoisted read")
}

func TestLoopExitsBeforeRead(t *testing.T) {
	t.Parallel()
	read := mk(isa.OpGetStructFieldIntT0, 1, 4, 0)
	filler := mk(isa.OpLoadIntConst, 7, 0, 0)
	tests := []struct {
		name      string
		body      []isa.Instruction
		loop      loopRange
		readPC    int
		wantExits []int
		wantOK    bool
	}{
		{
			name:      "read at the header has no exits",
			body:      []isa.Instruction{read, filler, tier1Jump(-3)},
			loop:      loopRange{header: 0, latch: 2},
			readPC:    0,
			wantExits: nil,
			wantOK:    true,
		},
		{
			name:      "conditional exit before the read is recorded",
			body:      []isa.Instruction{condJump(isa.OpJumpIfFalse, 3, 3), read, filler, tier1Jump(-4), filler},
			loop:      loopRange{header: 0, latch: 3},
			readPC:    1,
			wantExits: []int{4},
			wantOK:    true,
		},
		{
			name:      "fused exit with an extension word is recorded and its footprint skipped",
			body:      []isa.Instruction{mk(isa.OpDrillTier1, uint8(isa.SubOpLtIntJumpFalse), 2, 0), mk(isa.OpExt, 4, 0, 0), read, filler, tier1Jump(-5), filler},
			loop:      loopRange{header: 0, latch: 4},
			readPC:    2,
			wantExits: []int{6},
			wantOK:    true,
		},
		{
			name:      "in-loop jump landing on the read is harmless",
			body:      []isa.Instruction{condJump(isa.OpJumpIfTrue, 3, 1), filler, read, tier1Jump(-4)},
			loop:      loopRange{header: 0, latch: 3},
			readPC:    2,
			wantExits: nil,
			wantOK:    true,
		},
		{
			name:   "in-loop jump landing after the read refuses",
			body:   []isa.Instruction{condJump(isa.OpJumpIfTrue, 3, 2), read, filler, filler, tier1Jump(-5)},
			loop:   loopRange{header: 0, latch: 4},
			readPC: 1,
			wantOK: false,
		},
		{
			name:   "return in the prefix refuses",
			body:   []isa.Instruction{mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1), read, tier1Jump(-3)},
			loop:   loopRange{header: 0, latch: 2},
			readPC: 1,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			exits, ok := loopExitsBeforeRead(tt.body, tt.loop, tt.readPC)
			require.Equal(t, tt.wantOK, ok)
			if !ok {
				return
			}
			require.Equal(t, tt.wantExits, exits)
		})
	}
}

func TestMergeLoopsSharingHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		loops []loopRange
		want  []loopRange
	}{
		{name: "empty stays empty", loops: nil, want: nil},
		{
			name:  "distinct headers are kept and ordered by span",
			loops: []loopRange{{header: 0, latch: 10}, {header: 3, latch: 5}},
			want:  []loopRange{{header: 3, latch: 5}, {header: 0, latch: 10}},
		},
		{
			name:  "two back-edges to one header become the widest range",
			loops: []loopRange{{header: 2, latch: 6}, {header: 2, latch: 9}, {header: 12, latch: 13}},
			want:  []loopRange{{header: 12, latch: 13}, {header: 2, latch: 9}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, mergeLoopsSharingHeader(tt.loops))
		})
	}
}
