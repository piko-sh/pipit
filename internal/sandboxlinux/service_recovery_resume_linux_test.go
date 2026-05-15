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

func TestServiceRecoveryResumeReleaseGuards(t *testing.T) {
	var absent *ServiceRecoveryClaim
	if err := absent.ResumeRelease(context.Background(), "/"); !errors.Is(err, errClosed) {
		t.Fatal("missing owner accepted:", err)
	}
	owner := &ServiceRecoveryClaim{snapshot: &sandboxbroker.LinuxRecoveryClaim{}}
	if err := owner.ResumeRelease(nil, "/"); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("missing context accepted:", err)
	}
	if err := owner.ResumeRelease(context.Background(), "relative"); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("relative parent accepted:", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := owner.ResumeRelease(ctx, "/"); !errors.Is(err, context.Canceled) || owner.pending {
		t.Fatal("cancelled resume changed ownership:", err)
	}
	for _, busy := range []*ServiceRecoveryClaim{
		{snapshot: &sandboxbroker.LinuxRecoveryClaim{}, recovering: true},
		{snapshot: &sandboxbroker.LinuxRecoveryClaim{}, process: &WorkerProcess{}},
	} {
		if err := busy.ResumeRelease(context.Background(), "/"); !errors.Is(err, ErrServiceBusy) {
			t.Fatal("unfinished helper allowed final release:", err)
		}
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.ResumeRelease(context.Background(), "/"); !errors.Is(err, errClosed) {
		t.Fatal("aborted claim accepted:", err)
	}
	failure := errors.New("terminal failure")
	owner = &ServiceRecoveryClaim{released: true, closed: true, releaseResult: failure}
	if err := owner.ResumeRelease(context.Background(), "/"); !errors.Is(err, failure) {
		t.Fatal("resumed release lost cached result:", err)
	}
}

func TestReadyRecoveryServiceRejectsOrdinaryDirectory(t *testing.T) {
	group, err := pinReadyRecoveryService(t.TempDir(), serviceRecoveryIdentity{})
	if group != nil {
		_ = group.closeHandles()
	}
	if err == nil || group != nil {
		t.Fatal("ordinary filesystem accepted as removed service:", err)
	}
}
