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
	"testing"

	"github.com/stretchr/testify/require"
)

var twAuditKept []twItem

func TestTypedLicenceWalkRevokesAStructStringUnderTheReuseAudit(t *testing.T) {
	AuditReuse(AuditOptions{IncludeActiveRuns: true})
	arena := newAuditArena(t)
	arena.attachLicencePolicy(&VMLimits{})
	require.True(t, arena.licensed)
	owner := arena.slabOwner
	text := arenaString(arena, "a struct field's string kept by the host across reuse")
	twAuditKept = []twItem{{ID: 1, Name: text}}
	noteHandOff(arena, handOffUntracedContainer, reflect.ValueOf(twAuditKept))
	PutRegisterArena(arena)
	var hits []AuditHit
	for _, hit := range auditHitsForOwner(AuditReuse(AuditOptions{IncludeActiveRuns: true}), owner) {
		if hit.Class == AuditReleaseHit || hit.Class == AuditOutsideReach {
			hits = append(hits, hit)
		}
	}
	runtime.KeepAlive(twAuditKept)
	twAuditKept = nil
	t.Logf("typed walk struct string: %d hit(s)", len(hits))
	require.Empty(t, hits, "the byte slab must not be recycled under the host's struct field")
}
