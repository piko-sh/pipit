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
	"crypto/sha256"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/sandboxbroker"
)

func TestNativeWorkerPrivateImageStore(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup-v2 parent")
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := sandboxbroker.OpenLinuxImageStore(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var audit auditCapture
	config := WorkerConfig{Tenant: testTenant,
		Executable:         os.Getenv("PIPIT_TEST_WORKER_BINARY"),
		WatchdogExecutable: os.Getenv("PIPIT_TEST_HOST_WATCHDOG_BINARY"),
		CgroupParent:       parent, ImageStore: store, Lifetime: 5 * time.Second,
	}
	for path, digest := range map[string]*[sha256.Size]byte{
		config.Executable: &config.Digest, config.WatchdogExecutable: &config.WatchdogDigest,
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		*digest = sha256.Sum256(data)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	process, err := LaunchWorker(logging.ContextWithLogger(ctx, slog.New(&audit)), config)
	if process != nil {
		defer process.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	if process.image == nil || process.watchdog == nil || process.watchdog.image == nil {
		t.Fatal("missing protected worker or watchdog image")
	}
	for _, image := range []*WorkerImage{process.image, process.watchdog.image} {
		root, err := image.Root()
		if err != nil || filepath.Dir(root) != directory || image.store != store {
			t.Fatal("worker or watchdog escaped private image storage:", err)
		}
	}
	if err := store.Close(); err == nil {
		t.Fatal("live native images lost storage ownership")
	}
	cancel()
	if err := process.Wait(); !errors.Is(err, context.Canceled) {
		t.Fatal("worker did not report cancellation:", err)
	}
	if err := process.cleanup(); err != nil {
		t.Fatal(err)
	}
	if !audit.seen("isolated.launch") || !audit.seen("isolated.admitted") || !audit.seen("isolated.exit") {
		t.Fatal("launch did not emit its audit events:", audit.messages())
	}
	if err := store.Close(); err != nil {
		t.Fatal("native cleanup retained image storage:", err)
	}
}

type auditCapture struct {
	mutex    sync.Mutex
	captured []string
}

func (capture *auditCapture) Enabled(context.Context, slog.Level) bool { return true }

func (capture *auditCapture) Handle(_ context.Context, record slog.Record) error {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()
	capture.captured = append(capture.captured, record.Message)
	return nil
}

func (capture *auditCapture) WithAttrs([]slog.Attr) slog.Handler { return capture }

func (capture *auditCapture) WithGroup(string) slog.Handler { return capture }

func (capture *auditCapture) seen(message string) bool {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()
	return slices.Contains(capture.captured, message)
}

func (capture *auditCapture) messages() []string {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()
	return append([]string(nil), capture.captured...)
}
