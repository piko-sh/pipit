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
	var absent *IsolatedFilesystemRecovery
	if err := absent.Close(); err != nil {
		t.Fatal(err)
	}
	if err := absent.Recover(context.Background()); !errors.Is(err, ErrIsolatedUsed) {
		t.Fatal("missing owner accepted:", err)
	}
	fixture := &filesystemRecoveryFixture{failure: errors.New("retryable cleanup")}
	owner := &IsolatedFilesystemRecovery{recovery: fixture}
	if err := owner.Recover(nil); !errors.Is(err, ErrInvalidIsolatedConfig) {
		t.Fatal("nil context accepted:", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := owner.Recover(ctx); !errors.Is(err, context.Canceled) || fixture.calls.Load() != 0 {
		t.Fatal("cancelled attempt reached native recovery:", err)
	}
	if err := owner.Recover(context.Background()); !errors.Is(err, fixture.failure) {
		t.Fatal("cleanup failure lost:", err)
	}
	if err := owner.Close(); !errors.Is(err, fixture.failure) {
		t.Fatal("failed close lost ownership error:", err)
	}
	fixture.failure = nil
	var callers sync.WaitGroup
	for range 16 {
		callers.Go(func() {
			if err := owner.Recover(context.Background()); err != nil {
				t.Error(err)
			}
		})
		callers.Go(func() {
			if err := owner.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	callers.Wait()
	if fixture.calls.Load() != 17 || fixture.closes.Load() != 17 {
		t.Fatal("retry or serialisation dropped lifecycle calls")
	}
	if recovery, err := NewIsolatedFilesystemRecovery(context.Background(), IsolatedFilesystemConfig{}); recovery != nil || !errors.Is(err, ErrInvalidIsolatedConfig) {
		t.Fatal("invalid recovery configuration accepted:", err)
	}
}
