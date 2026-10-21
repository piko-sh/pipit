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
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLicenceBlindCounterReadingIsALoadOnceMarked(t *testing.T) {
	var counter licenceBlindCounter
	require.False(t, counter.bump(), "no reading yet: the walk need not count")
	require.Equal(t, uint64(0), counter.read())
	require.True(t, counter.stale())
	word := counter.word.Load()
	require.Equal(t, uint64(0), counter.read(), "a second reading returns the same count")
	require.Equal(t, word, counter.word.Load(), "and leaves the word as it was")
	require.True(t, counter.bump())
	require.False(t, counter.stale())
	require.False(t, counter.bump(), "no reading since the count moved")
	require.Equal(t, uint64(1), counter.read())
	require.True(t, counter.bump())
	require.Equal(t, uint64(2), counter.Load())
}

func TestLicenceBlindCounterConcurrentReadingsSeeEveryCount(t *testing.T) {
	var counter licenceBlindCounter
	const rounds = 20000
	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			for range rounds {
				seen := counter.read()
				counter.bump()
				require.Greater(t, counter.Load(), seen, "a bump after a reading counts")
			}
		})
	}
	group.Wait()
}
