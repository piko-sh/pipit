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
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/fault"
)

const (
	enginePackagePrefix = "pipit.sh/pipit/internal/engine."
	siteFramesReported  = 8
)

var dispatchSafepoints = []string{
	"(*VM).runDispatchCheckpoint",
	"(*VM).runMetered",
	"(*VM).unmeteredCheckpoint",
}

var handlerFrames = []string{
	"handle", "asmCall", "pathBTrampoline", "flatDispatchSwitch", "runPathBBatch",
	"ChargeArenaAllocation", "(*RegisterArena).Grow", "(*RegisterArena).Alloc",
}

type safepointRun struct {
	err             error
	violations      []string
	collections     int
	deferred        int64
	overshoots      int64
	safepointPanics int64
}

var dispatchLoops = []struct {
	name    string
	options []app.Option
}{
	{name: "default", options: nil},
	{name: "go_dispatch", options: []app.Option{app.WithForceGoDispatch()}},
}

func collectionSite() string {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(3, pcs)])
	var names []string
	for {
		frame, more := frames.Next()
		names = append(names, strings.TrimPrefix(frame.Function, enginePackagePrefix))
		if !more {
			break
		}
	}
	quoted := strings.Join(names[:min(len(names), siteFramesReported)], " < ")
	if len(names) < 3 || names[0] != "(*RegisterArena).MinorGC" || names[1] != "(*VM).runPendingCheckpoints" {
		return "collection outside runPendingCheckpoints: " + quoted
	}
	safepoint := false
	for _, name := range dispatchSafepoints {
		safepoint = safepoint || names[2] == name
	}
	if !safepoint {
		return "checkpoint serviced outside a dispatch safepoint: " + quoted
	}
	for _, name := range names {
		for _, handler := range handlerFrames {
			if strings.HasPrefix(name, handler) {
				return "collection inside " + name + ": " + quoted
			}
		}
	}
	return ""
}

func runUnderSiteHook(t *testing.T, options []app.Option) safepointRun {
	t.Helper()
	run, result := runSourceUnderSiteHook(t, options, arenaRecyclingChurn)
	if run.err == nil {
		require.EqualValues(t, 97920+2016+15, result)
	}
	return run
}

func runSourceUnderSiteHook(t *testing.T, options []app.Option, source string) (safepointRun, any) {
	t.Helper()
	var run safepointRun
	restore := engine.SetMinorGCSiteHook(func() {
		run.collections++
		if violation := collectionSite(); violation != "" {
			run.violations = append(run.violations, violation)
		}
	})
	defer restore()
	before := engine.LicenceDiagnostics().Reuse
	result, err := newTestService(t, options...).Eval(t.Context(), source)
	after := engine.LicenceDiagnostics().Reuse
	run.err = err
	run.deferred = after.DeferredCollections - before.DeferredCollections
	run.overshoots = after.OvershootPanics - before.OvershootPanics
	run.safepointPanics = after.SafepointBudgetPanics - before.SafepointBudgetPanics
	return run, result
}

func TestCollectionRunsOnlyAtDispatchBoundaries(t *testing.T) {
	for _, loop := range dispatchLoops {
		t.Run(loop.name, func(t *testing.T) {
			var deferred int64
			for mib := uint64(1); mib <= 8; mib++ {
				options := append([]app.Option{app.WithMaxArenaSizeBytes(mib << 20)}, loop.options...)
				run := runUnderSiteHook(t, options)
				require.Emptyf(t, run.violations, "%d MiB: every collection must run at a dispatch safepoint", mib)
				require.Positivef(t, run.collections, "%d MiB: the run must collect", mib)
				if loop.name == "default" {
					require.Zerof(t, run.overshoots, "%d MiB: the assembly loop must reach a safepoint within the overshoot allowance", mib)
				}
				if run.err != nil {
					require.ErrorIsf(t, run.err, fault.ErrArenaBudgetExceeded, "%d MiB", mib)
					require.EqualValuesf(t, 1, run.safepointPanics+run.overshoots, "%d MiB: one budget panic ends the run", mib)
				}
				deferred += run.deferred
			}
			require.Positive(t, deferred, "the sweep must raise the pending-collection flag")
		})
	}
}

func TestCollectionCountsMatchAcrossDispatchLoops(t *testing.T) {
	collections := map[string]int{}
	for _, loop := range dispatchLoops {
		for _, mib := range []uint64{12, 16} {
			options := append([]app.Option{app.WithMaxArenaSizeBytes(mib << 20), app.WithArenaFactory(engine.NewRegisterArena)}, loop.options...)
			run := runUnderSiteHook(t, options)
			require.NoErrorf(t, run.err, "%s loop at %d MiB", loop.name, mib)
			require.Emptyf(t, run.violations, "%s loop at %d MiB", loop.name, mib)
			collections[loop.name] += run.collections
		}
	}
	require.GreaterOrEqual(t, 2*collections["default"], collections["go_dispatch"], "collections per loop: %v", collections)
	require.GreaterOrEqual(t, 2*collections["go_dispatch"], collections["default"], "collections per loop: %v", collections)
	t.Logf("collections per dispatch loop: %v", collections)
}
