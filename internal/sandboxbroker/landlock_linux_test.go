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

package sandboxbroker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestLinuxBrokerLandlock(t *testing.T) {
	if os.Getenv("PIPIT_TEST_BROKER_LANDLOCK") == "child" {
		exerciseBrokerLandlock(t)
		return
	}
	if os.Getenv("PIPIT_TEST_BROKER_LANDLOCK") == "recovery" {
		exerciseRecoveryLandlock(t)
		return
	}
	if testing.CoverMode() != "" {
		t.Skip("the re-executed child cannot write a coverage profile through its own Landlock confinement")
	}
	if _, err := brokerLandlockABI(); err != nil {
		if os.Getenv("PIPIT_REQUIRE_BROKER_LANDLOCK") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, mode := range []string{"child", "recovery"} {
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLinuxBrokerLandlock$", "-test.v")
		command.Env = append(os.Environ(), "PIPIT_TEST_BROKER_LANDLOCK="+mode, "PIPIT_TEST_BROKER_ROOT="+t.TempDir())
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("Landlock %s: %v\n%s", mode, err, output)
		}
		t.Logf("%s", output)
	}
}

func exerciseBrokerLandlock(t *testing.T) {
	root := os.Getenv("PIPIT_TEST_BROKER_ROOT")
	grants := make([]LinuxRootGrant, 0, 3)
	for _, grant := range []struct {
		name   string
		rights Rights
	}{{"read", Read}, {"write", Write}, {"list", List}} {
		directory := filepath.Join(root, grant.name)
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "file"), []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		grants = append(grants, LinuxRootGrant{Name: grant.name, Path: directory, Rights: grant.rights})
	}
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	bootstrap, err := PrepareLinuxBootstrap(grants, FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	files, err := bootstrap.Files()
	if err != nil {
		t.Fatal(err)
	}
	backend, err := adoptLinuxFilesystem(files, testBrokerProc(t))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if err := bootstrap.Close(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 4)
	sealed := make(chan struct{})
	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			ready <- struct{}{}
			<-sealed
			if _, err := os.ReadFile(outside); !errors.Is(err, os.ErrPermission) {
				t.Errorf("existing thread escaped: %v", err)
			}
			if value, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0); err != nil || value != 1 {
				t.Errorf("existing thread retained privilege escalation: %d: %v", value, err)
			}
		})
	}
	for range 4 {
		<-ready
	}
	if err := backend.SealProcess(); err != nil {
		close(sealed)
		group.Wait()
		t.Fatal(err)
	}
	close(sealed)
	group.Wait()
	testLandlockDirectAccess(t, root, outside)
	testLandlockBackendAccess(t, backend)
	if err := backend.SealProcess(); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal("resealed mutable policy:", err)
	}
}

