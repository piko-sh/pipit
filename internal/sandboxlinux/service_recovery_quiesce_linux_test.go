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
	"sync"
	"testing"

	"pipit.sh/pipit/internal/sandboxbroker"
)

func TestServiceRecoveryQuiesceInvalidState(t *testing.T) {
	var absent *ServiceRecoveryClaim
	if err := absent.Quiesce(context.Background(), t.TempDir()); !errors.Is(err, errClosed) {
		t.Fatal("missing owner accepted:", err)
	}
	owner := &ServiceRecoveryClaim{snapshot: &sandboxbroker.LinuxRecoveryClaim{}}
	defer owner.Close()
	if err := owner.Quiesce(nil, t.TempDir()); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("missing cleanup context accepted:", err)
	}
	if err := owner.Quiesce(context.Background(), "relative"); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("relative parent accepted:", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := owner.Quiesce(ctx, t.TempDir()); !errors.Is(err, context.Canceled) || owner.group != nil || owner.pending {
		t.Fatal("cancelled cleanup touched resources:", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Quiesce(context.Background(), t.TempDir()); !errors.Is(err, errClosed) {
		t.Fatal("closed claim accepted cleanup:", err)
	}
}

func TestServiceRecoveryRetainsPendingOwnership(t *testing.T) {
	owner := &ServiceRecoveryClaim{pending: true}
	var closers sync.WaitGroup
	for range 8 {
		closers.Go(func() {
			if err := owner.Close(); !errors.Is(err, ErrServiceBusy) {
				t.Error("unfinished termination released ownership:", err)
			}
		})
	}
	closers.Wait()
	if owner.closed {
		t.Fatal("pending claim marked closed")
	}
	owner.pending = false
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryServiceRejectsOrdinaryFiles(t *testing.T) {
	parent := t.TempDir()
	directory := filepath.Join(parent, serviceGroupNameFor(testTenant))
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(directory, "cgroup.kill")
	if err := os.WriteFile(control, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	group, err := pinRecoveryService(parent, serviceRecoveryIdentity{})
	if group != nil {
		_ = group.closeHandles()
	}
	if !errors.Is(err, ErrUnavailable) || group != nil {
		t.Fatal("ordinary files accepted as original service:", err)
	}
	data, err := os.ReadFile(control)
	if err != nil || string(data) != "untouched" {
		t.Fatal("unverified control was modified:", err)
	}
}
