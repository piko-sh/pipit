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

//go:build linux && (amd64 || arm64)

package sandboxworker

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxlinux"
)

func TestNativeFilesystemRelay(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	workerPath, brokerPath := os.Getenv("PIPIT_TEST_WORKER_BINARY"), os.Getenv("PIPIT_TEST_FILESYSTEM_BROKER_BINARY")
	if parent == "" || workerPath == "" || brokerPath == "" {
		t.Skip("requires delegated cgroups and static worker and broker fixtures")
	}
	workerConfig := filesystemRelayConfig(t, workerPath, parent)
	brokerConfig := filesystemRelayConfig(t, brokerPath, parent)
	for _, mode := range []string{"read-write-list", "initialiser", "denied-root", "host-denied", "write-only-list-denied", "ignored-io", "final-call", "compile-error", "cancelled"} {
		t.Run(mode, func(t *testing.T) { exerciseFilesystemRelay(t, workerConfig, brokerConfig, mode) })
	}
}

func filesystemRelayConfig(t *testing.T, executable, parent string) sandboxlinux.WorkerConfig {
	t.Helper()
	image, err := os.ReadFile(executable)
	require.NoError(t, err)
	watchdogPath := os.Getenv("PIPIT_TEST_HOST_WATCHDOG_BINARY")
	watchdogImage, err := os.ReadFile(watchdogPath)
	require.NoError(t, err)
	return sandboxlinux.WorkerConfig{Tenant: "test-tenant", ImageStore: nil,
		WatchdogExecutable: watchdogPath, WatchdogDigest: sha256.Sum256(watchdogImage),
		Executable: executable, CgroupParent: parent, Digest: sha256.Sum256(image),
		Limits: sandboxlinux.Limits{}, Lifetime: 10 * time.Second, OutputBytes: 64 << 10,
	}
}

