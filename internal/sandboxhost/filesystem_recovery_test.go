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

package sandboxhost

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type filesystemRecoveryFixture struct {
	failure error
	active  atomic.Bool
	calls   atomic.Int32
	closes  atomic.Int32
}

func (fixture *filesystemRecoveryFixture) Recover(_ context.Context) error {
	if !fixture.active.CompareAndSwap(false, true) {
		return errors.New("concurrent recovery")
	}
	defer fixture.active.Store(false)
	runtime.Gosched()
	fixture.calls.Add(1)
	return fixture.failure
}

func (fixture *filesystemRecoveryFixture) Close() error {
	if !fixture.active.CompareAndSwap(false, true) {
		return errors.New("concurrent closure")
	}
	defer fixture.active.Store(false)
	runtime.Gosched()
	fixture.closes.Add(1)
	return fixture.failure
}

func TestIsolatedFilesystemRecoveryAPI(t *testing.T) {
	t.Parallel()
	var absent *IsolatedFilesystemRecovery
	require.NoError(t, absent.Close())
	require.ErrorIs(t, absent.Recover(t.Context()), ErrIsolatedUsed, "missing owner accepted")
	fixture := &filesystemRecoveryFixture{failure: errors.New("retryable cleanup")}
	owner := &IsolatedFilesystemRecovery{recovery: fixture}
	require.ErrorIs(t, owner.Recover(nil), ErrInvalidIsolatedConfig, "nil context accepted")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, owner.Recover(ctx), context.Canceled, "cancelled attempt reached native recovery")
	require.Equal(t, int32(0), fixture.calls.Load())
	require.ErrorIs(t, owner.Recover(t.Context()), fixture.failure, "the cleanup failure is kept")
	require.ErrorIs(t, owner.Close(), fixture.failure, "a failed close reports the ownership error")
	fixture.failure = nil
	var callers sync.WaitGroup
	for range 16 {
		callers.Go(func() {
			assert.NoError(t, owner.Recover(t.Context()))
		})
		callers.Go(func() {
			assert.NoError(t, owner.Close())
		})
	}
	callers.Wait()
	require.Equal(t, int32(17), fixture.calls.Load(), "retry or serialisation dropped lifecycle calls")
	require.Equal(t, int32(17), fixture.closes.Load())
	recovery, err := NewIsolatedFilesystemRecovery(t.Context(), IsolatedFilesystemConfig{})
	require.Nil(t, recovery, "invalid recovery configuration accepted")
	require.ErrorIs(t, err, ErrInvalidIsolatedConfig)
}
