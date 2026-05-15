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
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestCseStructFieldReadGeneralBank(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 6, 4, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[0].Op)
	require.Equal(t, isa.OpLoadIntConst, compiledFunction.Body[1].Op)
	require.Equal(t, isa.OpMoveGeneral, compiledFunction.Body[2].Op)
	require.Equal(t, uint8(6), compiledFunction.Body[2].A, "matched read keeps its dest reg")
	require.Equal(t, uint8(5), compiledFunction.Body[2].B, "MOVE_GENERAL source is first-read dest")
}

func TestCseStructFieldReadScalarTier0Int(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldIntT0, 3, 4, 7),
		mk(isa.OpLoadIntConst, 9, 0, 0),
		mk(isa.OpGetStructFieldIntT0, 8, 4, 7),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetStructFieldIntT0, compiledFunction.Body[0].Op)
	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[2].Op)
	require.Equal(t, uint8(isa.SubOpMoveInt), compiledFunction.Body[2].A)
	require.Equal(t, uint8(8), compiledFunction.Body[2].B, "MOVE_INT dest preserves matched read dest")
	require.Equal(t, uint8(3), compiledFunction.Body[2].C, "MOVE_INT source is first-read dest")
}

func TestCseStructFieldReadScalarTier0Uint(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldUint, 2, 1, 5),
		mk(isa.OpGetStructFieldUint, 3, 1, 5),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[1].Op)
	require.Equal(t, uint8(isa.SubOpMoveUint), compiledFunction.Body[1].A)
	require.Equal(t, uint8(3), compiledFunction.Body[1].B)
	require.Equal(t, uint8(2), compiledFunction.Body[1].C)
}

func TestCseStructFieldReadScalarTier0Float(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldFloat, 0, 1, 5),
		mk(isa.OpGetStructFieldFloat, 2, 1, 5),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[1].Op)
	require.Equal(t, uint8(isa.SubOpMoveFloat), compiledFunction.Body[1].A)
}

func TestCseStructFieldReadScalarTier0Bool(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldBool, 0, 1, 5),
		mk(isa.OpGetStructFieldBool, 2, 1, 5),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[1].Op)
	require.Equal(t, uint8(isa.SubOpMoveBool), compiledFunction.Body[1].A)
}

func TestCseStructFieldReadTier1String(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpGetStructFieldString), 5, 4),
		mk(isa.OpExt, 10, 0, 0),
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpDrillTier1, uint8(isa.SubOpGetStructFieldString), 6, 4),
		mk(isa.OpExt, 10, 0, 0),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[0].Op, "first read preserved")
	require.Equal(t, uint8(isa.SubOpGetStructFieldString), compiledFunction.Body[0].A)
	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[3].Op)
	require.Equal(t, uint8(isa.SubOpMoveString), compiledFunction.Body[3].A, "second read became MOVE_STRING")
	require.Equal(t, uint8(6), compiledFunction.Body[3].B, "MOVE_STRING dest preserved")
	require.Equal(t, uint8(5), compiledFunction.Body[3].C, "MOVE_STRING source is first-read dest")
	require.Equal(t, isa.OpNop, compiledFunction.Body[4].Op, "trailing EXT word nopped")
}

func TestCseStructFieldReadBailsOnInterveningCall(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 6, 4, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[2].Op, "second read preserved after call")
}

func TestCseStructFieldReadBailsOnInterveningSetDifferentField(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpSetStructFieldGeneral, 4, 7, 3),
		mk(isa.OpGetStructFieldGeneral, 6, 4, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[2].Op, "second read preserved after set to a different field of same receiver (conservative)")
}

func TestCseStructFieldReadBailsOnReceiverWrite(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpMoveGeneral, 4, 7, 0),
		mk(isa.OpGetStructFieldGeneral, 6, 4, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[2].Op, "second read preserved after receiver overwrite")
}

func TestCseStructFieldReadMovePropagation(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpMoveGeneral, 9, 5, 0),
		mk(isa.OpGetStructFieldGeneral, 6, 4, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpMoveGeneral, compiledFunction.Body[2].Op, "second read CSE'd via alias")
	require.Equal(t, uint8(6), compiledFunction.Body[2].A)
	require.Equal(t, uint8(5), compiledFunction.Body[2].B, "smaller-index alias chosen as source")
}

func TestCseStructFieldReadDifferentReceiverNotMatched(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpGetStructFieldGeneral, 6, 7, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[1].Op, "different receiver, not a match")
}

func TestCseStructFieldReadDifferentLayoutNotMatched(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpGetStructFieldGeneral, 6, 4, 3),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[1].Op, "different layout, not a match")
}

