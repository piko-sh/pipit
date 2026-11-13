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

//go:build pipit_reuse_audit

package engine

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func auditHitsForOwner(hits []AuditHit, owner uint64) []AuditHit {
	var matching []AuditHit
	for _, hit := range hits {
		if hit.OwnerID == owner {
			matching = append(matching, hit)
		}
	}
	return matching
}

func auditHitKinds(hits []AuditHit) []string {
	kinds := make([]string, 0, len(hits))
	for _, hit := range hits {
		kinds = append(kinds, hit.SlabKind)
	}
	slices.Sort(kinds)
	return kinds
}

func newAuditArena(t *testing.T) *RegisterArena {
	t.Helper()
	arena := NewRegisterArena()
	arena.auditAcquired("test")
	require.NotZero(t, arena.slabOwner)
	return arena
}

func retireChunks(t *testing.T, arena *RegisterArena) {
	t.Helper()
	arena.recycleChunks = true
	setArenaBudget(t, arena, 1<<30)
	for len(arena.genericBytesSlab) < genericBytesSlabChunk {
		arena.growGenericBytesSlab(1)
	}
	arena.growGenericBytesSlab(1)
	growSliceHeaderSlabToChunk(t, arena)
	arena.growSliceHeaderSlab()
	require.Equal(t, genericBytesSlabChunk, cap(arena.oldGenericByteSlabs[len(arena.oldGenericByteSlabs)-1]))
	require.Equal(t, sliceHeaderSlabChunk, cap(arena.oldSliceHeaderSlabs[len(arena.oldSliceHeaderSlabs)-1]))
}

func runCheckpointWithMidRunAudit(t *testing.T, arena *RegisterArena) {
	t.Helper()
	previous := AuditSetMidRunEvery(1)
	t.Cleanup(func() { AuditSetMidRunEvery(previous) })
	vm := newGCTestVM()
	vm.Arena = arena
	vm.checkpointFlags |= checkpointFlagGCPending
	checksBefore := AuditStats().MidRunChecks
	vm.runPendingCheckpoints()
	require.Equal(t, checksBefore+1, AuditStats().MidRunChecks, "the checkpoint must run a mid-run audit")
}

func TestAuditUnbarrieredStoreFiresAtRelease(t *testing.T) {
	t.Cleanup(auditClearLeakSink)
	arena := newAuditArena(t)
	owner := arena.slabOwner
	auditUnbarrieredStore(arena)
	PutRegisterArena(arena)

	hits := auditHitsForOwner(AuditReuse(AuditOptions{}), owner)
	require.Equal(t, []string{slabByte, slabGenericBytes, slabSliceHeader}, auditHitKinds(hits), "%v", hits)
	for _, hit := range hits {
		require.Equal(t, AuditReleaseHit, hit.Class)
		require.Equal(t, AuditEventRelease, hit.Event)
		require.Equal(t, "test", hit.VMKind)
		require.False(t, hit.FromGrow)
		require.Positive(t, hit.UsedBytes)
		require.True(t, hit.Stable)
	}
	if arenaParanoidEnabled {
		require.Equal(t, strings.Repeat("\xdd", len(auditLeakSink.text)), auditLeakSink.text, "the paranoid build poisons a released slab")
	} else {
		require.Equal(t, "pipit reuse audit leaked string\x00", auditLeakSink.text, "an abandoned slab keeps its contents")
	}
}

func TestAuditMidRunCheckpointRunsThroughTheSafepoint(t *testing.T) {
	t.Cleanup(auditClearLeakSink)
	arena := newAuditArena(t)
	owner := arena.slabOwner
	retireChunks(t, arena)
	auditUnbarrieredStoreRetired(arena)

	runCheckpointWithMidRunAudit(t, arena)
	require.Equal(t, uint32(1), arena.gcCount, "the checkpoint must have run MinorGC")
	auditClearLeakSink()

	hits := auditHitsForOwner(AuditReuse(AuditOptions{IncludeActiveRuns: true}), owner)
	require.Equal(t, []string{slabGenericBytes, slabSliceHeader}, auditHitKinds(hits), "%v", hits)
	for _, hit := range hits {
		require.Equal(t, AuditInRunOnly, hit.Class, "the leak was dropped before the release audit")
		require.Equal(t, AuditEventChunkRecycle, hit.Event)
		require.Equal(t, uint32(1), hit.GCCount)
	}
}

func auditMainOptions() []goleak.Option {
	return []goleak.Option{goleak.Cleanup(auditFinalReport)}
}

func auditFinalReport(code int) {
	hits := AuditReuse(AuditOptions{MaxPasses: 0, KeepRecords: false, IncludeActiveRuns: true})
	stats := AuditStats()
	_, _ = fmt.Fprintf(os.Stderr, "pipit-audit: final release audit: %d hits; recorded=%d skippedSmall=%d skippedEmpty=%d releaseAudits=%d midRunChecks=%d releaseHits=%d inRunUnrooted=%d outsideReach=%d inRunOnly=%d\n",
		len(hits), stats.Recorded, stats.SkippedSmall, stats.SkippedEmpty, stats.ReleaseAudits, stats.MidRunChecks,
		stats.ReleaseHits, stats.InRunUnrootedHits, stats.OutsideReachHits, stats.InRunOnlyHits)
	failing := 0
	for _, hit := range hits {
		_, _ = fmt.Fprintf(os.Stderr, "pipit-audit: final: %s\n", hit)
		if hit.RunID != 0 && (hit.Class == AuditReleaseHit || hit.Class == AuditOutsideReach) {
			failing++
		}
	}
	if code == 0 && failing > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "pipit-audit: FAIL: %d release and outside-reach hits on arenas a VM acquired\n", failing)
		code = 1
	}
	os.Exit(code)
}
