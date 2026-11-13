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
	"testing"

	"github.com/stretchr/testify/require"

	"piko.sh/asmgen"
)

func TestEmitUintConstArithKeepsThe32BitFormsToTheirLow32Bits(t *testing.T) {
	t.Parallel()

	const narrowing = "MOVL    DI, DI"
	tests := []struct {
		operation string
		applied   string
		narrows   bool
	}{
		{operation: "add", applied: "ADDQ    CX, DI"},
		{operation: "sub", applied: "SUBQ    CX, DI"},
		{operation: "and", applied: "ANDQ    CX, DI"},
		{operation: "add32", applied: "ADDQ    CX, DI", narrows: true},
		{operation: "sub32", applied: "SUBQ    CX, DI", narrows: true},
	}
	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			t.Parallel()
			e := asmgen.NewEmitter()

			New().EmitUintConstArith(e, tt.operation)

			assertContainsAll(t, e.String(), "CTX_UINT_CONSTS_BASE", tt.applied)
			_, narrows := uintConstantOperationAMD64(tt.operation)
			require.Equal(t, tt.narrows, narrows)
			if tt.narrows {
				require.Contains(t, e.String(), tt.applied+"\n\t"+narrowing, "the result is cut to 32 bits straight after the operation")
				return
			}
			require.NotContains(t, e.String(), narrowing, "a full-width form keeps every bit of its result")
		})
	}

	t.Run("refuses an operation it does not know", func(t *testing.T) {
		t.Parallel()

		require.Panics(t, func() { New().EmitUintConstArith(asmgen.NewEmitter(), "xor") },
			"an unknown operation must not become a handler that only copies its operand")
	})
}
