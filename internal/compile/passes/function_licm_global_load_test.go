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

func TestGlobalLoadHoistPreservesElementAliasing(t *testing.T) {
	for _, wide := range []bool{false, true} {
		t.Run(map[bool]string{false: "narrow", true: "wide"}[wide], func(t *testing.T) {
			body := []isa.Instruction{mk(isa.OpLoadIntConst, 0, 0, 0), mk(isa.OpNop, 0, 0, 0)}
			if wide {
				body = append(body, isa.NewTier1Instruction(isa.SubOpGetGlobalWide, 2, uint8(isa.RegisterGeneral)), mk(isa.OpExt, 1, 1, 0))
			} else {
				body = append(body, mk(isa.OpGetGlobal, 2, 7, uint8(isa.RegisterGeneral)))
			}
			body = append(body, mk(isa.OpSliceGetUint, 0, 2, 0), mk(isa.OpSliceSetUint, 2, 0, 0), tier1Jump(0))
			latch := len(body) - 1
			require.True(t, program.SetJumpTarget(body, latch, 1))
			cf := &program.CompiledFunction{Body: body}
			require.NoError(t, hoistLoopInvariantGlobalLoads(context.Background(), cf, nil))
			_, reg, width, ok := globalLoadDestination(cf.Body, 1)
			require.True(t, ok)
			require.Equal(t, uint8(2), reg)
			target, ok := program.JumpTargetAt(cf.Body, latch)
			require.True(t, ok)
			require.Equal(t, 1+width, target)
		})
	}
}

func TestGlobalLoadHoistRejectsInvalidationAndLiveEntry(t *testing.T) {
	cases := []struct {
		name          string
		before, after []isa.Instruction
	}{
		{name: "global rebinding", after: []isa.Instruction{mk(isa.OpSetGlobal, 3, 7, uint8(isa.RegisterGeneral))}},
		{name: "callback", after: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpCall, 0, 0)}},
		{name: "receiver assignment", after: []isa.Instruction{mk(isa.OpMoveGeneral, 2, 3, 0)}},
		{name: "header assignment", after: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSetStructFieldSliceByte, 3, 0), mk(isa.OpExt, 0, 0, 0)}},
		{name: "indirect assignment", after: []isa.Instruction{mk(isa.OpIndexSet, 3, 0, 2)}},
		{name: "live before load", before: []isa.Instruction{mk(isa.OpSliceGetUint, 0, 2, 0)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []isa.Instruction{mk(isa.OpLoadIntConst, 0, 0, 0), mk(isa.OpNop, 0, 0, 0)}
			body = append(body, tc.before...)
			body = append(body, mk(isa.OpGetGlobal, 2, 7, uint8(isa.RegisterGeneral)))
			body = append(body, tc.after...)
			body = append(body, tier1Jump(0))
			require.True(t, program.SetJumpTarget(body, len(body)-1, 1))
			original := slices.Clone(body)
			cf := &program.CompiledFunction{Body: body}
			require.NoError(t, hoistLoopInvariantGlobalLoads(context.Background(), cf, nil))
			require.Equal(t, original, cf.Body)
		})
	}
}

func TestGlobalLoadHoistPreservesZeroTripDestination(t *testing.T) {
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 0, 0, 0),
		mk(isa.OpJumpIfFalse, 0, 3, 0),
		mk(isa.OpGetGlobal, 2, 7, uint8(isa.RegisterGeneral)),
		mk(isa.OpSliceGetUint, 0, 2, 0),
		tier1Jump(-4),
		mk(isa.OpSliceGetUint, 0, 2, 0),
	}
	cf := &program.CompiledFunction{Body: slices.Clone(body)}
	require.NoError(t, hoistLoopInvariantGlobalLoads(context.Background(), cf, nil))
	require.Equal(t, body, cf.Body)
}

func TestGlobalLoadHoistChecksOutOfLineLoopBlocks(t *testing.T) {
	for name, instruction := range map[string]isa.Instruction{
		"store":    mk(isa.OpSetGlobal, 3, 7, uint8(isa.RegisterGeneral)),
		"callback": isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
		"pure":     mk(isa.OpNop, 0, 0, 0),
	} {
		t.Run(name, func(t *testing.T) {
			body := []isa.Instruction{
				mk(isa.OpLoadIntConst, 0, 0, 0),
				mk(isa.OpNop, 0, 0, 0),
				mk(isa.OpGetGlobal, 2, 7, uint8(isa.RegisterGeneral)),
				tier1Jump(3),
				mk(isa.OpSliceGetUint, 0, 2, 0),
				tier1Jump(-5),
				isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
				instruction,
				tier1Jump(-5),
			}
			cf := &program.CompiledFunction{Body: slices.Clone(body)}
			require.NoError(t, hoistLoopInvariantGlobalLoads(context.Background(), cf, nil))
			if name == "pure" {
				require.Equal(t, isa.OpGetGlobal, cf.Body[1].Op)
			} else {
				require.Equal(t, body, cf.Body)
			}
		})
	}
}

func TestExistingHoistsCheckOutOfLineLoopBlocks(t *testing.T) {
	for _, field := range []bool{false, true} {
		load := mk(isa.OpLoadIntConst, 1, 0, 0)
		mutation := mk(isa.OpLoadIntConst, 1, 1, 0)
		if field {
			load = mk(isa.OpGetStructFieldIntT0, 1, 4, 0)
			mutation = mk(isa.OpSetStructFieldIntT0, 4, 2, 0)
		}
		body := []isa.Instruction{
			mk(isa.OpLoadIntConst, 0, 0, 0),
			mk(isa.OpNop, 0, 0, 0),
			load,
			tier1Jump(3),
			mk(isa.OpAddInt, 0, 0, 1),
			tier1Jump(-5),
			isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
			mutation,
			tier1Jump(-5),
		}
		cf := &program.CompiledFunction{Body: slices.Clone(body)}
		if field {
			require.NoError(t, HoistLoopInvariantStructFieldReads(context.Background(), cf))
		} else {
			require.NoError(t, hoistLoopInvariantConstantLoads(context.Background(), cf, nil))
		}
		require.Equal(t, body, cf.Body)
	}
}
