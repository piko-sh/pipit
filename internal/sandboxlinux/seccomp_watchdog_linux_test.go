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

package sandboxlinux

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestHostWatchdogFilterPolicy(t *testing.T) {
	t.Parallel()
	filter, err := hostWatchdogFilter(42, 5, 6, []uint32{9})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		number    uint32
		arguments [6]uint64
		allow     bool
		verdict   uint32
	}{
		{"stderr", unix.SYS_WRITE, [6]uint64{2}, true, 0},
		{"runtime wake", unix.SYS_WRITE, [6]uint64{9}, true, 0},
		{"wake read", unix.SYS_READ, [6]uint64{9}, true, 0},
		{"ipc read", unix.SYS_READ, [6]uint64{3}, false, 0},
		{"ipc write", unix.SYS_WRITE, [6]uint64{3}, false, 0},
		{"host write", unix.SYS_WRITE, [6]uint64{5}, false, 0},
		{"unpositioned kill write", unix.SYS_WRITE, [6]uint64{6}, false, 0},
		{"cgroup type", unix.SYS_FSTATFS, [6]uint64{6}, true, 0},
		{"foreign cgroup type", unix.SYS_FSTATFS, [6]uint64{7}, false, 0},
		{"kill flags", unix.SYS_FCNTL, [6]uint64{6, unix.F_GETFL}, true, 0},
		{"duplicate kill", unix.SYS_FCNTL, [6]uint64{6, unix.F_DUPFD_CLOEXEC}, false, 0},
		{"host type", unix.SYS_FSTATFS, [6]uint64{5}, true, 0},
		{"host alive probe", unix.SYS_PIDFD_SEND_SIGNAL, [6]uint64{5}, false, 0},
		{"host signal", unix.SYS_PIDFD_SEND_SIGNAL, [6]uint64{5, uint64(unix.SIGKILL)}, false, 0},
		{"foreign host", unix.SYS_PIDFD_SEND_SIGNAL, [6]uint64{7}, false, 0},
		{"signal metadata", unix.SYS_PIDFD_SEND_SIGNAL, [6]uint64{5, 0, 1}, false, 0},
		{"signal flags", unix.SYS_PIDFD_SEND_SIGNAL, [6]uint64{5, 0, 0, 1}, false, 0},
		{"single poll", unix.SYS_PPOLL, [6]uint64{0, 1}, true, 0},
		{"multiple poll", unix.SYS_PPOLL, [6]uint64{0, 2}, false, 0},
		{"poll signal mask", unix.SYS_PPOLL, [6]uint64{0, 1, 0, 1}, false, 0},
		{"kill byte", unix.SYS_PWRITE64, [6]uint64{6, 0, 1, 0}, true, 0},
		{"kill other offset", unix.SYS_PWRITE64, [6]uint64{6, 0, 1, 1}, false, 0},
		{"kill multiple bytes", unix.SYS_PWRITE64, [6]uint64{6, 0, 2, 0}, false, 0},
		{"kill foreign file", unix.SYS_PWRITE64, [6]uint64{7, 0, 1, 0}, false, 0},
		{"high descriptor bits", unix.SYS_PWRITE64, [6]uint64{1<<32 | 6, 0, 1, 0}, false, 0},
		{"high length bits", unix.SYS_PWRITE64, [6]uint64{6, 0, 1<<32 | 1, 0}, false, 0},
		{"high offset bits", unix.SYS_PWRITE64, [6]uint64{6, 0, 1, 1 << 32}, false, 0},
		{"file open", unix.SYS_OPENAT, [6]uint64{}, false, 0},
		{"socket", unix.SYS_SOCKET, [6]uint64{}, false, 0},
		{"exec", unix.SYS_EXECVE, [6]uint64{}, false, unix.SECCOMP_RET_KILL_PROCESS},
		{"descriptor transfer", unix.SYS_SENDMSG, [6]uint64{}, false, 0},
		{"pidfd duplication", unix.SYS_PIDFD_GETFD, [6]uint64{}, false, 0},
		{"new pidfd", unix.SYS_PIDFD_OPEN, [6]uint64{}, false, 0},
		{"arbitrary signal", unix.SYS_KILL, [6]uint64{}, false, 0},
	} {
		expected := uint32(denySyscall)
		if test.allow {
			expected = unix.SECCOMP_RET_ALLOW
		}
		if test.verdict != 0 {
			expected = test.verdict
		}
		if actual := evaluateFilter(t, filter, workerAuditArchitecture, test.number, test.arguments); actual != expected {
			t.Fatalf("%s: got %#x want %#x", test.name, actual, expected)
		}
	}
	if actual := evaluateFilter(t, filter, workerAuditArchitecture^1, unix.SYS_PWRITE64, [6]uint64{6, 0, 1}); actual != unix.SECCOMP_RET_KILL_PROCESS {
		t.Fatal("watchdog accepted foreign syscall ABI")
	}
}

