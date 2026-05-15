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

func TestServiceRecoveryReleaseGuards(t *testing.T) {
	var absent *ServiceRecoveryClaim
	if err := absent.Release(context.Background()); !errors.Is(err, errClosed) {
		t.Fatal("missing claim accepted:", err)
	}
	for _, owner := range []*ServiceRecoveryClaim{
		{snapshot: &sandboxbroker.LinuxRecoveryClaim{}, group: &Group{}, quiesced: true},
		{snapshot: &sandboxbroker.LinuxRecoveryClaim{}, group: &Group{}, recovered: true},
		{snapshot: &sandboxbroker.LinuxRecoveryClaim{}, quiesced: true, recovered: true},
		{snapshot: &sandboxbroker.LinuxRecoveryClaim{}, group: &Group{}, quiesced: true, recovered: true, recovering: true},
		{snapshot: &sandboxbroker.LinuxRecoveryClaim{}, group: &Group{}, quiesced: true, recovered: true, process: &WorkerProcess{}},
	} {
		if err := owner.Release(context.Background()); !errors.Is(err, ErrServiceBusy) || owner.pending || owner.closed {
			t.Fatal("unfinished recovery released service:", err)
		}
	}
	owner := &ServiceRecoveryClaim{snapshot: &sandboxbroker.LinuxRecoveryClaim{}, group: &Group{}, quiesced: true, recovered: true}
	if err := owner.Release(nil); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("missing context accepted:", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := owner.Release(ctx); !errors.Is(err, context.Canceled) || owner.pending {
		t.Fatal("cancelled release changed ownership:", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Release(context.Background()); !errors.Is(err, errClosed) {
		t.Fatal("aborted claim treated as completed release:", err)
	}
}

func TestServiceRecoveryReleaseCachesTerminalError(t *testing.T) {
	failure := errors.New("terminal descriptor closure failure")
	owner := &ServiceRecoveryClaim{released: true, closed: true, releaseResult: failure}
	if err := owner.Release(context.Background()); !errors.Is(err, failure) {
		t.Fatal("release lost terminal failure:", err)
	}
	if err := owner.Close(); !errors.Is(err, failure) {
		t.Fatal("close lost terminal failure:", err)
	}
}
