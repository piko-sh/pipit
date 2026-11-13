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
	"testing"

	"github.com/stretchr/testify/require"

	"piko.sh/asmgen"
)

func TestUintCompareAndBranchTestsForEqualityOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		condition string
		want      string
	}{
		{condition: "EQ", want: "BEQ"},
		{condition: "NE", want: "BNE"},
	}
	for _, tt := range tests {
		t.Run(tt.condition, func(t *testing.T) {
			t.Parallel()
			e := asmgen.NewEmitter()

			New().UintCompareAndBranch(e, tt.condition, "AX", "BX", "taken")

			assertContainsAll(t, e.String(), "CTX_UINTS_BASE", tt.want, "taken")
		})
	}

	for _, condition := range []string{"LT", "LE", "GT", "GE", ""} {
		t.Run("refuses "+condition, func(t *testing.T) {
			t.Parallel()

			require.Panics(t, func() { New().UintCompareAndBranch(asmgen.NewEmitter(), condition, "AX", "BX", "taken") },
				"an ordered comparison of unsigned values needs an unsigned branch, which this emitter does not choose")
		})
	}
}
