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
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/asmgen"
)

func TestEmitSetStructFieldGeneral(t *testing.T) {
	t.Parallel()
	e := asmgen.NewEmitter()
	New().EmitSetStructFieldGeneral(e)
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
		"·handlerPathBShimSetStructFieldGeneral(SB)",
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

func TestEmitSetStructFieldGeneralStoresANonNilPointerOnlyThroughTheRevokedEpochRoute(t *testing.T) {
	t.Parallel()
	emitter := asmgen.NewEmitter()
	New().EmitSetStructFieldGeneral(emitter)
	var lines []string
	for line := range strings.SplitSeq(emitter.String(), "\n") {
		trimmed := strings.Join(strings.Fields(line), " ")
		if trimmed != "" && !strings.HasPrefix(trimmed, "//") {
			lines = append(lines, trimmed)
		}
	}
	route := func(write string) []string {
		return []string{
			"MOVD CTX_ARENA_ROUTE_STATE(R19), R2", "CBZ R2, sf_shim",
			"MOVBU ROUTE_LOCAL_BLIND(R2), R7", "CBZ R7, sf_shim",
			"AND $FLAG_RO, R3, R7", "CBNZ R7, sf_shim",
			"MOVD CTX_FRAME_POINTER(R19), R7", "MOVD $CALLFRAME_SIZE, R3", "MUL R3, R7, R7",
			"MOVD CTX_CSTACK_BASE(R19), R3", "ADD R7, R3, R3", "MOVD CF_FUNCTION(R3), R3",
			"MOVD FN_FIELD_STORE_GRANTS(R3), R3", "CBNZ R3, sf_shim",
			"MOVD ROUTE_NOTE_SEQ(R2), R3", "ADD $1, R3, R3", "MOVD R3, ROUTE_NOTE_SEQ(R2)",
			"MOVD $·licenceCounting(SB), R3", "MOVBU (R3), R3", "CBZ R3, " + write,
			"MOVD ROUTE_NOTES_UNTRACED(R2), R3", "ADD $1, R3, R3", "MOVD R3, ROUTE_NOTES_UNTRACED(R2)",
			"MOVD ROUTE_NOTES_BLIND(R2), R3", "ADD $1, R3, R3", "MOVD R3, ROUTE_NOTES_BLIND(R2)",
		}
	}
	arms := []struct {
		store, write string
		want, stores []string
	}{
		{
			store: "ss_ptr_store:", write: "ss_ptr_write:",
			want:   append([]string{"CBZ R4, ss_ptr_write"}, route("ss_ptr_write")...),
			stores: []string{"MOVD R4, (R6)"},
		},
		{
			store: "ss_iface_store:", write: "ss_iface_write:",
			want: append([]string{"CBZ R4, ss_iface_write", "AND $0x1F, R3, R7", "CMP $22, R7", "BNE sf_shim"},
				route("ss_iface_write")...),
			stores: []string{"MOVD R9, 0(R6)", "MOVD R4, 8(R6)"},
		},
	}
	for _, arm := range arms {
		start := slices.Index(lines, arm.store)
		end := slices.Index(lines, arm.write)
		require.GreaterOrEqual(t, start, 0, "label %s emitted", arm.store)
		require.Greater(t, end, start, "label %s follows %s", arm.write, arm.store)
		require.Equal(t, arm.want, lines[start+1:end], "%s reaches %s with a non-nil word only through the route", arm.store, arm.write)
		require.Equal(t, arm.stores, lines[end+1:end+1+len(arm.stores)], "%s stores the word", arm.write)
	}
	nilStart := slices.Index(lines, "ss_ptr_nil:")
	require.GreaterOrEqual(t, nilStart, 0, "label ss_ptr_nil: emitted")
	require.Equal(t, []string{"MOVD ZR, (R6)", "B ss_done"}, lines[nilStart+1:nilStart+3], "the pointer arm stores a nil word")
	ifaceNil := slices.Index(lines, "ss_iface_nil:")
	require.GreaterOrEqual(t, ifaceNil, 0, "label ss_iface_nil: emitted")
	require.Equal(t, []string{"MOVD ZR, R4", "B ss_iface_write"}, lines[ifaceNil+1:ifaceNil+3], "the any arm stores a nil word")
}
