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

//go:build !safe && !(js && wasm)

package engine

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type deepChainNode struct {
	next  *deepChainNode
	label string
}

type foreignStorageClass struct {
	name    string
	build   func(owner *RegisterArena) (reflect.Value, func() string)
	clobber func(owner *RegisterArena)
}

func TestHandOffOfAFunctionRevokesTheEpoch(t *testing.T) {
	a := newArena(t)
	noteHandOff(a, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	require.True(t, a.licenceState.epochRevoked)
	require.Equal(t, epochOpaqueFunction, a.licenceState.epochReason)
}

func TestHandOffOfAnUpvalueCellRevokesTheEpoch(t *testing.T) {
	arena := newArena(t)
	cells := arena.allocUpvalueCells(1)
	noteHandOff(arena, handOffSpawn, reflect.ValueOf(&cells[0]))
	require.True(t, arena.licenceState.epochRevoked)
	require.Equal(t, epochUpvalueCell, arena.licenceState.epochReason)
}

func TestWalkExhaustionRevokesTheEpoch(t *testing.T) {
	arena := newArena(t)
	wide := make([]string, licenceWalkLeafBudget+1)
	noteHandOff(arena, handOffWalkExhausted, reflect.ValueOf(wide))
	require.True(t, arena.licenceState.epochRevoked)
	require.Equal(t, epochProbeExhausted, arena.licenceState.epochReason)
}

func TestWalksShareOneLeafBudgetPerEpoch(t *testing.T) {
	arena := newArena(t)
	children := make([]*string, 0, 64)
	for !arena.licenceState.epochRevoked {
		children = append(children, new(arenaString(arena, "child")))
		noteHandOff(arena, handOffUntracedContainer, reflect.ValueOf(children))
		require.Less(t, len(children), licenceWalkLeafBudget, "re-walking a growing value must exhaust the epoch's budget")
	}
	require.Equal(t, epochWalkBudget, arena.licenceState.epochReason)
	before := licenceTotal().blindHandOffs.Load()
	noteHandOff(arena, handOffUntracedContainer, reflect.ValueOf(children))
	require.Equal(t, before+1, licenceTotal().blindHandOffs.Load(), "the epoch's later hand-offs are not walked")

	arena.Reset()
	arena.attachLicencePolicy(&VMLimits{})
	require.Equal(t, licenceWalkLeafBudget, arena.licenceState.walkBudget, "each epoch starts with the whole budget")
}

func TestSharedWalkSettlesAMemoisedContainer(t *testing.T) {
	type node struct {
		Name string
		Next *node
	}
	arena := newGoroutineArenaInTree(t, &ResourceTracker{})
	held := &node{Name: "host", Next: &node{Name: "tail"}}
	noteHandOff(arena, handOffNativeArgUnknown, reflect.ValueOf(held))
	require.False(t, arena.licenceState.epochRevoked, "test harness: the private walk finishes")
	arena.licenceAlwaysShared = true
	entries, hits := arena.licenceState.memo.count, blindScopeStatistics().SharedMemoHits
	sharedWalks := licenceTotal().sharedWalks.Load()
	noteHandOff(arena, handOffNativeArgUnknown, reflect.ValueOf(held))
	require.False(t, arena.licenceState.epochRevoked, "the shared hand-off is settled from the memo")
	require.Equal(t, sharedWalks, licenceTotal().sharedWalks.Load(), "no shared stop")
	require.Greater(t, blindScopeStatistics().SharedMemoHits, hits)
	require.Equal(t, entries, arena.licenceState.memo.count, "a shared walk never writes the memo")
	arena.Reset()
}

func TestSharedWalkDoesNotMemoise(t *testing.T) {
	arena := newGoroutineArenaInTree(t, &ResourceTracker{})
	t.Cleanup(arena.Reset)
	arena.licenceAlwaysShared = true
	host := []*int{new(int)}
	noteHandOff(arena, handOffNativeArgUnknown, reflect.ValueOf(host))
	require.Zero(t, arena.licenceState.memo.count, "a shared walk records nothing in the memo")
	require.True(t, arena.licenceState.epochRevoked, "the shared walk stops at the unmemoised Go-heap backing")
}

func TestReuseHazardExhaustedWalkNamesNothingForeign(t *testing.T) {
	observations := &hazardObservations{}
	const text = "behind a chain the walk cannot finish"
	caller := newArena(t)
	callback := newArena(t)
	head := &deepChainNode{label: arenaString(caller, text)}
	tail := head
	for range 2 * licenceWalkDepth {
		tail.next = &deepChainNode{label: "heap"}
		tail = tail.next
	}
	callerSlab := slabBase(caller.byteSlab)
	t.Cleanup(func() { dropForeignPending(callerSlab) })
	noteHandOff(callback, handOffUntracedContainer, reflect.ValueOf(head))
	require.True(t, callback.licenceState.epochRevoked, "test harness: the callback's walk must run out and revoke its epoch")
	observations.note("callback epoch reason=%s; foreign entry pending against the caller=%v",
		licenceEpochReasonNames[callback.licenceState.epochReason], foreignPending(callerSlab))
	callback.Reset()
	caller.Reset()
	relicence(caller)
	overwriteStrings(caller, len(text))
	observations.compareString("host's string behind the chain", text, head.label)
	runtime.KeepAlive(head)
	assertNoHazard(t, observations, "")
}

func TestReuseHazardForeignWalkNamesTheCallersBank(t *testing.T) {
	observations := &hazardObservations{}
	caller := newArena(t)
	callback := newArena(t)
	slot := callerBankPointer(t, caller)
	bank := slabBase(caller.intSlab)
	t.Cleanup(func() { dropForeignPending(bank) })
	noteHandOff(callback, handOffUntracedContainer, reflect.ValueOf(slot))
	observations.note("foreign entry pending against the caller's bank=%v", foreignPending(bank))
	callback.Reset()
	reuseCallerBank(caller, slot, observations)
	assertNoHazard(t, observations, "")
}

func foreignStorageClasses() []foreignStorageClass {
	return []foreignStorageClass{
		{
			name: "byte-slab-control",
			build: func(owner *RegisterArena) (reflect.Value, func() string) {
				s := arenaString(owner, "control string in the owner's byte slab")
				return reflect.ValueOf(s), func() string { return s }
			},
			clobber: func(owner *RegisterArena) { overwriteStrings(owner, 64) },
		},
		{
			name: "int-backing",
			build: func(owner *RegisterArena) (reflect.Value, func() string) {
				xs := owner.AllocIntBacking(4)
				for i := range xs {
					xs[i] = int64(1000 + i)
				}
				return reflect.ValueOf(xs), func() string { return fmt.Sprint(xs) }
			},
			clobber: func(owner *RegisterArena) {
				for i, ys := 0, owner.AllocIntBacking(4); i < len(ys); i++ {
					ys[i] = -7
				}
			},
		},
		{
			name: "string-backing",
			build: func(owner *RegisterArena) (reflect.Value, func() string) {
				xs := owner.AllocStringBacking(2)
				xs[0], xs[1] = "heap-one", "heap-two"
				return reflect.ValueOf(xs), func() string { return strings.Join(xs, ",") }
			},
			clobber: func(owner *RegisterArena) {
				ys := owner.AllocStringBacking(2)
				ys[0], ys[1] = "clobbered", "clobbered"
			},
		},
		{
			name: "generic-bytes",
			build: func(owner *RegisterArena) (reflect.Value, func() string) {
				cell := (*[2]int64)(owner.AllocBytes(16, 8))
				cell[0], cell[1] = 2000, 2001
				return reflect.ValueOf(cell), func() string { return fmt.Sprint(*cell) }
			},
			clobber: func(owner *RegisterArena) {
				cell := (*[2]int64)(owner.AllocBytes(16, 8))
				cell[0], cell[1] = -9, -9
			},
		},
		{
			name: "int-box",
			build: func(owner *RegisterArena) (reflect.Value, func() string) {
				box := owner.allocIntBox(3000)
				return reflect.ValueOf(box), func() string { return fmt.Sprint(*box) }
			},
			clobber: func(owner *RegisterArena) { owner.allocIntBox(-11) },
		},
	}
}

func TestReuseHazardForeignRevocationCoversEveryStorageClass(t *testing.T) {
	for _, kind := range foreignStorageClasses() {
		t.Run(kind.name, func(t *testing.T) {
			observations := &hazardObservations{}
			owner := newArena(t)
			other := newArena(t)
			value, read := kind.build(owner)
			want := read()
			pendingBefore := licenceForeignRevokedCount.Load()
			noteHandOff(other, handOffUntracedContainer, value)
			ownerRevoked := len(owner.licenceState.revoked) != 0 || owner.licenceState.epochRevoked
			otherRevoked := len(other.licenceState.revoked) != 0 || other.licenceState.epochRevoked
			foreignRecorded := licenceForeignRevokedCount.Load() > pendingBefore
			observations.note("after the foreign hand-off: owner revoked=%v, other revoked=%v, foreign entry recorded=%v",
				ownerRevoked, otherRevoked, foreignRecorded)
			t.Cleanup(func() { dropForeignPending(slabBase(owner.byteSlab)) })
			owner.Reset()
			relicence(owner)
			for range 4 {
				kind.clobber(owner)
			}
			observations.compareString(kind.name+" value after the owner's next epoch", want, read())
			runtime.KeepAlive(value)
			observations.note("class=%s", kind.name)
			assertNoHazard(t, observations, "")
		})
	}
}
