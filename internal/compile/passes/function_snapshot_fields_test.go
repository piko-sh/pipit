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
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func snapshotFixture() *program.CompiledFunction {
	return &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpMoveGeneral, 1, 0, engine.MoveGeneralModeSnapshot),
			mk(isa.OpSetStructFieldIntT0, 0, 0, 0),
			mk(isa.OpGetStructFieldIntT0, 1, 1, 0),
			isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 0, 1),
			mk(isa.OpExt, 1, 0, 0),
			isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 2, isa.RegisterGeneral: 2, isa.RegisterSliceByte: 1},
		StructLayoutTable: []program.StructFieldLayout{
			{PathLength: 1, Kind: uint8(reflect.Int), RegisterKind: uint8(isa.RegisterInt)},
			{PathLength: 1, Path: [isa.StructFieldLayoutMaxPathDepth]uint8{1}, Kind: uint8(reflect.Slice), RegisterKind: uint8(isa.RegisterSliceByte)},
		},
		TypeTable: []reflect.Type{reflect.TypeFor[struct {
			Offset int
			Data   []byte
		}]()},
	}
}

func runSnapshotPass(t *testing.T, cf *program.CompiledFunction) {
	t.Helper()
	require.NoError(t, (scalarizeSnapshotsPass{}).Run(context.Background(), &PassContext{Analysis: newFunctionAnalysis(cf)}, cf))
}

func TestSnapshotFieldsCaptureBeforeSourceMutation(t *testing.T) {
	cf := snapshotFixture()
	runSnapshotPass(t, cf)
	require.Equal(t, mk(isa.OpGetStructFieldIntT0, 2, 0, 0), cf.Body[0])
	require.Equal(t, isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 1, 0), cf.Body[1])
	require.Equal(t, mk(isa.OpExt, 1, 0, 0), cf.Body[2])
	require.Equal(t, mk(isa.OpSetStructFieldIntT0, 0, 0, 0), cf.Body[3])
	require.Equal(t, isa.NewTier1Instruction(isa.SubOpMoveInt, 1, 2), cf.Body[4])
	require.Equal(t, isa.NewTier1Instruction(isa.SubOpMoveSliceByte, 0, 1), cf.Body[5])
	require.Equal(t, uint32(3), cf.NumRegisters[isa.RegisterInt])
	require.NotNil(t, cf.AliasInfo)
}

func TestSnapshotFieldsRefuseObservableCopies(t *testing.T) {
	for name, op := range map[string]isa.Instruction{
		"whole value":    mk(isa.OpMoveGeneral, 0, 1, engine.MoveGeneralModeAlias),
		"field mutation": mk(isa.OpSetStructFieldIntT0, 1, 0, 0),
		"call":           isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
		"opaque":         mk(isa.Opcode(255), 255, 1, 0),
	} {
		t.Run(name, func(t *testing.T) {
			cf := snapshotFixture()
			cf.Body[1] = op
			old := slices.Clone(cf.Body)
			runSnapshotPass(t, cf)
			require.Equal(t, old, cf.Body)
		})
	}
	t.Run("embedded pointer path", func(t *testing.T) {
		cf := snapshotFixture()
		cf.StructLayoutTable[0].PathLength = 2
		old := slices.Clone(cf.Body)
		runSnapshotPass(t, cf)
		require.Equal(t, old, cf.Body)
	})
	t.Run("register overflow", func(t *testing.T) {
		cf := snapshotFixture()
		cf.NumRegisters[isa.RegisterInt] = 256
		old := slices.Clone(cf.Body)
		runSnapshotPass(t, cf)
		require.Equal(t, old, cf.Body)
	})
}

