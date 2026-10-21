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

package isa

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func handOffRowsForTest() []OpSpec {
	var rows []OpSpec
	for _, tier := range [][]OpSpec{tierMainSpecs, tierSub1Specs, tierSub2Specs, tierSub3Specs} {
		rows = append(rows, tier...)
	}
	return rows
}

func TestSpecRowsDeclareTheirHandOffs(t *testing.T) {
	t.Parallel()
	for _, row := range handOffRowsForTest() {
		if !row.RequiresHandOff() {
			continue
		}
		_, declared := row.HandOffs()
		assert.Truef(t, declared,
			"%s mutates memory, calls or publishes but declares no hand-off. Every such row must "+
				"say what its handler does with a value that may outlive the arena, so that every store "+
				"that can publish an arena reference goes through a barrier or a hand-off: "+
				"add .noHandOff(), .copies(route), .revokes(route, causes...), .borrows() or "+
				".grants() to its row in spec_tier*.go.", row)
	}
}

func TestSpecHandOffEntriesAreWellFormed(t *testing.T) {
	t.Parallel()
	for _, row := range handOffRowsForTest() {
		require.Falsef(t, len(row.HandOff) != 0 && (row.Has(specMeta) || row.Has(specReserved)),
			"%s is a drill marker or reserved slot and cannot declare hand-offs", row)
		assertHandOffEntriesWellFormed(t, row.String(), row.HandOff)
	}
	for _, boundary := range HandOffBoundaries() {
		require.NotEmptyf(t, boundary.Roots, "boundary %s names no roots", boundary.Name)
		require.NotEmptyf(t, boundary.HandOffs, "boundary %s declares no hand-offs", boundary.Name)
		assertHandOffEntriesWellFormed(t, "boundary "+boundary.Name, boundary.HandOffs)
	}
}

func assertHandOffEntriesWellFormed(t *testing.T, owner string, entries []HandOffEntry) {
	t.Helper()
	seen := map[[2]uint8]bool{}
	for _, entry := range entries {
		require.Lessf(t, entry.Route, numHandOffRoutes, "%s: route out of range", owner)
		route := handOffRoutes[entry.Route]
		assert.Containsf(t, route.Kinds, entry.Kind, "%s: route %s does not admit a %s entry", owner, route.Name, entry.Kind)
		key := [2]uint8{uint8(entry.Kind), uint8(entry.Route)}
		assert.Falsef(t, seen[key], "%s: duplicate %s entry on %s", owner, entry.Kind, route.Name)
		seen[key] = true
		if entry.Kind == handOffNone {
			assert.Lenf(t, entries, 1, "%s: a none entry must stand alone", owner)
		}
		switch entry.Kind {
		case HandOffRevoke, HandOffBorrow:
			require.NotEmptyf(t, entry.Causes, "%s: a %s entry must name its causes", owner, entry.Kind)
			revokes := false
			for _, cause := range entry.Causes {
				require.Lessf(t, cause, NumHandOffCauses, "%s: cause out of range", owner)
				revokes = revokes || cause.Revokes()
			}
			assert.Truef(t, revokes, "%s: a %s entry must name at least one revoking cause", owner, entry.Kind)
		default:
			assert.Emptyf(t, entry.Causes, "%s: a %s entry names no causes", owner, entry.Kind)
		}
	}
}

var (
	callingOperations = []int{
		FlatIndexOf(OpDefer),
		FlatIndexOf(OpGo),
		FlatIndexOfSub1(SubOpCall),
		FlatIndexOfSub1(SubOpTailCall),
		FlatIndexOfSub1(SubOpCallNative),
		FlatIndexOfSub1(SubOpCallBuiltin),
		FlatIndexOfSub1(SubOpCallMethod),
		FlatIndexOfSub1(SubOpCallIIFE),
		FlatIndexOfSub1(SubOpCallScalar),
		FlatIndexOfSub1(SubOpCallMethodInlineable),
	}
	publishingOperations = []int{
		FlatIndexOf(OpSendToChannel),
		FlatIndexOfSub1(SubOpSelect),
		FlatIndexOfSub2(SubOpTier2Panic),
		FlatIndexOfSub2(SubOpTier2Return),
		FlatIndexOfSub3(SubOpTier3ReturnVoid),
	}
)

