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

import "testing"

func TestReuseHazardDisableMinorGCSurvivesPoolReuse(t *testing.T) {
	observations := &hazardObservations{}
	limits := VMLimits{}
	abandon := limits.abandonsArenas()
	callbackArena := acquireRegisterArena(abandon)
	callbackArena.attachLicencePolicy(&limits)
	callbackArena.disableMinorGC = true
	releaseRegisterArena(abandon, callbackArena, true)

	childArena := acquireRegisterArena(abandon)
	sameArena := childArena == callbackArena
	inherited := childArena.disableMinorGC
	childArena.bytesAllocated = 1 << 40
	collects := childArena.gcShouldRun()
	childArena.bytesAllocated = 0
	childArena.disableMinorGC = false
	releaseRegisterArena(abandon, childArena, false)

	observations.note("child acquisition drew the callback's arena=%t", sameArena)
	observations.note("child gcShouldRun with 1 TiB allocated=%t", collects)
	reason := ""
	switch {
	case inherited:
		observations.fire("engine-state", "the child arena inherited disableMinorGC from the callback's arena")
	case !sameArena:
		reason = "the pool handed the child a different arena"
	}
	assertNoHazard(t, observations, reason)
}
