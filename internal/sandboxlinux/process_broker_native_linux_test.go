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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxwire"
)

func TestNativeFilesystemBrokerSupervised(t *testing.T) {
	executable, parent := os.Getenv("PIPIT_TEST_FILESYSTEM_BROKER_BINARY"), os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if executable == "" || parent == "" {
		t.Skip("requires a static filesystem broker and delegated cgroup")
	}
	version, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 || version < 8 {
		if os.Getenv("PIPIT_REQUIRE_BROKER_LANDLOCK") == "1" {
			t.Fatalf("Landlock TSYNC unavailable: %d: %v", version, errno)
		}
		t.Skipf("Landlock TSYNC unavailable: %d: %v", version, errno)
	}
	image, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	config := WorkerConfig{Tenant: testTenant, Executable: executable, CgroupParent: parent, Digest: sha256.Sum256(image),
		Limits: Limits{}, Lifetime: 5 * time.Second, OutputBytes: 64 << 10}
	config.WatchdogExecutable = os.Getenv("PIPIT_TEST_HOST_WATCHDOG_BINARY")
	watchdogImage, err := os.ReadFile(config.WatchdogExecutable)
	if err != nil {
		t.Fatal(err)
	}
	config.WatchdogDigest = sha256.Sum256(watchdogImage)
	for _, mode := range []string{
		"success", "denied", "replay", "deadline", "invalid-config", "call-budget",
		"client", "client-limit", "client-denied", "client-cancel",
		"journalled-client", "journalled-failure",
		"journalled-recovery", "journalled-recovery-collision",
		"journalled-recovery-failed-broker", "journalled-recovery-cleanup",
		"journalled-recovery-approval", "journalled-recovery-aggregate",
		"journalled-recovery-checkpoint",
		"journal-owner", "journal-owner-remnant", "journal-owner-collision", "journal-owner-approval",
		"journal-owner-cleanup", "journal-owner-denied", "journal-owner-concurrent", "journal-owner-aggregate",
		"journal-owner-startup-failure",
	} {
		t.Run(mode, func(t *testing.T) { exerciseSupervisedFilesystemBroker(t, config, mode) })
	}
}

