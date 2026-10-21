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

package passes

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoopVisitsFollowALatchAHoistMoves(t *testing.T) {
	t.Parallel()
	var visits loopVisits
	inner, later := loopRange{header: 10, latch: 20}, loopRange{header: 40, latch: 50}
	require.False(t, visits.finished(inner))
	visits.finish(inner)
	visits.finish(later)

	visits.lifted(5, 30, 2)
	require.False(t, visits.finished(inner), "the inner loop's latch moved up with the words lifted past it")
	require.True(t, visits.finished(loopRange{header: 12, latch: 22}))
	require.True(t, visits.finished(later), "a latch after the lifted words stays where it was")

	visits.lifted(25, 60, 1)
	require.True(t, visits.finished(loopRange{header: 12, latch: 22}), "a latch before the hoist's header stays where it was")
	require.True(t, visits.finished(loopRange{header: 41, latch: 51}))
}