func TestSnapshotFieldsRejectMixedDefinitions(t *testing.T) {
	cf := snapshotFixture()
	cf.Body = []isa.Instruction{
		mk(isa.OpMoveGeneral, 1, 0, engine.MoveGeneralModeSnapshot),
		mk(isa.OpJumpIfFalse, 0, 1, 0),
		mk(isa.OpMoveGeneral, 1, 2, engine.MoveGeneralModeAlias),
		mk(isa.OpGetStructFieldIntT0, 0, 1, 0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, program.SetJumpTarget(cf.Body, 1, 3))
	old := slices.Clone(cf.Body)
	runSnapshotPass(t, cf)
	require.Equal(t, old, cf.Body)
}

func TestSnapshotFieldsCaptureOutOfLineRead(t *testing.T) {
	cf := snapshotFixture()
	cf.Body = []isa.Instruction{
		mk(isa.OpMoveGeneral, 1, 0, engine.MoveGeneralModeSnapshot),
		tier1Jump(1),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
		mk(isa.OpGetStructFieldIntT0, 0, 1, 0),
		tier1Jump(-3),
	}
	runSnapshotPass(t, cf)
	require.Equal(t, isa.OpGetStructFieldIntT0, cf.Body[0].Op)
	require.Equal(t, isa.NewTier1Instruction(isa.SubOpMoveInt, 0, 2), cf.Body[3])
}

func TestSnapshotFieldsPreserveDebugValues(t *testing.T) {
	cf := snapshotFixture()
	cf.DebugVarTable = &program.DebugVarTable{Entries: []program.DebugVarEntry{{StartPC: 1, EndPC: 5}}}
	runSnapshotPass(t, cf)
	require.Equal(t, mk(isa.OpMoveGeneral, 1, 0, engine.MoveGeneralModeDebugSnapshot), cf.Body[0])
	require.Equal(t, mk(isa.OpGetStructFieldIntT0, 2, 1, 0), cf.Body[1])
	require.Equal(t, isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 1, 1), cf.Body[2])
	require.Equal(t, 4, cf.DebugVarTable.Entries[0].StartPC)
	require.Equal(t, 8, cf.DebugVarTable.Entries[0].EndPC)
}

func TestSnapshotFieldsRelocateSourceAndJumpMetadata(t *testing.T) {
	cf := snapshotFixture()
	cf.Body = append([]isa.Instruction{tier1Jump(0)}, cf.Body...)
	files := []string{"source.go"}
	cf.DebugSourceMap = &program.SourceMap{Files: &files, Positions: make([]program.SourcePosition, len(cf.Body))}
	for i := range cf.DebugSourceMap.Positions {
		cf.DebugSourceMap.Positions[i].Line = int32(i + 1)
	}
	cf.ArenaSafeAllocPCs = map[int]bool{2: true}
	runSnapshotPass(t, cf)
	require.Len(t, cf.DebugSourceMap.Positions, len(cf.Body))
	target, ok := program.JumpTargetAt(cf.Body, 0)
	require.True(t, ok)
	require.Equal(t, 1, target)
	require.Equal(t, cf.DebugSourceMap.Positions[1], cf.DebugSourceMap.Positions[3])
	require.True(t, cf.ArenaSafeAllocPCs[4])
}

func TestSnapshotFieldsIgnoreUnreachablePaddingPredecessors(t *testing.T) {
	cf := snapshotFixture()
	cf.Body = []isa.Instruction{
		mk(isa.OpMoveGeneral, 1, 0, engine.MoveGeneralModeSnapshot),
		mk(isa.OpGetStructFieldIntT0, 0, 1, 0),
		tier1Jump(2),
		mk(isa.OpNop, 0, 0, 0),
		mk(isa.OpNop, 0, 0, 0),
		mk(isa.OpGetStructFieldIntT0, 1, 1, 0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	runSnapshotPass(t, cf)
	require.Equal(t, isa.OpGetStructFieldIntT0, cf.Body[0].Op)
	require.Equal(t, isa.NewTier1Instruction(isa.SubOpMoveInt, 1, 2), cf.Body[5])
}
