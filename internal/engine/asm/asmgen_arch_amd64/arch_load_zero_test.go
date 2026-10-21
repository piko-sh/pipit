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

func TestEmitLoadZeroClearsTheFourScalarBanksAndLeavesTheRestToTheShim(t *testing.T) {
	t.Parallel()

	e := asmgen.NewEmitter()

	New().EmitLoadZero(e, "handlerPathBShimProbe")

	text := e.String()
	assertContainsAll(t, text, "$REGKIND_INT", "$REGKIND_UINT", "$REGKIND_FLOAT", "$REGKIND_BOOL", "zero_done:", "zero_shim:")
	shim := strings.Index(text, "zero_shim:")
	require.Positive(t, shim)
	require.Contains(t, text[shim:], "·handlerPathBShimProbe(SB)", "a bank that is not zeroed here goes to the shim")
	require.NotContains(t, text[:shim], "handlerPathBShimProbe", "nothing reaches the shim but the last refusal")
	for _, bank := range []string{"REGKIND_STRING", "REGKIND_GENERAL"} {
		require.NotContains(t, text, bank, "a bank that holds a pointer is never zeroed in the loop")
	}
}
