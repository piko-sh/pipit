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

func TestClaimedFilesystemRecoveryRequiresQuiescence(t *testing.T) {
	var absent *ServiceRecoveryClaim
	if err := absent.RecoverFilesystem(context.Background(), WorkerConfig{Tenant: testTenant}, nil); !errors.Is(err, errClosed) {
		t.Fatal("missing claim accepted:", err)
	}
	owner := &ServiceRecoveryClaim{snapshot: &sandboxbroker.LinuxRecoveryClaim{}}
	defer owner.Close()
	if err := owner.RecoverFilesystem(nil, WorkerConfig{Tenant: testTenant}, nil); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("missing context accepted:", err)
	}
	if err := owner.RecoverFilesystem(context.Background(), WorkerConfig{Tenant: testTenant}, nil); !errors.Is(err, ErrServiceBusy) {
		t.Fatal("recovery started before termination proof:", err)
	}
	owner.group, owner.quiesced = &Group{}, true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := owner.RecoverFilesystem(ctx, WorkerConfig{Tenant: testTenant}, nil); !errors.Is(err, context.Canceled) || owner.recovering || owner.pending {
		t.Fatal("cancelled recovery admitted work:", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.RecoverFilesystem(context.Background(), WorkerConfig{Tenant: testTenant}, nil); !errors.Is(err, errClosed) {
		t.Fatal("closed claim admitted recovery:", err)
	}
}

func TestClaimedFilesystemRecoveryRetainsUnfinishedOwnership(t *testing.T) {
	for _, owner := range []*ServiceRecoveryClaim{
		{recovering: true},
		{process: &WorkerProcess{}},
	} {
		if err := owner.Close(); !errors.Is(err, ErrServiceBusy) || owner.closed {
			t.Fatal("unfinished recovery discarded ownership:", err)
		}
	}
}
