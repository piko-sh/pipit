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

//go:build pipit_store_census && !safe && !(js && wasm)

package engine

import (
	"runtime"
	"testing"
	"time"
	"weak"

	"github.com/stretchr/testify/require"
)

func TestCensusStatesDoNotKeepAnArenaAlive(t *testing.T) {
	t.Parallel()

	var states censusWeakStates[RegisterArena, *censusVMState]
	collected := make(chan struct{})
	func() {
		arena := &RegisterArena{}
		runtime.AddCleanup(arena, func(done chan struct{}) { close(done) }, collected)
		states.store(arena, &censusVMState{})
	}()

	require.Eventually(t, func() bool {
		runtime.GC()
		select {
		case <-collected:
			return true
		default:
			return false
		}
	}, 10*time.Second, 10*time.Millisecond, "an arena held only by the census is collected")
}

func TestCensusStatesForgetACollectedKey(t *testing.T) {
	t.Parallel()

	var states censusWeakStates[RegisterArena, *censusVMState]
	func() {
		arena := &RegisterArena{}
		first := &censusVMState{}
		stored, loaded := states.loadOrStore(arena, first)
		require.Same(t, first, stored)
		require.False(t, loaded)
		again, loaded := states.loadOrStore(arena, &censusVMState{})
		require.Same(t, first, again)
		require.True(t, loaded)
		replacement := &censusVMState{}
		states.store(arena, replacement)
		got, ok := states.load(arena)
		require.True(t, ok)
		require.Same(t, replacement, got)
	}()

	require.Eventually(t, func() bool {
		runtime.GC()
		entries := 0
		states.entries.Range(func(_, _ any) bool {
			entries++
			return true
		})
		return entries == 0
	}, 10*time.Second, 10*time.Millisecond, "the collected key's entry is forgotten")
}

func TestCensusStatesForgetADeletedKey(t *testing.T) {
	t.Parallel()

	var states censusWeakStates[GlobalStore, *censusGlobalLog]
	store := &GlobalStore{}
	states.store(store, &censusGlobalLog{})
	states.delete(store)
	_, ok := states.load(store)
	require.False(t, ok)
	runtime.KeepAlive(store)
}

func TestCensusStatesReplaceAnEntryOfAnotherType(t *testing.T) {
	t.Parallel()

	var states censusWeakStates[RegisterArena, *censusVMState]
	arena := &RegisterArena{}
	states.entries.Store(weak.Make(arena), "not a census state")
	value := &censusVMState{}

	stored, loaded := states.loadOrStore(arena, value)

	require.Same(t, value, stored, "a caller never receives a nil state from a mistyped entry")
	require.False(t, loaded)
	again, found := states.load(arena)
	require.True(t, found)
	require.Same(t, value, again, "the mistyped entry is replaced")
	runtime.KeepAlive(arena)
}
