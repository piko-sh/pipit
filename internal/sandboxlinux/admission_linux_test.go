//go:build linux && (amd64 || arm64)

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

package sandboxlinux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestAdmissionBoundsAndTransfer(t *testing.T) {
	t.Parallel()
	var gate admission
	release, err := gate.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	waiter := make(chan func(), 1)
	go func() {
		next, _ := gate.acquire(ctx)
		waiter <- next
	}()
	waitAdmissionPending(t, &gate)
	if extra, err := gate.acquire(ctx); extra != nil || !errors.Is(err, errAdmissionFull) {
		t.Fatalf("excess launch admitted: %v", err)
	}
	release()
	release()
	next := <-waiter
	if next == nil {
		t.Fatal("queued launch lost its lease")
	}
	gate.mutex.Lock()
	active := gate.active
	gate.mutex.Unlock()
	if !active {
		t.Fatal("duplicate release freed another caller's lease")
	}
	next()
	final, err := gate.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	final()
}

func TestServiceCleanupRetainsAdmissionOnFailure(t *testing.T) {
	t.Parallel()
	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	released := false
	var process WorkerProcess
	process.context = context.Background()
	process.serviceGroup = &Group{directory: directory}
	process.releaseAdmission = func() { released = true }
	if err := process.cleanup(); err == nil || released {
		t.Fatal("failed service cleanup released admission")
	}
	process.serviceGroup.closed = true
	if err := process.cleanup(); err != nil || !released {
		t.Fatalf("completed service cleanup retained admission: %v", err)
	}
}

func TestNativeServiceAdmissionPrecedesImage(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup-v2 parent")
	}
	group, err := newServiceGroup(parent, Limits{}, testTenant)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer group.closeGroup(ctx)
	process, err := LaunchWorker(ctx, WorkerConfig{Tenant: testTenant, CgroupParent: parent, Executable: "/missing-worker"})
	if process != nil {
		defer process.Close()
	}
	if !errors.Is(err, ErrServiceBusy) {
		t.Fatalf("occupied service slot reached executable preparation: %v", err)
	}
	if process == nil || process.image != nil || process.group != nil || process.serviceGroup != nil {
		t.Fatal("occupied service slot allocated worker resources")
	}
}

func TestAdmissionCancellationRace(t *testing.T) {
	t.Parallel()
	for range 200 {
		var gate admission
		release, err := gate.acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			if next, err := gate.acquire(ctx); err == nil {
				next()
			}
		}()
		waitAdmissionPending(t, &gate)
		var racers sync.WaitGroup
		racers.Go(cancel)
		racers.Go(release)
		racers.Wait()
		<-finished
		gate.mutex.Lock()
		occupied := gate.active || gate.pending != nil
		gate.mutex.Unlock()
		if occupied {
			t.Fatal("cancellation leaked admission capacity")
		}
	}
}

func TestAdmissionCancelledQueueVacatesSlot(t *testing.T) {
	t.Parallel()
	var gate admission
	release, err := gate.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := gate.acquire(ctx)
		done <- err
	}()
	waitAdmissionPending(t, &gate)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	gate.mutex.Lock()
	queued := gate.pending != nil
	active := gate.active
	gate.mutex.Unlock()
	if queued || !active {
		t.Fatal("queued cancellation altered the active lease")
	}
	if lease, err := gate.acquire(ctx); lease != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request acquired resources: %v", err)
	}
}

func TestWorkerCleanupRetainsAdmission(t *testing.T) {
	t.Parallel()
	var gate admission
	release, err := gate.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	image, err := newWorkerImage()
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	blocker := filepath.Join(image.directory, "unexpected")
	if err := os.WriteFile(blocker, []byte("retain ownership"), 0600); err != nil {
		t.Fatal(err)
	}
	var process WorkerProcess
	process.releaseAdmission = release
	process.image = image
	if err := process.cleanup(); err == nil {
		t.Fatal("unexpected image entry did not prevent cleanup")
	}
	if !gate.active || process.releaseAdmission == nil {
		t.Fatal("failed cleanup released capacity")
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := process.cleanup(); err != nil {
		t.Fatal(err)
	}
	if gate.active || process.releaseAdmission != nil {
		t.Fatal("successful cleanup retained capacity")
	}
	if err := process.cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchWorkerAdmissionBeforePreparation(t *testing.T) {
	release, err := workerAdmission.acquire(context.Background(), testTenant)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		lease, err := workerAdmission.acquire(ctx, testTenant)
		if lease != nil {
			lease()
		}
		done <- err
	}()
	waitAdmissionPending(t, workerAdmission.gate(testTenant))
	process, err := LaunchWorker(context.Background(), WorkerConfig{Tenant: testTenant})
	if process != nil || !errors.Is(err, errAdmissionFull) {
		t.Fatalf("worker preparation bypassed admission: process=%v error=%v", process, err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	deadline, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	process, err = LaunchWorker(deadline, WorkerConfig{Tenant: testTenant})
	if process != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued worker ignored deadline: process=%v error=%v", process, err)
	}
}

func waitAdmissionPending(t *testing.T, gate *admission) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		gate.mutex.Lock()
		pending := gate.pending != nil
		gate.mutex.Unlock()
		if pending {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("request did not enter the admission queue")
}
