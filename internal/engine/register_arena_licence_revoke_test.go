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
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	foreignTestOwner      = uint64(1) << 62
	foreignTestOtherOwner = foreignTestOwner + 1
	foreignTestBase       = uintptr(1) << 40
)

func pendForeignRevocation(owner uint64, base uintptr) {
	licenceForeignRevokedMu.Lock()
	defer licenceForeignRevokedMu.Unlock()
	key := licenceForeignKey{start: base, owner: owner}
	if _, found := licenceForeignRevoked.lookup(key); !found {
		licenceForeignRevoked.record(key, handOffNativeArgUnknown)
		licenceForeignRevokedCount.Add(1)
	}
}

func pendingForeignRevocation(owner uint64, base uintptr) bool {
	licenceForeignRevokedMu.Lock()
	defer licenceForeignRevokedMu.Unlock()
	_, found := licenceForeignRevoked.lookup(licenceForeignKey{start: base, owner: owner})
	return found
}

func forgetForeignRevocationsOf(owners ...uint64) {
	for _, owner := range owners {
		dropOwnerForeignRevocations(owner)
	}
}

func TestDropOwnerForeignRevocationsDropsOnlyThatOwner(t *testing.T) {
	bases := []uintptr{foreignTestBase, foreignTestBase + 64, foreignTestBase + 128}
	t.Cleanup(func() { forgetForeignRevocationsOf(foreignTestOwner, foreignTestOtherOwner) })
	before := licenceForeignRevokedCount.Load()
	pendForeignRevocation(foreignTestOwner, bases[0])
	pendForeignRevocation(foreignTestOwner, bases[1])
	pendForeignRevocation(foreignTestOtherOwner, bases[2])
	pendForeignRevocation(foreignTestOtherOwner, bases[0])

	dropOwnerForeignRevocations(foreignTestOwner)

	require.False(t, pendingForeignRevocation(foreignTestOwner, bases[0]))
	require.False(t, pendingForeignRevocation(foreignTestOwner, bases[1]))
	require.True(t, pendingForeignRevocation(foreignTestOtherOwner, bases[2]), "another arena's entry stays")
	require.True(t, pendingForeignRevocation(foreignTestOtherOwner, bases[0]), "another arena's entry at the same start stays")
	require.Equal(t, before+2, licenceForeignRevokedCount.Load())
}

func BenchmarkDropOwnerForeignRevocations(b *testing.B) {
	for _, others := range []int{0, 1_000, 100_000} {
		b.Run(fmt.Sprintf("others=%d", others), func(b *testing.B) {
			owners := make([]uint64, 0, others+1)
			for index := range others {
				owner := foreignTestOtherOwner + uint64(index)
				owners = append(owners, owner)
				pendForeignRevocation(owner, foreignTestBase+uintptr(index+1)*64)
			}
			owners = append(owners, foreignTestOwner)
			b.Cleanup(func() { forgetForeignRevocationsOf(owners...) })
			for b.Loop() {
				pendForeignRevocation(foreignTestOwner, foreignTestBase)
				dropOwnerForeignRevocations(foreignTestOwner)
			}
		})
	}
}

func TestPendingForeignRevocationsIndexByOwner(t *testing.T) {
	var pending pendingForeignRevocations
	first := licenceForeignKey{start: 0x100, owner: 1}
	second := licenceForeignKey{start: 0x200, owner: 1}
	third := licenceForeignKey{start: 0x300, owner: 1}
	other := licenceForeignKey{start: 0x100, owner: 2}

	pending.record(first, handOffSpawn)
	pending.record(second, handOffSharedStore)
	pending.record(third, handOffSharedStore)
	pending.record(other, handOffSpawn)
	pending.record(second, handOffNativeArgRetains)
	require.Equal(t, 4, pending.len(), "recording a pending key again replaces its cause")

	cause, found := pending.lookup(second)
	require.True(t, found)
	require.Equal(t, handOffNativeArgRetains, cause)

	require.True(t, pending.forget(first), "the inline entry is forgotten")
	require.False(t, pending.forget(first), "and only once")
	for _, key := range []licenceForeignKey{second, third, other} {
		_, found := pending.lookup(key)
		require.True(t, found, "%v is still pending", key)
	}
	require.Equal(t, 3, pending.len())
	require.Len(t, collectForeignKeys(&pending), 3)

	require.Equal(t, 2, pending.forgetOwner(1))
	require.Zero(t, pending.forgetOwner(1))
	_, found = pending.lookup(other)
	require.True(t, found, "another owner's entry at the same start stays")
	require.Equal(t, 1, pending.len())

	require.True(t, pending.forget(other))
	require.Zero(t, pending.len())
	require.Empty(t, pending.byOwner, "an owner with nothing pending is removed")
}

func collectForeignKeys(pending *pendingForeignRevocations) []licenceForeignKey {
	var keys []licenceForeignKey
	for key := range pending.all() {
		keys = append(keys, key)
	}
	return keys
}