func TestCseStructFieldReadPostSetGetGeneralBank(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpSetStructFieldGeneral, 4, 9, 2),
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
	}

	compiledFunction := &program.CompiledFunction{
		Body: body,
		StructLayoutTable: []program.StructFieldLayout{
			{}, {}, {Kind: uint8(reflect.Pointer)},
		},
	}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpMoveGeneral, compiledFunction.Body[2].Op, "GET after SET became MOVE")
	require.Equal(t, uint8(5), compiledFunction.Body[2].A)
	require.Equal(t, uint8(9), compiledFunction.Body[2].B, "MOVE_GENERAL source is SET's value register")
}

func TestCseStructFieldReadPostSetGetBailsOnMutation(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpSetStructFieldGeneral, 4, 9, 2),
		mk(isa.OpMoveGeneral, 9, 8, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[2].Op, "value reg overwritten; GET preserved")
}

func TestCseStructFieldReadBailsAtJumpTarget(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(0)
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpJumpIfFalse, 0, lo, hi),
		mk(isa.OpGetStructFieldGeneral, 6, 4, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[2].Op, "PC 2 is a jump target of PC 1's JUMP_IF_FALSE; CSE refused")
}

func TestCseStructFieldReadEmitsAliasMode(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldGeneral, 2, 0, 0),
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 3, 0, 0),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpMoveGeneral, compiledFunction.Body[2].Op)
	require.Equal(t, engine.MoveGeneralModeAlias, compiledFunction.Body[2].C,
		"the replaced read returns an addressable view; a dynamic-mode move would snapshot it "+
			"and any later store through the register would be lost")
}

func TestCseStructFieldReadBailsOnInterveningElementStore(t *testing.T) {
	t.Parallel()
	for _, store := range []isa.Opcode{
		isa.OpSliceSetInt, isa.OpSliceSetFloat, isa.OpSliceSetString,
		isa.OpSliceSetBool, isa.OpSliceSetUint, isa.OpIndexSet,
	} {
		body := []isa.Instruction{
			mk(isa.OpGetStructFieldGeneral, 2, 0, 0),
			mk(store, 2, 0, 1),
			mk(isa.OpGetStructFieldGeneral, 3, 0, 0),
		}
		compiledFunction := &program.CompiledFunction{Body: body}
		_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
		require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[2].Op,
			"%v writes through its collection operand, so it must terminate the scan", store)
	}
}

func TestCseStructFieldReadGetFieldEmitsAliasMode(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetField, 2, 0, 0),
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpGetField, 3, 0, 0),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpMoveGeneral, compiledFunction.Body[2].Op)
	require.Equal(t, engine.MoveGeneralModeAlias, compiledFunction.Body[2].C)
}

func TestCseStructFieldReadRejectsSnapshotModeAliasCandidate(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpMoveGeneral, 1, 5, engine.MoveGeneralModeSnapshot),
		mk(isa.OpGetStructFieldGeneral, 6, 4, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	if compiledFunction.Body[2].Op == isa.OpMoveGeneral {
		require.NotEqual(t, uint8(1), compiledFunction.Body[2].B,
			"a snapshot-mode move destination holds a detached copy, so it is not address-identical "+
				"to the field and must never become the rewrite source")
	}
}

func TestCseStructFieldReadAcceptsAliasModeAliasCandidate(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		mk(isa.OpMoveGeneral, 1, 5, engine.MoveGeneralModeAlias),
		mk(isa.OpGetStructFieldGeneral, 6, 4, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpMoveGeneral, compiledFunction.Body[2].Op,
		"an alias-mode move stays address-identical, so it remains a valid rewrite source")
}

func TestCseStructFieldReadPostSetGetBailsOnArrayField(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpSetStructFieldGeneral, 4, 9, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 0),
	}
	compiledFunction := &program.CompiledFunction{
		Body:              body,
		StructLayoutTable: []program.StructFieldLayout{{Kind: uint8(reflect.Array)}},
	}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[1].Op,
		"an array field must be re-read: no move from the setter's source reproduces an "+
			"addressable view onto the struct")
}

func TestCseStructFieldReadPostSetGetBailsOnStructField(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpSetStructFieldGeneral, 4, 9, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 0),
	}
	compiledFunction := &program.CompiledFunction{
		Body:              body,
		StructLayoutTable: []program.StructFieldLayout{{Kind: uint8(reflect.Struct)}},
	}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[1].Op)
}

func TestCseStructFieldReadPostSetGetBailsOnUnknownLayout(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpSetStructFieldGeneral, 4, 9, 7),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 7),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = ElideRedundantStructFieldRead(context.Background(), compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[1].Op,
		"an unresolvable layout index must fail closed rather than permit the rewrite")
}
