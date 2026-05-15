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

	"pipit.sh/pipit/internal/sandboxworker"
)

func isolatedSessionFixture(t *testing.T) (*IsolatedSession, *isolatedFixture) {
	t.Helper()
	_, process := isolatedWorkerFixture(t)
	client, err := sandboxworker.OpenSession(process,
		sandboxworker.Configuration{Profile: sandboxworker.SessionProfile, Imports: []string{"math"}})
	if err != nil {
		t.Fatal(err)
	}
	session := &IsolatedSession{process: process, client: client, active: atomic.Bool{}}
	t.Cleanup(func() { _ = session.Close() })
	return session, process
}

func TestIsolatedSessionRetainedStateAndClose(t *testing.T) {
	t.Parallel()
	session, _ := isolatedSessionFixture(t)
	for _, source := range []string{"import \"math\"", "var value = math.Sqrt(144)"} {
		if _, err := session.Submit(context.Background(), source); err != nil {
			t.Fatal(err)
		}
	}
	result, err := session.Submit(context.Background(), "value + 30")
	if err != nil || string(result.Value) != "42" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-session.Done():
	default:
		t.Fatal("closed session did not notify lifecycle observers")
	}
	if _, err := session.Submit(context.Background(), "42"); !errors.Is(err, ErrIsolatedSessionClosed) {
		t.Fatalf("closed session reused: %v", err)
	}
}

func TestIsolatedSessionFailureDiscardsState(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"import \"os\"", "[]int{42}", "go func() {}()"} {
		session, process := isolatedSessionFixture(t)
		if _, err := session.Submit(context.Background(), "var value = 42"); err != nil {
			t.Fatal(err)
		}
		result, err := session.Submit(context.Background(), source)
		if err == nil || result.Value != nil || process.closes.Load() == 0 {
			t.Fatalf("failed session was not discarded: %+v %v", result, err)
		}
		if _, err := session.Submit(context.Background(), "value"); !errors.Is(err, ErrIsolatedSessionClosed) {
			t.Fatalf("failed session reused: %v", err)
		}
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
	ctx, cancel := context.WithCancel(context.Background())
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
	rejectedContext, reject := context.WithCancel(context.Background())
	reject()
	if _, err := session.Submit(rejectedContext, "1"); !errors.Is(err, ErrIsolatedSessionBusy) {
		t.Errorf("concurrent cancelled submission changed admission: %v", err)
	}
	if process.closes.Load() != 0 {
		t.Error("rejected caller cancelled the active submission")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation lost: %v", err)
	}
	if process.closes.Load() == 0 {
		t.Fatal("caller cancellation did not close worker")
	}
	if _, err := session.Submit(context.Background(), "1"); !errors.Is(err, ErrIsolatedSessionClosed) {
		t.Fatal(err)
	}
}

func TestIsolatedSessionCleanupFailureRetainsOwner(t *testing.T) {
	t.Parallel()
	session, process := isolatedSessionFixture(t)
	failure := errors.New("native cleanup failed")
	process.closeErr = failure
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := session.Submit(ctx, "42")
	if !errors.Is(err, context.Canceled) || !errors.Is(err, failure) || result.Value != nil {
		t.Fatalf("cleanup failure suppressed: %+v %v", result, err)
	}
	process.closeErr = nil
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if process.closes.Load() < 2 {
		t.Fatal("native cleanup was not retried")
	}
	if _, err := session.Submit(context.Background(), "42"); !errors.Is(err, ErrIsolatedSessionClosed) {
		t.Fatal(err)
	}
}

func TestIsolatedSessionInvalidConfiguration(t *testing.T) {
	t.Parallel()
	if session, err := NewIsolatedSession(context.Background(), IsolatedConfig{Tenant: "test-tenant"}); session != nil || !errors.Is(err, ErrInvalidIsolatedConfig) {
		t.Fatalf("missing approval accepted: %v %v", session, err)
	}
	var config IsolatedConfig
	config.WorkerPath = "/approved-worker"
	config.WorkerSHA256[0] = 1
	config.WatchdogPath = "/approved-watchdog"
	config.WatchdogSHA256[0] = 1
	config.Lifetime = isolatedSessionLifetime + 1
	if session, err := NewIsolatedSession(context.Background(), config); session != nil || !errors.Is(err, ErrInvalidIsolatedConfig) {
		t.Fatalf("excessive session lifetime accepted: %v %v", session, err)
	}
	var absent *IsolatedSession
	select {
	case <-absent.Done():
	default:
		t.Fatal("nil session lifecycle notification blocked")
	}
	if absent.Close() != nil || absent.Diagnostics() != "" {
		t.Fatal("nil session cleanup failed")
	}
	if _, err := absent.Submit(context.Background(), "42"); !errors.Is(err, ErrIsolatedSessionClosed) {
		t.Fatal(err)
	}
}
