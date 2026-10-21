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

package program

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPCSetHoldsTheProgramCountersAdded(t *testing.T) {
	t.Parallel()

	var empty PCSet
	require.False(t, empty.Has(0), "the nil set is empty")
	require.Zero(t, empty.Count())
	require.Nil(t, empty.pcs())
	require.Nil(t, PCSetOf(), "no program counter leaves the set nil")
	require.Nil(t, PCSetOf(-1), "a negative program counter is ignored")

	set := PCSetOf(3, 64, 0, 200, 64)
	require.Equal(t, []int{0, 3, 64, 200}, set.pcs())
	require.Equal(t, 4, set.Count())
	for _, pc := range []int{0, 3, 64, 200} {
		require.True(t, set.Has(pc), "pc %d", pc)
	}
	for _, pc := range []int{-1, 1, 63, 65, 199, 201, 256, 1 << 20} {
		require.False(t, set.Has(pc), "pc %d", pc)
	}
	require.Equal(t, PCSetOf(0, 3, 64, 200), set, "sets of the same program counters are equal")
}

func TestPCSetWithKeepsWhatTheSetHeld(t *testing.T) {
	t.Parallel()

	set := PCSetOf(5)
	grown := set.With(130)
	require.Equal(t, []int{5, 130}, grown.pcs())
	require.Equal(t, grown, grown.With(-7), "a negative program counter changes nothing")
	require.Equal(t, grown, grown.With(5), "adding a member changes nothing")
}