func TestHostWatchdogReadyFilter(t *testing.T) {
	t.Parallel()
	filter, err := watchdogReadyFilter(42, 5, 6, 7, []uint32{9})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		number    uint32
		arguments [6]uint64
		allowed   bool
	}{
		{unix.SYS_WRITE, [6]uint64{3, 0, 1}, false},
		{unix.SYS_SENDTO, [6]uint64{3, 0, 1, watchdogReadySendFlags}, true},
		{unix.SYS_SENDTO, [6]uint64{3, 0, 2, watchdogReadySendFlags}, false},
		{unix.SYS_SENDTO, [6]uint64{3, 0, 1, 0}, false},
		{unix.SYS_SENDTO, [6]uint64{3, 0, 1, watchdogReadySendFlags, 1}, false},
		{unix.SYS_WRITE, [6]uint64{3, 0, 2}, false},
		{unix.SYS_READ, [6]uint64{3}, false},
		{unix.SYS_READ, [6]uint64{7}, false},
		{unix.SYS_PPOLL, [6]uint64{0, 2}, true},
		{unix.SYS_PPOLL, [6]uint64{0, 1}, false},
		{unix.SYS_TIMERFD_CREATE, [6]uint64{}, false},
		{unix.SYS_TIMERFD_SETTIME, [6]uint64{7}, false},
	} {
		expected := uint32(denySyscall)
		if test.allowed {
			expected = unix.SECCOMP_RET_ALLOW
		}
		if actual := evaluateFilter(t, filter, workerAuditArchitecture, test.number, test.arguments); actual != expected {
			t.Fatalf("watchdog readiness rule %d: got %#x want %#x", test.number, actual, expected)
		}
	}
	for _, roles := range [][3]uint32{{3, 6, 7}, {5, 3, 7}, {5, 6, 3}, {5, 6, 5}, {5, 6, 6}, {5, 6, 9}} {
		if _, err := watchdogReadyFilter(42, roles[0], roles[1], roles[2], []uint32{9}); err == nil {
			t.Fatalf("overlapping watchdog readiness roles accepted: %v", roles)
		}
	}
}

func TestHostWatchdogFilterRejectsConflictingRoles(t *testing.T) {
	t.Parallel()
	for _, roles := range [][2]uint32{{1, 6}, {5, 2}, {5, 5}, {1 << 31, 6}, {5, 1 << 31}, {9, 6}, {5, 9}} {
		if _, err := hostWatchdogFilter(42, roles[0], roles[1], []uint32{9}); err == nil {
			t.Fatalf("invalid descriptor roles accepted: %v", roles)
		}
	}
	for _, process := range []int{0, -1} {
		if _, err := hostWatchdogFilter(process, 5, 6, nil); err == nil {
			t.Fatal("invalid process identity accepted")
		}
	}
	filter, err := workerFilter(42)
	if err != nil {
		t.Fatal(err)
	}
	for _, number := range []uint32{unix.SYS_PWRITE64, unix.SYS_PIDFD_SEND_SIGNAL, unix.SYS_PPOLL} {
		if actual := evaluateFilter(t, filter, workerAuditArchitecture, number, [6]uint64{6, 0, 1}); actual != denySyscall {
			t.Fatal("watchdog authority leaked into source profile")
		}
	}
}
