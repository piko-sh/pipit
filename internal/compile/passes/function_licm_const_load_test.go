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

func smallIntConst(dest, value uint8) isa.Instruction {
	return mk(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), dest, value)
}

func exitTestedConstLoop(load, use isa.Instruction, exitBlock ...isa.Instruction) []isa.Instruction {
	return slices.Concat([]isa.Instruction{
		mk(isa.OpLoadIntConst, 9, 0, 0),
		condJump(isa.OpJumpIfFalse, 3, 3),
		load,
		use,
		tier1Jump(-4),
	}, exitBlock)
}

func TestConstantLoadDestination(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		inst     isa.Instruction
		wantKind isa.RegisterKind
		wantReg  uint8
		wantOK   bool
	}{
		{name: "int const", inst: mk(isa.OpLoadIntConst, 4, 1, 0), wantKind: isa.RegisterInt, wantReg: 4, wantOK: true},
		{name: "float const", inst: mk(isa.OpLoadFloatConst, 2, 0, 0), wantKind: isa.RegisterFloat, wantReg: 2, wantOK: true},
		{name: "string const", inst: mk(isa.OpLoadStringConst, 0, 0, 0), wantKind: isa.RegisterString, wantReg: 0, wantOK: true},
		{name: "uint const", inst: mk(isa.OpLoadUintConst, 7, 0, 0), wantKind: isa.RegisterUint, wantReg: 7, wantOK: true},
		{name: "small int const", inst: smallIntConst(5, 1), wantKind: isa.RegisterInt, wantReg: 5, wantOK: true},
		{name: "small uint const", inst: mk(isa.OpDrillTier1, uint8(isa.SubOpLoadUintConstSmall), 6, 1), wantKind: isa.RegisterUint, wantReg: 6, wantOK: true},
		{name: "general const must stay fresh", inst: mk(isa.OpLoadGeneralConst, 1, 0, 0), wantOK: false},
		{name: "composite zero reuse must stay fresh", inst: mk(isa.OpLoadCompositeZeroReuse, 1, 0, 0), wantOK: false},
		{name: "complex const is not in the family", inst: mk(isa.OpLoadComplexConst, 1, 0, 0), wantOK: false},
		{name: "bool load is not in the family", inst: mk(isa.OpDrillTier1, uint8(isa.SubOpLoadBool), 1, 1), wantOK: false},
		{name: "field read is not a constant", inst: mk(isa.OpGetStructFieldIntT0, 1, 4, 0), wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kind, reg, ok := constantLoadDestination(tt.inst)
			require.Equal(t, tt.wantOK, ok)
			if !ok {
				return
			}
			require.Equal(t, tt.wantKind, kind)
			require.Equal(t, tt.wantReg, reg)
		})
	}
}

func TestHoistLoopInvariantConstantLoads(t *testing.T) {
	t.Parallel()
	stringLoad := mk(isa.OpLoadStringConst, 0, 0, 0)
	stringIndex := mk(isa.OpStringIndex, 1, 0, 2)
	deadOnExit := mk(isa.OpLoadStringConst, 0, 1, 0)
	liveOnExit := mk(isa.OpStringIndex, 5, 0, 2)
	tests := []struct {
		name        string
		body        []isa.Instruction
		resultKinds []isa.RegisterKind
		wantHoist   bool
	}{
		{
			name:      "hoists a string constant behind the exit test when the destination is dead on exit",
			body:      exitTestedConstLoop(stringLoad, stringIndex, deadOnExit),
			wantHoist: true,
		},
		{
			name:      "hoists when the exit runs off the end of the body",
			body:      exitTestedConstLoop(stringLoad, stringIndex),
			wantHoist: true,
		},
		{
			name:      "refuses when the destination is live on the exit",
			body:      exitTestedConstLoop(stringLoad, stringIndex, liveOnExit),
			wantHoist: false,
		},
		{
			name:      "hoists a small int constant",
			body:      exitTestedConstLoop(smallIntConst(5, 7), mk(isa.OpAddInt, 6, 6, 5), mk(isa.OpLoadIntConst, 5, 0, 0)),
			wantHoist: true,
		},
		{
			name:      "hoists a uint constant",
			body:      exitTestedConstLoop(mk(isa.OpLoadUintConst, 5, 0, 0), mk(isa.OpAddUint, 6, 6, 5), mk(isa.OpLoadUintConst, 5, 1, 0)),
			wantHoist: true,
		},
		{
			name:      "refuses when another instruction in the loop writes the destination",
			body:      exitTestedConstLoop(smallIntConst(5, 7), smallIntConst(5, 8), mk(isa.OpLoadIntConst, 5, 0, 0)),
			wantHoist: false,
		},
		{
			name:      "refuses when the destination is read before the load",
			body:      exitTestedConstLoop(mk(isa.OpAddInt, 6, 6, 5), smallIntConst(5, 7), mk(isa.OpLoadIntConst, 5, 0, 0)),
			wantHoist: false,
		},
		{
			name:      "refuses a general constant even when everything else holds",
			body:      exitTestedConstLoop(mk(isa.OpLoadGeneralConst, 5, 0, 0), mk(isa.OpMoveGeneral, 6, 5, 0), mk(isa.OpLoadGeneralConst, 5, 1, 0)),
			wantHoist: false,
		},
		{
			name:        "refuses when the destination is a result slot read by the return past the exit",
			body:        exitTestedConstLoop(mk(isa.OpLoadIntConst, 0, 0, 0), mk(isa.OpAddInt, 6, 6, 0), mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)),
			resultKinds: []isa.RegisterKind{isa.RegisterInt},
			wantHoist:   false,
		},
		{
			name: "hoists a load that runs before the exit test",
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 9, 0, 0),
				stringLoad,
				condJump(isa.OpJumpIfFalse, 3, 2),
				stringIndex,
				tier1Jump(-4),
				liveOnExit,
			},
			wantHoist: true,
		},
		{
			name: "refuses when the loop header is entered only by a jump",
			body: []isa.Instruction{
				tier1Jump(2),
				stringLoad,
				stringIndex,
				tier1Jump(-2),
			},
			wantHoist: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: tt.body, ResultKinds: tt.resultKinds}
			originalLen := len(tt.body)
			require.NoError(t, hoistLoopInvariantConstantLoads(context.Background(), compiledFunction, nil))
			require.Equal(t, originalLen, len(compiledFunction.Body), "a hoist never changes the body length")
			require.Equal(t, tt.wantHoist, len(compiledFunction.PeepholeProvenance) > 0, "hoist recorded in the provenance")
		})
	}
}

