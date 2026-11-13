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
)

func TestTypedCacheReadsAWrongTypedValueAsAbsent(t *testing.T) {
	var cache typedCache[string, int]
	cache.entries.Store("wrong", "not an int")

	value, found := cache.load("wrong")
	require.False(t, found, "a value of another type is not a hit")
	require.Zero(t, value)

	stored, loaded := cache.loadOrStore("wrong", 7)
	require.False(t, loaded, "a wrongly typed entry is replaced")
	require.Equal(t, 7, stored)
	value, found = cache.load("wrong")
	require.True(t, found)
	require.Equal(t, 7, value)

	cache.entries.Store("taken", 3.5)
	value, found = cache.loadAndDelete("taken")
	require.False(t, found)
	require.Zero(t, value)
	_, found = cache.entries.Load("taken")
	require.False(t, found, "it is removed all the same")
}

func TestTypedCacheOperations(t *testing.T) {
	var cache typedCache[int, *int]
	first, second := new(1), new(2)

	_, found := cache.load(1)
	require.False(t, found)
	cache.store(1, first)
	value, found := cache.load(1)
	require.True(t, found)
	require.Same(t, first, value)

	value, loaded := cache.loadOrStore(1, second)
	require.True(t, loaded)
	require.Same(t, first, value)

	previous, replaced := cache.swap(1, second)
	require.True(t, replaced)
	require.Same(t, first, previous)
	_, replaced = cache.swap(2, first)
	require.False(t, replaced)

	require.False(t, cache.compareAndDelete(1, first), "only the value it holds")
	require.True(t, cache.compareAndDelete(1, second))
	value, found = cache.loadAndDelete(2)
	require.True(t, found)
	require.Same(t, first, value)

	cache.store(3, first)
	cache.delete(3)
	_, found = cache.load(3)
	require.False(t, found)
	cache.store(4, first)
	cache.clear()
	_, found = cache.load(4)
	require.False(t, found)
}
