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
	"testing"
	"time"
)

func TestFilesystemAggregateAdmissionBeforeCreation(t *testing.T) {
	release, err := filesystemAggregateAdmission.acquire(context.Background(), testTenant)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		owner, err := NewFilesystemAggregate(ctx, "invalid", Limits{}, testTenant)
		if owner != nil {
			err = errors.Join(err, owner.Close(ctx))
		}
		done <- err
	}()
	waitAdmissionPending(t, filesystemAggregateAdmission.gate(testTenant))
	if owner, err := NewFilesystemAggregate(ctx, "invalid", Limits{}, testTenant); owner != nil || !errors.Is(err, errAdmissionFull) {
		t.Fatal("excess constructor reached resource creation:", err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("queued construction was not cancelled:", err)
	}
	release()
	if owner, err := NewFilesystemAggregate(context.Background(), "invalid", Limits{}, testTenant); owner != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatal("released constructor did not reach native validation:", err)
	}
	if filesystemAggregateAdmission.gate(testTenant) != nil {
		t.Fatal("failed construction leaked admission")
	}
}

func TestFilesystemAggregateRetainsAdmissionOnCleanupFailure(t *testing.T) {
	var gate admission
	release, err := gate.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	owner := &FilesystemAggregate{Group: &Group{}, releaseAdmission: release}
	if err := owner.Close(nil); err == nil {
		t.Fatal("cleanup without context succeeded")
	}
	gate.mutex.Lock()
	active := gate.active
	gate.mutex.Unlock()
	if !active {
		t.Fatal("failed cleanup released aggregate admission")
	}
	owner.Group.closed = true
	if err := owner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	gate.mutex.Lock()
	active = gate.active
	gate.mutex.Unlock()
	if active {
		t.Fatal("completed cleanup retained aggregate admission")
	}
	if err := owner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFilesystemAggregateRejectsMissingContext(t *testing.T) {
	if owner, err := NewFilesystemAggregate(nil, "invalid", Limits{}, testTenant); owner != nil || !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("missing context accepted:", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if owner, err := NewFilesystemAggregate(ctx, "invalid", Limits{}, testTenant); owner != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled context reached resource creation:", err)
	}
}
