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
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
)

func TestVMLimitsUnwindDefaults(t *testing.T) {
	t.Parallel()
	limits := &VMLimits{}
	require.Equal(t, abortUnwindAllowance{cost: 100_000, wall: time.Second, deferWallMax: 500 * time.Millisecond}, limits.abortUnwindAllowance())
	require.Equal(t, defaultAbortUnwindAllowance, limits.abortUnwindAllowance())
	require.Equal(t, 200*time.Millisecond, limits.abortUnwindNativeGrace())
	require.Equal(t, 10*time.Second, limits.goroutineJoinUnwindCap())
	require.Equal(t, int64(64), limits.leakedUnwinderCap())
	require.Equal(t, 4_194_304, limits.lockWalkMaxValues())
}

func TestVMLimitsUnwindFieldsReplaceTheDefaults(t *testing.T) {
	t.Parallel()
	limits := &VMLimits{
		AbortUnwindCost:         7,
		AbortUnwindWall:         time.Minute,
		AbortUnwindDeferWallMax: time.Millisecond,
		AbortUnwindNativeGrace:  time.Hour,
		GoroutineJoinUnwindCap:  time.Nanosecond,
		LeakedUnwinderCap:       2,
		LockWalkMaxValues:       3,
	}
	require.Equal(t, abortUnwindAllowance{cost: 7, wall: time.Minute, deferWallMax: time.Millisecond}, limits.abortUnwindAllowance())
	require.Equal(t, time.Hour, limits.abortUnwindNativeGrace())
	require.Equal(t, time.Nanosecond, limits.goroutineJoinUnwindCap())
	require.Equal(t, int64(2), limits.leakedUnwinderCap())
	require.Equal(t, 3, limits.lockWalkMaxValues())

	negative := &VMLimits{AbortUnwindCost: -1, AbortUnwindWall: -time.Second, LeakedUnwinderCap: -1, LockWalkMaxValues: -1}
	require.Equal(t, defaultAbortUnwindAllowance, negative.abortUnwindAllowance(), "a value that is not positive leaves the default")
	require.Equal(t, int64(64), negative.leakedUnwinderCap())
	require.Equal(t, 4_194_304, negative.lockWalkMaxValues())
}

func TestAbortUnwindSharesFollowTheAllowance(t *testing.T) {
	t.Parallel()
	allowance := abortUnwindAllowance{cost: 300, wall: 30 * time.Millisecond, deferWallMax: 5 * time.Millisecond}
	share := abortUnwindReservedShare(allowance, 1)
	require.Equal(t, int64(150), share.cost, "half the total, under the per-defer cost cap")
	require.Equal(t, 5*time.Millisecond, share.wall, "the configured per-defer wall cap")
	rest := abortUnwindRemainder(allowance, 1)
	require.Equal(t, allowance.cost, share.cost+rest.cost)
	require.Equal(t, allowance.wall, share.wall+rest.wall)
	nested, _ := fundNested(unwindShare{}, unwindShare{cost: 100, wall: time.Second}, 1, 5*time.Millisecond)
	require.Equal(t, 5*time.Millisecond, nested.wall, "a nested call's wall slice is capped at the configured maximum")
}

func TestLeakedUnwinderCapComesFromTheLimits(t *testing.T) {
	tracker := &ResourceTracker{}
	leak := func() error {
		vm, _, _ := newStandardVM(t)
		vm.Limits.Tracker = tracker
		vm.Limits.LeakedUnwinderCap = 2
		run := &abortUnwindRun{vm: vm, tracker: tracker, leakCap: vm.Limits.leakedUnwinderCap()}
		run.deferStarted.Store(time.Now().UnixNano())
		return run.abandon(time.Now())
	}
	require.NotErrorIs(t, leak(), fault.ErrUnwindLeakLimit)
	require.ErrorIs(t, leak(), fault.ErrUnwindLeakLimit, "the second leak reaches a cap of two")
}

func TestWaitForGoroutinesStopsAtTheUnwindCap(t *testing.T) {
	t.Parallel()
	tracker := newJoinTracker()
	defer tracker.ReleaseGoroutine()
	tracker.unwinding.Add(1)
	defer tracker.unwinding.Add(-1)
	started := time.Now()
	require.False(t, waitForGoroutines(tracker, 20*time.Millisecond, 50*time.Millisecond))
	require.Less(t, time.Since(started), 5*time.Second, "the join gave up at the configured cap, not the ten-second default")
}
