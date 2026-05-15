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

func TestEmitSetStructFieldGeneralT0(t *testing.T) {
	t.Parallel()
	e := asmgen.NewEmitter()
	New().EmitSetStructFieldGeneralT0(e)
	output := e.String()
	for _, want := range []string{
		"$runtime·writeBarrier(SB), R3",
		"OFF_LAYOUT_FLAGS(R5), R6",
		"ss_flags:",
		"$22, R6",
		"$20, R6",
		"$LAYOUT_FLAG_CYCLE_BROKEN, R6",
		"ABI_TYPE_PTR_ELEM(R9), R9",
		"OFF_LAYOUT_FIELD_TYPE_INDEX(R5), R4",
		"OFF_LAYOUT_TYPE_INDEX(R5), R4",
		"FN_TYPE_TABLE_LEN(R7), R3",
		"ssp_ptr_recv:", "ssp_have_base:", "ssi_ptr_recv:", "ssi_have_base:",
		"ss_ptr_store:", "ss_ptr_nil:", "ss_iface_data:", "ss_iface_store:", "ss_iface_nil:",
		"ss_done:", "sf_shim:",
		"·handlerPathBShimSetStructFieldGeneralT0(SB)",
	} {
		require.Contains(t, output, want)
	}
	require.Equal(t, 1, strings.Count(output, "DISPATCH_NEXT"), "one dispatch macro per handler: it defines a label")
	for _, kind := range []string{"$18", "$19", "$21", "$26"} {
		require.Contains(t, output, "CMP  "+kind+", R4", "direct-interface held kind %s accepted by the cycle-broken arm", kind)
	}
	require.Contains(t, output, "R4, (R6)", "pointer arm stores the data word through the field pointer")
	require.Contains(t, output, "R4, 8(R6)", "cycle-broken arm stores the eface data word")
	seen := map[string]int{}
	for line := range strings.SplitSeq(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(trimmed, "//") {
			seen[trimmed]++
		}
	}
	for label, count := range seen {
		require.Equalf(t, 1, count, "label %s defined once", label)
	}
}
