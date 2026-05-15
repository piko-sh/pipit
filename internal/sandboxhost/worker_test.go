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

func (process *isolatedFixture) Close() error {
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
			if worker, err := NewIsolatedWorker(context.Background(), config); worker != nil || !errors.Is(err, ErrInvalidIsolatedConfig) {
				t.Fatalf("worker accepted incomplete supervisor approval: %v %v", worker, err)
			}
			if session, err := NewIsolatedSession(context.Background(), config); session != nil || !errors.Is(err, ErrInvalidIsolatedConfig) {
				t.Fatalf("session accepted incomplete supervisor approval: %v %v", session, err)
			}
			images, err := PrepareIsolatedImages(context.Background(), config)
			if images != nil || (!errors.Is(err, ErrInvalidIsolatedConfig) && !errors.Is(err, ErrIsolatedUnavailable)) {
				t.Fatalf("image staging accepted incomplete supervisor approval: %v %v", images, err)
			}
		})
	}
}

func TestIsolatedWorkerSingleSubmission(t *testing.T) {
	t.Parallel()
	worker, _ := isolatedWorkerFixture(t)
	result, err := worker.EvalFile("package main\nfunc answer() int { return 42 }", "answer")
	if err != nil || string(result.Value) != "42" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if _, err := worker.Eval("1"); !errors.Is(err, ErrIsolatedUsed) {
		t.Fatalf("worker reuse accepted: %v", err)
	}
}

func TestIsolatedWorkerConcurrentAdmission(t *testing.T) {
	t.Parallel()
	worker, _ := isolatedWorkerFixture(t)
	var accepted atomic.Int32
	var rejected atomic.Int32
	var calls sync.WaitGroup
	for range 16 {
		calls.Go(func() {
			_, err := worker.Eval("1")
			switch {
			case err == nil:
				accepted.Add(1)
			case errors.Is(err, ErrIsolatedUsed):
				rejected.Add(1)
			default:
				t.Errorf("unexpected result: %v", err)
			}
		})
	}
	calls.Wait()
	if accepted.Load() != 1 || rejected.Load() != 15 {
		t.Fatalf("accepted=%d rejected=%d", accepted.Load(), rejected.Load())
	}
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
		result, err := worker.Eval("42")
		if !errors.Is(err, failure) || result.Value != nil || result.Output != "" || process.closes.Load() == 0 {
			t.Fatalf("unverified result escaped: %+v error=%v", result, err)
		}
	}
}

func TestIsolatedWorkerCloseAndInvalidConfiguration(t *testing.T) {
	t.Parallel()
	worker, _ := isolatedWorkerFixture(t)
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.Eval("1"); !errors.Is(err, ErrIsolatedUsed) {
		t.Fatal("closed worker accepted source")
	}
	if worker, err := NewIsolatedWorker(context.Background(), IsolatedConfig{Tenant: "test-tenant"}); worker != nil || !errors.Is(err, ErrInvalidIsolatedConfig) {
		t.Fatalf("missing approval accepted: worker=%v error=%v", worker, err)
	}
	var absent *IsolatedWorker
	if absent.Close() != nil || absent.Diagnostics() != "" {
		t.Fatal("nil worker cleanup is not safe")
	}
	if _, err := absent.Eval("1"); !errors.Is(err, ErrIsolatedUsed) {
		t.Fatal("nil worker accepted source")
	}
}

func isolatedWorkerFixture(t *testing.T) (*IsolatedWorker, *isolatedFixture) {
	t.Helper()
	host, peer := net.Pipe()
	var process isolatedFixture
	process.Conn = host
	process.done = make(chan struct{})
	go func() {
		defer peer.Close()
		process.serveErr = sandboxworker.Serve(peer)
		close(process.done)
	}()
	t.Cleanup(func() { _ = process.Close() })
	return &IsolatedWorker{process: &process, imports: nil, used: atomic.Bool{}}, &process
}
