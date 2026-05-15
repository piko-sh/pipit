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

package asmgen_arch_amd64

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/asmgen"
)

func TestEmitCallInlineMaybeSetupGeneralBankFastArm(t *testing.T) {
	t.Parallel()
	e := asmgen.NewEmitter()
	(&amd64InlineCallOps{}).emitCallInlineMaybeSetupGeneralBank(e)
	output := e.String()
	assertContainsAll(t, output,
		"ACI_IS_FAST_PATH(AX), $3",
		"runtime·writeBarrier(SB), $0",
		"ACI_CALLEE_NUM_GENERAL(AX), DI",
		"CTX_ARENA_GEN_IDX(R15), CX",
		"CTX_ARENA_GENERAL_SLAB_CAP(R15)",
		"ACI_NUM_GENERAL_ARGS(AX), SI",
		"CTX_GENERALS_BASE(R15), R14",
		"(ACI_GENERAL_ARG_SRCS)(AX)(R12*8), R13",
		"ci_general_bank_check_loop:", "ci_general_bank_check_next:", "ci_general_bank_alloc:",
		"CTX_ARENA_GENERAL_SLAB(R15), CX",
		"CX, CF_REGS_GENERAL_PTR(BX)", "DI, CF_REGS_GENERAL_LEN(BX)", "DI, CF_REGS_GENERAL_CAP(BX)",
		"$1, CF_HAS_GENERAL_ALLOC(BX)",
		"DX, CTX_ARENA_GEN_IDX(R15)",
		"$1, CTX_ARENA_BANKS_DIRTY(R15)",
		"ci_general_bank_copy_loop:", "ci_general_bank_save:",
		"CTX_DISPATCH_SAVES(R15), DX",
		"ACI_CALLEE_BODY(AX), DI", "ACI_CALLEE_UINT_CONSTS(AX), DI", "DI, 48(DX)",
		"ci_general_bank_slow:",
		"·handlerCallInlineSetupGeneralBank(SB)",
		"ci_no_general_bank:",
	)
	for _, kind := range []string{"$25", "$17"} {
		require.Contains(t, output, "CMPQ    DX, "+kind, "struct and array arguments (%s) go to the slow arm", kind)
	}
	require.Less(t, strings.Index(output, "ci_general_bank_slow:"), strings.Index(output, "ci_no_general_bank:"), "slow arm falls through into the shared exit")
	require.Less(t, strings.Index(output, "JMP     ci_no_general_bank"), strings.Index(output, "ci_general_bank_slow:"), "fast arm jumps over the slow arm")
	require.NotContains(t, output, "DISPATCH_NEXT", "the stage never dispatches; the reload stage does")
}

func TestEmitReturnInlineCopiesGeneralResult(t *testing.T) {
	t.Parallel()
	e := asmgen.NewEmitter()
	(&amd64InlineCallOps{}).EmitReturnInline(e)
	output := e.String()
	assertContainsAll(t, output,
		"CMPQ    AX, $3", "JE      ri_check_general", "ri_check_general:",
		"runtime·writeBarrier(SB), $0",
		"CF_REGS_GENERAL_LEN(DI), $0",
		"CF_REGS_GENERAL_PTR(DI), AX", "CF_REGS_GENERAL_PTR(R12), BX",
		"LEAQ    (CX)(CX*2), CX", "CX, 16(BX)",
	)
	require.Less(t, strings.Index(output, "ri_check_general:"), strings.Index(output, "ri_multi:"), "general arm sits with the single-result arms")
	require.Less(t, strings.Index(output, "ri_check_general:"), strings.Index(output, "ri_no_retval:"), "copy precedes the arena clears")
}
