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
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func newJoinTracker() *ResourceTracker {
	tracker := &ResourceTracker{}
	tracker.GoroutineCount.Add(1)
	tracker.RegisterGoroutine()
	tracker.MarkGoroutineStarted()
	return tracker
}

func TestWaitForGoroutinesWaitsWhileAVMUnwinds(t *testing.T) {
	t.Parallel()
	grace := 50 * time.Millisecond

	t.Run("exits after a long unwinding", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			tracker := newJoinTracker()
			tracker.unwinding.Add(1)
			go func() {
				time.Sleep(6 * grace)
				tracker.unwinding.Add(-1)
				tracker.ReleaseGoroutine()
			}()
			started := time.Now()
			require.True(t, WaitForGoroutines(tracker, grace))
			require.GreaterOrEqual(t, time.Since(started), 6*grace, "the join waited for the unwinding goroutine to exit")
		})
	})

	t.Run("stuck once the unwinding ended", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			tracker := newJoinTracker()
			defer tracker.ReleaseGoroutine()
			tracker.unwinding.Add(1)
			go func() {
				time.Sleep(4 * grace)
				tracker.unwinding.Add(-1)
			}()
			started := time.Now()
			require.False(t, WaitForGoroutines(tracker, grace))
			waited := time.Since(started)
			require.GreaterOrEqual(t, waited, 5*grace-goroutineJoinPollInterval, "the join waited while the VM unwound, then a grace from its last poll")
			require.Less(t, waited, 6*grace, "the join gave up one grace after the unwinding ended")
		})
	})
}
