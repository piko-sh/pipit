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

	"pipit.sh/pipit/internal/sandboxbroker"
)

func TestFilesystemBrokerNilOwner(t *testing.T) {
	var owner *FilesystemBroker
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Execute(context.Background(), nil); !errors.Is(err, sandboxbroker.ErrClosed) {
		t.Fatal(err)
	}
	if !owner.Released() {
		t.Fatal("nil owner reported unreleased resources")
	}
	if owner.RecoveryPending() {
		t.Fatal("nil owner reported pending recovery")
	}
}

func TestAggregateLaunchRequiresParent(t *testing.T) {
	worker, err := LaunchWorkerInGroup(context.Background(), WorkerConfig{Tenant: testTenant}, nil)
	if worker != nil || !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("source launch accepted absent aggregate:", worker, err)
	}
}

func TestRecoveryLaunchRequiresOriginalOwner(t *testing.T) {
	var source WorkerProcess
	for _, original := range []*WorkerProcess{nil, &source} {
		process, err := LaunchFilesystemRecovery(context.Background(), WorkerConfig{Tenant: testTenant}, original, nil)
		if process != nil || !errors.Is(err, ErrInvalidLimits) {
			t.Fatal("unbound recovery launch admitted:", process, err)
		}
	}
	process, err := launchFilesystemRecoveryInGroup(context.Background(), WorkerConfig{Tenant: testTenant}, nil, nil, nil)
	if process != nil || !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("recovery accepted absent aggregate:", process, err)
	}
}
