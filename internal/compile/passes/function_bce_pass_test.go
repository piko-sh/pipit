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
		mk(isa.OpDrillTier1, uint8(isa.SubOpLenSliceIntDirect), 1, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpLtInt, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpSliceGetIntDirect, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{0}}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetIntDirectUnchecked, compiledFunction.Body[4].Op,
		"slice read inside proven-safe range becomes unchecked")
	require.Equal(t, uint8(4), compiledFunction.Body[4].A)
	require.Equal(t, uint8(0), compiledFunction.Body[4].B)
	require.Equal(t, uint8(2), compiledFunction.Body[4].C)
}

func TestBceRewritesSetAfterLenLtJump(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLenSliceIntDirect), 1, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpLtInt, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpSliceSetIntDirect, 0, 2, 4),
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{0}}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceSetIntDirectUnchecked, compiledFunction.Body[4].Op,
		"slice write inside proven-safe range becomes unchecked")
}

func TestBceRefusesNegativeConstantIndex(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLenSliceIntDirect), 1, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpLtInt, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpSliceGetIntDirect, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{-1}}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetIntDirect, compiledFunction.Body[4].Op,
		"a negative index satisfies signed index < len yet is out of range; "+
			"the unchecked rewrite must be refused")
}

func TestBceRefusesIndexWithUnknownSign(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLenSliceIntDirect), 1, 0),
		mk(isa.OpLtInt, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpSliceGetIntDirect, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetIntDirect, compiledFunction.Body[3].Op,
		"index register with no proven lower bound must not be rewritten")
}

func TestBceRewritesGetWithSmallConstIndex(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLenSliceIntDirect), 1, 0),
		mk(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 2, 3),
		mk(isa.OpLtInt, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpSliceGetIntDirect, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetIntDirectUnchecked, compiledFunction.Body[4].Op,
		"a small-const index is provably non-negative and may be rewritten")
}

func TestBceRefusesWhenLenRegOverwritten(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLenSliceIntDirect), 1, 0),
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpLtInt, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpSliceGetIntDirect, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetIntDirect, compiledFunction.Body[5].Op,
		"len register overwritten before lt-jump invalidates the fact")
}

func TestBceRefusesWhenIndexOverwrittenAfterFact(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLenSliceIntDirect), 1, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpLtInt, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpSliceGetIntDirect, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetIntDirect, compiledFunction.Body[5].Op,
		"index register overwritten after the proof invalidates the fact")
}

func TestBceRefusesAcrossCall(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLenSliceIntDirect), 1, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpLtInt, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
		mk(isa.OpSliceGetIntDirect, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetIntDirect, compiledFunction.Body[5].Op,
		"call between proof and access invalidates the fact conservatively")
}

func TestBceRefusesAtJumpTarget(t *testing.T) {
	t.Parallel()
	jumpOver, _ := jumpOffset(0)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLenSliceIntDirect), 1, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpLtInt, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, jumpOver, 0),
		mk(isa.OpSliceGetIntDirect, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetIntDirect, compiledFunction.Body[4].Op,
		"access PC is a jump target; fact does not survive")
}

func TestBceDifferentSliceNoMatch(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLenSliceIntDirect), 1, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpLtInt, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpSliceGetIntDirect, 4, 7, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetIntDirect, compiledFunction.Body[4].Op,
		"fact was for slice register 0; access uses 7 - no rewrite")
}

func TestBceRewritesReflectGetAfterLenLtJump(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLen), 1, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpLtInt, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpSliceGetInt, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{0}}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetIntUnchecked, compiledFunction.Body[4].Op,
		"reflect-bank slice read inside proven-safe range becomes unchecked")
	require.Equal(t, uint8(4), compiledFunction.Body[4].A)
	require.Equal(t, uint8(0), compiledFunction.Body[4].B)
	require.Equal(t, uint8(2), compiledFunction.Body[4].C)
}

