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
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxworker"
)

type isolatedFixture struct {
	net.Conn
	done     chan struct{}
	serveErr error
	waitErr  error
	closeErr error
	closes   atomic.Int32
}

func (process *isolatedFixture) Done() <-chan struct{} { return process.done }

func (process *isolatedFixture) Wait() error {
	<-process.done
	return errors.Join(process.serveErr, process.waitErr)
}

func (process *isolatedFixture) Close(_ context.Context) error {
	process.closes.Add(1)
	_ = process.Conn.Close()
	<-process.done
	return process.closeErr
}

func (process *isolatedFixture) Output() string { return "" }

func (process *isolatedFixture) ChargeOutput(count int) error {
	if count < 0 || count > 64<<10 {
		return errors.New("fixture output overflow")
	}
	return nil
}

func TestIsolatedWatchdogApprovalRequired(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"missing-path", "relative-path", "missing-digest"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var config IsolatedConfig
			config.WorkerPath = filepath.Join(t.TempDir(), "worker")
			config.WorkerSHA256[0] = 1
			config.WatchdogPath = filepath.Join(t.TempDir(), "watchdog")
			config.WatchdogSHA256[0] = 2
			switch mode {
			case "missing-path":
				config.WatchdogPath = ""
			case "relative-path":
				config.WatchdogPath = "watchdog"
			case "missing-digest":
				config.WatchdogSHA256[0] = 0
			}
			worker, err := NewIsolatedWorker(t.Context(), config)
			require.Nil(t, worker, "worker accepted incomplete supervisor approval")
			require.ErrorIs(t, err, ErrInvalidIsolatedConfig)
			session, err := NewIsolatedSession(t.Context(), config)
			require.Nil(t, session, "session accepted incomplete supervisor approval")
			require.ErrorIs(t, err, ErrInvalidIsolatedConfig)
			images, err := PrepareIsolatedImages(t.Context(), config)
			require.Nil(t, images, "image staging refuses incomplete supervisor approval")
			require.True(t, errors.Is(err, ErrInvalidIsolatedConfig) || errors.Is(err, ErrIsolatedUnavailable),
				"image staging refuses incomplete supervisor approval: %v", err)
		})
	}
}

func TestIsolatedWorkerSingleSubmission(t *testing.T) {
	t.Parallel()
	worker, _ := isolatedWorkerFixture(t)
	result, err := worker.EvalFile(t.Context(), "package main\nfunc answer() int { return 42 }", "answer")
	require.NoError(t, err, "result=%+v", result)
	require.Equal(t, "42", string(result.Value))
	_, err = worker.Eval(t.Context(), "1")
	require.ErrorIs(t, err, ErrIsolatedUsed, "worker reuse accepted")
}

func TestIsolatedWorkerConcurrentAdmission(t *testing.T) {
	t.Parallel()
	worker, _ := isolatedWorkerFixture(t)
	var accepted atomic.Int32
	var rejected atomic.Int32
	var calls sync.WaitGroup
	for range 16 {
		calls.Go(func() {
			_, err := worker.Eval(t.Context(), "1")
			switch {
			case err == nil:
				accepted.Add(1)
			case errors.Is(err, ErrIsolatedUsed):
				rejected.Add(1)
			default:
				assert.Failf(t, "the call succeeds or reports ErrIsolatedUsed", "%v", err)
			}
		})
	}
	calls.Wait()
	require.Equal(t, int32(1), accepted.Load())
	require.Equal(t, int32(15), rejected.Load())
}

func TestIsolatedWorkerSuppressesUnreapedResults(t *testing.T) {
	t.Parallel()
	failure := errors.New("native lifecycle failure")
	for _, cleanup := range []bool{false, true} {
		worker, process := isolatedWorkerFixture(t)
		if cleanup {
			process.closeErr = failure
		} else {
			process.waitErr = failure
		}
		result, err := worker.Eval(t.Context(), "42")
		require.ErrorIs(t, err, failure, "unverified result escaped: %+v", result)
		require.Nil(t, result.Value)
		require.Empty(t, result.Output)
		require.NotEqual(t, int32(0), process.closes.Load())
	}
}

func TestIsolatedWorkerCloseAndInvalidConfiguration(t *testing.T) {
	t.Parallel()
	worker, _ := isolatedWorkerFixture(t)
	require.NoError(t, worker.Close(t.Context()))
	_, err := worker.Eval(t.Context(), "1")
	require.ErrorIs(t, err, ErrIsolatedUsed, "closed worker accepted source")
	worker, err = NewIsolatedWorker(t.Context(), IsolatedConfig{Tenant: "test-tenant"})
	require.Nil(t, worker, "missing approval accepted")
	require.ErrorIs(t, err, ErrInvalidIsolatedConfig)
	var absent *IsolatedWorker
	require.NoError(t, absent.Close(t.Context()), "nil worker cleanup is not safe")
	require.Empty(t, absent.Diagnostics())
	_, err = absent.Eval(t.Context(), "1")
	require.ErrorIs(t, err, ErrIsolatedUsed, "nil worker accepted source")
}

func isolatedWorkerFixture(t *testing.T) (*IsolatedWorker, *isolatedFixture) {
	t.Helper()
	host, peer := net.Pipe()
	var process isolatedFixture
	process.Conn = host
	process.done = make(chan struct{})
	go func() {
		defer peer.Close()
		process.serveErr = sandboxworker.Serve(t.Context(), peer)
		close(process.done)
	}()
	t.Cleanup(func() { _ = process.Close(t.Context()) })
	return &IsolatedWorker{process: &process, imports: nil, used: atomic.Bool{}}, &process
}
