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
		{name: "stderr", number: unix.SYS_WRITE, arguments: [6]uint64{2}, allow: true, verdict: 0},
		{name: "runtime wake", number: unix.SYS_WRITE, arguments: [6]uint64{9}, allow: true, verdict: 0},
		{name: "wake read", number: unix.SYS_READ, arguments: [6]uint64{9}, allow: true, verdict: 0},
		{name: "ipc read", number: unix.SYS_READ, arguments: [6]uint64{3}, allow: false, verdict: 0},
		{name: "ipc write", number: unix.SYS_WRITE, arguments: [6]uint64{3}, allow: false, verdict: 0},
		{name: "host write", number: unix.SYS_WRITE, arguments: [6]uint64{5}, allow: false, verdict: 0},
		{name: "unpositioned kill write", number: unix.SYS_WRITE, arguments: [6]uint64{6}, allow: false, verdict: 0},
		{name: "cgroup type", number: unix.SYS_FSTATFS, arguments: [6]uint64{6}, allow: true, verdict: 0},
		{name: "foreign cgroup type", number: unix.SYS_FSTATFS, arguments: [6]uint64{7}, allow: false, verdict: 0},
		{name: "kill flags", number: unix.SYS_FCNTL, arguments: [6]uint64{6, unix.F_GETFL}, allow: true, verdict: 0},
		{name: "duplicate kill", number: unix.SYS_FCNTL, arguments: [6]uint64{6, unix.F_DUPFD_CLOEXEC}, allow: false, verdict: 0},
		{name: "host type", number: unix.SYS_FSTATFS, arguments: [6]uint64{5}, allow: true, verdict: 0},
		{name: "host alive probe", number: unix.SYS_PIDFD_SEND_SIGNAL, arguments: [6]uint64{5}, allow: false, verdict: 0},
		{name: "host signal", number: unix.SYS_PIDFD_SEND_SIGNAL, arguments: [6]uint64{5, uint64(unix.SIGKILL)}, allow: false, verdict: 0},
		{name: "foreign host", number: unix.SYS_PIDFD_SEND_SIGNAL, arguments: [6]uint64{7}, allow: false, verdict: 0},
		{name: "signal metadata", number: unix.SYS_PIDFD_SEND_SIGNAL, arguments: [6]uint64{5, 0, 1}, allow: false, verdict: 0},
		{name: "signal flags", number: unix.SYS_PIDFD_SEND_SIGNAL, arguments: [6]uint64{5, 0, 0, 1}, allow: false, verdict: 0},
		{name: "single poll", number: unix.SYS_PPOLL, arguments: [6]uint64{0, 1}, allow: true, verdict: 0},
		{name: "multiple poll", number: unix.SYS_PPOLL, arguments: [6]uint64{0, 2}, allow: false, verdict: 0},
		{name: "poll signal mask", number: unix.SYS_PPOLL, arguments: [6]uint64{0, 1, 0, 1}, allow: false, verdict: 0},
		{name: "kill byte", number: unix.SYS_PWRITE64, arguments: [6]uint64{6, 0, 1, 0}, allow: true, verdict: 0},
		{name: "kill other offset", number: unix.SYS_PWRITE64, arguments: [6]uint64{6, 0, 1, 1}, allow: false, verdict: 0},
		{name: "kill multiple bytes", number: unix.SYS_PWRITE64, arguments: [6]uint64{6, 0, 2, 0}, allow: false, verdict: 0},
		{name: "kill foreign file", number: unix.SYS_PWRITE64, arguments: [6]uint64{7, 0, 1, 0}, allow: false, verdict: 0},
		{name: "high descriptor bits", number: unix.SYS_PWRITE64, arguments: [6]uint64{1<<32 | 6, 0, 1, 0}, allow: false, verdict: 0},
		{name: "high length bits", number: unix.SYS_PWRITE64, arguments: [6]uint64{6, 0, 1<<32 | 1, 0}, allow: false, verdict: 0},
		{name: "high offset bits", number: unix.SYS_PWRITE64, arguments: [6]uint64{6, 0, 1, 1 << 32}, allow: false, verdict: 0},
		{name: "file open", number: unix.SYS_OPENAT, arguments: [6]uint64{}, allow: false, verdict: 0},
		{name: "socket", number: unix.SYS_SOCKET, arguments: [6]uint64{}, allow: false, verdict: 0},
		{name: "exec", number: unix.SYS_EXECVE, arguments: [6]uint64{}, allow: false, verdict: unix.SECCOMP_RET_KILL_PROCESS},
		{name: "descriptor transfer", number: unix.SYS_SENDMSG, arguments: [6]uint64{}, allow: false, verdict: 0},
		{name: "pidfd duplication", number: unix.SYS_PIDFD_GETFD, arguments: [6]uint64{}, allow: false, verdict: 0},
		{name: "new pidfd", number: unix.SYS_PIDFD_OPEN, arguments: [6]uint64{}, allow: false, verdict: 0},
		{name: "arbitrary signal", number: unix.SYS_KILL, arguments: [6]uint64{}, allow: false, verdict: 0},
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
		{number: unix.SYS_WRITE, arguments: [6]uint64{3, 0, 1}, allowed: false},
		{number: unix.SYS_SENDTO, arguments: [6]uint64{3, 0, 1, watchdogReadySendFlags}, allowed: true},
		{number: unix.SYS_SENDTO, arguments: [6]uint64{3, 0, 2, watchdogReadySendFlags}, allowed: false},
		{number: unix.SYS_SENDTO, arguments: [6]uint64{3, 0, 1, 0}, allowed: false},
		{number: unix.SYS_SENDTO, arguments: [6]uint64{3, 0, 1, watchdogReadySendFlags, 1}, allowed: false},
		{number: unix.SYS_WRITE, arguments: [6]uint64{3, 0, 2}, allowed: false},
		{number: unix.SYS_READ, arguments: [6]uint64{3}, allowed: false},
		{number: unix.SYS_READ, arguments: [6]uint64{7}, allowed: false},
		{number: unix.SYS_PPOLL, arguments: [6]uint64{0, 2}, allowed: true},
		{number: unix.SYS_PPOLL, arguments: [6]uint64{0, 1}, allowed: false},
		{number: unix.SYS_TIMERFD_CREATE, arguments: [6]uint64{}, allowed: false},
		{number: unix.SYS_TIMERFD_SETTIME, arguments: [6]uint64{7}, allowed: false},
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
