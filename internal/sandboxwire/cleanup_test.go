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

package sandboxwire_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/sandboxwire"
)

func TestCleanupContextKeepsValuesAndDropsCancellation(t *testing.T) {
	t.Parallel()
	_, logger := logging.From(t.Context())
	parent, cancel := context.WithCancel(logging.WithLogger(t.Context(), logger))
	cancel()

	cleanup, release := sandboxwire.CleanupContext(parent)
	defer release()
	require.NoError(t, cleanup.Err(), "the cleanup outlives the cancelled parent")
	_, got := logging.From(cleanup)
	require.Same(t, logger, got, "the cleanup keeps the parent's logger")
	deadline, bounded := cleanup.Deadline()
	require.True(t, bounded, "the cleanup is bounded")
	require.WithinDuration(t, time.Now().Add(sandboxwire.CleanupTimeout), deadline, time.Second)
}

func TestContextErrKeepsCause(t *testing.T) {
	t.Parallel()
	live, release := sandboxwire.CleanupContext(t.Context())
	require.NoError(t, sandboxwire.ContextErr(live), "a live context has no error")
	release()
	require.ErrorIs(t, sandboxwire.ContextErr(live), context.Canceled, "a released cleanup reports its cancellation")

	expired, stop := context.WithTimeoutCause(t.Context(), time.Nanosecond, sandboxwire.ErrCleanupTimedOut)
	defer stop()
	<-expired.Done()
	err := sandboxwire.ContextErr(expired)
	require.ErrorIs(t, err, sandboxwire.ErrCleanupTimedOut, "the cause is kept")
	require.ErrorIs(t, err, context.DeadlineExceeded, "the context's own error is kept")

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	require.Equal(t, context.Canceled, sandboxwire.ContextErr(cancelled), "a cause equal to the error is not repeated")
}