func TestHoistLoopInvariantConstantLoadsPlacesTheLoadAtTheHeader(t *testing.T) {
	t.Parallel()
	body := exitTestedConstLoop(mk(isa.OpLoadStringConst, 0, 3, 0), mk(isa.OpStringIndex, 1, 0, 2), mk(isa.OpLoadStringConst, 0, 1, 0))
	compiledFunction := &program.CompiledFunction{Body: body}
	require.NoError(t, hoistLoopInvariantConstantLoads(context.Background(), compiledFunction, nil))
	require.Equal(t, 6, len(compiledFunction.Body))
	require.Equal(t, isa.OpLoadStringConst, compiledFunction.Body[1].Op, "load sits at the old header")
	require.Equal(t, uint8(3), compiledFunction.Body[1].B, "constant index travels with the load")
	require.Equal(t, isa.OpJumpIfFalse, compiledFunction.Body[2].Op, "exit test follows the hoisted load")
	require.Equal(t, isa.OpStringIndex, compiledFunction.Body[3].Op, "the loop body follows with no remnant")
	target, isJump := program.JumpTargetAt(compiledFunction.Body, 2)
	require.True(t, isJump)
	require.Equal(t, 5, target, "exit still lands on the exit block")
	backEdge, isJump := program.JumpTargetAt(compiledFunction.Body, 4)
	require.True(t, isJump)
	require.Equal(t, 2, backEdge, "back-edge skips the hoisted load")
	require.Equal(t, program.PeepholeAnnotation{Kind: peepholeRewriteLicmHoist, Origin: 2, OriginFunction: -1}, compiledFunction.PeepholeProvenance[1])
}

func TestHoistLoopInvariantConstantLoadsHonoursTheCap(t *testing.T) {
	t.Parallel()
	const loads = maxLicmConstHoistsPerFunction + 2
	body := make([]isa.Instruction, 0, loads+3)
	body = append(body, mk(isa.OpLoadIntConst, 30, 0, 0))
	for reg := range uint8(loads) {
		body = append(body, mk(isa.OpLoadStringConst, reg, 0, 0))
	}
	body = append(body, mk(isa.OpLoadIntConst, 31, 0, 0), tier1Jump(-int16(loads+2)))
	compiledFunction := &program.CompiledFunction{Body: body}
	require.NoError(t, hoistLoopInvariantConstantLoads(context.Background(), compiledFunction, nil))
	require.Equal(t, maxLicmConstHoistsPerFunction, len(compiledFunction.PeepholeProvenance), "exactly the cap's worth of hoists")
	backEdge, isJump := program.JumpTargetAt(compiledFunction.Body, len(body)-1)
	require.True(t, isJump)
	require.Equal(t, 1+maxLicmConstHoistsPerFunction, backEdge, "each hoist moved the loop header past one load")
}

func TestPostPurityPipelineEnablesConstantLoadHoisting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		pipeline []pass
		want     bool
	}{
		{name: "first pass leaves constant loads to the fusers", pipeline: functionPipeline(DefaultOptions()), want: false},
		{name: "post-purity pass hoists constant loads", pipeline: PostPurityPipeline(DefaultOptions(), nil), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			found := false
			for _, pass := range tt.pipeline {
				licm, ok := pass.(licmPass)
				if !ok {
					continue
				}
				found = true
				require.Equal(t, tt.want, licm.constantLoads)
			}
			require.True(t, found, "pipeline carries a licm pass")
		})
	}
}

func TestRegisterDeadFromTreatsResultSlotsPrecisely(t *testing.T) {
	t.Parallel()
	returnOne := mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	tests := []struct {
		name string
		body []isa.Instruction
		want bool
	}{
		{
			name: "result slot overwritten before the return is dead",
			body: []isa.Instruction{mk(isa.OpLoadStringConst, 0, 1, 0), returnOne},
			want: true,
		},
		{
			name: "result slot read by the return is live",
			body: []isa.Instruction{mk(isa.OpLoadIntConst, 4, 0, 0), returnOne},
			want: false,
		},
		{
			name: "result slot stays live when the function defers a call",
			body: []isa.Instruction{mk(isa.OpDefer, 3, 0, 0), mk(isa.OpLoadStringConst, 0, 1, 0), returnOne},
			want: false,
		},
		{
			name: "result slot is dead past a tail call that does not pass it",
			body: []isa.Instruction{mk(isa.OpDrillTier1, uint8(isa.SubOpTailCall), 0, 0)},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{
				Body:        tt.body,
				ResultKinds: []isa.RegisterKind{isa.RegisterString},
				CallSites:   []program.CallSite{{}},
			}
			require.Equal(t, tt.want, registerDeadFrom(compiledFunction, tt.body, 0, isa.RegisterString, 0))
		})
	}
}
