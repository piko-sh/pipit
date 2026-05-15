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

func TestEmitSetStructFieldGeneralT0(t *testing.T) {
	t.Parallel()
	e := asmgen.NewEmitter()
	New().EmitSetStructFieldGeneralT0(e)
	output := e.String()
	assertContainsAll(t, output,
		"runtime·writeBarrier(SB), $0",
		"OFF_LAYOUT_FLAGS(SI), CX",
		"ss_flags:",
		"OFF_LAYOUT_KIND(SI), $22",
		"OFF_LAYOUT_KIND(SI), $20",
		"$LAYOUT_FLAG_CYCLE_BROKEN",
		"ABI_TYPE_PTR_ELEM(CX), CX",
		"OFF_LAYOUT_FIELD_TYPE_INDEX(SI), SI",
		"OFF_LAYOUT_TYPE_INDEX(SI), BX",
		"FN_TYPE_TABLE_LEN(AX)",
		"ssp_ptr_recv:", "ssp_have_base:", "ssi_ptr_recv:", "ssi_have_base:",
		"ss_ptr_store:", "ss_ptr_nil:", "ss_iface_data:", "ss_iface_store:", "ss_iface_nil:",
		"ss_done:", "sf_shim:",
		"·handlerPathBShimSetStructFieldGeneralT0(SB)",
	)
	require.Equal(t, 1, strings.Count(output, "DISPATCH_NEXT"), "one dispatch macro per handler: it defines a label")
	for _, kind := range []string{"$18", "$19", "$21", "$26"} {
		require.Contains(t, output, "CMPQ    SI, "+kind, "direct-interface held kind %s accepted by the cycle-broken arm", kind)
	}
	require.Contains(t, output, "MOVQ    AX, (DI)", "pointer arm stores the data word through the field pointer")
	require.Contains(t, output, "MOVQ    BX, 8(DI)", "cycle-broken arm stores the eface data word")
}

func TestEmitSetStructFieldGeneralT0LabelsAreUnique(t *testing.T) {
	t.Parallel()
	e := asmgen.NewEmitter()
	New().EmitSetStructFieldGeneralT0(e)
	seen := map[string]int{}
	for line := range strings.SplitSeq(e.String(), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(trimmed, "//") {
			seen[trimmed]++
		}
	}
	for label, count := range seen {
		require.Equalf(t, 1, count, "label %s defined once", label)
	}
}
