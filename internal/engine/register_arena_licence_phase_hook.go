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

//go:build pipit_testhooks

package engine

import "sync/atomic"

// licenceReleasePhaseHookFunc observes a licensed release reaching a phase. It runs on
// the releasing goroutine, which waits for it to return.
//
// Takes arena (*RegisterArena) which is the releasing arena.
// Takes phase (licenceReleasePhase) which is the point reached.
type licenceReleasePhaseHookFunc func(arena *RegisterArena, phase licenceReleasePhase)

// licenceReleasePhaseHook is the installed hook, or nil.
var licenceReleasePhaseHook atomic.Pointer[licenceReleasePhaseHookFunc]

// setLicenceReleasePhaseHook installs hook for the release-phase tests; nil removes it.
//
// Takes hook (licenceReleasePhaseHookFunc) which is the observer, or nil.
//
// Returns func() which restores the previous hook.
func setLicenceReleasePhaseHook(hook licenceReleasePhaseHookFunc) func() {
	var next *licenceReleasePhaseHookFunc
	if hook != nil {
		next = &hook
	}
	previous := licenceReleasePhaseHook.Swap(next)
	return func() { licenceReleasePhaseHook.Store(previous) }
}

// licenceReleasePause runs the installed hook, if any, at phase.
//
// Takes arena (*RegisterArena) which is the releasing arena.
// Takes phase (licenceReleasePhase) which is the point reached.
func licenceReleasePause(arena *RegisterArena, phase licenceReleasePhase) {
	if hook := licenceReleasePhaseHook.Load(); hook != nil {
		(*hook)(arena, phase)
	}
}
