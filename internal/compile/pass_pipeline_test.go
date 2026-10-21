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

package compile

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/compile/passes"
)

func TestFieldStoreGrantsAreRevalidatedAfterTheInliner(t *testing.T) {
	t.Parallel()
	pipeline := ProgramPipeline(passes.Options{})
	names := make([]string, 0, len(pipeline))
	for _, pass := range pipeline {
		names = append(names, pass.name)
	}
	inliner := slices.Index(names, "inliner")
	revalidation := slices.Index(names, "revalidate-field-store-grants")
	require.GreaterOrEqual(t, inliner, 0)
	require.Greater(t, revalidation, inliner, "the inliner's re-fusion runs after escape analysis, so its grants are re-checked after it")
	require.Less(t, revalidation, slices.Index(names, "post-purity-peephole"))
}
