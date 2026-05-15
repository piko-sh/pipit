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
	"errors"
	"os"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRuntimeWakeDescriptorRequiresLocalEventFD(t *testing.T) {
	t.Parallel()
	directory, err := unix.Open("/proc/self/fd", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(directory)
	for _, flags := range []int{unix.EFD_CLOEXEC | unix.EFD_NONBLOCK, unix.EFD_NONBLOCK} {
		descriptor, err := unix.Eventfd(0, flags)
		if err != nil {
			t.Fatal(err)
		}
		wake, found, err := runtimeWakeDescriptor(directory, strconv.Itoa(descriptor))
		_ = unix.Close(descriptor)
		if err != nil || found != (flags&unix.EFD_CLOEXEC != 0) {
			t.Fatalf("eventfd provenance was not enforced: %v, %v", found, err)
		}
		if found && wake != uint32(descriptor) {
			t.Fatalf("wrong runtime descriptor: %d", wake)
		}
	}
	file, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, found, err := runtimeWakeDescriptor(directory, strconv.Itoa(int(file.Fd()))); err != nil || found {
		t.Fatalf("ordinary file acquired runtime I/O authority: %v, %v", found, err)
	}
	for _, name := range []string{"", "-1", "invalid", "4294967301"} {
		if _, _, err := runtimeWakeDescriptor(directory, name); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("malformed descriptor accepted: %q: %v", name, err)
		}
	}
}

func TestRuntimeWakeFilter(t *testing.T) {
	t.Parallel()
	filter, err := workerFilterWithWakeDescriptors(42, []uint32{9})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		number     uint32
		descriptor uint64
		allowed    bool
	}{
		{number: unix.SYS_READ, descriptor: 9, allowed: true},
		{number: unix.SYS_WRITE, descriptor: 9, allowed: true},
		{number: unix.SYS_WRITEV, descriptor: 9, allowed: false},
		{number: unix.SYS_READ, descriptor: 10, allowed: false},
		{number: unix.SYS_WRITE, descriptor: 10, allowed: false},
		{number: unix.SYS_READ, descriptor: 0, allowed: false},
		{number: unix.SYS_WRITE, descriptor: 1<<32 | 9, allowed: false},
	} {
		verdict := evaluateFilter(t, filter, workerAuditArchitecture, test.number, [6]uint64{test.descriptor})
		if (verdict == unix.SECCOMP_RET_ALLOW) != test.allowed {
			t.Fatalf("unexpected runtime I/O verdict for %+v: %#x", test, verdict)
		}
	}
	for _, invalid := range [][]uint32{{0}, {1}, {2}, {3}, {9, 9}, {1 << 31}, {4, 5, 6, 7, 8, 9, 10, 11, 12}} {
		if _, err := workerFilterWithWakeDescriptors(42, invalid); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("invalid wake-up descriptor list accepted: %v: %v", invalid, err)
		}
	}
}

func TestWorkerIORejectsUnpreparedUse(t *testing.T) {
	t.Parallel()
	var workerIO WorkerIO
	if err := workerIO.installSyscallFilter(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unprepared runtime filter accepted: %v", err)
	}
	if os.Getpid() != 1 {
		if _, err := prepareWorkerIO(); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("worker I/O preparation accepted host process: %v", err)
		}
	}
}
