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

//go:build pipit_reuse_audit && !safe && !(js && wasm)

package engine

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var nestedCallKeptString string

func runNestedDeclaredCall(t *testing.T) []AuditHit {
	t.Helper()
	AuditReuse(AuditOptions{IncludeActiveRuns: true})
	caller := newArenaInTree(t, &ResourceTracker{})
	caller.auditAcquired("caller")
	owner := caller.slabOwner
	inner := newArenaInTree(t, &ResourceTracker{})
	inner.setLicenceVM(&VM{reentrantInterpreterVM: true, debugKind: DebugThreadCallback})
	noteHandOff(inner, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	require.True(t, inner.licenceState.epochRevoked, "test harness: the nested call's epoch is revoked, so its next hand-off is blind")
	lent := arenaBytesToString(caller, []byte("lent-by-the-caller-to-its-native-0123456789"))
	require.True(t, caller.referencesArenaString(lent), "test harness: the string must be over the caller's byte slab")
	nestedCallKeptString = lent
	noteHandOff(inner, handOffUntracedContainer, reflect.ValueOf(lent))
	inner.Reset()
	PutRegisterArena(caller)
	var hits []AuditHit
	for _, hit := range auditHitsForOwner(AuditReuse(AuditOptions{IncludeActiveRuns: true}), owner) {
		if hit.Class == AuditReleaseHit || hit.Class == AuditOutsideReach {
			hits = append(hits, hit)
		}
	}
	runtime.KeepAlive(nestedCallKeptString)
	nestedCallKeptString = ""
	return hits
}

func TestTopLevelCheckKeepsANestedDeclaredCallOffTheRootPath(t *testing.T) {
	require.False(t, noVMFrameOnGoroutine(), "a nested declared call must take the fallback")
	require.Empty(t, runNestedDeclaredCall(t), "the fallback counts the nested call's blind hand-off process-wide, so the caller gives the slab up")
}