func exerciseSupervisedFilesystemBroker(t *testing.T, config WorkerConfig, mode string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(mode, "journal-owner") {
		runJournalledOwner(t, config, root, mode)
		return
	}
	bootstrap, err := sandboxbroker.PrepareLinuxBootstrap([]sandboxbroker.LinuxRootGrant{
		{Name: "data", Path: root, Rights: sandboxbroker.Read | sandboxbroker.Write | sandboxbroker.List},
	}, sandboxbroker.FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	var recovery *sandboxbroker.LinuxRecoveryAuthority
	if strings.HasPrefix(mode, "journalled") {
		recovery, err = bootstrap.RecoveryAuthority()
		if err != nil {
			t.Fatal(err)
		}
		defer recovery.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	releaseWorker, err := workerAdmission.acquire(ctx, testTenant)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseWorker()
	var aggregate *Group
	if mode == "journalled-recovery-aggregate" {
		aggregate, err = newGroup(config.CgroupParent, config.Limits)
		if aggregate != nil {
			defer func() {
				cleanup, cancel := context.WithTimeout(context.Background(), workerCleanupTimeout)
				defer cancel()
				if err := aggregate.closeGroup(cleanup); err != nil {
					t.Error(err)
				}
			}()
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	process, err := launchFilesystemBroker(ctx, config, bootstrap, aggregate)
	if process != nil {
		defer process.Close()
	}
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if err := bootstrap.Close(); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(mode, "journalled") {
		runJournalledBrokerClient(t, config, process, recovery, bootstrap.StagingNamespace(), root, mode)
		return
	}
	if strings.HasPrefix(mode, "client") {
		runSupervisedBrokerClient(t, process, mode)
		return
	}
	if err := process.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	codec, err := sandboxwire.New(process, process, sandboxwire.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	readBrokerFrame(t, codec, sandboxwire.Hello, 0)
	configuration := `{"profile":"filesystem-broker-v1"}`
	if mode == "invalid-config" {
		configuration = `{"profile":"filesystem-broker-v1","roots":[]}`
	}
	writeBrokerFrame(t, codec, sandboxwire.Configure, 0, configuration)
	if mode == "invalid-config" {
		if _, err := codec.Read(); err == nil {
			t.Fatal("accepted authority in protocol configuration")
		}
		if err := process.Wait(); err == nil {
			t.Fatal("invalid configuration was not terminal")
		}
		return
	}
	readBrokerFrame(t, codec, sandboxwire.Ready, 0)
	if mode == "call-budget" {
		runBrokerCallBudget(t, process, codec)
		return
	}
	if mode == "deadline" {
		if err := process.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		if err := process.Wait(); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("idle phase deadline did not kill broker:", err)
		}
		return
	}
	if mode == "denied" {
		writeBrokerFrame(t, codec, sandboxwire.Run, 1, `{"operation":"fs.read","root":"missing","path":"secret","max_bytes":4}`)
		message := readBrokerFrame(t, codec, sandboxwire.Result, 1)
		var response sandboxbroker.FilesystemResponse
		if err := message.DecodePayload(&response); err != nil || response.Code != "denied" || response.Data != nil {
			t.Fatalf("%+v: %v", response, err)
		}
		if err := process.Wait(); err == nil {
			t.Fatal("denied operation was not terminal")
		}
		return
	}
	runSuccessfulBrokerOperations(t, process, codec, root, mode)
}

func runSupervisedBrokerClient(t *testing.T, process *WorkerProcess, mode string) {
	t.Helper()
	limits := sandboxbroker.FilesystemLimits{}
	count := 100
	if mode == "client-limit" {
		limits.Calls, limits.Outstanding, count = 2, 1, 2
	}
	client, err := sandboxbroker.OpenFilesystemClient(context.Background(), process,
		[]sandboxbroker.RootGrant{{Name: "data", Rights: sandboxbroker.Read}}, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if mode == "client-denied" {
		_, err := client.Execute(context.Background(), []byte(`{"operation":"fs.write","root":"data","path":"file","data":""}`))
		if !errors.Is(err, sandboxbroker.ErrDenied) {
			t.Fatal("host did not reject write:", err)
		}
		return
	}
	if mode == "client-cancel" {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := client.Execute(ctx, []byte(`{"operation":"fs.read","root":"data","path":"file","max_bytes":1}`))
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled client admitted operation:", err)
		}
		return
	}
	for range count {
		response, err := client.Execute(context.Background(), []byte(`{"operation":"fs.read","root":"data","path":"file","max_bytes":1}`))
		if err != nil || string(response.Data) != "o" {
			t.Fatalf("client result: %+v: %v", response, err)
		}
	}
	if _, err := client.Execute(context.Background(), []byte(`{}`)); !errors.Is(err, sandboxbroker.ErrClosed) {
		t.Fatal("exhausted client accepted operation:", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if process.releaseAdmission != nil {
		t.Fatal("client retained broker admission")
	}
}

func runBrokerCallBudget(t *testing.T, process *WorkerProcess, codec *sandboxwire.Codec) {
	t.Helper()
	for identity := uint64(1); identity <= 100; identity++ {
		writeBrokerFrame(t, codec, sandboxwire.Run, identity, `{"operation":"fs.read","root":"data","path":"file","max_bytes":1}`)
		message := readBrokerFrame(t, codec, sandboxwire.Result, identity)
		var response sandboxbroker.FilesystemResponse
		if err := message.DecodePayload(&response); err != nil || response.Code != "" || string(response.Data) != "o" {
			t.Fatalf("operation %d: %+v: %v", identity, response, err)
		}
	}
	if err := process.Wait(); err != nil {
		t.Fatal("final response not followed by successful reaping:", err)
	}
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}
}

func runSuccessfulBrokerOperations(t *testing.T, process *WorkerProcess, codec *sandboxwire.Codec, root, mode string) {
	t.Helper()
	writeBrokerFrame(t, codec, sandboxwire.Run, 1, `{"operation":"fs.read","root":"data","path":"file","max_bytes":64}`)
	message := readBrokerFrame(t, codec, sandboxwire.Result, 1)
	var response sandboxbroker.FilesystemResponse
	if err := message.DecodePayload(&response); err != nil || response.Code != "" || string(response.Data) != "original" {
		t.Fatalf("%+v: %v", response, err)
	}
	identity := uint64(2)
	if mode == "replay" {
		identity = 1
	}
	writeBrokerFrame(t, codec, sandboxwire.Run, identity, `{"operation":"fs.write","root":"data","path":"file","data":"bmV3"}`)
	if mode == "replay" {
		if _, err := codec.Read(); err == nil {
			t.Fatal("replayed operation accepted")
		}
		if err := process.Wait(); err == nil {
			t.Fatal("replay did not terminate broker")
		}
		data, err := os.ReadFile(filepath.Join(root, "file"))
		if err != nil || string(data) != "original" {
			t.Fatalf("replay caused I/O: %q: %v", data, err)
		}
		return
	}
	message = readBrokerFrame(t, codec, sandboxwire.Result, 2)
	if err := message.DecodePayload(&response); err != nil || response.Code != "" || response.Written != 3 {
		t.Fatalf("%+v: %v", response, err)
	}
	writeBrokerFrame(t, codec, sandboxwire.Run, 3, `{"operation":"fs.list","root":"data","path":".","max_entries":16}`)
	message = readBrokerFrame(t, codec, sandboxwire.Result, 3)
	if err := message.DecodePayload(&response); err != nil || response.Code != "" || len(response.Entries) != 1 {
		t.Fatalf("%+v: %v", response, err)
	}
	writeBrokerFrame(t, codec, sandboxwire.Close, 0, "{}")
	if err := process.Wait(); err != nil {
		t.Fatalf("reap: %v: %s", err, process.Output())
	}
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}
	if process.releaseAdmission != nil {
		t.Fatal("broker admission retained after cleanup")
	}
	data, err := os.ReadFile(filepath.Join(root, "file"))
	if err != nil || string(data) != "new" {
		t.Fatalf("publication: %q: %v", data, err)
	}
}

func writeBrokerFrame(t *testing.T, codec *sandboxwire.Codec, kind sandboxwire.Kind, identity uint64, payload string) {
	t.Helper()
	if err := codec.Write(sandboxwire.Message{Kind: kind, ID: identity, Payload: []byte(payload)}); err != nil {
		t.Fatal(err)
	}
}

func readBrokerFrame(t *testing.T, codec *sandboxwire.Codec, kind sandboxwire.Kind, identity uint64) sandboxwire.Message {
	t.Helper()
	message, err := codec.Read()
	if err != nil || message.Kind != kind || message.ID != identity {
		t.Fatalf("frame: %+v: %v", message, err)
	}
	return message
}
