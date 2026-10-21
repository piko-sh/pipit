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

//go:build !safe && !(js && wasm) && (amd64 || arm64)

package engine

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServedChainNestsAndUnwinds(t *testing.T) {
	skipWithoutGoroutineTokens(t)
	root := runningVMOnArena(newArenaInTree(t, &ResourceTracker{}), false)
	outer := runningVMOnArena(newCallbackArenaInTree(t, &ResourceTracker{}), true)
	inner := runningVMOnArena(newCallbackArenaInTree(t, &ResourceTracker{}), true)
	leave := enterServedChains(t, root, outer, inner)
	require.True(t, inner.enterServedChain(), "a nested entry of the same VM only counts")
	inner.leaveServedChain()
	require.Same(t, outer, inner.servedChainRun.below)
	require.Same(t, root, outer.servedChainRun.below)
	require.Nil(t, root.servedChainRun.below)
	chain, ok := inner.Arena.servedChain()
	require.True(t, ok)
	require.Equal(t, []*RegisterArena{outer.Arena, root.Arena}, chain, "innermost first, down to the root")
	_, ok = outer.Arena.servedChain()
	require.False(t, ok, "a VM that is not innermost on its goroutine finds no chain")
	leave()
	require.Nil(t, servedChainTop(currentGoroutineToken()), "the goroutine's stack is empty again")
	require.Zero(t, inner.servedChainRun.token.Load())
}

func TestCallbackBlindHandOffMarksItsChainOnly(t *testing.T) {
	skipWithoutGoroutineTokens(t)
	creatorTree := &ResourceTracker{}
	caller := runningVMOnArena(newArenaInTree(t, &ResourceTracker{}), false)
	callback := runningVMOnArena(newCallbackArenaInTree(t, creatorTree), true)
	unrelated := newArenaInTree(t, &ResourceTracker{})
	leave := enterServedChains(t, caller, callback)
	defer leave()
	global := licenceBlindCallbacks.all.Load()
	treeBlind := creatorTree.licenceBlind.all.Load()
	noteHandOff(callback.Arena, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	require.True(t, callback.Arena.licenceState.epochRevoked, "test harness: an opaque function value revokes the epoch")
	require.True(t, caller.Arena.readBlindHandOffs().all, "the served caller sees the callback's blind hand-off")
	require.True(t, caller.Arena.blindSinceReading())
	require.Greater(t, creatorTree.licenceBlind.all.Load(), treeBlind, "the callback still counts in its own tree")
	require.Equal(t, global, licenceBlindCallbacks.all.Load(), "nothing is counted process-wide")
	require.False(t, unrelated.readBlindHandOffs().all, "an arena the callback does not serve sees nothing")
	for _, a := range []*RegisterArena{caller.Arena, callback.Arena, unrelated} {
		a.Reset()
	}
}

func TestServedChainFallsBackOffTheGoroutine(t *testing.T) {
	skipWithoutGoroutineTokens(t)
	caller := runningVMOnArena(newArenaInTree(t, &ResourceTracker{}), false)
	callback := runningVMOnArena(newCallbackArenaInTree(t, &ResourceTracker{}), true)
	unrelated := newArenaInTree(t, &ResourceTracker{})
	leave := enterServedChains(t, caller, callback)
	defer leave()
	var done sync.WaitGroup
	done.Go(func() {
		noteHandOff(callback.Arena, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	})
	done.Wait()
	require.True(t, unrelated.readBlindHandOffs().all, "off its goroutine the callback counts process-wide")
	require.True(t, caller.Arena.readBlindHandOffs().all)
	for _, a := range []*RegisterArena{caller.Arena, callback.Arena, unrelated} {
		a.Reset()
	}
}

func TestServedChainFallsBackWithoutAServedRoot(t *testing.T) {
	skipWithoutGoroutineTokens(t)
	callback := runningVMOnArena(newCallbackArenaInTree(t, &ResourceTracker{}), true)
	unrelated := newArenaInTree(t, &ResourceTracker{})
	leave := enterServedChains(t, callback)
	defer leave()
	_, ok := callback.Arena.servedChain()
	require.False(t, ok)
	noteHandOff(callback.Arena, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	require.True(t, unrelated.readBlindHandOffs().all, "a callback with no served root counts process-wide")
	callback.Arena.Reset()
	unrelated.Reset()
}

func TestServedChainMarksTheCallbackANestedCallbackServes(t *testing.T) {
	skipWithoutGoroutineTokens(t)
	root := runningVMOnArena(newArenaInTree(t, &ResourceTracker{}), false)
	outer := runningVMOnArena(newCallbackArenaInTree(t, &ResourceTracker{}), true)
	inner := runningVMOnArena(newCallbackArenaInTree(t, &ResourceTracker{}), true)
	leave := enterServedChains(t, root, outer, inner)
	defer leave()
	require.False(t, outer.Arena.readBlindHandOffs().all, "test harness: the outer callback starts clean")
	noteHandOff(inner.Arena, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	marked := outer.Arena.readBlindHandOffs().all
	t.Logf("nested callback marked=%v root marked=%v", marked, root.Arena.readBlindHandOffs().all)
	require.True(t, marked, "the callback whose native call is innermost must be marked")
	require.True(t, root.Arena.readBlindHandOffs().all)
	for _, a := range []*RegisterArena{root.Arena, outer.Arena, inner.Arena} {
		a.Reset()
	}
}
