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
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestNativeWorkerPrivileges(t *testing.T) {
	skipUnderCoverage(t)
	if os.Getenv("PIPIT_PRIVILEGES_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeWorkerPrivileges$")
		command.Env = append(os.Environ(), "PIPIT_PRIVILEGES_CHILD=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("worker privilege fixture failed: %v\n%s", err, output)
		}
		return
	}
	checkWorkerPrivilegeThreads(t, false)
}

func TestNativeSealedWorkerPrivileges(t *testing.T) {
	skipUnderCoverage(t)
	if os.Getenv("PIPIT_SEALED_PRIVILEGES_CHILD") == "1" {
		checkWorkerPrivilegeThreads(t, true)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeSealedWorkerPrivileges$")
	command.Env = append(os.Environ(), "PIPIT_SEALED_PRIVILEGES_CHILD=1")
	command.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:                 unix.CLONE_NEWUSER,
		UidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
		GidMappingsEnableSetgroups: false,
	}
	output, err := command.CombinedOutput()
	if os.Getenv("PIPIT_TEST_REQUIRE_USERNS") != "1" &&
		(errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOSYS)) {
		t.Skipf("native user namespace unavailable: %v", err)
	}
	if err != nil {
		t.Fatalf("sealed privilege fixture failed: %v\n%s", err, output)
	}
}

func checkWorkerPrivilegeThreads(t *testing.T, sealed bool) {
	t.Helper()
	ready := make(chan struct{}, 24)
	probe := make(chan struct{})
	stop := make(chan struct{})
	defer close(stop)
	results := make(chan error, 24)
	startThread := func() {
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			ready <- struct{}{}
			<-probe
			results <- workerPrivilegeState(sealed)
			<-stop
		}()
	}
	for range 8 {
		startThread()
		<-ready
	}
	restrict := restrictWorkerPrivileges
	if sealed {
		seedWorkerCapabilities(t)
		restrict = sealWorkerPrivileges
	}
	if err := restrict(); err != nil {
		t.Fatal(err)
	}
	close(probe)
	for range 8 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	for range 16 {
		startThread()
		<-ready
	}
	for range 16 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if err := restrictWorkerPrivileges(); err != nil {
		t.Fatalf("repeated restriction failed: %v", err)
	}
	if !sealed {
		if err := sealWorkerPrivileges(); !errors.Is(err, ErrUnavailable) || !errors.Is(err, unix.EPERM) {
			t.Fatalf("sealing without CAP_SETPCAP did not fail closed: %v", err)
		}
	}
	if err := installSyscallFilter(); err != nil {
		t.Fatalf("filter after privilege removal failed: %v", err)
	}
}

func workerPrivilegeState(sealed bool) error {
	value, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if err != nil || value != 1 {
		return fmt.Errorf("no_new_privs = %d: %w", value, err)
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3, Pid: 0}
	capabilities := [2]unix.CapUserData{}
	_, _, errno := unix.RawSyscall(unix.SYS_CAPGET,
		uintptr(unsafe.Pointer(&header)), uintptr(unsafe.Pointer(&capabilities[0])), 0)
	runtime.KeepAlive(&header)
	runtime.KeepAlive(&capabilities)
	if errno != 0 {
		return fmt.Errorf("reading capabilities: %w", errno)
	}
	if capabilities != [2]unix.CapUserData{} {
		return fmt.Errorf("capabilities remain: %+v", capabilities)
	}
	if sealed {
		return sealedWorkerPrivilegeState()
	}
	return nil
}

func sealedWorkerPrivilegeState() error {
	securebits, err := unix.PrctlRetInt(unix.PR_GET_SECUREBITS, 0, 0, 0, 0)
	if err != nil || securebits&workerSecurebits != workerSecurebits || securebits&secureKeepCapabilities != 0 {
		return fmt.Errorf("unexpected securebits %#x: %w", securebits, err)
	}
	if dumpable, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0); err != nil || dumpable != 0 {
		return fmt.Errorf("process remains dumpable %d: %w", dumpable, err)
	}
	for capability := range uintptr(maximumCapabilityBits) {
		value, err := unix.PrctlRetInt(unix.PR_CAPBSET_READ, capability, 0, 0, 0)
		if errors.Is(err, unix.EINVAL) && capability > 0 {
			break
		}
		if err != nil || value != 0 {
			return fmt.Errorf("bounding capability %d = %d: %w", capability, value, err)
		}
		value, err = unix.PrctlRetInt(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_IS_SET, capability, 0, 0)
		if err != nil || value != 0 {
			return fmt.Errorf("ambient capability %d = %d: %w", capability, value, err)
		}
	}
	if err := unix.Prctl(unix.PR_SET_SECUREBITS, 0, 0, 0, 0); !errors.Is(err, unix.EPERM) {
		return fmt.Errorf("securebits could be unlocked: %w", err)
	}
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_RAISE, unix.CAP_CHOWN, 0, 0); !errors.Is(err, unix.EPERM) {
		return fmt.Errorf("ambient capability could be restored: %w", err)
	}
	return nil
}

func seedWorkerCapabilities(t *testing.T) {
	t.Helper()
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3, Pid: 0}
	capabilities := [2]unix.CapUserData{}
	_, _, errno := unix.RawSyscall(unix.SYS_CAPGET,
		uintptr(unsafe.Pointer(&header)), uintptr(unsafe.Pointer(&capabilities[0])), 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	if capabilities[0].Effective&(1<<unix.CAP_SETPCAP) == 0 {
		t.Fatal("user namespace did not grant CAP_SETPCAP")
	}
	capabilities[0].Inheritable = 1 << unix.CAP_CHOWN
	_, _, errno = syscall.AllThreadsSyscall(unix.SYS_CAPSET,
		uintptr(unsafe.Pointer(&header)), uintptr(unsafe.Pointer(&capabilities[0])), 0)
	runtime.KeepAlive(&header)
	runtime.KeepAlive(&capabilities)
	if errno != 0 {
		t.Fatal(errno)
	}
	_, _, errno = syscall.AllThreadsSyscall6(unix.SYS_PRCTL,
		unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_RAISE, unix.CAP_CHOWN, 0, 0, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
}
