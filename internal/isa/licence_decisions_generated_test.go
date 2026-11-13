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

	"github.com/stretchr/testify/require"
)

func TestGeneratedDecisionTablesAgreeWithTheSpec(t *testing.T) {

	var routes []handOffRouteSpec
	for _, route := range handOffRoutesUnder(true) {
		if route.Name != "none" {
			routes = append(routes, route)
		}
	}
	require.Len(t, ModelRoutes, len(routes), "the decision table and the spec list the same routes")
	for _, route := range routes {
		model, ok := ModelRoutes[route.Name]
		require.Truef(t, ok, "the decision table names route %s", route.Name)
		require.Equalf(t, [2]string{route.Outcome.String(), route.Granularity.String()}, model, "route %s", route.Name)
	}
	for _, route := range handOffRoutesUnder(false) {
		if route.Name == "none" {
			continue
		}
		model := ModelRoutes[route.Name]
		if route.Name == "closure-capture" {
			require.Equal(t, "epoch", route.Granularity.String(), "without cell walks closure capture revokes the epoch")
			require.Equal(t, model[0], route.Outcome.String())
			continue
		}
		require.Equalf(t, [2]string{route.Outcome.String(), route.Granularity.String()}, model, "route %s without cell walks", route.Name)
	}
	for name := range ModelCauseRevokes {
		_, ok := handOffCauseByName(name)
		require.Truef(t, ok, "decision-table cause %s is a Go cause", name)
	}
	classCause := map[string]HandOffCause{
		"unknown":       HandOffNativeArgUnknown,
		"borrows":       HandOffNativeArgBorrows,
		"resultAliases": HandOffNativeArgResultAliases,
		"retains":       HandOffNativeArgRetains,
	}
	require.Len(t, ModelBorrowClassRevokes, len(classCause))
	for class, revokes := range ModelBorrowClassRevokes {
		cause, ok := classCause[class]
		require.Truef(t, ok, "decision-table class %s", class)
		require.Equalf(t, revokes, cause.Revokes(), "class %s (cause %s)", class, cause)
	}
}

func TestHandOffCauseRevokesFollowsTheDecisionTable(t *testing.T) {
	for cause := range NumHandOffCauses {
		want, modelled := ModelCauseRevokes[cause.String()]
		if !modelled {
			want = true
		}
		require.Equalf(t, want, cause.Revokes(), "cause %s", cause)
	}
	require.False(t, HandOffNativeArgBorrows.Revokes())
	require.False(t, HandOffNativeArgResultAliases.Revokes())
	require.True(t, NumHandOffCauses.Revokes(), "an out-of-range cause revokes")
}
