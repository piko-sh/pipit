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

package asmgen_arch_arm64

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/asmgen"
)

func TestEmitCallInlineMaybeSetupGeneralBankFastArm(t *testing.T) {
	t.Parallel()
	e := asmgen.NewEmitter()
	(&arm64InlineCallOps{}).emitCallInlineMaybeSetupGeneralBank(e)
	output := e.String()
	for _, want := range []string{
		"ACI_IS_FAST_PATH(R2), R3",
		"$runtime·writeBarrier(SB), R3",
		"ACI_CALLEE_NUM_GENERAL(R2), R5",
		"CTX_ARENA_GEN_IDX(R19), R4",
		"CTX_ARENA_GENERAL_SLAB_CAP(R19), R7",
		"ACI_NUM_GENERAL_ARGS(R2), R7",
		"CTX_GENERALS_BASE(R19), R10",
		"$ACI_GENERAL_ARG_SRCS, R3, R11",
		"ci_general_bank_check_loop:", "ci_general_bank_check_next:", "ci_general_bank_alloc:",
		"CTX_ARENA_GENERAL_SLAB(R19), R3",
		"R4, CF_REGS_GENERAL_PTR(R9)", "R5, CF_REGS_GENERAL_LEN(R9)", "R5, CF_REGS_GENERAL_CAP(R9)",
		"R3, CF_HAS_GENERAL_ALLOC(R9)",
		"R6, CTX_ARENA_GEN_IDX(R19)",
		"R3, CTX_ARENA_BANKS_DIRTY(R19)",
		"ci_general_bank_copy_loop:", "ci_general_bank_save:",
		"CTX_DISPATCH_SAVES(R19), R3",
		"ACI_CALLEE_BODY(R2), R11", "ACI_CALLEE_UINT_CONSTS(R2), R11", "R11, 48(R3)",
		"ci_general_bank_slow:",
		"·handlerCallInlineSetupGeneralBank(SB)",
		"ci_no_general_bank:",
		"CMP  $25, R3", "CMP  $17, R3",
	} {
		require.Contains(t, output, want)
	}
	require.Less(t, strings.Index(output, "ci_general_bank_slow:"), strings.Index(output, "ci_no_general_bank:"), "slow arm falls through into the shared exit")
	require.NotContains(t, output, "DISPATCH_NEXT", "the stage never dispatches; the reload stage does")
}

func TestEmitReturnInlineCopiesGeneralResult(t *testing.T) {
	t.Parallel()
	e := asmgen.NewEmitter()
	(&arm64InlineCallOps{}).EmitReturnInline(e)
	output := e.String()
	for _, want := range []string{
		"CMP  $3, R1", "BEQ  ri_check_general", "ri_check_general:",
		"$runtime·writeBarrier(SB), R1",
		"CF_REGS_GENERAL_LEN(R8), R1",
		"CF_REGS_GENERAL_PTR(R8), R1", "CF_REGS_GENERAL_PTR(R22), R5",
		"R3, 16(R5)",
	} {
		require.Contains(t, output, want)
	}
	require.Less(t, strings.Index(output, "ri_check_general:"), strings.Index(output, "ri_no_retval:"), "copy precedes the arena clears")
}
