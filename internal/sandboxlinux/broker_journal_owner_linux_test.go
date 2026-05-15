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
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxbroker"
)

func TestJournalledOwnerWaitsForReservations(t *testing.T) {
	var gate admission
	release, err := gate.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var state filesystemBrokerRecovery
	state.base = context.Background()
	state.releaseAdmission = release
	owner := &FilesystemBroker{process: nil, client: nil, recovery: &state}
	if !state.beginOperation() {
		t.Fatal("fresh owner rejected reservation")
	}
	done := make(chan error, 1)
	go func() { done <- owner.Close() }()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		state.mutex.Lock()
		closed := state.closed
		state.mutex.Unlock()
		if closed {
			break
		}
		select {
		case <-deadline.C:
			state.active.Done()
			t.Fatal("closure did not stop admission")
		case <-ticker.C:
		}
	}
	if state.beginOperation() {
		state.active.Done()
		t.Fatal("closing owner admitted new reservation")
	}
	select {
	case err := <-done:
		state.active.Done()
		t.Fatal("close returned with active reservation:", err)
	default:
	}
	gate.mutex.Lock()
	active := gate.active
	gate.mutex.Unlock()
	if !active {
		state.active.Done()
		t.Fatal("unfinished owner released lifecycle admission")
	}
	state.active.Done()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !state.complete || owner.Close() != nil {
		t.Fatal("empty owner did not close idempotently")
	}
	gate.mutex.Lock()
	active = gate.active
	gate.mutex.Unlock()
	if active {
		t.Fatal("completed owner retained lifecycle admission")
	}
}

func TestJournalledOwnerAdmissionPrecedesPreparation(t *testing.T) {
	release, err := journalledOwnerAdmission.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		owner, err := OpenJournalledFilesystemBroker(ctx, WorkerConfig{Tenant: testTenant}, nil, sandboxbroker.FilesystemLimits{}, "relative")
		if owner != nil {
			err = errors.Join(err, owner.Close())
		}
		done <- err
	}()
	waitAdmissionPending(t, &journalledOwnerAdmission)
	owner, err := OpenJournalledFilesystemBroker(ctx, WorkerConfig{Tenant: testTenant}, nil, sandboxbroker.FilesystemLimits{}, "relative")
	if owner != nil || !errors.Is(err, errAdmissionFull) {
		t.Fatal("excess owner reached journal preparation:", err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("queued owner was not cancelled:", err)
	}
	release()
	owner, err = OpenJournalledFilesystemBroker(context.Background(), WorkerConfig{Tenant: testTenant}, nil, sandboxbroker.FilesystemLimits{}, "relative")
	if !errors.Is(err, sandboxbroker.ErrInvalidPolicy) || owner == nil {
		t.Fatal("released slot did not reach preparation:", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	journalledOwnerAdmission.mutex.Lock()
	active := journalledOwnerAdmission.active
	journalledOwnerAdmission.mutex.Unlock()
	if active {
		t.Fatal("failed preparation leaked admission")
	}
}

func TestJournalledOwnerAdmissionLifetime(t *testing.T) {
	release, err := journalledOwnerAdmission.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	config := WorkerConfig{Tenant: testTenant, Lifetime: 20 * time.Millisecond}
	owner, err := OpenJournalledFilesystemBroker(context.Background(), config, nil, sandboxbroker.FilesystemLimits{}, "relative")
	if owner != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("queue escaped owner lifetime:", err)
	}
}

func TestJournalledOwnerRejectsInvalidStartup(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	owner, err := OpenJournalledFilesystemBroker(context.Background(), WorkerConfig{Tenant: testTenant}, nil, sandboxbroker.FilesystemLimits{}, directory)
	if err == nil {
		t.Fatal("non-private storage accepted")
	}
	if owner != nil {
		if err := owner.Close(); err != nil || !owner.recovery.complete {
			t.Fatal("partial startup retained resources:", err)
		}
	}
	if owner, err := OpenJournalledFilesystemBroker(nil, WorkerConfig{Tenant: testTenant}, nil, sandboxbroker.FilesystemLimits{}, directory); err == nil || owner != nil {
		t.Fatal("missing context accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if owner, err := OpenJournalledFilesystemBroker(ctx, WorkerConfig{Tenant: testTenant}, nil, sandboxbroker.FilesystemLimits{}, directory); !errors.Is(err, context.Canceled) || owner != nil {
		t.Fatal("cancelled startup accepted:", err)
	}
	if owner, err := openJournalledFilesystemBrokerInGroup(context.Background(), WorkerConfig{Tenant: testTenant}, nil, sandboxbroker.FilesystemLimits{}, directory, nil); !errors.Is(err, ErrInvalidLimits) || owner != nil {
		t.Fatal("missing aggregate accepted:", err)
	}
}

func TestJournalledOwnerRejectsRootOverlapBeforeLaunch(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	grants := []sandboxbroker.LinuxRootGrant{{Name: "data", Path: directory, Rights: sandboxbroker.Read}}
	owner, err := OpenJournalledFilesystemBroker(context.Background(), WorkerConfig{Tenant: testTenant}, grants,
		sandboxbroker.FilesystemLimits{}, directory)
	if !errors.Is(err, sandboxbroker.ErrDenied) || owner == nil {
		t.Fatal("overlapping journal reached native admission:", err)
	}
	if owner.process != nil || !owner.recovery.complete || owner.RecoveryPending() {
		t.Fatal("rejected policy launched a process or retained recovery")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}
