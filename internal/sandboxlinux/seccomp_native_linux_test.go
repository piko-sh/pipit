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
	"errors"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestNativeSeccomp(t *testing.T) {
	skipUnderCoverage(t)
	if os.Getenv("PIPIT_SECCOMP_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeSeccomp$")
		command.Env = append(os.Environ(), "PIPIT_SECCOMP_CHILD=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("native filter fixture failed: %v\n%s", err, output)
		}
		return
	}
	started := make(chan struct{})
	probe := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		close(started)
		<-probe
		descriptor, err := unix.Open("/dev/null", unix.O_RDONLY, 0)
		if err == nil {
			_ = unix.Close(descriptor)
		}
		finished <- err
	}()
	<-started
	if err := installSyscallFilter(); err != nil {
		t.Fatal(err)
	}
	close(probe)
	if err := <-finished; !errors.Is(err, unix.EPERM) {
		t.Fatalf("existing thread escaped filter: %v", err)
	}
	descriptor, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
	if err == nil {
		_ = unix.Close(descriptor)
	}
	if !errors.Is(err, unix.EPERM) {
		t.Fatalf("socket creation escaped filter: %v", err)
	}

	for _, number := range []uintptr{unix.SYS_CLONE3, unix.SYS_IO_URING_SETUP, unix.SYS_OPENAT} {
		_, _, errno := unix.RawSyscall(number, 0, 0, 0)
		if errno != unix.EPERM {
			t.Fatalf("syscall %d escaped filter: %v", number, errno)
		}
	}
	allocation := make([]byte, 4<<20)
	for index := range allocation {
		allocation[index] = 1
	}
	runtime.KeepAlive(allocation)
	debug.FreeOSMemory()
}

func TestNativeSeccompRejectsUnsynchronisedThread(t *testing.T) {
	if os.Getenv("PIPIT_SECCOMP_DIVERGENT_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeSeccompRejectsUnsynchronisedThread$")
		command.Env = append(os.Environ(), "PIPIT_SECCOMP_DIVERGENT_CHILD=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("divergent filter fixture failed: %v\n%s", err, output)
		}
		return
	}
	ready := make(chan error, 1)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
			ready <- err
			return
		}
		filter := []unix.SockFilter{{Code: unix.BPF_RET | unix.BPF_K, Jt: 0, Jf: 0, K: unix.SECCOMP_RET_ALLOW}}
		program := unix.SockFprog{Len: 1, Filter: &filter[0]}
		_, _, errno := unix.RawSyscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, 0, uintptr(unsafe.Pointer(&program)))
		runtime.KeepAlive(filter)
		if errno != 0 {
			ready <- errno
			return
		}
		ready <- nil
		<-stop
	}()
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	if err := installSyscallFilter(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("partially synchronised filter reported success: %v", err)
	}
}
