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

//go:build integration && !safe && !(js && wasm)

package bytecode_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/fault"
)

func TestAbortedGoroutineRunsItsDeferredWaitGroupDone(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled := abortUnwindCompile(t, lane.options)
			before := engine.LicenceDiagnostics().AbortUnwind
			abortedBefore, reuseBefore := engine.LicenceDiagnostics().AbortedArenas, engine.LicenceDiagnostics().Reuse
			_, err := abortUnwindCall(t, service, compiled, "WaitGroupDone")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
			after := engine.LicenceDiagnostics().AbortUnwind
			require.Greater(t, after.Completed, before.Completed, "the aborted goroutine's defers must run to the end")
			require.Greater(t, engine.LicenceDiagnostics().AbortedArenas, abortedBefore, "the arena the defers ran on is still aborted and abandoned")
			require.Greater(t, engine.LicenceDiagnostics().Reuse.BlindHandOffs, reuseBefore.BlindHandOffs,
				"the abort still counts as a blind hand-off of its tree")
		})
	}
}

func TestAbortedGoroutineReleasesAGlobalMutex(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled := abortUnwindCompile(t, lane.options)
			_, err := abortUnwindCall(t, service, compiled, "MutexHeld")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
			result, err := abortUnwindCall(t, service, compiled, "LockFree")
			require.NoError(t, err, "a later execution must find the global mutex free")
			require.Equal(t, 7, result)
		})
	}
}

func TestAbortedGoroutineCutsOffADeferThatLoopsForever(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled := abortUnwindCompile(t, lane.options)
			before := engine.LicenceDiagnostics().AbortUnwind
			_, err := abortUnwindCall(t, service, compiled, "DeferLoopsForever")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded, "the execution reports the abort, not the defer's stop")
			after := engine.LicenceDiagnostics().AbortUnwind
			require.Greater(t, after.Cut, before.Cut, "the looping defer must be cut off by the allowance")
			result, err := abortUnwindCall(t, service, compiled, "LockFree")
			require.NoError(t, err)
			require.Equal(t, 7, result)
		})
	}
}

func TestAbortedGoroutineDeferThatPanicsDoesNotStopTheUnwinding(t *testing.T) {
	for _, lane := range abortUnwindLanes {
		t.Run(lane.name, func(t *testing.T) {
			service, compiled := abortUnwindCompile(t, lane.options)
			before := engine.LicenceDiagnostics().AbortUnwind
			_, err := abortUnwindCall(t, service, compiled, "DeferPanics")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
			require.NotContains(t, fmt.Sprint(err), "raised in a defer", "the defer's panic must not replace the abort")
			after := engine.LicenceDiagnostics().AbortUnwind
			require.Greater(t, after.DeferPanics, before.DeferPanics)
			require.Greater(t, after.Completed, before.Completed)
		})
	}
}
