//go:build linux && (amd64 || arm64) && !race

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
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxwire"
)

func TestNativeFilesystemBrokerFilter(t *testing.T) {
	skipUnderCoverage(t)
	if os.Getenv("PIPIT_BROKER_FILTER_CHILD") == "1" {
		exerciseNativeFilesystemBrokerFilter(t)
		return
	}
	version, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 || version < 8 {
		if os.Getenv("PIPIT_REQUIRE_BROKER_LANDLOCK") == "1" {
			t.Fatalf("Landlock TSYNC unavailable: %d: %v", version, errno)
		}
		t.Skipf("Landlock TSYNC unavailable: %d: %v", version, errno)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeFilesystemBrokerFilter$")
	command.Env = append(os.Environ(), "PIPIT_BROKER_FILTER_CHILD=1", "PIPIT_BROKER_FILTER_ROOT="+root)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("broker filter subprocess: %v\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(root, "file"))
	if err != nil || string(data) != "new" {
		t.Fatalf("confined write not published: %q: %v", data, err)
	}
}

func exerciseNativeFilesystemBrokerFilter(t *testing.T) {
	root := os.Getenv("PIPIT_BROKER_FILTER_ROOT")
	backend, err := sandboxbroker.OpenLinuxFilesystem([]sandboxbroker.LinuxRootGrant{
		{Name: "data", Path: root, Rights: sandboxbroker.Read | sandboxbroker.Write | sandboxbroker.List},
	}, sandboxbroker.FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	started, probe := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		close(started)
		<-probe
		descriptor, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
		if err == nil {
			_ = unix.Close(descriptor)
		}
		finished <- err
	}()
	<-started
	if err := backend.SealProcess(); err != nil {
		t.Fatal(err)
	}
	filter, err := filesystemBrokerFilter(unix.Getpid(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := installWorkerFilter(filter); err != nil {
		t.Fatal(err)
	}
	close(probe)
	if err := <-finished; !errors.Is(err, unix.EPERM) {
		t.Fatal("existing thread escaped broker filter:", err)
	}

	for _, number := range []uintptr{unix.SYS_CLONE3, unix.SYS_IO_URING_SETUP, unix.SYS_SOCKET} {
		_, _, errno := unix.RawSyscall(number, 0, 0, 0)
		if errno != unix.EPERM {
			t.Fatalf("forbidden syscall %d: %v", number, errno)
		}
	}
	payloads := []string{
		`{"operation":"fs.read","root":"data","path":"file","max_bytes":64}`,
		`{"operation":"fs.write","root":"data","path":"file","data":"bmV3"}`,
		`{"operation":"fs.list","root":"data","path":".","max_entries":16}`,
	}
	for index, payload := range payloads {
		result, err := backend.Execute(sandboxwire.Message{Kind: sandboxwire.Call, ID: uint64(index + 1), Payload: json.RawMessage(payload)})
		if err != nil {
			t.Fatalf("operation %d failed under combined confinement: %v", index, err)
		}
		switch index {
		case 0:
			if string(result.Data) != "original" {
				t.Fatal("incorrect read")
			}
		case 1:
			if result.Written != 3 {
				t.Fatal("incorrect write count")
			}
		case 2:
			if len(result.Entries) != 1 || result.Entries[0] != "file" {
				t.Fatal("incorrect listing")
			}
		}
	}
}