func TestSpecCallingAndPublishingRowsAreFlagged(t *testing.T) {
	t.Parallel()
	for _, slot := range callingOperations {
		row, ok := SpecAt(Tier(slot/SlotsPerTier), uint8(slot%SlotsPerTier))
		require.Truef(t, ok, "slot %d has no specification row", slot)
		assert.Truef(t, row.Calls(), "%s transfers control to a callee and must carry .calls()", row)
	}
	for _, slot := range publishingOperations {
		row, ok := SpecAt(Tier(slot/SlotsPerTier), uint8(slot%SlotsPerTier))
		require.Truef(t, ok, "slot %d has no specification row", slot)
		assert.Truef(t, row.publishesValue(), "%s publishes a value and must carry .publishes()", row)
	}
}

func TestHandOffRouteTableIsComplete(t *testing.T) {
	t.Parallel()
	names := map[string]bool{}
	for index, route := range handOffRouteList() {
		require.Equalf(t, HandOffRoute(index), route.Route, "route table row %d is out of order", index)
		require.NotEmptyf(t, route.Name, "route %d has no name", index)
		require.Falsef(t, names[route.Name], "route name %q repeats", route.Name)
		names[route.Name] = true
		require.NotEmptyf(t, route.Kinds, "route %s admits no kind", route.Name)
	}
	causes := map[string]bool{}
	for cause := range NumHandOffCauses {
		name := cause.String()
		require.NotEqual(t, "unknown", name)
		require.False(t, causes[name], "cause name %q repeats", name)
		causes[name] = true
		back, ok := handOffCauseByName(name)
		require.True(t, ok)
		require.Equal(t, cause, back)
	}
}

func probeRow() OpSpec {
	row := main0(OpAddInt64)
	row.Name, row.Handler = "PROBE", "handleProbe"
	return row
}

func TestSpecUndeclaredRowFailsTheHandOffCheck(t *testing.T) {
	t.Parallel()
	row := probeRow().mutates()
	_, declared := row.HandOffs()
	require.False(t, declared, "a mutating row without an entry must read as undeclared")
	pure := probeRow().pure()
	entries, declared := pure.HandOffs()
	require.False(t, declared)
	require.Equal(t, []HandOffEntry{{Kind: handOffNone, Route: RouteNone, Causes: nil}}, entries,
		"a pure row that neither calls nor publishes hands nothing off")
	calling := probeRow().pure().calls()
	_, declared = calling.HandOffs()
	require.False(t, declared)
	require.True(t, calling.RequiresHandOff(), "a calling row must declare even when pure")
}

func TestHandOffNamesAreStable(t *testing.T) {
	require.Equal(t, "revoke", HandOffRevoke.String())
	require.Equal(t, "unknown", numHandOffKinds.String(), "a kind out of range")
	require.Equal(t, "container-store", RouteContainerStore.String())
	require.Equal(t, "unknown", numHandOffRoutes.String(), "a route out of range")
	require.Equal(t, "copy-then-revoke", outcomeCopyThenRevoke.String())
	require.Equal(t, "revoke", outcomeRevoke.String())
	require.Equal(t, "by-class", outcomeByClass.String())
	require.Equal(t, "discharge", outcomeDischarge.String())
	require.Equal(t, "none", handOffOutcome(0).String())
	require.Equal(t, "epoch", granularityEpoch.String())
	require.Equal(t, "none", handOffGranularity(0).String())
}

func TestHandOffReentryFunctionsAreACopy(t *testing.T) {
	functions := HandOffReentryFunctions()
	require.Contains(t, functions, "VM.unwindAfterAbort")
	functions[0] = "changed"
	require.NotEqual(t, "changed", HandOffReentryFunctions()[0], "the caller cannot change the table")
}

func TestOpSpecReportsItsDeclaredHandOffs(t *testing.T) {
	row, known := SpecFor(OpDefer)
	require.True(t, known)
	require.True(t, row.HasHandOffKind(HandOffRevoke))
	require.False(t, row.HasHandOffKind(HandOffBorrow))
	require.NotEmpty(t, row.DeclaredCauses(), "a revoking row names its causes")

	move, known := SpecFor(OpMoveGeneral)
	require.True(t, known)
	require.Empty(t, move.DeclaredCauses(), "a row with no entries names no cause")
}

func TestHandOffCauseByName(t *testing.T) {
	cause, found := handOffCauseByName(HandOffCause(0).String())
	require.True(t, found)
	require.Equal(t, HandOffCause(0), cause)
	_, found = handOffCauseByName("no such cause")
	require.False(t, found)
}