func TestBceRewritesReflectSetAfterLenLtJump(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLen), 1, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpLtInt, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpSliceSetInt, 0, 2, 4),
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{0}}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceSetIntUnchecked, compiledFunction.Body[4].Op,
		"reflect-bank slice write inside proven-safe range becomes unchecked")
}

func TestBceReflectBankDoesNotMatchTypedBankFact(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLenSliceIntDirect), 1, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpLtInt, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpSliceGetInt, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetInt, compiledFunction.Body[4].Op,
		"typed-bank len fact must not authorise a reflect-bank rewrite even though the slice register number matches")
}

func TestBceTypedBankDoesNotMatchReflectBankFact(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLen), 1, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpLtInt, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		mk(isa.OpSliceGetIntDirect, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetIntDirect, compiledFunction.Body[4].Op,
		"reflect-bank len fact must not authorise a typed-bank rewrite even though the slice register number matches")
}

func TestBceStableDerefPreservesFactAcrossRedundantDeref(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 1),
		mk(isa.OpDeref, 1, 0, 0),
		mk(isa.OpDrillTier1, uint8(isa.SubOpLen), 1, 1),
		mk(isa.OpLtInt, 2, 0, 1),
		mk(isa.OpJumpIfFalse, 2, 4, 0),
		mk(isa.OpDeref, 1, 0, 0),
		mk(isa.OpSliceGetInt, 3, 1, 0),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetIntUnchecked, compiledFunction.Body[6].Op,
		"idempotent re-DEREF of the same pointer-to-slice must preserve the BCE proof so the access at PC 6 rewrites to unchecked")
}

func TestBceStableDerefDoesNotPreserveAcrossSourceWrite(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 1),
		mk(isa.OpDeref, 1, 0, 0),
		mk(isa.OpDrillTier1, uint8(isa.SubOpLen), 1, 1),
		mk(isa.OpLtInt, 2, 0, 1),
		mk(isa.OpJumpIfFalse, 2, 5, 0),
		mk(isa.OpDeref, 0, 1, 0),
		mk(isa.OpDeref, 1, 0, 0),
		mk(isa.OpSliceGetInt, 3, 1, 0),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetInt, compiledFunction.Body[7].Op,
		"a write to the DEREF source register between two DEREFs invalidates the idempotency - the value could differ")
}

func TestBceStableDerefDoesNotPreserveAcrossNonDerefWrite(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 1),
		mk(isa.OpDeref, 1, 0, 0),
		mk(isa.OpDrillTier1, uint8(isa.SubOpLen), 1, 1),
		mk(isa.OpLtInt, 2, 0, 1),
		mk(isa.OpJumpIfFalse, 2, 5, 0),
		mk(isa.OpMoveGeneral, 1, 5, 0),
		mk(isa.OpDeref, 1, 0, 0),
		mk(isa.OpSliceGetInt, 3, 1, 0),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetInt, compiledFunction.Body[7].Op,
		"a non-DEREF write to the destination register before the second DEREF should still allow re-establishment, but the current rule only matches against the most recent producer pattern so the rewrite remains conservative")
}

func TestBceRewritesGetAfterFusedLtJump(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLenSliceIntDirect), 1, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 2, 1),
		mk(isa.OpExt, lo, hi, 0),
		mk(isa.OpSliceGetIntDirect, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{0}}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetIntDirectUnchecked, compiledFunction.Body[4].Op,
		"slice read guarded by the fused compare-and-branch becomes unchecked")
	require.Equal(t, isa.OpExt, compiledFunction.Body[3].Op, "the offset word is untouched")
}

func TestBceIgnoresFusedLtJumpWithSwappedOperands(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLenSliceIntDirect), 1, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 1, 2),
		mk(isa.OpExt, lo, hi, 0),
		mk(isa.OpSliceGetIntDirect, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{0}}
	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
	require.Equal(t, isa.OpSliceGetIntDirect, compiledFunction.Body[4].Op,
		"len < index proves nothing about the index")
}
