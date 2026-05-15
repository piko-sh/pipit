//go:build !linux || (!amd64 && !arm64)

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

package sandboxhost_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"

	"pipit.sh/pipit/internal/sandboxhost"
)

func TestIsolatedEmbeddingUnavailable(t *testing.T) {
	t.Parallel()
	var config sandboxhost.IsolatedConfig
	config.WorkerPath = filepath.Join(t.TempDir(), "worker")
	config.WorkerSHA256 = [sha256.Size]byte{1}
	config.WatchdogPath = config.WorkerPath
	config.WatchdogSHA256 = config.WorkerSHA256
	config.Tenant = "test-tenant"
	config.StateDirectory = t.TempDir()
	worker, err := sandboxhost.NewIsolatedWorker(context.Background(), config)
	if worker != nil || !errors.Is(err, sandboxhost.ErrIsolatedUnavailable) {
		t.Fatalf("unsupported platform did not fail closed: worker=%v error=%v", worker, err)
	}
	session, err := sandboxhost.NewIsolatedSession(context.Background(), config)
	if session != nil || !errors.Is(err, sandboxhost.ErrIsolatedUnavailable) {
		t.Fatalf("unsupported session platform did not fail closed: session=%v error=%v", session, err)
	}
	filesystem, err := sandboxhost.NewIsolatedFilesystemWorker(context.Background(), sandboxhost.IsolatedFilesystemConfig{
		Worker: config, BrokerPath: config.WorkerPath, BrokerSHA256: config.WorkerSHA256,
		Roots: nil, Limits: sandboxhost.FilesystemLimits{},
	})
	if filesystem != nil || !errors.Is(err, sandboxhost.ErrIsolatedUnavailable) {
		t.Fatalf("unsupported filesystem platform did not fail closed: worker=%v error=%v", filesystem, err)
	}
	recovery, err := sandboxhost.NewIsolatedFilesystemRecovery(context.Background(), sandboxhost.IsolatedFilesystemConfig{
		Worker: config, BrokerPath: config.WorkerPath, BrokerSHA256: config.WorkerSHA256,
		Roots: nil, Limits: sandboxhost.FilesystemLimits{},
	})
	if recovery != nil || !errors.Is(err, sandboxhost.ErrIsolatedUnavailable) {
		t.Fatalf("unsupported recovery platform did not fail closed: owner=%v error=%v", recovery, err)
	}
	workerRecovery, err := sandboxhost.NewIsolatedRecovery(context.Background(), config)
	if workerRecovery != nil || !errors.Is(err, sandboxhost.ErrIsolatedUnavailable) {
		t.Fatalf("unsupported worker recovery platform did not fail closed: owner=%v error=%v", workerRecovery, err)
	}
}
