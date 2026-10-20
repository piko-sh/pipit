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
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxworker"
)

func isolatedSessionFixture(t *testing.T) (*IsolatedSession, *isolatedFixture) {
	t.Helper()
	_, process := isolatedWorkerFixture(t)
	client, err := sandboxworker.OpenSession(process,
		sandboxworker.Configuration{Profile: sandboxworker.SessionProfile, Imports: []string{"math"}})
	require.NoError(t, err)
	session := &IsolatedSession{process: process, client: client, active: atomic.Bool{}}
	t.Cleanup(func() { _ = session.Close() })
	return session, process
}

func TestIsolatedSessionRetainedStateAndClose(t *testing.T) {
	t.Parallel()
	session, _ := isolatedSessionFixture(t)
	for _, source := range []string{"import \"math\"", "var value = math.Sqrt(144)"} {
		_, err := session.Submit(t.Context(), source)
		require.NoError(t, err)
	}
	result, err := session.Submit(t.Context(), "value + 30")
	require.NoError(t, err, "result=%+v", result)
	require.Equal(t, "42", string(result.Value))
	require.NoError(t, session.Close())
	select {
	case <-session.Done():
	default:
		require.FailNow(t, "a closed session notifies lifecycle observers")
	}
	_, err = session.Submit(t.Context(), "42")
	require.ErrorIs(t, err, ErrIsolatedSessionClosed, "closed session reused")
}

func TestIsolatedSessionFailureDiscardsState(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"import \"os\"", "[]int{42}", "go func() {}()"} {
		session, process := isolatedSessionFixture(t)
		_, err := session.Submit(t.Context(), "var value = 42")
		require.NoError(t, err)
		result, err := session.Submit(t.Context(), source)
		require.Error(t, err, "failed session was not discarded: %+v", result)
		require.Nil(t, result.Value)
		require.NotEqual(t, int32(0), process.closes.Load())
		_, err = session.Submit(t.Context(), "value")
		require.ErrorIs(t, err, ErrIsolatedSessionClosed, "failed session reused")
	}
}

type sessionBlockingConn struct {
	net.Conn
	entered chan struct{}
	stopped chan struct{}
	once    sync.Once
	paused  atomic.Bool
}

func (stream *sessionBlockingConn) Read(data []byte) (int, error) {
	if stream.paused.CompareAndSwap(false, true) {
		close(stream.entered)
		<-stream.stopped
		return 0, io.ErrClosedPipe
	}
	return stream.Conn.Read(data)
}

func (stream *sessionBlockingConn) Close() error {
	stream.once.Do(func() { close(stream.stopped) })
	return stream.Conn.Close()
}

func TestIsolatedSessionCallerCancellationAndConcurrentAdmission(t *testing.T) {
	t.Parallel()
	session, process := isolatedSessionFixture(t)
	stream := &sessionBlockingConn{Conn: process.Conn, entered: make(chan struct{}),
		stopped: make(chan struct{}), once: sync.Once{}, paused: atomic.Bool{}}
	process.Conn = stream
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		result, err := session.Submit(ctx, "42")
		if result.Value != nil {
			done <- errors.New("cancelled result escaped")
			return
		}
		done <- err
	}()
	<-stream.entered
	rejectedContext, reject := context.WithCancel(t.Context())
	reject()
	_, err := session.Submit(rejectedContext, "1")
	assert.ErrorIs(t, err, ErrIsolatedSessionBusy, "a cancelled concurrent submission keeps admission")
	assert.Equal(t, int32(0), process.closes.Load(), "rejected caller cancelled the active submission")
	cancel()
	require.ErrorIs(t, <-done, context.Canceled, "caller cancellation is kept")
	require.NotEqual(t, int32(0), process.closes.Load(), "caller cancellation closes the worker")
	_, err = session.Submit(t.Context(), "1")
	require.ErrorIs(t, err, ErrIsolatedSessionClosed)
}

func TestIsolatedSessionCleanupFailureRetainsOwner(t *testing.T) {
	t.Parallel()
	session, process := isolatedSessionFixture(t)
	failure := errors.New("native cleanup failed")
	process.closeErr = failure
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := session.Submit(ctx, "42")
	require.ErrorIs(t, err, context.Canceled, "cleanup failure suppressed: %+v", result)
	require.ErrorIs(t, err, failure)
	require.Nil(t, result.Value)
	process.closeErr = nil
	require.NoError(t, session.Close())
	require.GreaterOrEqual(t, process.closes.Load(), int32(2), "native cleanup is retried")
	_, err = session.Submit(t.Context(), "42")
	require.ErrorIs(t, err, ErrIsolatedSessionClosed)
}

func TestIsolatedSessionInvalidConfiguration(t *testing.T) {
	t.Parallel()
	session, err := NewIsolatedSession(t.Context(), IsolatedConfig{Tenant: "test-tenant"})
	require.Nil(t, session, "missing approval accepted")
	require.ErrorIs(t, err, ErrInvalidIsolatedConfig)
	var config IsolatedConfig
	config.WorkerPath = "/approved-worker"
	config.WorkerSHA256[0] = 1
	config.WatchdogPath = "/approved-watchdog"
	config.WatchdogSHA256[0] = 1
	config.Lifetime = isolatedSessionLifetime + 1
	session, err = NewIsolatedSession(t.Context(), config)
	require.Nil(t, session, "excessive session lifetime accepted")
	require.ErrorIs(t, err, ErrInvalidIsolatedConfig)
	var absent *IsolatedSession
	select {
	case <-absent.Done():
	default:
		require.FailNow(t, "a nil session's lifecycle notification does not block")
	}
	require.NoError(t, absent.Close(), "nil session cleanup failed")
	require.Empty(t, absent.Diagnostics())
	_, err = absent.Submit(t.Context(), "42")
	require.ErrorIs(t, err, ErrIsolatedSessionClosed)
}