func exerciseFilesystemRelay(t *testing.T, workerConfig, brokerConfig sandboxlinux.WorkerConfig, mode string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "file"), []byte("original"), 0600))
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	aggregate, err := sandboxlinux.NewFilesystemAggregate(ctx, workerConfig.CgroupParent, workerConfig.Limits, workerConfig.Tenant)
	if aggregate != nil {
		defer func() {
			cleanup, stop := context.WithTimeout(t.Context(), 5*time.Second)
			defer stop()
			assert.NoError(t, aggregate.Close(cleanup))
		}()
	}
	require.NoError(t, err)
	rights := sandboxbroker.Read | sandboxbroker.Write | sandboxbroker.List
	if mode == "host-denied" {
		rights = sandboxbroker.Read
	}
	if mode == "write-only-list-denied" {
		rights = sandboxbroker.Write
	}
	grants := []sandboxbroker.LinuxRootGrant{{Name: "data", Path: root, Rights: rights}}

	storageBase := t.TempDir()
	for _, name := range []string{"journal", "checkpoint", "approval"} {
		require.NoError(t, os.Mkdir(filepath.Join(storageBase, name), 0700))
	}
	storage := sandboxlinux.FilesystemRecoveryStorage{
		JournalDirectory:    filepath.Join(storageBase, "journal"),
		CheckpointDirectory: filepath.Join(storageBase, "checkpoint"),
		ApprovalDirectory:   filepath.Join(storageBase, "approval"),
	}
	binding, err := sandboxlinux.RecoveryHostBinding(ctx, brokerConfig.Digest)
	require.NoError(t, err)
	storage.Binding = binding
	brokerStoreDir := t.TempDir()
	require.NoError(t, os.Chmod(brokerStoreDir, 0700))
	brokerStore, err := sandboxbroker.OpenLinuxImageStore(brokerStoreDir, grants,
		[]string{storage.JournalDirectory, storage.CheckpointDirectory, storage.ApprovalDirectory})
	require.NoError(t, err)
	defer brokerStore.Close()
	storage.ServiceContext, err = aggregate.Group.RecoveryIdentityWithImages(brokerStore)
	require.NoError(t, err)
	brokerConfig.ImageStore = brokerStore
	workerConfig.ImageStore = nativeImageStoreWithGrants(t, grants)
	broker, err := sandboxlinux.OpenCheckpointedFilesystemBrokerInGroup(ctx, brokerConfig, grants, sandboxbroker.FilesystemLimits{}, storage, aggregate.Group)
	if broker != nil {
		defer broker.Close(t.Context())
	}
	require.NoError(t, err)
	worker, err := sandboxlinux.LaunchWorkerInGroup(ctx, workerConfig, aggregate.Group)
	if worker != nil {
		defer worker.Close(t.Context())
	}
	require.NoError(t, err)
	config := FilesystemConfiguration{
		Profile: FilesystemProfile, Imports: []string{"pipit/fs"},
		Roots:  []sandboxbroker.RootGrant{{Name: "data", Rights: sandboxbroker.Read | sandboxbroker.Write | sandboxbroker.List}},
		Limits: &sandboxbroker.FilesystemLimits{},
	}
	source, want := filesystemRelaySource(mode)
	if mode == "cancelled" {
		cancel()
	}
	response, err := ExchangeFilesystem(ctx, worker, config, Request{Kind: "file", Source: source, Entrypoint: "run"}, broker)
	switch mode {
	case "cancelled":
		require.ErrorIs(t, err, context.Canceled, "cancellation propagates")
	case "host-denied":
		require.Error(t, err, "the host policy is enforced")
	case "write-only-list-denied":
		require.Error(t, err, "a write-only grant refuses a directory listing")
	case "ignored-io":
		if err == nil {
			require.NotEqual(t, "ok", response.Code, "ignored failed I/O does not return success")
		}
	case "denied-root", "compile-error":
		require.NoError(t, err, "the evaluation fails inside the worker: %+v", response)
		require.Equal(t, "evaluation_failed", response.Code)
	default:
		requireWorkerOK(t, err, worker, "relay")
		require.Equal(t, "ok", response.Code, "relay: %+v", response)
		require.Equal(t, want, string(response.Value))
	}
	select {
	case <-worker.Done():
	default:
		require.FailNow(t, "the source process is reaped")
	}
	data, err := os.ReadFile(filepath.Join(root, "file"))
	expected := "original"
	if mode == "read-write-list" {
		expected = "changed"
	}
	require.NoError(t, err, "unexpected publication: %q", data)
	require.Equal(t, expected, string(data))
}

func filesystemRelaySource(mode string) (string, string) {
	prefix := "package main\nimport \"pipit/fs\"\n"
	switch mode {
	case "read-write-list":
		return prefix + `func run() string {
   data, err := fs.Read("data","file",64)
   if err != nil { return "read failed" }
   if fs.Write("data","file",[]byte("changed")) != nil { return "write failed" }
   names, _, _ := fs.List("data",".",4)
   return string(data)+":"+names[0]
  }`, `"original:file"`
	case "initialiser":
		return prefix + `var value = load()
  func load() string { data,_ := fs.Read("data","file",64); return string(data) }
  func run() string { return value }`, `"original"`
	case "host-denied":
		return prefix + `func run() bool { _ = fs.Write("data","file",[]byte("bad")); return true }`, ""
	case "write-only-list-denied":
		return prefix + `func run() bool { _,_,_ = fs.List("data",".",4); return true }`, ""
	case "ignored-io":
		return prefix + `func run() bool { _,_ = fs.Read("data","missing",1); return true }`, ""
	case "denied-root":
		return prefix + `func run() bool { _,_ = fs.Read("missing","file",1); return true }`, ""
	case "final-call":
		return prefix + `func run() int { for count := 0; count < 100; count++ { _,_ = fs.Read("data","file",1) }; return 100 }`, "100"
	default:
		return prefix + `func run() { fs.Remove("data","file") }`, ""
	}
}
