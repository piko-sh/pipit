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
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	pageRunKeptBacking   []int64
	pageRunLeakedBacking []int64
)

func auditPageRunRelease(t *testing.T, leak bool) (hits []AuditHit, before AuditCounters, after AuditCounters) {
	t.Helper()
	AuditReuse(AuditOptions{IncludeActiveRuns: true})
	arena := pagedArena(t)
	arena.auditAcquired("page-run")
	owner := arena.slabOwner
	handed := arena.AllocIntBacking(4)
	handed[0] = 42
	for range 8 {
		arena.AllocIntBacking(4)
	}
	if leak {
		pageRunLeakedBacking = arena.AllocIntBacking(4)
	}
	noteHandOff(arena, handOffUntracedContainer, reflect.ValueOf(handed))
	pageRunKeptBacking = handed
	before = AuditStats()
	PutRegisterArena(arena)
	for _, hit := range auditHitsForOwner(AuditReuse(AuditOptions{IncludeActiveRuns: true}), owner) {
		if hit.SlabKind == slabIntBacking && hit.Class == AuditReleaseHit {
			hits = append(hits, hit)
		}
	}
	after = AuditStats()
	require.Equal(t, int64(42), pageRunKeptBacking[0])
	return hits, before, after
}

func TestAuditStillSeesAReferenceIntoAKeptPageRun(t *testing.T) {
	hits, _, after := auditPageRunRelease(t, true)
	require.NotZero(t, after.RangeHeapDumps)
	require.NotEmpty(t, hits, "a backing in the kept run that the host holds is a release hit")
	pageRunKeptBacking, pageRunLeakedBacking = nil, nil
}
