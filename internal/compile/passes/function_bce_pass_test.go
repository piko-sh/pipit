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
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestBceRewritesGetAfterLenLtJump(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthSliceInt64), 1, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpGetSliceInt64Element, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{0}}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetSliceInt64ElementUnchecked, compiledFunction.Body[4].Op,
		"slice read inside proven-safe range becomes unchecked")
	require.Equal(t, uint8(4), compiledFunction.Body[4].A)
	require.Equal(t, uint8(0), compiledFunction.Body[4].B)
	require.Equal(t, uint8(2), compiledFunction.Body[4].C)
}

func TestBceRewritesSetAfterLenLtJump(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthSliceInt64), 1, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpSetSliceInt64Element, 0, 2, 4),
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{0}}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSetSliceInt64ElementUnchecked, compiledFunction.Body[4].Op,
		"slice write inside proven-safe range becomes unchecked")
}

func TestBceRefusesNegativeConstantIndex(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthSliceInt64), 1, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpGetSliceInt64Element, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{-1}}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetSliceInt64Element, compiledFunction.Body[4].Op,
		"a negative index satisfies signed index < len yet is out of range; "+
			"the unchecked rewrite must be refused")
}

func TestBceRefusesIndexWithUnknownSign(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthSliceInt64), 1, 0),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpGetSliceInt64Element, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetSliceInt64Element, compiledFunction.Body[3].Op,
		"index register with no proven lower bound must not be rewritten")
}

func TestBceRewritesGetWithSmallConstIndex(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthSliceInt64), 1, 0),
		mk(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), 2, 3),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpGetSliceInt64Element, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetSliceInt64ElementUnchecked, compiledFunction.Body[4].Op,
		"a small-const index is provably non-negative and may be rewritten")
}

func TestBceRefusesWhenLenRegOverwritten(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthSliceInt64), 1, 0),
		mk(isa.OpLoadInt64Constant, 1, 0, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpGetSliceInt64Element, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetSliceInt64Element, compiledFunction.Body[5].Op,
		"len register overwritten before lt-jump invalidates the fact")
}

func TestBceRefusesWhenIndexOverwrittenAfterFact(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthSliceInt64), 1, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpGetSliceInt64Element, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetSliceInt64Element, compiledFunction.Body[5].Op,
		"index register overwritten after the proof invalidates the fact")
}

func TestBceRefusesAcrossCall(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthSliceInt64), 1, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
		mk(isa.OpGetSliceInt64Element, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetSliceInt64Element, compiledFunction.Body[5].Op,
		"call between proof and access invalidates the fact conservatively")
}

func TestBceRefusesAtJumpTarget(t *testing.T) {
	t.Parallel()
	jumpOver, _ := jumpOffset(0)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthSliceInt64), 1, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, jumpOver, 0),
		mk(isa.OpGetSliceInt64Element, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetSliceInt64Element, compiledFunction.Body[4].Op,
		"access PC is a jump target; fact does not survive")
}

func TestBceDifferentSliceNoMatch(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthSliceInt64), 1, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpGetSliceInt64Element, 4, 7, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetSliceInt64Element, compiledFunction.Body[4].Op,
		"fact was for slice register 0; access uses 7 - no rewrite")
}

func TestBceRewritesReflectGetAfterLenLtJump(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthGeneral), 1, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpGetElementInt64, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{0}}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetElementInt64Unchecked, compiledFunction.Body[4].Op,
		"reflect-bank slice read inside proven-safe range becomes unchecked")
	require.Equal(t, uint8(4), compiledFunction.Body[4].A)
	require.Equal(t, uint8(0), compiledFunction.Body[4].B)
	require.Equal(t, uint8(2), compiledFunction.Body[4].C)
}

func TestBceRewritesReflectSetAfterLenLtJump(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthGeneral), 1, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpSetElementInt64, 0, 2, 4),
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{0}}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSetElementInt64Unchecked, compiledFunction.Body[4].Op,
		"reflect-bank slice write inside proven-safe range becomes unchecked")
}

