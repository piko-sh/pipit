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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuditNamesAreStable(t *testing.T) {
	events := map[AuditEvent]string{
		AuditEventRelease:      "release",
		AuditEventChunkRecycle: "chunkRecycle",
		AuditEventMarkDeadDrop: "markDeadDrop",
		AuditEvent(0):          "unknown",
	}
	for event, name := range events {
		require.Equal(t, name, event.String())
	}
	classes := map[AuditHitClass]string{
		AuditReleaseHit:    "release-hit",
		AuditInRunUnrooted: "in-run-unrooted",
		AuditOutsideReach:  "outside-reach",
		AuditInRunOnly:     "in-run-only",
		AuditHitClass(99):  "unknown",
	}
	for class, name := range classes {
		require.Equal(t, name, class.String())
	}
}

func TestAuditHitRendersOnOneLine(t *testing.T) {
	hit := AuditHit{Class: AuditReleaseHit, Event: AuditEventRelease, SlabKind: "bytes", CapBytes: 64, Function: "main.run", Address: 0x10}
	rendered := hit.String()
	require.Contains(t, rendered, "release-hit event=release kind=bytes cap=64")
	require.Contains(t, rendered, `fn="main.run"`)
	require.Contains(t, rendered, "addr=0x10")
	require.NotContains(t, rendered, "\n")
}

func TestActiveDiagnosticBuildsNamesTheBuild(t *testing.T) {
	builds := ActiveDiagnosticBuilds()
	require.Equal(t, arenaAuditEnabled, builds.ReuseAudit)
	require.Equal(t, storeCensusBuilt, builds.StoreCensus)
	require.Equal(t, arenaParanoidEnabled, builds.ArenaParanoid)
	require.Equal(t, abortInjectBuilt, builds.TestHooks)
}