func testLandlockDirectAccess(t *testing.T, root, outside string) {
	t.Helper()
	for _, name := range []string{outside, filepath.Join(root, "write", "file"), filepath.Join(root, "list", "file")} {
		if _, err := os.ReadFile(name); !errors.Is(err, os.ErrPermission) {
			t.Errorf("read allowed %q: %v", name, err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "read", "file")); err != nil || string(data) != "original" {
		t.Errorf("approved read: %q: %v", data, err)
	}
	if _, err := os.ReadDir(filepath.Join(root, "read")); !errors.Is(err, os.ErrPermission) {
		t.Errorf("read grant listed directory: %v", err)
	}
	if _, err := os.ReadDir(filepath.Join(root, "list")); err != nil {
		t.Error("approved listing:", err)
	}
	for _, name := range []string{outside, filepath.Join(root, "read", "file"), filepath.Join(root, "list", "file")} {
		if err := os.WriteFile(name, []byte("changed"), 0600); !errors.Is(err, os.ErrPermission) {
			t.Errorf("write allowed %q: %v", name, err)
		}
	}
	for _, name := range []string{"read", "list", "write"} {
		if err := unix.Mkfifo(filepath.Join(root, name, "fifo"), 0600); !errors.Is(err, unix.EACCES) {
			t.Errorf("special file allowed: %v", err)
		}
	}
	descriptor, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(descriptor)
	if err := unix.Bind(descriptor, &unix.SockaddrInet4{Port: 0, Addr: [4]byte{127, 0, 0, 1}}); !errors.Is(err, unix.EACCES) {
		t.Error("TCP bind allowed:", err)
	}
	if err := unix.Connect(descriptor, &unix.SockaddrInet4{Port: 9, Addr: [4]byte{127, 0, 0, 1}}); !errors.Is(err, unix.EACCES) {
		t.Error("TCP connect allowed:", err)
	}
}

func testLandlockBackendAccess(t *testing.T, backend *LinuxFilesystem) {
	t.Helper()
	read := brokerMessage(1, `{"operation":"fs.read","root":"read","path":"file","max_bytes":64}`)
	if result, err := backend.Execute(read); err != nil || string(result.Data) != "original" {
		t.Fatalf("confined read: %+v: %v", result, err)
	}
	write := brokerMessage(2, `{"operation":"fs.write","root":"write","path":"file","data":"bmV3"}`)
	if result, err := backend.Execute(write); err != nil || result.Written != 3 {
		t.Fatalf("confined staged write: %+v: %v", result, err)
	}
	list := brokerMessage(3, `{"operation":"fs.list","root":"list","path":".","max_entries":16}`)
	if result, err := backend.Execute(list); err != nil || len(result.Entries) != 1 {
		t.Fatalf("confined listing: %+v: %v", result, err)
	}
	denied := brokerMessage(4, `{"operation":"fs.list","root":"write","path":".","max_entries":16}`)
	if _, err := backend.Execute(denied); !errors.Is(err, ErrDenied) {
		t.Fatal("native write metadata permission leaked to protocol:", err)
	}
	root, record := stageRecoveryRemnant(t, backend, "write", "file")
	for range 2 {
		if err := recoverLinuxStaging(root, backend.namespace, record); err != nil {
			t.Fatal("confined staging cleanup or retry failed:", err)
		}
	}
}

func TestLinuxBrokerRejectsLateSealing(t *testing.T) {
	backend := openTestFilesystem(t, t.TempDir(), Read, FilesystemLimits{})
	if _, err := backend.Execute(brokerMessage(1, readPayload)); err == nil {
		t.Fatal("missing file read succeeded")
	}
	if err := backend.SealProcess(); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal(err)
	}
	if _, err := backend.Execute(brokerMessage(2, readPayload)); !errors.Is(err, ErrConfinement) {
		t.Fatal("failed sealing did not stop admission:", err)
	}
	if err := backend.SealProcess(); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal("retry accepted:", err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if err := backend.SealProcess(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	var absent *LinuxFilesystem
	if err := absent.SealProcess(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestBrokerLandlockRights(t *testing.T) {

	const (
		readFile = unix.LANDLOCK_ACCESS_FS_READ_FILE
		readDir  = unix.LANDLOCK_ACCESS_FS_READ_DIR
		writeSet = unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR |
			unix.LANDLOCK_ACCESS_FS_MAKE_REG | unix.LANDLOCK_ACCESS_FS_REMOVE_FILE
	)
	for _, test := range []struct {
		name   string
		rights Rights
		want   uint64
	}{
		{"none", 0, 0},
		{"read", Read, readFile},
		{"list", List, readDir},
		{"write", Write, writeSet},
		{"read+list", Read | List, readFile | readDir},
		{"read+write", Read | Write, readFile | writeSet},
		{"list+write", List | Write, readDir | writeSet},
		{"read+list+write", Read | List | Write, readFile | writeSet},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := brokerLandlockRights(test.rights)
			if got != test.want {
				t.Fatalf("rights %b -> %#x, want %#x", test.rights, got, test.want)
			}

			if got & ^uint64(brokerFilesystemAccess) != 0 {
				t.Fatal("mask escaped the allowed set")
			}
			for _, forbidden := range []uint64{unix.LANDLOCK_ACCESS_FS_EXECUTE, unix.LANDLOCK_ACCESS_FS_REFER, unix.LANDLOCK_ACCESS_FS_TRUNCATE} {
				if got&forbidden != 0 {
					t.Fatalf("mask granted a forbidden right %#x", forbidden)
				}
			}
		})
	}
}
