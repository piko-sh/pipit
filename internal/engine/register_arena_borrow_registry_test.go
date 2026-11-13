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

package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

type borrowTestHost struct{ kept []string }

func (h *borrowTestHost) Keep(s string) { h.kept = append(h.kept, s) }

func (h *borrowTestHost) Look(s string) int { return len(s) }

func TestBorrowClassRevocationFollowsTheDecisionTable(t *testing.T) {
	t.Parallel()
	classes := []BorrowClass{BorrowUnknown, BorrowBorrows, BorrowResultAliases, BorrowRetains}
	require.Len(t, isa.ModelBorrowClassRevokes, len(classes), "the decision table names every class")
	for _, class := range classes {
		revokes, named := isa.ModelBorrowClassRevokes[class.String()]
		require.Truef(t, named, "the decision table names class %s", class)
		require.Equalf(t, revokes, class.handOffCause().Revokes(), "class %s (cause %s)", class, class.handOffCause())
	}
}

func TestAliasAwareRegistryFlagsGcTagDisagreements(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{
		"strings.Fields: resultAliases, gc retains",
		"strings.Split: resultAliases, gc retains",
	}, aliasAwareBorrowRegistry().gcTagDisagreements())
}
