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

//go:build pipit_reuse_audit && !safe && !(js && wasm) && (amd64 || arm64)

package engine

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var nestedCallbackKeptString string

func TestServedChainKeepsANestedCallbacksSlabFromReuse(t *testing.T) {
	skipWithoutGoroutineTokens(t)
	AuditReuse(AuditOptions{IncludeActiveRuns: true})
	root := runningVMOnArena(newArenaInTree(t, &ResourceTracker{}), false)
	outer := runningVMOnArena(newCallbackArenaInTree(t, &ResourceTracker{}), true)
	inner := runningVMOnArena(newCallbackArenaInTree(t, &ResourceTracker{}), true)
	outer.Arena.auditAcquired("outer-callback")
	owner := outer.Arena.slabOwner
	leave := enterServedChains(t, root, outer, inner)
	noteHandOff(inner.Arena, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	require.True(t, inner.Arena.licenceState.epochRevoked, "test harness: the inner callback's next hand-off is blind")
	lent := arenaBytesToString(outer.Arena, []byte("lent-by-the-outer-callback-to-its-native-0123456789"))
	require.True(t, outer.Arena.referencesArenaString(lent), "test harness: the string must be over outer's byte slab")
	nestedCallbackKeptString = lent
	noteHandOff(inner.Arena, handOffUntracedContainer, reflect.ValueOf(lent))
	leave()
	inner.Arena.Reset()
	PutRegisterArena(outer.Arena)
	var hits []AuditHit
	for _, hit := range auditHitsForOwner(AuditReuse(AuditOptions{IncludeActiveRuns: true}), owner) {
		if hit.Class == AuditReleaseHit || hit.Class == AuditOutsideReach {
			hits = append(hits, hit)
		}
	}
	runtime.KeepAlive(nestedCallbackKeptString)
	nestedCallbackKeptString = ""
	root.Arena.Reset()
	t.Logf("nested callback slab: %d hit(s)", len(hits))
	require.Empty(t, hits, "the outer callback's slab must not be recycled under the host's string")
}
