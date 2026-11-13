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
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTheMarshalShadowMemoStopsGrowingAtItsLimit(t *testing.T) {
	t.Parallel()
	memo := &CompiledFunction{}
	for key := range boundedMemoLimit + 100 {
		memo.StoreMarshalShadow(key, true)
	}
	require.Equal(t, boundedMemoLimit, memo.MarshalShadowLen())
	_, stored := memo.MarshalShadowMemo.Load(boundedMemoLimit + 50)
	require.False(t, stored, "a key past the limit is computed again, not stored")
	memo.StoreMarshalShadow(1, false)
	value, stored := memo.MarshalShadowMemo.Load(1)
	require.True(t, stored)
	require.Equal(t, false, value, "a stored key can still be replaced")
	require.Equal(t, boundedMemoLimit, memo.MarshalShadowLen())
}

func TestTheMarshalShadowMemoCountsConcurrentStoresOnce(t *testing.T) {
	t.Parallel()
	memo := &CompiledFunction{}
	var wait sync.WaitGroup
	for range 8 {
		wait.Go(func() {
			for key := range boundedMemoLimit * 2 {
				memo.StoreMarshalShadow(key, key)
			}
		})
	}
	wait.Wait()
	require.Equal(t, boundedMemoLimit, memo.MarshalShadowLen())
}