func TestBceReflectBankDoesNotMatchTypedBankFact(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthSliceInt64), 1, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpGetElementInt64, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetElementInt64, compiledFunction.Body[4].Op,
		"typed-bank len fact must not authorise a reflect-bank rewrite even though the slice register number matches")
}

func TestBceTypedBankDoesNotMatchReflectBankFact(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthGeneral), 1, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpGetSliceInt64Element, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetSliceInt64Element, compiledFunction.Body[4].Op,
		"reflect-bank len fact must not authorise a typed-bank rewrite even though the slice register number matches")
}

func TestBceStableDerefPreservesFactAcrossRedundantDeref(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), 0, 1),
		mk(isa.OpDereference, 1, 0, 0),
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthGeneral), 1, 1),
		mk(isa.OpLessThanInt64, 2, 0, 1),
		mk(isa.OpJumpIfFalse, 2, 4, 0),
		mk(isa.OpDereference, 1, 0, 0),
		mk(isa.OpGetElementInt64, 3, 1, 0),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetElementInt64Unchecked, compiledFunction.Body[6].Op,
		"idempotent re-DEREFERENCE of the same pointer-to-slice must preserve the BCE proof so the access at PC 6 rewrites to unchecked")
}

func TestBceStableDerefDoesNotPreserveAcrossSourceWrite(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), 0, 1),
		mk(isa.OpDereference, 1, 0, 0),
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthGeneral), 1, 1),
		mk(isa.OpLessThanInt64, 2, 0, 1),
		mk(isa.OpJumpIfFalse, 2, 5, 0),
		mk(isa.OpDereference, 0, 1, 0),
		mk(isa.OpDereference, 1, 0, 0),
		mk(isa.OpGetElementInt64, 3, 1, 0),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetElementInt64, compiledFunction.Body[7].Op,
		"a write to the DEREFERENCE source register between two DEREFERENCE instructions invalidates the idempotency - the value could differ")
}

func TestBceStableDerefDoesNotPreserveAcrossNonDerefWrite(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), 0, 1),
		mk(isa.OpDereference, 1, 0, 0),
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthGeneral), 1, 1),
		mk(isa.OpLessThanInt64, 2, 0, 1),
		mk(isa.OpJumpIfFalse, 2, 5, 0),
		mk(isa.OpMoveGeneral, 1, 5, 0),
		mk(isa.OpDereference, 1, 0, 0),
		mk(isa.OpGetElementInt64, 3, 1, 0),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetElementInt64, compiledFunction.Body[7].Op,
		"a non-DEREFERENCE write to the destination register before the second DEREFERENCE should still allow re-establishment, but the current rule only matches against the most recent producer pattern so the rewrite remains conservative")
}

func TestBceRewritesGetAfterFusedLtJump(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthSliceInt64), 1, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		isa.NewTier1Instruction(isa.SubOpLessThanInt64ThenJumpIfFalse, 2, 1),
		mk(isa.OpExtension, lo, hi, 0),
		mk(isa.OpGetSliceInt64Element, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{0}}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetSliceInt64ElementUnchecked, compiledFunction.Body[4].Op,
		"slice read guarded by the fused compare-and-branch becomes unchecked")
	require.Equal(t, isa.OpExtension, compiledFunction.Body[3].Op, "the offset word is untouched")
}

func TestBceIgnoresFusedLtJumpWithSwappedOperands(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthSliceInt64), 1, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		isa.NewTier1Instruction(isa.SubOpLessThanInt64ThenJumpIfFalse, 1, 2),
		mk(isa.OpExtension, lo, hi, 0),
		mk(isa.OpGetSliceInt64Element, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{0}}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpGetSliceInt64Element, compiledFunction.Body[4].Op,
		"len < index proves nothing about the index")
}
