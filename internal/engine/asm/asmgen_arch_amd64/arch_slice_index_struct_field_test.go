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

func TestEmitSliceIndexStructFieldScalarStoreMakesEveryCheckOfTheReadBeforeItWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		float  bool
	}{
		{name: "an integer source", source: "MOVQ    (R8)(AX*8), CX"},
		{name: "a float source", source: "MOVQ    (R9)(AX*8), CX", float: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			read, store := asmgen.NewEmitter(), asmgen.NewEmitter()

			New().EmitSliceIndexStructFieldScalar(read, "$2", "$6", tt.float, "handlerPathBShimProbe")
			New().EmitSliceIndexStructFieldScalarStore(store, "$2", "$6", tt.float, "handlerPathBShimProbe")

			const fieldRead = "MOVQ    (DI), CX"
			checks, _, isRead := strings.Cut(read.String(), fieldRead)
			require.True(t, isRead, "the read loads the field once its address is known")
			require.Positive(t, strings.Count(checks, "element_field_shim"))
			written, opens := strings.CutPrefix(store.String(), checks)
			require.True(t, opens, "the store opens with the checks of the read, unchanged")
			require.NotContains(t, written, fieldRead, "the store never reads the field")
			loaded, stored := strings.Index(written, tt.source), strings.Index(written, "MOVQ    CX, (DI)")
			require.GreaterOrEqual(t, loaded, 0, "the source register is loaded from its bank")
			require.Greater(t, stored, loaded, "the field is written after the source is loaded")
			require.Greater(t, strings.Index(written, "INCQ    R14"), stored, "the extension word is stepped over after the write")
			require.Equal(t, 1, strings.Count(written, "element_field_shim:"))
			require.Contains(t, written, "·handlerPathBShimProbe(SB)")
		})
	}
}

func TestEmitSliceIndexStructFieldScalarStoreAcceptsOnlyTheKindsItIsGiven(t *testing.T) {
	t.Parallel()
	one, two := asmgen.NewEmitter(), asmgen.NewEmitter()

	New().EmitSliceIndexStructFieldScalarStore(one, "$14", "", true, "handlerPathBShimProbe")
	New().EmitSliceIndexStructFieldScalarStore(two, "$2", "$6", false, "handlerPathBShimProbe")

	require.Contains(t, one.String(), "CMPQ    CX, $14")
	require.NotContains(t, one.String(), "element_field_kind_ok")
	require.Contains(t, two.String(), "CMPQ    CX, $2")
	require.Contains(t, two.String(), "CMPQ    CX, $6")
	require.Contains(t, two.String(), "element_field_kind_ok:")
}
