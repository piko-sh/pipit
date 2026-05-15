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

//go:build integration

package bytecode_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestPeepholeFiresOnCompiledOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		code        string
		wantFusedOp isa.Opcode
		wantSubOp   isa.SubOpcode
	}{
		{
			name:        "rune_to_string_then_concat",
			code:        `r := 'y'; "x" + string(r)`,
			wantFusedOp: isa.OpConcatRuneString,
		},
		{
			name:        "string_index_uint_to_int",
			code:        `s := "abc"; int(s[0])`,
			wantFusedOp: isa.OpStringIndexToInt,
		},
		{
			name:        "len_string_lt_in_for_loop",
			code:        `s := "hello"; for i := 0; i < len(s); i++ { _ = i }`,
			wantFusedOp: isa.OpDrillTier1,
			wantSubOp:   isa.SubOpLenStringLtJumpFalse,
		},
		{
			name:        "nil_check_eq",
			code:        `var x interface{}; if x == nil { _ = 1 }`,
			wantFusedOp: isa.OpDrillTier1,
			wantSubOp:   isa.SubOpEqInterfaceNil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := compileExpression(t, tc.code)
			require.NotNil(t, compiledFunction)
			found := walkBodyForOpcode(compiledFunction, tc.wantFusedOp, tc.wantSubOp)
			assert.True(t, found,
				"expected fused %s/%d in compiled bytecode (or any nested function); "+
					"if missing, the peephole pass is silently disabled - "+
					"check the corresponding fuse* function in function.go.\n"+
					"Top-level disassembly:\n%s",
				tc.wantFusedOp, tc.wantSubOp, compiledFunction.Disassemble())
		})
	}
}

func walkBodyForOpcode(compiledFunction *program.CompiledFunction, op isa.Opcode, subOp isa.SubOpcode) bool {
	if compiledFunction == nil {
		return false
	}
	for _, instr := range compiledFunction.Body {
		if instr.Op != op {
			continue
		}
		if op == isa.OpDrillTier1 && isa.SubOpcode(instr.A) != subOp {
			continue
		}
		return true
	}
	for _, nested := range program.ExportFunctions(compiledFunction) {
		if walkBodyForOpcode(nested, op, subOp) {
			return true
		}
	}
	return false
}
