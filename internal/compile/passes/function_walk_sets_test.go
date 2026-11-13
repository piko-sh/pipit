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

func TestReachedWordsCountsEachWordOnce(t *testing.T) {
	t.Parallel()
	var reached reachedWords
	require.False(t, reached.has(7))
	for _, pc := range []int{7, 300, 7, -1, 0} {
		reached.add(pc)
	}
	require.Equal(t, 3, reached.count, "a word added twice and a negative one are not counted")
	require.True(t, reached.has(7))
	require.True(t, reached.has(300))
	require.True(t, reached.has(0))
	require.False(t, reached.has(8))
	require.False(t, reached.has(-1))

	var none *reachedWords
	require.False(t, none.has(7), "a nil set holds nothing")
}

func TestEnteredSetRemembersEntriesPastItsInlineOnes(t *testing.T) {
	t.Parallel()
	var entered enteredSet
	for pc := range enteredInline + 4 {
		require.Truef(t, entered.enter(pc*100), "entry %d is new", pc)
	}
	require.Equal(t, enteredInline, entered.count)
	for pc := range enteredInline + 4 {
		require.Falsef(t, entered.enter(pc*100), "entry %d was taken before", pc)
	}
	require.True(t, entered.enter(5), "an entry between two taken ones is still new")
	require.False(t, entered.enter(5))
}
