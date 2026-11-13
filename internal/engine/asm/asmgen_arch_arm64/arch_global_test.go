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

func TestEmitGlobalHandlersTakeTheScalarBanksAndLeaveTheRestToTheShim(t *testing.T) {
	t.Parallel()

	tests := []struct {
		emit func(arch *BytecodeARM64Arch, e *asmgen.Emitter, shimSymbol string)
		name string
		wide bool
	}{
		{name: "a read", emit: (*BytecodeARM64Arch).EmitGetGlobal},
		{name: "a wide read", emit: (*BytecodeARM64Arch).EmitGetGlobalWide, wide: true},
		{name: "a store", emit: (*BytecodeARM64Arch).EmitSetGlobal},
		{name: "a wide store", emit: (*BytecodeARM64Arch).EmitSetGlobalWide, wide: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := asmgen.NewEmitter()

			tt.emit(New(), e, "handlerPathBShimProbe")

			text := e.String()
			assertContainsAll(t, text, "CTX_GLOBALS", "global_shim:", "global_done:")
			shim := strings.Index(text, "global_shim:")
			require.Positive(t, shim)
			require.Contains(t, text[shim:], "·handlerPathBShimProbe(SB)")
			require.NotContains(t, text[:shim], "handlerPathBShimProbe", "the shim is reached only through its label")
			require.Equal(t, tt.wide, strings.Contains(text[:shim], "global_done:\n\tADD  $1, R20, R20"), "only a wide form steps over an extension word")
		})
	}
}
