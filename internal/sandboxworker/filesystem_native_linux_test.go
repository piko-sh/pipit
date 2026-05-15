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

package sandboxworker

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if err != nil {
		t.Fatal(err)
	}
	watchdogPath := os.Getenv("PIPIT_TEST_HOST_WATCHDOG_BINARY")
	watchdogImage, err := os.ReadFile(watchdogPath)
	if err != nil {
		t.Fatal(err)
	}
	return sandboxlinux.WorkerConfig{Tenant: "test-tenant", ImageStore: nil,
		WatchdogExecutable: watchdogPath, WatchdogDigest: sha256.Sum256(watchdogImage),
		Executable: executable, CgroupParent: parent, Digest: sha256.Sum256(image),
		Limits: sandboxlinux.Limits{}, Lifetime: 10 * time.Second, OutputBytes: 64 << 10,
	}
}

func exerciseFilesystemRelay(t *testing.T, workerConfig, brokerConfig sandboxlinux.WorkerConfig, mode string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	aggregate, err := sandboxlinux.NewFilesystemAggregate(ctx, workerConfig.CgroupParent, workerConfig.Limits, workerConfig.Tenant)
	if aggregate != nil {
		defer func() {
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if err := aggregate.Close(cleanup); err != nil {
				t.Error(err)
			}
		}()
	}
	if err != nil {
		t.Fatal(err)
	}
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
		if err := os.Mkdir(filepath.Join(storageBase, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	storage := sandboxlinux.FilesystemRecoveryStorage{
		JournalDirectory:    filepath.Join(storageBase, "journal"),
		CheckpointDirectory: filepath.Join(storageBase, "checkpoint"),
		ApprovalDirectory:   filepath.Join(storageBase, "approval"),
	}
	binding, err := sandboxlinux.RecoveryHostBinding(ctx, brokerConfig.Digest)
	if err != nil {
		t.Fatal(err)
	}
	storage.Binding = binding
	brokerStoreDir := t.TempDir()
	if err := os.Chmod(brokerStoreDir, 0700); err != nil {
		t.Fatal(err)
	}
	brokerStore, err := sandboxbroker.OpenLinuxImageStore(brokerStoreDir, grants,
		[]string{storage.JournalDirectory, storage.CheckpointDirectory, storage.ApprovalDirectory})
	if err != nil {
		t.Fatal(err)
	}
	defer brokerStore.Close()
	storage.ServiceContext, err = aggregate.Group.RecoveryIdentityWithImages(brokerStore)
	if err != nil {
		t.Fatal(err)
	}
	brokerConfig.ImageStore = brokerStore
	workerConfig.ImageStore = nativeImageStoreWithGrants(t, grants)
	broker, err := sandboxlinux.OpenCheckpointedFilesystemBrokerInGroup(ctx, brokerConfig, grants, sandboxbroker.FilesystemLimits{}, storage, aggregate.Group)
	if broker != nil {
		defer broker.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	worker, err := sandboxlinux.LaunchWorkerInGroup(ctx, workerConfig, aggregate.Group)
	if worker != nil {
		defer worker.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
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
	if mode == "cancelled" {
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation not propagated:", err)
		}
	} else if mode == "host-denied" {
		if err == nil {
			t.Fatal("host policy bypassed")
		}
	} else if mode == "write-only-list-denied" {
		if err == nil {
			t.Fatal("write-only grant allowed a directory listing")
		}
	} else if mode == "ignored-io" {
		if err == nil && response.Code == "ok" {
			t.Fatal("ignored failed I/O returned success")
		}
	} else if mode == "denied-root" || mode == "compile-error" {
		if err != nil || response.Code != "evaluation_failed" {
			t.Fatalf("expected evaluation failure: %+v: %v", response, err)
		}
	} else if err != nil || response.Code != "ok" || string(response.Value) != want {
		t.Fatalf("relay: %+v: %v\nworker diagnostics: %s", response, err, worker.Output())
	}
	select {
	case <-worker.Done():
	default:
		t.Fatal("source process not reaped")
	}
	data, err := os.ReadFile(filepath.Join(root, "file"))
	expected := "original"
	if mode == "read-write-list" {
		expected = "changed"
	}
	if err != nil || string(data) != expected {
		t.Fatalf("unexpected publication: %q: %v", data, err)
	}
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
