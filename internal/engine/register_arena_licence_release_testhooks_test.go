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

//go:build pipit_testhooks && !safe && !(js && wasm)

package engine

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type releasePhasePlacement struct {
	name  string
	phase licenceReleasePhase
	blind bool
}

func TestReuseHazardReleasePhasesAgainstAForeignHandOff(t *testing.T) {
	placements := []releasePhasePlacement{
		{name: "blind-before-release", phase: 0, blind: true},
		{name: "blind-after-snapshot", phase: licencePhaseSnapshot, blind: true},
		{name: "blind-after-adopt", phase: licencePhaseAdopted, blind: true},
		{name: "foreign-before-release", phase: 0, blind: false},
		{name: "foreign-after-snapshot", phase: licencePhaseSnapshot, blind: false},
		{name: "foreign-after-adopt", phase: licencePhaseAdopted, blind: false},
		{name: "foreign-before-reset", phase: licencePhaseDetached, blind: false},
	}
	const text = "lent bytes handed off during the owner's release"
	for _, placement := range placements {
		t.Run(placement.name, func(t *testing.T) {
			observations := &hazardObservations{}
			owner := newArena(t)
			lent := arenaString(owner, text)
			ownerSlab := slabBase(owner.byteSlab)
			t.Cleanup(func() { dropForeignPending(ownerSlab) })
			other := newArena(t)
			if placement.blind {
				noteHandOff(other, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
			}
			handOff := func() {
				done := make(chan struct{})
				go func() {
					defer close(done)
					noteHandOffString(other, handOffUntracedContainer, lent)
				}()
				<-done
			}
			ran := false
			if placement.phase == 0 {
				handOff()
				ran = true
			} else {
				restore := setLicenceReleasePhaseHook(func(arena *RegisterArena, phase licenceReleasePhase) {
					if arena == owner && phase == placement.phase && !ran {
						ran = true
						handOff()
					}
				})
				defer restore()
			}
			owner.Reset()
			if !ran {
				observations.note("the release never reached phase %d", placement.phase)
			}
			recycled := slabBase(owner.byteSlab) == ownerSlab
			pending := foreignPending(ownerSlab)
			relicence(owner)
			overwriteStrings(owner, len(text))
			observations.compareString("lent string after the owner's next epoch", text, lent)
			observations.note("placement=%s: owner recycled its byte slab=%v, foreign entry left pending=%v", placement.name, recycled, pending)
			runtime.KeepAlive(lent)
			other.Reset()
			reason := ""
			if !ran {
				reason = "the release never reached the placement's phase"
			}
			assertNoHazard(t, observations, reason)
		})
	}
}
